// Package cli implements the spmeta command line: argument parsing,
// subcommand dispatch and exit codes.
package cli

import (
	"context"
	"errors"
	"flag"
	"io"
	"strings"
)

// Run executes spmeta with args (without the program name) and returns the
// process exit code. It never calls os.Exit so tests can drive it directly.
func Run(ctx context.Context, args []string, stdout, stderr io.Writer, info BuildInfo) int {
	if len(args) > 0 {
		switch args[0] {
		case "version":
			if len(args) > 1 {
				return usageError(stderr, "unexpected argument %q for version", args[1])
			}
			printVersion(stdout, info)
			return exitOK
		case "help":
			return runHelp(args[1:], stdout, stderr)
		}
	}
	return runRoot(ctx, args, stdout, stderr, info)
}

func runHelp(args []string, stdout, stderr io.Writer) int {
	if len(args) > 0 {
		return usageError(stderr, "no help available for %q", args[0])
	}
	printUsage(stdout)
	return exitOK
}

func runRoot(_ context.Context, args []string, stdout, stderr io.Writer, info BuildInfo) int {
	fs := flag.NewFlagSet("spmeta", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	showVersion := fs.Bool("version", false, "")

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

	// Backends arrive in later tasks; until then there is nothing to read.
	_, _ = io.WriteString(stderr, "spmeta: no source available in this build\n")
	return exitNoSource
}
