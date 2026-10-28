package cli

import (
	"errors"
	"fmt"
	"io"

	"github.com/bkuberek/spmeta/internal/backend"
)

// Exit codes for kinds of backend failure come from backend.ExitCode; these
// are the ones the CLI itself produces. Public contract: docs/design/cli.md.
const (
	exitOK    = 0
	exitUsage = 2
)

// printError writes the failure as "spmeta: <message>" and, when the error
// carries one, "hint: <hint>" (docs/design/architecture.md, Typed errors).
func printError(stderr io.Writer, err error) {
	var be *backend.Error
	if !errors.As(err, &be) {
		_, _ = fmt.Fprintf(stderr, "spmeta: %s\n", err)
		return
	}
	_, _ = fmt.Fprintf(stderr, "spmeta: %s\n", be)
	if be.Hint != "" {
		_, _ = fmt.Fprintf(stderr, "hint: %s\n", be.Hint)
	}
}
