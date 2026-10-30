package cli

import (
	"fmt"
	"io"
	"runtime"
	"strings"

	"github.com/bkuberek/spmeta/internal/reference"
)

const usageHint = "Run 'spmeta --help' for usage."

// BuildInfo describes the running binary. Empty fields are omitted from output.
type BuildInfo struct {
	Version string
	Commit  string
	Date    string
}

// printHelp writes the help page of topic ("" for spmeta itself), which
// internal/reference renders.
func printHelp(w io.Writer, topic string) {
	text, _ := reference.Help(topic)
	_, _ = io.WriteString(w, text)
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
