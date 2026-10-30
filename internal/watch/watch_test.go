package watch_test

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/bkuberek/spmeta/internal/backend"
	"github.com/bkuberek/spmeta/internal/model"
	"github.com/bkuberek/spmeta/internal/watch"
)

const interval = time.Second

type step struct {
	np  model.NowPlaying
	err error
	// during runs inside the read, as a player signal arriving mid-read does.
	during func()
}

// scripted replays steps in order and cancels the context when asked to
// read past the end, which is how every test ends its run.
type scripted struct {
	steps  []step
	next   int
	cancel context.CancelFunc
}

func (s *scripted) Read(ctx context.Context) (model.NowPlaying, error) {
	if s.next >= len(s.steps) {
		s.cancel()
		return model.NowPlaying{}, ctx.Err()
	}
	st := s.steps[s.next]
	s.next++
	if st.during != nil {
		st.during()
	}
	return st.np, st.err
}

type harness struct {
	emitted []string
	errs    []string
	sleeps  []time.Duration
	// wakeable records, per sleep, whether a wake could end it.
	wakeable []bool
}

func (h *harness) run(t *testing.T, steps []step, mutate ...func(*watch.Options)) error {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	opts := watch.Options{
		Interval: interval,
		Emit: func(np *model.NowPlaying) error {
			if np == nil {
				h.emitted = append(h.emitted, "nil")
				return nil
			}
			h.emitted = append(h.emitted, fmt.Sprintf("%s/%s", *np.Title, np.State))
			return nil
		},
		Sleep: func(ctx context.Context, d time.Duration, wake <-chan struct{}) error {
			h.sleeps = append(h.sleeps, d)
			h.wakeable = append(h.wakeable, wake != nil)
			return ctx.Err()
		},
		Errorf: func(format string, args ...any) { h.errs = append(h.errs, fmt.Sprintf(format, args...)) },
	}
	for _, m := range mutate {
		m(&opts)
	}
	return watch.Run(ctx, &scripted{steps: steps, cancel: cancel}, opts)
}

func ptr[T any](v T) *T { return &v }

func track(title string) model.NowPlaying {
	return model.NowPlaying{
		SchemaVersion: 1,
		Source:        model.SourceMacOS,
		State:         model.StatePlaying,
		Type:          model.ItemTrack,
		Title:         ptr(title),
		Artist:        ptr("Alvan"),
		Album:         ptr("Home"),
		PositionMS:    ptr(int64(1000)),
		FetchedAt:     time.Date(2026, 10, 27, 17, 40, 0, 0, time.UTC),
	}
}

func with(np model.NowPlaying, f func(*model.NowPlaying)) model.NowPlaying {
	f(&np)
	return np
}

func ok(np model.NowPlaying) step         { return step{np: np} }
func fail(err error) step                 { return step{err: err} }
func be(k backend.Kind, msg string) error { return &backend.Error{Kind: k, Message: msg} }

