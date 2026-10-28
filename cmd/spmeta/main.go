// Command spmeta prints the track Spotify is currently playing.
package main

import (
	"context"
	"os"
	"os/signal"
	"regexp"
	"runtime/debug"
	"strings"
	"syscall"

	"github.com/bkuberek/spmeta/internal/cli"
)

// Set by release builds with -ldflags "-X main.version=... -X main.commit=... -X main.date=...".
var pseudoVersion = regexp.MustCompile(`-\d{14}-[0-9a-f]{12}`)

var (
	version = "dev"
	commit  = ""
	date    = ""
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	code := cli.Run(ctx, os.Args[1:], os.Stdout, os.Stderr, buildInfo())
	stop()
	os.Exit(code)
}

// buildInfo returns the ldflags values, falling back to Go build information
// for "go install" builds where no flags were injected.
func buildInfo() cli.BuildInfo {
	info := cli.BuildInfo{Version: version, Commit: commit, Date: date}
	if version != "dev" {
		return info
	}
	bi, ok := debug.ReadBuildInfo()
	if !ok {
		return info
	}
	if v := bi.Main.Version; isReleaseVersion(v) {
		info.Version = v
	}
	for _, s := range bi.Settings {
		switch s.Key {
		case "vcs.revision":
			if info.Commit == "" {
				info.Commit = shorten(s.Value)
			}
		case "vcs.time":
			if info.Date == "" {
				info.Date = s.Value
			}
		}
	}
	return info
}

// isReleaseVersion reports whether v is a tagged module version. Local builds
// carry "(devel)" or a VCS-derived pseudo-version, which stay reported as "dev".
func isReleaseVersion(v string) bool {
	return v != "" && v != "(devel)" && !pseudoVersion.MatchString(v) && !strings.HasSuffix(v, "+dirty")
}

func shorten(rev string) string {
	const n = 7
	if len(rev) > n {
		return rev[:n]
	}
	return rev
}
