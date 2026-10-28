package backend

import (
	"errors"
	"time"

	"github.com/bkuberek/spmeta/internal/model"
)

// Kind classifies a backend failure and selects the process exit code.
type Kind int

// Kinds of backend failure. Each maps to one exit code.
const (
	KindUnexpected Kind = iota + 1
	KindNothingPlaying
	KindNotAvailable
	KindAuthRequired
	KindPermissionDenied
	KindRateLimited
)

// Error is the error type backends return.
type Error struct {
	Kind       Kind
	Source     model.Source  // empty when not tied to a source
	Message    string        // one line, user-facing, no secrets
	Hint       string        // next step for the user; may be empty
	RetryAfter time.Duration // KindRateLimited only
	Err        error         // wrapped cause; logged only under --debug
}

func (e *Error) Error() string {
	switch {
	case e.Message != "":
		return e.Message
	case e.Err != nil:
		return e.Err.Error()
	default:
		return "unexpected error"
	}
}

func (e *Error) Unwrap() error { return e.Err }

// KindOf returns the Kind of a *Error found with errors.As, or KindUnexpected.
func KindOf(err error) Kind {
	var be *Error
	if errors.As(err, &be) {
		return be.Kind
	}
	return KindUnexpected
}

// ExitCode maps a Kind to the process exit code (docs/design/cli.md).
func ExitCode(k Kind) int {
	switch k {
	case KindNothingPlaying:
		return 3
	case KindNotAvailable:
		return 4
	case KindAuthRequired:
		return 5
	case KindPermissionDenied:
		return 6
	case KindRateLimited:
		return 7
	default:
		return 1
	}
}

// rank orders kinds for Worse; higher wins.
func rank(k Kind) int {
	switch k {
	case KindPermissionDenied:
		return 6
	case KindAuthRequired:
		return 5
	case KindRateLimited:
		return 4
	case KindNothingPlaying:
		return 2
	case KindNotAvailable:
		return 1
	default:
		return 3
	}
}

// Worse returns whichever error has the higher precedence: permission denied,
// auth required, rate limited, unexpected, nothing playing, not available. A
// nil error has the lowest precedence and ties return a.
func Worse(a, b error) error {
	switch {
	case a == nil:
		return b
	case b == nil:
		return a
	case rank(KindOf(b)) > rank(KindOf(a)):
		return b
	default:
		return a
	}
}
