package resolve

import (
	"context"
	"sync"
	"time"

	"github.com/bkuberek/spmeta/internal/backend"
	"github.com/bkuberek/spmeta/internal/model"
)

// Throttle wraps b so that reads less than interval after the previous one
// return that read's result or error instead of calling b. A read that ends
// because ctx was cancelled is not remembered. A nil now uses time.Now.
func Throttle(b backend.Backend, interval time.Duration, now func() time.Time) backend.Backend {
	if now == nil {
		now = time.Now
	}
	return &throttled{inner: b, interval: interval, now: now}
}

type throttled struct {
	inner    backend.Backend
	interval time.Duration
	now      func() time.Time

	mu     sync.Mutex
	has    bool
	at     time.Time
	cached model.NowPlaying
	err    error
}

func (t *throttled) Source() model.Source { return t.inner.Source() }

func (t *throttled) Read(ctx context.Context) (model.NowPlaying, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	start := t.now()
	if t.has && start.Sub(t.at) < t.interval {
		return t.cached, t.err
	}
	np, err := t.inner.Read(ctx)
	if ctx.Err() != nil {
		t.has = false
		return np, err
	}
	t.has, t.at, t.cached, t.err = true, start, np, err
	return np, err
}
