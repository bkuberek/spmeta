// Package cli implements the spmeta command line: argument parsing,
// subcommand dispatch and exit codes.
package cli

import (
	"context"
	"io"

	"github.com/bkuberek/spmeta/internal/config"
)

// Run executes spmeta with args (without the program name) and returns the
// process exit code. It never calls os.Exit so tests can drive it directly.
func Run(ctx context.Context, args []string, stdout, stderr io.Writer, info BuildInfo, opts ...Option) int {
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
	o := options{env: config.OSEnv()}
	for _, opt := range opts {
		opt(&o)
	}
	if len(args) > 0 && args[0] == "doctor" {
		return runDoctor(ctx, args[1:], stdout, stderr, info, o)
	}
	return runRoot(ctx, args, stdout, stderr, info, o)
}

func runHelp(args []string, stdout, stderr io.Writer) int {
	if len(args) > 0 {
		if args[0] == "doctor" && len(args) == 1 {
			printDoctorUsage(stdout)
			return exitOK
		}
		return usageError(stderr, "no help available for %q", args[0])
	}
	printUsage(stdout)
	return exitOK
}
