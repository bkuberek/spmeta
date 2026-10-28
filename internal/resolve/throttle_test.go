package resolve_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/bkuberek/spmeta/internal/backend"
	"github.com/bkuberek/spmeta/internal/model"
	"github.com/bkuberek/spmeta/internal/resolve"
)

type clock struct{ t time.Time }

func (c *clock) now() time.Time          { return c.t }
func (c *clock) advance(d time.Duration) { c.t = c.t.Add(d) }

func TestThrottleCachesWithinInterval(t *testing.T) {
	f := web(model.StatePlaying)
	c := &clock{t: time.Date(2026, 10, 27, 12, 0, 0, 0, time.UTC)}
	b := resolve.Throttle(f, 3*time.Second, c.now)

	first, err := b.Read(context.Background())
	if err != nil || first.State != model.StatePlaying {
		t.Fatalf("first read = %+v, %v", first, err)
	}
	f.np.State = model.StatePaused
	c.advance(2999 * time.Millisecond)
	second, err := b.Read(context.Background())
	if err != nil || second.State != model.StatePlaying {
		t.Errorf("read inside the interval = %+v, %v; want the cached result", second, err)
	}
	if f.calls != 1 {
		t.Errorf("backend calls = %d, want 1", f.calls)
	}

	c.advance(time.Millisecond)
	third, err := b.Read(context.Background())
	if err != nil || third.State != model.StatePaused {
		t.Errorf("read at the interval = %+v, %v; want a fresh result", third, err)
	}
	if f.calls != 2 {
		t.Errorf("backend calls = %d, want 2", f.calls)
	}
}

func TestThrottleCachesErrors(t *testing.T) {
	f := failing(model.SourceWeb, backend.KindRateLimited, "rate limited")
	c := &clock{t: time.Unix(0, 0)}
	b := resolve.Throttle(f, time.Second, c.now)

	_, err1 := b.Read(context.Background())
	_, err2 := b.Read(context.Background())
	if err1 == nil || !errors.Is(err2, err1) {
		t.Errorf("errors = %v, %v; want the same cached error", err1, err2)
	}
	if f.calls != 1 {
		t.Errorf("backend calls = %d, want 1", f.calls)
	}
}

func TestThrottleDoesNotCacheCancellation(t *testing.T) {
	f := web(model.StatePlaying)
	f.err = context.Canceled
	c := &clock{t: time.Unix(0, 0)}
	b := resolve.Throttle(f, time.Hour, c.now)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := b.Read(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v, want context.Canceled", err)
	}
	f.err = nil
	if _, err := b.Read(context.Background()); err != nil {
		t.Errorf("read after a cancelled one = %v, want a fresh read", err)
	}
}

func TestThrottleKeepsSource(t *testing.T) {
	b := resolve.Throttle(web(model.StatePlaying), time.Second, nil)
	if b.Source() != model.SourceWeb {
		t.Errorf("Source = %v, want web", b.Source())
	}
	if _, err := b.Read(context.Background()); err != nil {
		t.Errorf("Read with the default clock = %v", err)
	}
}
