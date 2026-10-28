// Package watch polls a reader and emits a line only when the item, its
// state or its device changes (docs/design/architecture.md, Watch loop).
package watch

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/bkuberek/spmeta/internal/backend"
	"github.com/bkuberek/spmeta/internal/model"
)

const (
	defaultRetryAfter = 5 * time.Second
	maxBackoff        = 30 * time.Second
	backoffAfter      = 5
	nothingKey        = "none"
	unitSeparator     = "\x1f"
)

// Reader reads the current item.
type Reader interface {
	Read(ctx context.Context) (model.NowPlaying, error)
}

// Options configures Run.
type Options struct {
	// Interval is the pause between reads.
	Interval time.Duration
	// Emit writes one change; a nil np means nothing is playing.
	Emit func(np *model.NowPlaying) error
	// Sleep waits d or until ctx ends, returning ctx.Err() in that case.
	// Nil uses a timer.
	Sleep func(ctx context.Context, d time.Duration) error
	// Errorf reports a transient error as one stderr line. Nil discards.
	Errorf func(format string, args ...any)
}

// Run polls r until ctx is cancelled, which returns nil, or a fatal error
// occurs, which Run returns without printing it. A failing Emit also stops
// the loop.
func Run(ctx context.Context, r Reader, opts Options) error {
	if opts.Sleep == nil {
		opts.Sleep = sleep
	}
	if opts.Errorf == nil {
		opts.Errorf = func(string, ...any) {}
	}
	l := loop{r: r, opts: opts}
	return l.run(ctx)
}

type loop struct {
	r    Reader
	opts Options

	lastKey    string
	emitted    bool
	failures   int
	lastErrMsg string
}

func (l *loop) run(ctx context.Context) error {
	for {
		np, err := l.r.Read(ctx)
		if ctx.Err() != nil {
			return nil
		}
		wait, err := l.step(np, err)
		if err != nil {
			return err
		}
		if l.opts.Sleep(ctx, wait) != nil && ctx.Err() != nil {
			return nil
		}
	}
}

// step handles one read result and returns how long to wait before the next.
func (l *loop) step(np model.NowPlaying, err error) (time.Duration, error) {
	if err == nil {
		l.settle()
		return l.opts.Interval, l.emitIfChanged(changeKey(&np), &np)
	}
	var be *backend.Error
	errors.As(err, &be)
	switch backend.KindOf(err) {
	case backend.KindNothingPlaying, backend.KindNotAvailable:
		l.settle()
		return l.opts.Interval, l.emitIfChanged(nothingKey, nil)
	case backend.KindRateLimited:
		if be != nil && be.RetryAfter > 0 {
			return be.RetryAfter, nil
		}
		return defaultRetryAfter, nil
	case backend.KindAuthRequired, backend.KindPermissionDenied:
		return 0, err
	default:
		l.report(err, be)
		l.failures++
		return l.backoff(), nil
	}
}

// settle clears the failure state after a read that reached a source.
func (l *loop) settle() {
	l.failures = 0
	l.lastErrMsg = ""
}

func (l *loop) emitIfChanged(key string, np *model.NowPlaying) error {
	if l.emitted && key == l.lastKey {
		return nil
	}
	if err := l.opts.Emit(np); err != nil {
		return &backend.Error{Kind: backend.KindUnexpected, Message: "cannot write output", Err: fmt.Errorf("write: %w", err)}
	}
	l.lastKey, l.emitted = key, true
	return nil
}

// report prints a transient error once per distinct message.
func (l *loop) report(err error, be *backend.Error) {
	msg := err.Error()
	if msg == l.lastErrMsg {
		return
	}
	l.lastErrMsg = msg
	l.opts.Errorf("spmeta: %s", msg)
	if be != nil && be.Hint != "" {
		l.opts.Errorf("hint: %s", be.Hint)
	}
}

// backoff returns the pause after a transient failure: the interval, doubled
// for each failure from the fifth consecutive one, capped at 30 seconds or at
// the interval when that is longer.
func (l *loop) backoff() time.Duration {
	limit := max(maxBackoff, l.opts.Interval)
	d := l.opts.Interval
	for n := backoffAfter; n <= l.failures && d < limit; n++ {
		d = min(d*2, limit)
	}
	return d
}

// changeKey identifies what a line shows: source, state, type, item identity
// and device. Position and fetch time are left out so progress stays silent.
func changeKey(np *model.NowPlaying) string {
	var id string
	if np.URI != nil {
		id = *np.URI
	} else {
		id = strings.Join([]string{deref(np.Title), deref(np.Artist), deref(np.Album)}, unitSeparator)
	}
	device := ""
	if np.Device != nil {
		device = np.Device.Name
	}
	return strings.Join([]string{string(np.Source), string(np.State), string(np.Type), id, device}, unitSeparator)
}

func deref(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

func sleep(ctx context.Context, d time.Duration) error {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}
