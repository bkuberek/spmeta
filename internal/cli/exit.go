package cli

// Exit codes are public contract; see docs/design/cli.md.
const (
	exitOK           = 0
	exitError        = 1
	exitUsage        = 2
	exitNothing      = 3
	exitNoSource     = 4
	exitAuthRequired = 5
	exitPermission   = 6
	exitRateLimited  = 7
)
