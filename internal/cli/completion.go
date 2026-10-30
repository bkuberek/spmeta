package cli

import (
	"errors"
	"flag"
	"io"
	"strings"

	"github.com/bkuberek/spmeta/internal/reference"
)

// newCompletionFlags returns the flags of completion: none besides -h.
func newCompletionFlags() *flag.FlagSet {
	set := flag.NewFlagSet("spmeta completion", flag.ContinueOnError)
	set.SetOutput(io.Discard)
	return set
}

// runCompletion prints the completion script for the shell named in args.
func runCompletion(args []string, stdout, stderr io.Writer) int {
	set := newCompletionFlags()
	if err := set.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			printHelp(stdout, "completion")
			return exitOK
		}
		if name, ok := strings.CutPrefix(err.Error(), "flag provided but not defined: -"); ok {
			return usageError(stderr, "unknown flag --%s", name)
		}
		return usageError(stderr, "%v", err)
	}
	switch set.NArg() {
	case 0:
		shells := reference.Shells()
		last := len(shells) - 1
		return usageError(stderr, "completion needs a shell: %s or %s", strings.Join(shells[:last], ", "), shells[last])
	case 1:
	default:
		return usageError(stderr, "unexpected argument %q for completion", set.Arg(1))
	}
	script, err := reference.Completion(set.Arg(0))
	if err != nil {
		return usageError(stderr, "%v", err)
	}
	_, _ = io.WriteString(stdout, script)
	return exitOK
}