func TestChangeDetection(t *testing.T) {
	a := track("A")
	tests := []struct {
		name  string
		steps []step
		want  []string
	}{
		{"first result is emitted", []step{ok(a)}, []string{"A/playing"}},
		{"position and fetched_at changes are silent", []step{
			ok(a),
			ok(with(a, func(n *model.NowPlaying) { n.PositionMS = ptr(int64(5000)); n.FetchedAt = n.FetchedAt.Add(time.Second) })),
			ok(with(a, func(n *model.NowPlaying) { n.PositionMS = nil })),
		}, []string{"A/playing"}},
		{"new track emits", []step{ok(a), ok(track("B"))}, []string{"A/playing", "B/playing"}},
		{"state change emits", []step{
			ok(a), ok(with(a, func(n *model.NowPlaying) { n.State = model.StatePaused })), ok(a),
		}, []string{"A/playing", "A/paused", "A/playing"}},
		{"device change emits", []step{
			ok(with(a, func(n *model.NowPlaying) { n.Device = &model.Device{Name: "Kitchen"} })),
			ok(with(a, func(n *model.NowPlaying) { n.Device = &model.Device{Name: "Office"} })),
			ok(with(a, func(n *model.NowPlaying) { n.Device = &model.Device{Name: "Office", Type: "Speaker"} })),
			ok(a),
		}, []string{"A/playing", "A/playing", "A/playing"}},
		{"source change emits", []step{
			ok(a), ok(with(a, func(n *model.NowPlaying) { n.Source = model.SourceWeb })),
		}, []string{"A/playing", "A/playing"}},
		{"type change emits", []step{
			ok(a), ok(with(a, func(n *model.NowPlaying) { n.Type = model.ItemEpisode })),
		}, []string{"A/playing", "A/playing"}},
		{"uri identifies the item over its title", []step{
			ok(with(a, func(n *model.NowPlaying) { n.URI = ptr("spotify:track:1") })),
			ok(with(a, func(n *model.NowPlaying) { n.URI = ptr("spotify:track:1"); n.Title = ptr("A (Remastered)") })),
			ok(with(a, func(n *model.NowPlaying) { n.URI = ptr("spotify:track:2") })),
		}, []string{"A/playing", "A/playing"}},
		{"without uri the album is part of the identity", []step{
			ok(a), ok(with(a, func(n *model.NowPlaying) { n.Album = ptr("Live") })),
		}, []string{"A/playing", "A/playing"}},
		{"artist and title boundaries do not collide", []step{
			ok(with(a, func(n *model.NowPlaying) { n.Title = ptr("x"); n.Artist = ptr("yz") })),
			ok(with(a, func(n *model.NowPlaying) { n.Title = ptr("xy"); n.Artist = ptr("z") })),
		}, []string{"x/playing", "xy/playing"}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var h harness
			if err := h.run(t, tc.steps); err != nil {
				t.Fatalf("Run = %v, want nil", err)
			}
			if !slices.Equal(h.emitted, tc.want) {
				t.Errorf("emitted = %v, want %v", h.emitted, tc.want)
			}
			if len(h.errs) != 0 {
				t.Errorf("unexpected stderr lines: %v", h.errs)
			}
		})
	}
}

func TestNothingPlaying(t *testing.T) {
	a := track("A")
	nothing := fail(be(backend.KindNothingPlaying, "nothing is playing"))
	gone := fail(be(backend.KindNotAvailable, "Spotify is not running"))
	tests := []struct {
		name  string
		steps []step
		want  []string
	}{
		{"first result nothing is emitted", []step{nothing}, []string{"nil"}},
		{"transition to nothing emits once", []step{ok(a), nothing, nothing, nothing}, []string{"A/playing", "nil"}},
		{"not available counts as nothing", []step{ok(a), gone, nothing, gone}, []string{"A/playing", "nil"}},
		{"item after nothing emits again", []step{ok(a), nothing, ok(a)}, []string{"A/playing", "nil", "A/playing"}},
		{"same item after nothing emits again", []step{ok(a), gone, ok(with(a, func(n *model.NowPlaying) { n.PositionMS = ptr(int64(9)) }))}, []string{"A/playing", "nil", "A/playing"}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var h harness
			if err := h.run(t, tc.steps); err != nil {
				t.Fatalf("Run = %v, want nil", err)
			}
			if !slices.Equal(h.emitted, tc.want) {
				t.Errorf("emitted = %v, want %v", h.emitted, tc.want)
			}
			if len(h.errs) != 0 {
				t.Errorf("nothing playing must not print: %v", h.errs)
			}
		})
	}
}

func TestSleepsInterval(t *testing.T) {
	var h harness
	if err := h.run(t, []step{ok(track("A")), ok(track("A"))}); err != nil {
		t.Fatal(err)
	}
	if want := []time.Duration{interval, interval}; !slices.Equal(h.sleeps, want) {
		t.Errorf("sleeps = %v, want %v", h.sleeps, want)
	}
}

func TestRateLimited(t *testing.T) {
	a := track("A")
	limited := func(after time.Duration) step {
		return fail(&backend.Error{Kind: backend.KindRateLimited, Message: "rate limited", RetryAfter: after})
	}
	var h harness
	err := h.run(t, []step{ok(a), limited(7 * time.Second), limited(0), ok(a), ok(track("B"))})
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"A/playing", "B/playing"}; !slices.Equal(h.emitted, want) {
		t.Errorf("emitted = %v, want %v", h.emitted, want)
	}
	if want := []time.Duration{interval, 7 * time.Second, 5 * time.Second, interval, interval}; !slices.Equal(h.sleeps, want) {
		t.Errorf("sleeps = %v, want %v", h.sleeps, want)
	}
	if len(h.errs) != 0 {
		t.Errorf("rate limits must stay silent: %v", h.errs)
	}
}

