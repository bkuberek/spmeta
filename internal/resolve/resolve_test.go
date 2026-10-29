package resolve_test

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"strings"
	"testing"

	"github.com/bkuberek/spmeta/internal/backend"
	"github.com/bkuberek/spmeta/internal/model"
	"github.com/bkuberek/spmeta/internal/resolve"
)

type fake struct {
	src   model.Source
	np    model.NowPlaying
	err   error
	calls int
}

func (f *fake) Source() model.Source { return f.src }

func (f *fake) Read(context.Context) (model.NowPlaying, error) {
	f.calls++
	return f.np, f.err
}

func local(state model.State) *fake {
	return &fake{src: model.SourceMacOS, np: model.NowPlaying{Source: model.SourceMacOS, State: state}}
}

func web(state model.State) *fake {
	return &fake{src: model.SourceWeb, np: model.NowPlaying{Source: model.SourceWeb, State: state}}
}

func failing(src model.Source, k backend.Kind, msg string) *fake {
	return &fake{src: src, err: &backend.Error{Kind: k, Source: src, Message: msg}}
}

// iface keeps a nil *fake from becoming a non-nil backend.Backend.
func iface(f *fake) backend.Backend {
	if f == nil {
		return nil
	}
	return f
}

func TestAutoRows(t *testing.T) {
	notRunning := func() *fake { return failing(model.SourceMacOS, backend.KindNotAvailable, "Spotify is not running") }
	tests := []struct {
		name      string
		local     *fake
		web       *fake
		wantFrom  model.Source
		wantState model.State
		wantWeb   int
	}{
		{"1 local playing skips web", local(model.StatePlaying), web(model.StatePlaying), model.SourceMacOS, model.StatePlaying, 0},
		{"1 local playing without web", local(model.StatePlaying), nil, model.SourceMacOS, model.StatePlaying, 0},
		{"2 local paused, web playing", local(model.StatePaused), web(model.StatePlaying), model.SourceWeb, model.StatePlaying, 1},
		{"2 local stopped, web playing", local(model.StateStopped), web(model.StatePlaying), model.SourceWeb, model.StatePlaying, 1},
		{"2 local unavailable, web playing", notRunning(), web(model.StatePlaying), model.SourceWeb, model.StatePlaying, 1},
		{"2 local permission denied, web playing", failing(model.SourceMacOS, backend.KindPermissionDenied, "denied"), web(model.StatePlaying), model.SourceWeb, model.StatePlaying, 1},
		{"2 local unexpected, web playing", failing(model.SourceMacOS, backend.KindUnexpected, "boom"), web(model.StatePlaying), model.SourceWeb, model.StatePlaying, 1},
		{"3 local paused, web paused", local(model.StatePaused), web(model.StatePaused), model.SourceMacOS, model.StatePaused, 1},
		{"3 local stopped, web error", local(model.StateStopped), failing(model.SourceWeb, backend.KindRateLimited, "slow down"), model.SourceMacOS, model.StateStopped, 1},
		{"3 local paused, web nothing playing", local(model.StatePaused), failing(model.SourceWeb, backend.KindNothingPlaying, "idle"), model.SourceMacOS, model.StatePaused, 1},
		{"3 local paused without web", local(model.StatePaused), nil, model.SourceMacOS, model.StatePaused, 0},
		{"4 local unavailable, web paused", notRunning(), web(model.StatePaused), model.SourceWeb, model.StatePaused, 1},
		{"4 local nothing playing, web paused", failing(model.SourceMacOS, backend.KindNothingPlaying, "idle"), web(model.StatePaused), model.SourceWeb, model.StatePaused, 1},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			r := &resolve.Resolver{Mode: resolve.ModeAuto, Local: tc.local, Web: iface(tc.web)}
			got, err := r.Read(context.Background())
			if err != nil {
				t.Fatalf("Read() error = %v", err)
			}
			if got.Source != tc.wantFrom || got.State != tc.wantState {
				t.Errorf("Read() = %s/%s, want %s/%s", got.Source, got.State, tc.wantFrom, tc.wantState)
			}
			if tc.local.calls != 1 {
				t.Errorf("local calls = %d, want 1", tc.local.calls)
			}
			if tc.web != nil && tc.web.calls != tc.wantWeb {
				t.Errorf("web calls = %d, want %d", tc.web.calls, tc.wantWeb)
			}
		})
	}
}

func TestAutoWithoutWebReturnsLocalError(t *testing.T) {
	want := failing(model.SourceMacOS, backend.KindNotAvailable, "Spotify is not running")
	r := &resolve.Resolver{Mode: resolve.ModeAuto, Local: want}
	_, err := r.Read(context.Background())
	if !errors.Is(err, want.err) {
		t.Errorf("Read() error = %v, want %v", err, want.err)
	}
}

// precedence is highest first, per ADR 0005.
var precedence = []backend.Kind{
	backend.KindPermissionDenied,
	backend.KindAuthRequired,
	backend.KindRateLimited,
	backend.KindUnexpected,
	backend.KindNothingPlaying,
	backend.KindNotAvailable,
}

