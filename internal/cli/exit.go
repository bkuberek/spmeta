package cli

// Exit codes for kinds of backend failure come from backend.ExitCode; these
// are the ones the CLI itself produces. Public contract: docs/design/cli.md.
const (
	exitOK    = 0
	exitUsage = 2
)