func TestUnexpectedErrors(t *testing.T) {
	a := track("A")
	boom := func(msg string) step { return fail(be(backend.KindUnexpected, msg)) }

	t.Run("printed once per distinct message and reset on success", func(t *testing.T) {
		var h harness
		err := h.run(t, []step{boom("x"), boom("x"), boom("y"), boom("y"), boom("x"), ok(a), boom("x"), boom("x")})
		if err != nil {
			t.Fatal(err)
		}
		want := []string{"spmeta: x", "spmeta: y", "spmeta: x", "spmeta: x"}
		if !slices.Equal(h.errs, want) {
			t.Errorf("stderr = %v, want %v", h.errs, want)
		}
		if want := []string{"A/playing"}; !slices.Equal(h.emitted, want) {
			t.Errorf("emitted = %v, want %v", h.emitted, want)
		}
	})

	t.Run("hint follows the message", func(t *testing.T) {
		var h harness
		err := h.run(t, []step{fail(&backend.Error{Kind: backend.KindUnexpected, Message: "timeout", Hint: "run `spmeta doctor`"})})
		if err != nil {
			t.Fatal(err)
		}
		if want := []string{"spmeta: timeout", "hint: run `spmeta doctor`"}; !slices.Equal(h.errs, want) {
			t.Errorf("stderr = %v, want %v", h.errs, want)
		}
	})

	t.Run("plain errors count as unexpected", func(t *testing.T) {
		var h harness
		if err := h.run(t, []step{fail(errors.New("disk on fire"))}); err != nil {
			t.Fatal(err)
		}
		if want := []string{"spmeta: disk on fire"}; !slices.Equal(h.errs, want) {
			t.Errorf("stderr = %v, want %v", h.errs, want)
		}
	})

	t.Run("backoff doubles after five consecutive failures up to 30s", func(t *testing.T) {
		steps := make([]step, 10)
		for i := range steps {
			steps[i] = boom("x")
		}
		steps = append(steps, ok(a))
		var h harness
		if err := h.run(t, steps); err != nil {
			t.Fatal(err)
		}
		s := time.Second
		want := []time.Duration{s, s, s, s, 2 * s, 4 * s, 8 * s, 16 * s, 30 * s, 30 * s, s}
		if !slices.Equal(h.sleeps, want) {
			t.Errorf("sleeps = %v, want %v", h.sleeps, want)
		}
	})

	t.Run("nothing playing resets the failure count", func(t *testing.T) {
		steps := []step{boom("x"), boom("x"), boom("x"), boom("x"), fail(be(backend.KindNothingPlaying, "n")), boom("x")}
		var h harness
		if err := h.run(t, steps); err != nil {
			t.Fatal(err)
		}
		for i, d := range h.sleeps {
			if d != interval {
				t.Errorf("sleep %d = %v, want %v", i, d, interval)
			}
		}
	})

	t.Run("a long interval is never shortened", func(t *testing.T) {
		steps := []step{boom("x"), boom("x"), boom("x"), boom("x"), boom("x")}
		var h harness
		if err := h.run(t, steps, func(o *watch.Options) { o.Interval = time.Minute }); err != nil {
			t.Fatal(err)
		}
		if got := h.sleeps[len(h.sleeps)-1]; got != time.Minute {
			t.Errorf("last sleep = %v, want 1m", got)
		}
	})
}

func TestFatalErrors(t *testing.T) {
	for _, kind := range []backend.Kind{backend.KindAuthRequired, backend.KindPermissionDenied} {
		t.Run(fmt.Sprint(backend.ExitCode(kind)), func(t *testing.T) {
			var h harness
			cause := &backend.Error{Kind: kind, Message: "fatal", Hint: "fix it"}
			err := h.run(t, []step{ok(track("A")), fail(cause), ok(track("B"))})
			if !errors.Is(err, cause) {
				t.Fatalf("Run = %v, want %v", err, cause)
			}
			if want := []string{"A/playing"}; !slices.Equal(h.emitted, want) {
				t.Errorf("emitted = %v, want %v", h.emitted, want)
			}
			if len(h.errs) != 0 {
				t.Errorf("Run must leave printing a returned error to the caller: %v", h.errs)
			}
			if len(h.sleeps) != 1 {
				t.Errorf("sleeps = %v, want none after the fatal error", h.sleeps)
			}
		})
	}
}

