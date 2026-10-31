package macos

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os/exec"
	"strings"
	"time"
)

const (
	pgrepPath    = "/usr/bin/pgrep"
	osascriptBin = "/usr/bin/osascript"
	waitDelay    = 100 * time.Millisecond
)

// Runner runs the two external programs the backend needs.
type Runner interface {
	// ProcessRunning reports whether a process with exactly this name runs.
	ProcessRunning(ctx context.Context, name string) (bool, error)
	// RunScript runs /usr/bin/osascript with the script on stdin.
	RunScript(ctx context.Context, script string) (stdout, stderr []byte, err error)
}

// ExecRunner is the production Runner.
type ExecRunner struct{}

// ProcessRunning runs pgrep -x. Exit status 0 means running, 1 means not
// running, anything else is an error.
func (ExecRunner) ProcessRunning(ctx context.Context, name string) (bool, error) {
	err := exec.CommandContext(ctx, pgrepPath, "-x", name).Run()
	if err == nil {
		return true, nil
	}
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) && exitErr.ExitCode() == 1 {
		return false, nil
	}
	return false, fmt.Errorf("pgrep: %w", err)
}

// listenerCommand returns the unstarted command that runs the change
// listener: osascript running a JavaScript for Automation program read from
// stdin.
func listenerCommand() *exec.Cmd {
	return exec.Command(osascriptBin, "-l", "JavaScript", "-")
}

// RunScript runs osascript with the script on stdin and returns stdout and
// stderr separately. A nonzero exit is returned as err.
func (ExecRunner) RunScript(ctx context.Context, script string) (stdout, stderr []byte, err error) {
	cmd := exec.CommandContext(ctx, osascriptBin, "-")
	cmd.Stdin = strings.NewReader(script)
	var out, errOut bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &errOut
	cmd.WaitDelay = waitDelay
	err = cmd.Run()
	return out.Bytes(), errOut.Bytes(), err
}
