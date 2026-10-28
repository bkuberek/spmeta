package cli

import (
	"fmt"
	"io"
	"runtime"
	"strings"
)

const usageHint = "Run 'spmeta --help' for usage."

const usageText = `spmeta prints the track Spotify is currently playing.

Usage:
  spmeta [flags]
  spmeta version
  spmeta help

Commands:
  version   Print version and build information
  help      Print this help

Flags:
  -h, --help      Print this help
      --version   Print version and build information
`

// BuildInfo describes the running binary. Empty fields are omitted from output.
type BuildInfo struct {
	Version string
	Commit  string
	Date    string
}

func printUsage(w io.Writer) {
	_, _ = io.WriteString(w, usageText)
}

func printVersion(w io.Writer, info BuildInfo) {
	version := info.Version
	if version == "" {
		version = "dev"
	}
	parts := make([]string, 0, 4)
	if info.Commit != "" {
		parts = append(parts, "commit "+info.Commit)
	}
	if info.Date != "" {
		parts = append(parts, "built "+info.Date)
	}
	parts = append(parts, runtime.Version(), runtime.GOOS+"/"+runtime.GOARCH)
	_, _ = fmt.Fprintf(w, "spmeta %s (%s)\n", version, strings.Join(parts, ", "))
}

func usageError(stderr io.Writer, format string, args ...any) int {
	_, _ = fmt.Fprintf(stderr, "spmeta: %s\n%s\n", fmt.Sprintf(format, args...), usageHint)
	return exitUsage
}
