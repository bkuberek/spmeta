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
  spmeta doctor [--debug] [--config PATH]
  spmeta version
  spmeta help

Commands:
  doctor    Report which sources work and why the others do not
  version   Print version and build information
  help      Print this help ('spmeta help doctor' for the doctor flags)

Flags:
      --source auto|local|web      Source to read (default auto)
      --format text|json|template  Output format (default text)
      --template STR               Template for --format template, for example '{artist} - {title}'
      --player NAME                Preferred MPRIS player (Linux)
      --config PATH                Read this configuration file instead of the default
      --quiet                      Print nothing; only set the exit code
      --debug                      Write diagnostics to stderr
  -h, --help                       Print this help
      --version                    Print version and build information

Environment: SPMETA_SOURCE, SPMETA_FORMAT, SPMETA_TEMPLATE, SPMETA_PLAYER, SPMETA_CONFIG.
Configuration: $XDG_CONFIG_HOME/spmeta/config.toml, else ~/.config/spmeta/config.toml.
`

const doctorUsageText = `spmeta doctor reports which sources spmeta can read and why the others cannot.

Usage:
  spmeta doctor [--debug] [--config PATH]

Flags:
      --config PATH  Read this configuration file instead of the default
      --debug        Write diagnostics to stderr
  -h, --help         Print this help

On macOS it may wait up to 120 s while you answer the Automation prompt.
Exit status: 0 when at least one source is usable, 4 when none is.
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

func printDoctorUsage(w io.Writer) {
	_, _ = io.WriteString(w, doctorUsageText)
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