func TestFatalOnFirstRead(t *testing.T) {
	var h harness
	err := h.run(t, []step{fail(be(backend.KindAuthRequired, "login"))})
	if backend.KindOf(err) != backend.KindAuthRequired {
		t.Fatalf("Run = %v, want auth required", err)
	}
	if len(h.emitted) != 0 {
		t.Errorf("emitted = %v, want none", h.emitted)
	}
}

func TestCancellation(t *testing.T) {
	t.Run("during sleep", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		r := &scripted{steps: []step{ok(track("A")), ok(track("B"))}, cancel: cancel}
		var emitted int
		err := watch.Run(ctx, r, watch.Options{
			Interval: interval,
			Emit:     func(*model.NowPlaying) error { emitted++; return nil },
			Sleep: func(ctx context.Context, _ time.Duration, _ <-chan struct{}) error {
				cancel()
				return ctx.Err()
			},
		})
		if err != nil {
			t.Errorf("Run = %v, want nil", err)
		}
		if emitted != 1 || r.next != 1 {
			t.Errorf("emitted %d, reads %d, want 1 and 1", emitted, r.next)
		}
	})

	t.Run("a read failing because of the cancellation is not reported", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		var errs []string
		r := readerFunc(func(context.Context) (model.NowPlaying, error) {
			return model.NowPlaying{}, be(backend.KindUnexpected, "context canceled")
		})
		err := watch.Run(ctx, r, watch.Options{
			Interval: interval,
			Emit:     func(*model.NowPlaying) error { t.Error("emit after cancel"); return nil },
			Sleep:    func(context.Context, time.Duration, <-chan struct{}) error { return nil },
			Errorf:   func(f string, a ...any) { errs = append(errs, fmt.Sprintf(f, a...)) },
		})
		if err != nil || len(errs) != 0 {
			t.Errorf("Run = %v, stderr %v; want nil and silence", err, errs)
		}
	})

	t.Run("a fatal error read after the cancellation still returns nil", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		r := readerFunc(func(context.Context) (model.NowPlaying, error) {
			cancel()
			return model.NowPlaying{}, be(backend.KindAuthRequired, "x")
		})
		err := watch.Run(ctx, r, watch.Options{Interval: interval, Emit: func(*model.NowPlaying) error { return nil }})
		if err != nil {
			t.Errorf("Run = %v, want nil", err)
		}
	})
}

type readerFunc func(context.Context) (model.NowPlaying, error)

func (f readerFunc) Read(ctx context.Context) (model.NowPlaying, error) { return f(ctx) }

func TestEmitFailureStopsTheLoop(t *testing.T) {
	var h harness
	cause := errors.New("broken pipe")
	err := h.run(t, []step{ok(track("A")), ok(track("B"))}, func(o *watch.Options) {
		o.Emit = func(*model.NowPlaying) error { return cause }
	})
	if backend.KindOf(err) != backend.KindUnexpected || !errors.Is(err, cause) {
		t.Fatalf("Run = %v, want an unexpected error wrapping the write failure", err)
	}
	if !strings.Contains(err.Error(), "write") {
		t.Errorf("message %q does not mention the write", err)
	}
}

