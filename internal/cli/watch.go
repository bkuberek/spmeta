package cli

import (
	"context"
	"fmt"
	"io"
	"time"

	"github.com/bkuberek/spmeta/internal/backend"
	"github.com/bkuberek/spmeta/internal/config"
	"github.com/bkuberek/spmeta/internal/model"
	"github.com/bkuberek/spmeta/internal/resolve"
	"github.com/bkuberek/spmeta/internal/watch"
)

const defaultLocalInterval = time.Second

// WithSleep replaces the waits between watch reads, before a Web retry and
// during login. Tests use it to avoid real delays.
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
// read line by line see it at once.
func runWatch(ctx context.Context, r watch.Reader, interval time.Duration, render renderFunc, stdout, stderr io.Writer, o options) int {
	err := watch.Run(ctx, r, watch.Options{
		Interval: interval,
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
		Sleep:  o.sleep,
		Errorf: func(format string, args ...any) { _, _ = fmt.Fprintf(stderr, format+"\n", args...) },
	})
	if err == nil {
		return exitOK
	}
	printError(stderr, err)
	return backend.ExitCode(backend.KindOf(err))
}
