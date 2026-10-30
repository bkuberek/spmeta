// Package watch polls a reader and emits a line only when the item, its
// state or its device changes. A wake channel lets a source that announces
// changes end a pause early (docs/design/architecture.md, Watch loop).
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

// MinWakeGap is the shortest pause between a read and one that a wake starts,
// so a source that floods notifications cannot make reads back to back.
const MinWakeGap = 100 * time.Millisecond

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
	// Wake, when not nil, announces that the item may have changed. A value
	// ends the pause after a read that reached a source, so the next read
	// starts at once, but never sooner than MinWakeGap after the previous
	// one. Pauses after errors run their full length. A closed Wake leaves
	// plain polling.
	Wake <-chan struct{}
	// Emit writes one change; a nil np means nothing is playing.
	Emit func(np *model.NowPlaying) error
	// Sleep waits d, or less when ctx ends (returning ctx.Err()) or a value
	// arrives on wake (returning nil). wake is nil when the pause must run
	// its full length; a closed wake never ends it. Nil uses a timer.
	Sleep func(ctx context.Context, d time.Duration, wake <-chan struct{}) error
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
	l := loop{r: r, opts: opts, wake: opts.Wake}
	return l.run(ctx)
}

type loop struct {
	r    Reader
	opts Options
	// wake is opts.Wake until it closes, then nil.
	wake <-chan struct{}

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
		wait, wakeable, err := l.step(np, err)
		if err != nil {
			return err
		}
		if l.pause(ctx, wait, wakeable) != nil && ctx.Err() != nil {
			return nil
		}
	}
}

// step handles one read result and returns how long to wait before the next
// and whether a wake may end that wait early.
func (l *loop) step(np model.NowPlaying, err error) (time.Duration, bool, error) {
	if err == nil {
		l.settle()
		return l.opts.Interval, true, l.emitIfChanged(changeKey(&np), &np)
	}
	var be *backend.Error
	errors.As(err, &be)
	switch backend.KindOf(err) {
	case backend.KindNothingPlaying, backend.KindNotAvailable:
		l.settle()
		return l.opts.Interval, true, l.emitIfChanged(nothingKey, nil)
	case backend.KindRateLimited:
		if be != nil && be.RetryAfter > 0 {
			return be.RetryAfter, false, nil
		}
		return defaultRetryAfter, false, nil
	case backend.KindAuthRequired, backend.KindPermissionDenied:
		return 0, false, err
	default:
		l.report(err, be)
		l.failures++
		return l.backoff(), false, nil
	}
}

// pause waits d before the next read. When wakeable, it first waits
// MinWakeGap, which no wake ends; a wake that arrived during the read or the
// gap then starts the next read, and one that arrives later ends the rest of
// the wait. Wakes still pending afterwards are dropped, because the next read
// covers them; a burst therefore costs one read.
func (l *loop) pause(ctx context.Context, d time.Duration, wakeable bool) error {
	if !wakeable || l.wake == nil {
		err := l.opts.Sleep(ctx, d, nil)
		l.drain()
		return err
	}
	gap := min(MinWakeGap, d)
	if err := l.opts.Sleep(ctx, gap, nil); err != nil {
		return err
	}
	if l.drain() || gap == d {
		return nil
	}
	err := l.opts.Sleep(ctx, d-gap, l.wake)
	l.drain()
	return err
}

// drain empties the wake channel without blocking and reports whether a wake
// was pending. A closed channel is dropped, which leaves plain polling.
func (l *loop) drain() bool {
	woke := false
	for range cap(l.wake) + 1 {
		select {
		case _, ok := <-l.wake:
			if !ok {
				l.wake = nil
				return woke
			}
			woke = true
		default:
			return woke
		}
	}
	return woke
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

// sleep is the default Sleep. A wake channel that closes stops ending the
// wait, which then runs to d.
func sleep(ctx context.Context, d time.Duration, wake <-chan struct{}) error {
	t := time.NewTimer(d)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-t.C:
			return nil
		case _, ok := <-wake:
			if ok {
				return nil
			}
			wake = nil
		}
	}
}
