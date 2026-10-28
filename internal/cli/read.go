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
	"github.com/bkuberek/spmeta/internal/format"
	"github.com/bkuberek/spmeta/internal/model"
)

const (
	formatText     = "text"
	formatJSON     = "json"
	formatTemplate = "template"
)

// Option customizes Run.
type Option func(*options)

type options struct {
	backend backend.Backend
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
	formatName := fs.String("format", formatText, "")
	tmplText := fs.String("template", "", "")
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

	explicit := map[string]bool{}
	fs.Visit(func(f *flag.Flag) { explicit[f.Name] = true })
	if explicit["template"] && !explicit["format"] {
		*formatName = formatTemplate
	}
	render, err := newRenderer(*formatName, *tmplText)
	if err != nil {
		return usageError(stderr, "%v", err)
	}

	log := slog.New(slog.DiscardHandler)
	if *debug && !*quiet {
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

	b := opts.backend
	var np *model.NowPlaying
	if b == nil {
		err = &backend.Error{Kind: backend.KindNotAvailable, Message: "no source available in this build"}
	} else {
		log.Debug("reading", "source", b.Source(), "format", *formatName)
		var item model.NowPlaying
		if item, err = b.Read(ctx); err == nil {
			np = &item
		}
	}

	// Only a clean read or "nothing playing" produces stdout; in JSON the
	// latter is the literal null.
	if !*quiet && (err == nil || backend.KindOf(err) == backend.KindNothingPlaying) {
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
	if !*quiet {
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

// newRenderer validates the format flags and, for templates, parses the
// template, so every usage error surfaces before any backend read.
func newRenderer(name, tmplText string) (renderFunc, error) {
	switch name {
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
		if tmplText == "" {
			return nil, errors.New("--format template requires a non-empty --template")
		}
		tpl, err := format.ParseTemplate(tmplText)
		if err != nil {
			return nil, fmt.Errorf("invalid template: %w", err)
		}
		return func(np *model.NowPlaying) ([]byte, error) {
			if np == nil {
				return nil, nil
			}
			return []byte(tpl.Render(np) + "\n"), nil
		}, nil
	default:
		return nil, fmt.Errorf("invalid --format %q (want text, json or template)", name)
	}
}