func TestAutoErrorPrecedence(t *testing.T) {
	for i, hi := range precedence {
		for _, lo := range precedence[i+1:] {
			for _, hiIsLocal := range []bool{true, false} {
				l := failing(model.SourceMacOS, lo, "local")
				w := failing(model.SourceWeb, hi, "web")
				if hiIsLocal {
					l = failing(model.SourceMacOS, hi, "local")
					w = failing(model.SourceWeb, lo, "web")
				}
				r := &resolve.Resolver{Mode: resolve.ModeAuto, Local: l, Web: w}
				_, err := r.Read(context.Background())
				if got := backend.KindOf(err); got != hi {
					t.Errorf("hi=%d lo=%d hiIsLocal=%v: kind = %d, want %d", hi, lo, hiIsLocal, got, hi)
				}
			}
		}
	}
}

func TestAutoSameKindLocalWins(t *testing.T) {
	r := &resolve.Resolver{
		Mode:  resolve.ModeAuto,
		Local: failing(model.SourceMacOS, backend.KindUnexpected, "local"),
		Web:   failing(model.SourceWeb, backend.KindUnexpected, "web"),
	}
	_, err := r.Read(context.Background())
	if err == nil || err.Error() != "local" {
		t.Errorf("Read() error = %v, want the local error", err)
	}
}

func TestLocalMode(t *testing.T) {
	t.Run("reads only local", func(t *testing.T) {
		l, w := local(model.StatePaused), web(model.StatePlaying)
		r := &resolve.Resolver{Mode: resolve.ModeLocal, Local: l, Web: w}
		got, err := r.Read(context.Background())
		if err != nil || got.Source != model.SourceMacOS {
			t.Fatalf("Read() = %v, %v, want the local item", got.Source, err)
		}
		if w.calls != 0 {
			t.Errorf("web calls = %d, want 0", w.calls)
		}
	})
	t.Run("maps the local error directly", func(t *testing.T) {
		l := failing(model.SourceMacOS, backend.KindNotAvailable, "Spotify is not running")
		w := web(model.StatePlaying)
		r := &resolve.Resolver{Mode: resolve.ModeLocal, Local: l, Web: w}
		_, err := r.Read(context.Background())
		if !errors.Is(err, l.err) || w.calls != 0 {
			t.Errorf("Read() error = %v, web calls = %d, want the local error and 0 calls", err, w.calls)
		}
	})
}

func TestWebMode(t *testing.T) {
	t.Run("reads only web", func(t *testing.T) {
		l, w := local(model.StatePlaying), web(model.StatePaused)
		r := &resolve.Resolver{Mode: resolve.ModeWeb, Local: l, Web: w}
		got, err := r.Read(context.Background())
		if err != nil || got.Source != model.SourceWeb {
			t.Fatalf("Read() = %v, %v, want the web item", got.Source, err)
		}
		if l.calls != 0 {
			t.Errorf("local calls = %d, want 0", l.calls)
		}
	})
	t.Run("maps the web error directly", func(t *testing.T) {
		w := failing(model.SourceWeb, backend.KindAuthRequired, "login expired")
		r := &resolve.Resolver{Mode: resolve.ModeWeb, Local: local(model.StatePlaying), Web: w}
		_, err := r.Read(context.Background())
		if !errors.Is(err, w.err) {
			t.Errorf("Read() error = %v, want %v", err, w.err)
		}
	})
	t.Run("nil web is not available", func(t *testing.T) {
		l := local(model.StatePlaying)
		r := &resolve.Resolver{Mode: resolve.ModeWeb, Local: l}
		_, err := r.Read(context.Background())
		if backend.KindOf(err) != backend.KindNotAvailable {
			t.Fatalf("kind = %d, want NotAvailable (err %v)", backend.KindOf(err), err)
		}
		var be *backend.Error
		if !errors.As(err, &be) || be.Message != "not logged in to the Spotify Web API" || be.Hint != "Run `spmeta auth login`." {
			t.Errorf("error = %+v, want the not-logged-in message and hint", be)
		}
		if l.calls != 0 {
			t.Errorf("local calls = %d, want 0", l.calls)
		}
	})
}

func TestUnknownMode(t *testing.T) {
	r := &resolve.Resolver{Mode: "bogus", Local: local(model.StatePlaying)}
	_, err := r.Read(context.Background())
	if backend.KindOf(err) != backend.KindUnexpected {
		t.Errorf("kind = %d, want Unexpected (err %v)", backend.KindOf(err), err)
	}
}

func TestDebugLogging(t *testing.T) {
	var buf bytes.Buffer
	log := slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug}))
	r := &resolve.Resolver{
		Mode:   resolve.ModeAuto,
		Local:  failing(model.SourceMacOS, backend.KindNotAvailable, "Spotify is not running"),
		Web:    web(model.StatePlaying),
		Logger: log,
	}
	if _, err := r.Read(context.Background()); err != nil {
		t.Fatalf("Read() error = %v", err)
	}
	out := buf.String()
	for _, want := range []string{"source=local", "source=web", "not_available", "playing"} {
		if !strings.Contains(out, want) {
			t.Errorf("log lacks %q:\n%s", want, out)
		}
	}
}

func TestNilLoggerIsSilent(t *testing.T) {
	r := &resolve.Resolver{Mode: resolve.ModeLocal, Local: local(model.StatePlaying)}
	if _, err := r.Read(context.Background()); err != nil {
		t.Fatalf("Read() error = %v", err)
	}
}
