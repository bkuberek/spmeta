package cli

import (
	"context"
	"errors"
	"flag"
	"io"
	"io/fs"
	"log/slog"
	"runtime"
	"strings"

	"github.com/bkuberek/spmeta/internal/backend"
	"github.com/bkuberek/spmeta/internal/backend/local"
	"github.com/bkuberek/spmeta/internal/config"
	"github.com/bkuberek/spmeta/internal/doctor"
	"github.com/bkuberek/spmeta/internal/hostapp"
)

// newDoctorFlags returns the flags of doctor, which internal/reference
// documents.
func newDoctorFlags() (set *flag.FlagSet, debug *bool, configFile *string) {
	set = flag.NewFlagSet("spmeta doctor", flag.ContinueOnError)
	set.SetOutput(io.Discard)
	return set, set.Bool("debug", false, ""), set.String("config", "", "")
}

func runDoctor(ctx context.Context, args []string, stdout, stderr io.Writer, info BuildInfo, opts options) int {
	set, debug, configFile := newDoctorFlags()
	if err := set.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			printHelp(stdout, "doctor")
			return exitOK
		}
		if name, ok := strings.CutPrefix(err.Error(), "flag provided but not defined: -"); ok {
			return usageError(stderr, "unknown flag --%s", name)
		}
		return usageError(stderr, "%v", err)
	}
	if set.NArg() > 0 {
		return usageError(stderr, "unexpected argument %q for doctor", set.Arg(0))
	}

	flags := config.Flags{Debug: *debug}
	set.Visit(func(f *flag.Flag) {
		if f.Name == "config" {
			flags.Config = configFile
		}
	})
	// A configuration problem is a finding to report, not a reason to stop:
	// the other checks still say whether a source works.
	settings, loadErr := config.Load(flags, opts.env)

	newLocal := opts.newLocal
	if newLocal == nil {
		newLocal = local.New
	}
	// The local backend doubles as the diagnostic hook: the macOS and MPRIS
	// backends implement the doctor interfaces, other platforms implement none.
	b := newLocal(local.Settings{Player: settings.Player})
	log := debugLogger(*debug, stderr)
	o := doctor.Options{
		Version: info.Version,
		OS:      runtime.GOOS,
		Arch:    runtime.GOARCH,
		Config:  configReport(settings, loadErr, opts.env),
		HostApp: hostApp(opts, log),
		Out:     stdout,
		Notice:  stderr,
		Log:     log,
		Web:     webReport(settings, opts, info, log),
	}
	o.MacOS, _ = b.(doctor.MacOSChecker)
	o.MPRIS, _ = b.(doctor.PlayerLister)

	if err := doctor.Run(ctx, o); err != nil {
		printError(stderr, err)
		return backend.ExitCode(backend.KindOf(err))
	}
	return exitOK
}

// hostApp returns doctor's lookup of the app macOS asks about, which runs
// only when doctor needs the name.
func hostApp(opts options, log *slog.Logger) func(context.Context) string {
	return func(ctx context.Context) string {
		term := getenv(opts.env, "TERM_PROGRAM")
		if opts.hostApp != nil {
			return opts.hostApp(ctx, term)
		}
		return hostapp.Resolve(ctx, term, log)
	}
}

// configReport tells doctor how the configuration resolved. It looks for the
// file itself because Settings does not record whether one was read.
func configReport(s config.Settings, loadErr error, env config.Env) doctor.ConfigReport {
	if loadErr != nil {
		return doctor.ConfigReport{Err: loadErr}
	}
	found := false
	if s.ConfigPath != "" && env.ReadFile != nil {
		_, err := env.ReadFile(s.ConfigPath)
		found = !errors.Is(err, fs.ErrNotExist)
	}
	return doctor.ConfigReport{Path: s.ConfigPath, Found: found, Warnings: s.Warnings}
}

func getenv(env config.Env, key string) string {
	if env.Getenv == nil {
		return ""
	}
	return env.Getenv(key)
}

// debugLogger writes debug lines to w when enabled and discards them
// otherwise, in the same format as the root command.
func debugLogger(enabled bool, w io.Writer) *slog.Logger {
	if !enabled {
		return slog.New(slog.DiscardHandler)
	}
	return slog.New(slog.NewTextHandler(w, &slog.HandlerOptions{
		Level: slog.LevelDebug,
		ReplaceAttr: func(groups []string, a slog.Attr) slog.Attr {
			if len(groups) == 0 && a.Key == slog.TimeKey {
				return slog.Attr{}
			}
			return a
		},
	}))
}
