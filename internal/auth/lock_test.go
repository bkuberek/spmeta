package auth

import (
	"context"
	"errors"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/bkuberek/spmeta/internal/backend"
)

func TestTryLockExcludesOtherHolders(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "token.json.lock")
	release, err := tryLock(path)
	if err != nil {
		t.Fatalf("first tryLock = %v", err)
	}
	if _, err := tryLock(path); !errors.Is(err, errLockHeld) {
		t.Errorf("second tryLock = %v, want errLockHeld", err)
	}
	release()
	release2, err := tryLock(path)
	if err != nil {
		t.Fatalf("tryLock after release = %v", err)
	}
	release2()
}

// lockedSource returns a TokenSource over an expired token whose lock file is
// already held, with a clock that only the injected Sleep advances.
func lockedSource(t *testing.T, sleep func(context.Context, time.Duration) error) *TokenSource {
	t.Helper()
	path := filepath.Join(t.TempDir(), "token.json")
	tok := Token{
		ClientID: "c", AccessToken: "a", RefreshToken: "r",
		ExpiresAt: time.Unix(1000, 0),
	}
	if err := WriteToken(path, tok); err != nil {
		t.Fatal(err)
	}
	release, err := tryLock(path + ".lock")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(release)
	clock := time.Unix(1000, 0)
	return &TokenSource{
		Path: path,
		Now:  func() time.Time { return clock },
		Sleep: func(ctx context.Context, d time.Duration) error {
			clock = clock.Add(d)
			return sleep(ctx, d)
		},
	}
}

func TestLockTimeoutIsUnexpected(t *testing.T) {
	t.Parallel()
	var polls atomic.Int32
	src := lockedSource(t, func(context.Context, time.Duration) error {
		polls.Add(1)
		return nil
	})
	_, err := src.Token(context.Background(), false)
	if backend.KindOf(err) != backend.KindUnexpected {
		t.Fatalf("err = %v, want KindUnexpected", err)
	}
	// Polling every 100 ms for 10 s is 100 sleeps between 101 attempts.
	if n := polls.Load(); n != 100 {
		t.Errorf("lock polls = %d, want 100", n)
	}
}

func TestLockWaitStopsWhenContextIsCancelled(t *testing.T) {
	t.Parallel()
	src := lockedSource(t, func(context.Context, time.Duration) error {
		return context.Canceled
	})
	_, err := src.Token(context.Background(), false)
	if backend.KindOf(err) != backend.KindUnexpected || !errors.Is(err, context.Canceled) {
		t.Errorf("err = %v, want KindUnexpected wrapping context.Canceled", err)
	}
}
