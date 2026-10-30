package cli

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"time"

	"github.com/bkuberek/spmeta/internal/backend"
	"github.com/bkuberek/spmeta/internal/config"
	"github.com/bkuberek/spmeta/internal/model"
	"github.com/bkuberek/spmeta/internal/resolve"
	"github.com/bkuberek/spmeta/internal/watch"
)

const defaultLocalInterval = time.Second

// WithSleep replaces the waits between watch reads, before a Web retry and
// during login. Tests use it to avoid real delays. A change notification does
// not end a replaced wait early.
func WithSleep(f func(ctx context.Context, d time.Duration) error) Option {
	return func(o *options) { o.sleep = f }
}

// WithClock replaces the clock of the Web backend: token expiry, the watch
// throttle, and the login flow.
func WithClock(now func() time.Time) Option {
	return func(o *options) { o.now = now }
}

// pollInterval picks the watch poll interval. With --source web it is the
// --interval flag when given, else web_interval; otherwise the interval from
// the flag or the configuration file, else one second. In auto mode the
// local backend sets the pace and Web is throttled separately.
func pollInterval(s config.Settings, flagGiven bool) time.Duration {
	if s.Source == string(resolve.ModeWeb) {
		if flagGiven {
			return s.Interval
		}
		return s.WebInterval
	}
	if s.Interval > 0 {
		return s.Interval
	}
	return defaultLocalInterval
}

// runWatch prints a line for each change until ctx ends (exit 0) or a fatal
// error stops it. Every line goes out in one Write call so consumers that
// read line by line see it at once. When r announces changes, a change is
// read at once; polling at interval continues either way.
func runWatch(ctx context.Context, r watch.Reader, interval time.Duration, render renderFunc, stdout, stderr io.Writer, log *slog.Logger, o options) int {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel() // stops the notifier with the loop
	err := watch.Run(ctx, r, watch.Options{
		Interval: interval,
		Wake:     changes(ctx, r, log),
		Emit: func(np *model.NowPlaying) error {
			out, err := render(np)
			if err != nil {
				return err
			}
			if len(out) == 0 {
				out = []byte("\n")
			}
			_, err = stdout.Write(out)
			return err
		},
		Sleep:  watchSleep(o.sleep),
		Errorf: func(format string, args ...any) { _, _ = fmt.Fprintf(stderr, format+"\n", args...) },
	})
	if err == nil {
		return exitOK
	}
	printError(stderr, err)
	return backend.ExitCode(backend.KindOf(err))
}

// changes starts the change notifications of r when it offers them. Failing
// to start them is not an error: watch mode polls, and only --debug says so.
func changes(ctx context.Context, r watch.Reader, log *slog.Logger) <-chan struct{} {
	n, ok := r.(backend.Notifier)
	if !ok {
		return nil
	}
	wake, err := n.Changes(ctx)
	if err != nil {
		log.Debug("change notifications unavailable, polling only", "error", err)
		return nil
	}
	log.Debug("change notifications on")
	return wake
}

// watchSleep adapts a WithSleep replacement to the watch loop. The
// replacement cannot be woken, so a change during its wait is read after it;
// a change pending when the wait would start still skips it. Nil keeps the
// loop's own timer.
func watchSleep(sleep func(context.Context, time.Duration) error) func(context.Context, time.Duration, <-chan struct{}) error {
	if sleep == nil {
		return nil
	}
	return func(ctx context.Context, d time.Duration, _ <-chan struct{}) error { return sleep(ctx, d) }
}
