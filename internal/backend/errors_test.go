package backend_test

import (
	"errors"
	"fmt"
	"testing"

	"github.com/bkuberek/spmeta/internal/backend"
)

func TestExitCode(t *testing.T) {
	t.Parallel()
	tests := []struct {
		kind backend.Kind
		want int
	}{
		{backend.KindUnexpected, 1},
		{backend.KindNothingPlaying, 3},
		{backend.KindNotAvailable, 4},
		{backend.KindAuthRequired, 5},
		{backend.KindPermissionDenied, 6},
		{backend.KindRateLimited, 7},
		{backend.Kind(0), 1},
		{backend.Kind(99), 1},
	}
	for _, tc := range tests {
		t.Run(fmt.Sprintf("kind %d", tc.kind), func(t *testing.T) {
			t.Parallel()
			if got := backend.ExitCode(tc.kind); got != tc.want {
				t.Errorf("ExitCode(%d) = %d, want %d", tc.kind, got, tc.want)
			}
		})
	}
}

func TestKindOf(t *testing.T) {
	t.Parallel()
	typed := &backend.Error{Kind: backend.KindAuthRequired, Message: "m"}
	tests := []struct {
		name string
		err  error
		want backend.Kind
	}{
		{"typed", typed, backend.KindAuthRequired},
		{"wrapped", fmt.Errorf("ctx: %w", typed), backend.KindAuthRequired},
		{"plain", errors.New("boom"), backend.KindUnexpected},
		{"nil", nil, backend.KindUnexpected},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := backend.KindOf(tc.err); got != tc.want {
				t.Errorf("KindOf = %d, want %d", got, tc.want)
			}
		})
	}
}

func TestErrorUnwrapAndMessage(t *testing.T) {
	t.Parallel()
	cause := errors.New("exit status 1")
	e := &backend.Error{Kind: backend.KindUnexpected, Message: "cannot read Spotify", Err: cause}
	if e.Error() != "cannot read Spotify" {
		t.Errorf("Error() = %q", e.Error())
	}
	if !errors.Is(e, cause) {
		t.Error("errors.Is(e, cause) = false")
	}
	var got *backend.Error
	if !errors.As(fmt.Errorf("w: %w", e), &got) || got != e {
		t.Error("errors.As did not find the *Error")
	}
}

func TestWorse(t *testing.T) {
	t.Parallel()
	mk := func(k backend.Kind, msg string) error { return &backend.Error{Kind: k, Message: msg} }
	order := []backend.Kind{
		backend.KindPermissionDenied,
		backend.KindAuthRequired,
		backend.KindRateLimited,
		backend.KindUnexpected,
		backend.KindNothingPlaying,
		backend.KindNotAvailable,
	}
	for i, hi := range order {
		for _, lo := range order[i+1:] {
			t.Run(fmt.Sprintf("%d beats %d", hi, lo), func(t *testing.T) {
				t.Parallel()
				a, b := mk(hi, "a"), mk(lo, "b")
				if got := backend.Worse(a, b); !errors.Is(got, a) {
					t.Errorf("Worse(hi, lo) = %v, want hi", got)
				}
				if got := backend.Worse(b, a); !errors.Is(got, a) {
					t.Errorf("Worse(lo, hi) = %v, want hi", got)
				}
			})
		}
	}

	t.Run("same kind keeps first", func(t *testing.T) {
		t.Parallel()
		a, b := mk(backend.KindNothingPlaying, "local"), mk(backend.KindNothingPlaying, "web")
		if got := backend.Worse(a, b); !errors.Is(got, a) {
			t.Errorf("got %v, want first", got)
		}
	})
	t.Run("nil is lowest", func(t *testing.T) {
		t.Parallel()
		a := mk(backend.KindNotAvailable, "a")
		if got := backend.Worse(nil, a); !errors.Is(got, a) {
			t.Errorf("Worse(nil, a) = %v", got)
		}
		if got := backend.Worse(a, nil); !errors.Is(got, a) {
			t.Errorf("Worse(a, nil) = %v", got)
		}
		if got := backend.Worse(nil, nil); got != nil {
			t.Errorf("Worse(nil, nil) = %v", got)
		}
	})
	t.Run("untyped error counts as unexpected", func(t *testing.T) {
		t.Parallel()
		plain := errors.New("boom")
		if got := backend.Worse(mk(backend.KindNothingPlaying, "n"), plain); !errors.Is(got, plain) {
			t.Errorf("got %v, want plain", got)
		}
	})
}