func TestDefaultSleep(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	r := &scripted{steps: []step{ok(track("A")), ok(track("B"))}, cancel: cancel}
	var got []string
	err := watch.Run(ctx, r, watch.Options{
		Interval: time.Millisecond,
		Emit: func(np *model.NowPlaying) error {
			got = append(got, *np.Title)
			return nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"A", "B"}; !slices.Equal(got, want) {
		t.Errorf("emitted = %v, want %v", got, want)
	}
}

// signal sends n wakes on ch during st's read, dropping those that do not
// fit, as a notifier does.
func signal(st step, ch chan struct{}, n int) step {
	st.during = func() {
		for range n {
			select {
			case ch <- struct{}{}:
			default:
			}
		}
	}
	return st
}

func withWake(ch <-chan struct{}) func(*watch.Options) {
	return func(o *watch.Options) { o.Wake = ch }
}

func TestWake(t *testing.T) {
	a, b := track("A"), track("B")
	// A wait that a wake may end is recorded as two sleeps: the gap, which
	// no wake ends, then the rest of the interval, which a wake may end.
	gap, rest := watch.MinWakeGap, interval-watch.MinWakeGap

	t.Run("a wake during a read starts the next read after the gap", func(t *testing.T) {
		wake := make(chan struct{}, 1)
		var h harness
		if err := h.run(t, []step{signal(ok(a), wake, 1), ok(b)}, withWake(wake)); err != nil {
			t.Fatal(err)
		}
		if want := []string{"A/playing", "B/playing"}; !slices.Equal(h.emitted, want) {
			t.Errorf("emitted = %v, want %v", h.emitted, want)
		}
		if want := []time.Duration{gap, gap, rest}; !slices.Equal(h.sleeps, want) {
			t.Errorf("sleeps = %v, want %v: B must be read after the gap, not the interval", h.sleeps, want)
		}
		if want := []bool{false, false, true}; !slices.Equal(h.wakeable, want) {
			t.Errorf("wakeable = %v, want %v", h.wakeable, want)
		}
	})

	t.Run("a flood of wakes cannot make reads back to back", func(t *testing.T) {
		wake := make(chan struct{}, 1)
		steps := []step{signal(ok(a), wake, 1), signal(ok(b), wake, 1), signal(ok(a), wake, 1)}
		var h harness
		if err := h.run(t, steps, withWake(wake)); err != nil {
			t.Fatal(err)
		}
		if want := []time.Duration{gap, gap, gap}; !slices.Equal(h.sleeps, want) {
			t.Errorf("sleeps = %v, want %v: every read must wait out the gap", h.sleeps, want)
		}
	})

	t.Run("a burst of wakes starts one read", func(t *testing.T) {
		wake := make(chan struct{}, 8)
		var h harness
		if err := h.run(t, []step{signal(ok(a), wake, 5), ok(b)}, withWake(wake)); err != nil {
			t.Fatal(err)
		}
		if want := []time.Duration{gap, gap, rest}; !slices.Equal(h.sleeps, want) {
			t.Errorf("sleeps = %v, want %v: five wakes must cost one read", h.sleeps, want)
		}
	})

	t.Run("waits after a read that reached a source can be woken after the gap", func(t *testing.T) {
		wake := make(chan struct{}, 1)
		nothing := fail(be(backend.KindNothingPlaying, "nothing"))
		var h harness
		if err := h.run(t, []step{ok(a), nothing}, withWake(wake)); err != nil {
			t.Fatal(err)
		}
		if want := []time.Duration{gap, rest, gap, rest}; !slices.Equal(h.sleeps, want) {
			t.Errorf("sleeps = %v, want %v", h.sleeps, want)
		}
		if want := []bool{false, true, false, true}; !slices.Equal(h.wakeable, want) {
			t.Errorf("wakeable = %v, want %v", h.wakeable, want)
		}
	})

	t.Run("waits after errors run their full length", func(t *testing.T) {
		wake := make(chan struct{}, 1)
		limited := fail(&backend.Error{Kind: backend.KindRateLimited, Message: "rate limited", RetryAfter: 7 * time.Second})
		boom := fail(be(backend.KindUnexpected, "x"))
		var h harness
		steps := []step{ok(a), signal(limited, wake, 1), signal(boom, wake, 1), ok(a)}
		if err := h.run(t, steps, withWake(wake)); err != nil {
			t.Fatal(err)
		}
		if want := []time.Duration{gap, rest, 7 * time.Second, interval, gap, rest}; !slices.Equal(h.sleeps, want) {
			t.Errorf("sleeps = %v, want %v", h.sleeps, want)
		}
		if want := []bool{false, true, false, false, false, true}; !slices.Equal(h.wakeable, want) {
			t.Errorf("wakeable = %v, want %v", h.wakeable, want)
		}
	})

	t.Run("a closed wake channel leaves plain polling", func(t *testing.T) {
		wake := make(chan struct{})
		closing := ok(b)
		closing.during = func() { close(wake) }
		var h harness
		if err := h.run(t, []step{ok(a), closing, ok(a)}, withWake(wake)); err != nil {
			t.Fatal(err)
		}
		if want := []time.Duration{gap, rest, gap, rest, interval}; !slices.Equal(h.sleeps, want) {
			t.Errorf("sleeps = %v, want %v", h.sleeps, want)
		}
		if want := []bool{false, true, false, false, false}; !slices.Equal(h.wakeable, want) {
			t.Errorf("wakeable = %v, want %v", h.wakeable, want)
		}
	})

	t.Run("without a wake channel every wait is plain", func(t *testing.T) {
		var h harness
		if err := h.run(t, []step{ok(a), ok(b)}); err != nil {
			t.Fatal(err)
		}
		if want := []bool{false, false}; !slices.Equal(h.wakeable, want) {
			t.Errorf("wakeable = %v, want %v", h.wakeable, want)
		}
	})
}

// runAsync runs run, which uses the default sleep, and fails the test when it
// does not return within a few seconds; cancel then stops it.
func runAsync(t *testing.T, cancel context.CancelFunc, run func() error) error {
	t.Helper()
	done := make(chan error, 1)
	go func() { done <- run() }()
	select {
	case err := <-done:
		return err
	case <-time.After(3 * time.Second):
		cancel()
		t.Fatalf("Run did not return; the wait was not cut short (after cancel: %v)", <-done)
		return nil
	}
}

func TestWakeEndsTheDefaultWait(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	wake := make(chan struct{}, 1)
	r := &scripted{steps: []step{ok(track("A")), ok(track("B"))}, cancel: cancel}
	var got []string
	opts := watch.Options{
		Interval: time.Hour,
		Wake:     wake,
		Emit: func(np *model.NowPlaying) error {
			got = append(got, *np.Title)
			if len(got) == 1 {
				// After the gap, so the wake ends the part of the wait it may end.
				time.AfterFunc(watch.MinWakeGap+50*time.Millisecond, func() { wake <- struct{}{} })
			} else {
				cancel()
			}
			return nil
		},
	}
	err := runAsync(t, cancel, func() error { return watch.Run(ctx, r, opts) })
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"A", "B"}; !slices.Equal(got, want) {
		t.Errorf("emitted = %v, want %v", got, want)
	}
}

func TestCancelDuringWakeableWait(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	r := &scripted{steps: []step{ok(track("A")), ok(track("B"))}, cancel: cancel}
	var emitted int
	opts := watch.Options{
		Interval: time.Hour,
		Wake:     make(chan struct{}),
		Emit: func(*model.NowPlaying) error {
			emitted++
			time.AfterFunc(watch.MinWakeGap+50*time.Millisecond, cancel)
			return nil
		},
	}
	err := runAsync(t, cancel, func() error { return watch.Run(ctx, r, opts) })
	if err != nil || emitted != 1 || r.next != 1 {
		t.Errorf("Run = %v, emitted %d, reads %d; want nil, 1 and 1", err, emitted, r.next)
	}
}

// A notifier that stops must not cost an early read: the wait it was part of
// runs to the end of the interval. Timers never fire early, so the check
// cannot fail spuriously; a late close only lets it pass vacuously.
func TestClosingWakeDuringTheDefaultWait(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	const wait = 2 * watch.MinWakeGap
	wake := make(chan struct{})
	var reads []time.Time
	r := readerFunc(func(context.Context) (model.NowPlaying, error) {
		reads = append(reads, time.Now())
		switch len(reads) {
		case 1:
			// Inside the part of the wait that a wake may end.
			time.AfterFunc(watch.MinWakeGap+wait/4, func() { close(wake) })
		case 2:
			cancel()
		}
		return track("A"), nil
	})
	opts := watch.Options{Interval: wait, Wake: wake, Emit: func(*model.NowPlaying) error { return nil }}
	if err := runAsync(t, cancel, func() error { return watch.Run(ctx, r, opts) }); err != nil {
		t.Fatal(err)
	}
	if len(reads) != 2 {
		t.Fatalf("reads = %d, want 2", len(reads))
	}
	if got := reads[1].Sub(reads[0]); got < wait {
		t.Errorf("second read %v after the first, want at least %v: the closed channel ended the wait", got, wait)
	}
}
