package cli

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"strings"

	"github.com/bkuberek/spmeta/internal/backend"
	"github.com/bkuberek/spmeta/internal/config"
	"github.com/bkuberek/spmeta/internal/format"
	"github.com/bkuberek/spmeta/internal/model"
)

const (
	formatText     = "text"
	formatJSON     = "json"
	formatTemplate = "template"
)

// Warnings print as "spmeta: warning: <text>" on stderr.
const warningPrefix = "spmeta: warning: "

// Option customizes Run.
type Option func(*options)

type options struct {
	backend backend.Backend
	env     config.Env
}

// WithEnv replaces the process environment, home directory and filesystem
// that configuration is read from. Tests use it to stay hermetic.
func WithEnv(env config.Env) Option {
	return func(o *options) { o.env = env }
}

// WithBackend sets the backend the root command reads. Without it the read
// exits 4: production builds have no source until the resolver exists.
func WithBackend(b backend.Backend) Option {
	return func(o *options) { o.backend = b }
}

// renderFunc turns the current item (nil when nothing is playing) into the
// bytes written to stdout.
type renderFunc func(np *model.NowPlaying) ([]byte, error)

func runRoot(ctx context.Context, args []string, stdout, stderr io.Writer, info BuildInfo, opts options) int {
	fs := flag.NewFlagSet("spmeta", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	showVersion := fs.Bool("version", false, "")
	source := fs.String("source", "", "")
	formatName := fs.String("format", "", "")
	tmplText := fs.String("template", "", "")
	player := fs.String("player", "", "")
	configFile := fs.String("config", "", "")
	interval := fs.Float64("interval", 0, "")
	watch := fs.Bool("watch", false, "")
	quiet := fs.Bool("quiet", false, "")
	debug := fs.Bool("debug", false, "")

	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			printUsage(stdout)
			return exitOK
		}
		if name, ok := strings.CutPrefix(err.Error(), "flag provided but not defined: -"); ok {
			dash := "--"
			if len(name) == 1 {
				dash = "-"
			}
			return usageError(stderr, "unknown flag %s%s", dash, name)
		}
		return usageError(stderr, "%v", err)
	}
	if fs.NArg() > 0 {
		return usageError(stderr, "unknown command %q", fs.Arg(0))
	}
	if *showVersion {
		printVersion(stdout, info)
		return exitOK
	}

	flags := config.Flags{Watch: *watch, Quiet: *quiet, Debug: *debug}
	fs.Visit(func(f *flag.Flag) {
		switch f.Name {
		case "source":
			flags.Source = source
		case "format":
			flags.Format = formatName
		case "template":
			flags.Template = tmplText
		case "player":
			flags.Player = player
		case "config":
			flags.Config = configFile
		case "interval":
			flags.Interval = interval
		}
	})
	settings, err := config.Load(flags, opts.env)
	if err != nil {
		return usageError(stderr, "%v", err)
	}
	if !settings.Quiet {
		for _, w := range settings.Warnings {
			_, _ = io.WriteString(stderr, warningPrefix+w+"\n")
		}
	}
	render, err := newRenderer(settings)
	if err != nil {
		return usageError(stderr, "%v", err)
	}

	log := slog.New(slog.DiscardHandler)
	if settings.Debug && !settings.Quiet {
		log = slog.New(slog.NewTextHandler(stderr, &slog.HandlerOptions{
			Level: slog.LevelDebug,
			ReplaceAttr: func(groups []string, a slog.Attr) slog.Attr {
				if len(groups) == 0 && a.Key == slog.TimeKey {
					return slog.Attr{}
				}
				return a
			},
		}))
	}
	log.Debug("settings", "config", settings.ConfigPath, "source", settings.Source, "format", settings.Format)

	b := opts.backend
	var np *model.NowPlaying
	if b == nil {
		err = &backend.Error{Kind: backend.KindNotAvailable, Message: "no source available in this build"}
	} else {
		log.Debug("reading", "source", b.Source(), "format", settings.Format)
		var item model.NowPlaying
		if item, err = b.Read(ctx); err == nil {
			np = &item
		}
	}

	// Only a clean read or "nothing playing" produces stdout; in JSON the
	// latter is the literal null.
	if !settings.Quiet && (err == nil || backend.KindOf(err) == backend.KindNothingPlaying) {
		if code := writeOutput(stdout, render, np); code != exitOK {
			printError(stderr, errors.New("cannot write output"))
			return code
		}
	}
	if err == nil {
		return exitOK
	}
	var be *backend.Error
	if errors.As(err, &be) && be.Err != nil {
		log.Debug("cause", "error", be.Err)
	}
	if !settings.Quiet {
		printError(stderr, err)
	}
	return backend.ExitCode(backend.KindOf(err))
}

// writeOutput renders np and writes it to stdout, returning a failure exit
// code only when rendering or writing fails.
func writeOutput(stdout io.Writer, render renderFunc, np *model.NowPlaying) int {
	out, err := render(np)
	if err == nil && len(out) > 0 {
		_, err = stdout.Write(out)
	}
	if err != nil {
		return backend.ExitCode(backend.KindUnexpected)
	}
	return exitOK
}

// newRenderer builds the output renderer for the resolved settings and, for
// templates, parses the template, so every usage error surfaces before any
// backend read.
func newRenderer(s config.Settings) (renderFunc, error) {
	switch s.Format {
	case formatText:
		return func(np *model.NowPlaying) ([]byte, error) {
			if np == nil {
				return nil, nil
			}
			return []byte(format.Text(np) + "\n"), nil
		}, nil
	case formatJSON:
		return format.JSON, nil
	case formatTemplate:
		tpl, err := format.ParseTemplate(s.Template)
		if err != nil {
			return nil, fmt.Errorf("invalid template from %s: %w", s.TemplateOrigin, err)
		}
		return func(np *model.NowPlaying) ([]byte, error) {
			if np == nil {
				return nil, nil
			}
			return []byte(tpl.Render(np) + "\n"), nil
		}, nil
	default:
		return nil, fmt.Errorf("unsupported format %q", s.Format)
	}
}
