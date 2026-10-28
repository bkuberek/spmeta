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
	return st.np, st.err
}

type harness struct {
	emitted []string
	errs    []string
	sleeps  []time.Duration
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
		Sleep: func(ctx context.Context, d time.Duration) error {
			h.sleeps = append(h.sleeps, d)
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
			Sleep: func(ctx context.Context, _ time.Duration) error {
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
			Sleep:    func(context.Context, time.Duration) error { return nil },
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
