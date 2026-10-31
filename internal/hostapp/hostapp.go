// Package hostapp names the application macOS asks about when spmeta needs
// Automation permission: the terminal or status bar that launched it. See
// docs/design/backend-macos.md, section Host application.
package hostapp

import (
	"bytes"
	"context"
	"log/slog"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"
)

const (
	psPath    = "/bin/ps"
	psTimeout = time.Second
	// maxHops bounds the walk up the process chain.
	maxHops = 64
	// launchdPID is the process every orphaned chain ends at.
	launchdPID = 1
	bundleDir  = ".app/Contents/MacOS/"
)

// notHosts are executables that never own the permission: shells, terminal
// multiplexers and the programs that sit between a host app and a shell. The
// non-bundle rule skips them; tmux and screen servers are daemonized, so
// their chain ends at launchd without ever reaching the terminal.
var notHosts = map[string]bool{
	"sh": true, "bash": true, "zsh": true, "fish": true, "dash": true, "ksh": true,
	"tcsh": true, "csh": true, "nu": true, "xonsh": true, "elvish": true, "pwsh": true,
	"tmux": true, "screen": true, "login": true, "sudo": true, "su": true, "env": true,
	"script": true, "sshd": true, "sshd-session": true, "go": true, "spmeta": true,
}

// termPrograms maps TERM_PROGRAM values to the name in the Automation list.
// An empty name means the value says nothing about the host: tmux sets
// TERM_PROGRAM=tmux and hides the terminal around it.
var termPrograms = map[string]string{
	"Apple_Terminal": "Terminal",
	"iTerm.app":      "iTerm",
	"ghostty":        "Ghostty",
	"WarpTerminal":   "Warp",
	"vscode":         "Visual Studio Code",
	"WezTerm":        "WezTerm",
	"tmux":           "",
}

// Resolve returns the name of the app macOS asks about, or "" when it cannot
// tell. It walks the parent chain of this process, then falls back to
// termProgram (the TERM_PROGRAM value). It is best-effort: a failure of ps
// only reaches log, at debug level, and never stops the caller. It starts one
// short process, so call it only when a message needs the name.
func Resolve(ctx context.Context, termProgram string, log *slog.Logger) string {
	return resolve(ctx, terminalProgram(termProgram, os.Getenv("LC_TERMINAL")), os.Getppid(), snapshot, log)
}

// terminalProgram recovers the terminal that tmux hides: iTerm sets
// LC_TERMINAL=iTerm2, which survives into tmux sessions.
func terminalProgram(termProgram, lcTerminal string) string {
	if (termProgram == "" || termProgram == "tmux") && lcTerminal == "iTerm2" {
		return "iTerm.app"
	}
	return termProgram
}

func resolve(ctx context.Context, termProgram string, ppid int, run func(context.Context) (string, error), log *slog.Logger) string {
	out, err := run(ctx)
	if err != nil {
		if log != nil {
			log.Debug("host app: process table unavailable", "cause", err)
		}
	} else if name := FromProcesses(out, ppid); name != "" {
		return name
	}
	return FromTermProgram(termProgram)
}

// FromTermProgram names the app for a TERM_PROGRAM value, or returns "" when
// the value is empty or hides the terminal. An unlisted value is used as is,
// without a trailing ".app".
func FromTermProgram(termProgram string) string {
	if name, ok := termPrograms[termProgram]; ok {
		return name
	}
	return strings.TrimSuffix(termProgram, ".app")
}

// FromProcesses finds the host app in a snapshot of the process table, one
// "pid ppid executable" line per process, walking up from pid start. Rules:
//
//  1. The first ancestor whose executable lives in an app bundle names the
//     host: the outermost ".app" directory in its path, without the suffix
//     (so a helper nested in Visual Studio Code.app names Visual Studio Code).
//  2. Without one, if the chain reaches launchd, the outermost ancestor that
//     is not a shell, multiplexer or similar (see notHosts) names the host,
//     by executable name. This covers status bars such as SketchyBar.
//  3. A broken chain, a cycle, or more than maxHops hops names nothing.
func FromProcesses(snapshot string, start int) string {
	procs := parse(snapshot)
	seen := make(map[int]bool)
	candidate := ""
	pid := start
	for range maxHops {
		if pid <= launchdPID {
			return candidate
		}
		p, ok := procs[pid]
		if !ok || seen[pid] {
			return ""
		}
		seen[pid] = true
		if name := bundleName(p.exe); name != "" {
			return name
		}
		if base := baseName(p.exe); !notHosts[base] {
			candidate = base
		}
		pid = p.ppid
	}
	return ""
}

type process struct {
	ppid int
	exe  string
}

// parse reads "pid ppid executable" lines; the executable may contain spaces.
// Lines that do not fit, such as a header, are skipped.
func parse(snapshot string) map[int]process {
	procs := make(map[int]process)
	for line := range strings.SplitSeq(snapshot, "\n") {
		pidText, rest, ok := strings.Cut(strings.TrimSpace(line), " ")
		if !ok {
			continue
		}
		ppidText, exe, ok := strings.Cut(strings.TrimSpace(rest), " ")
		if !ok {
			continue
		}
		pid, err1 := strconv.Atoi(pidText)
		ppid, err2 := strconv.Atoi(ppidText)
		if err1 != nil || err2 != nil {
			continue
		}
		procs[pid] = process{ppid: ppid, exe: strings.TrimSpace(exe)}
	}
	return procs
}

// bundleName returns the outermost app bundle name of an executable inside
// an app, or "" for any other path.
func bundleName(exe string) string {
	if !strings.Contains(exe, bundleDir) {
		return ""
	}
	prefix, _, _ := strings.Cut(exe, ".app/")
	return prefix[strings.LastIndex(prefix, "/")+1:]
}

// baseName is the executable's file name without a login shell's leading dash.
func baseName(exe string) string {
	return strings.TrimPrefix(exe[strings.LastIndex(exe, "/")+1:], "-")
}

// snapshot reads the whole process table with one ps run.
func snapshot(ctx context.Context) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, psTimeout)
	defer cancel()
	var out bytes.Buffer
	cmd := exec.CommandContext(ctx, psPath, "-axo", "pid=,ppid=,comm=")
	cmd.Stdout = &out
	if err := cmd.Run(); err != nil {
		return "", err
	}
	return out.String(), nil
}
