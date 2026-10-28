package cli_test

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/bkuberek/spmeta/internal/cli"
	"github.com/bkuberek/spmeta/internal/config"
)

func run(t *testing.T, args ...string) (code int, stdout, stderr string) {
	t.Helper()
	var out, errOut bytes.Buffer
	code = cli.Run(context.Background(), args, &out, &errOut, cli.BuildInfo{Version: "dev"}, cli.WithBackend(&fakeBackend{err: errNotRunning}), cli.WithEnv(config.Env{}))
	return code, out.String(), errOut.String()
}

func TestRunVersion(t *testing.T) {
	for _, args := range [][]string{{"version"}, {"--version"}, {"-version"}} {
		t.Run(strings.Join(args, " "), func(t *testing.T) {
			code, stdout, stderr := run(t, args...)
			if code != 0 {
				t.Fatalf("exit = %d, want 0 (stderr %q)", code, stderr)
			}
			if !strings.HasPrefix(stdout, "spmeta dev (") || !strings.HasSuffix(stdout, ")\n") {
				t.Errorf("stdout = %q, want line starting with %q", stdout, "spmeta dev (")
			}
			if stderr != "" {
				t.Errorf("stderr = %q, want empty", stderr)
			}
		})
	}
}

func TestRunVersionIncludesBuildInfo(t *testing.T) {
	var out bytes.Buffer
	info := cli.BuildInfo{Version: "0.1.0", Commit: "1a2b3c4", Date: "2026-10-27T17:40:00Z"}
	if code := cli.Run(context.Background(), []string{"version"}, &out, &bytes.Buffer{}, info); code != 0 {
		t.Fatalf("exit = %d, want 0", code)
	}
	for _, want := range []string{"spmeta 0.1.0 (", "commit 1a2b3c4", "built 2026-10-27T17:40:00Z"} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("stdout = %q, missing %q", out.String(), want)
		}
	}
}

func TestRunHelp(t *testing.T) {
	for _, args := range [][]string{{"help"}, {"-h"}, {"--help"}} {
		t.Run(strings.Join(args, " "), func(t *testing.T) {
			code, stdout, stderr := run(t, args...)
			if code != 0 {
				t.Fatalf("exit = %d, want 0 (stderr %q)", code, stderr)
			}
			if !strings.Contains(stdout, "Usage:") || !strings.Contains(stdout, "spmeta version") {
				t.Errorf("stdout = %q, want usage text", stdout)
			}
			if stderr != "" {
				t.Errorf("stderr = %q, want empty", stderr)
			}
		})
	}
}

func TestRunUnknownFlag(t *testing.T) {
	code, stdout, stderr := run(t, "--bogus")
	if code != 2 {
		t.Fatalf("exit = %d, want 2", code)
	}
	if stdout != "" {
		t.Errorf("stdout = %q, want empty", stdout)
	}
	if !strings.Contains(stderr, "--bogus") || !strings.Contains(stderr, "Run 'spmeta --help' for usage.") {
		t.Errorf("stderr = %q, want problem and usage hint", stderr)
	}
}

func TestRunUnknownCommand(t *testing.T) {
	code, stdout, stderr := run(t, "bogus")
	if code != 2 {
		t.Fatalf("exit = %d, want 2", code)
	}
	if stdout != "" {
		t.Errorf("stdout = %q, want empty", stdout)
	}
	if !strings.Contains(stderr, "bogus") || !strings.Contains(stderr, "Run 'spmeta --help' for usage.") {
		t.Errorf("stderr = %q, want problem and usage hint", stderr)
	}
}

func TestRunHelpUnknownTopic(t *testing.T) {
	code, _, stderr := run(t, "help", "bogus")
	if code != 2 {
		t.Fatalf("exit = %d, want 2", code)
	}
	if !strings.Contains(stderr, "bogus") {
		t.Errorf("stderr = %q, want topic named", stderr)
	}
}

func TestRunRootWebWithoutBackend(t *testing.T) {
	code, stdout, stderr := run(t, "--source", "web")
	if code != 4 {
		t.Fatalf("exit = %d, want 4", code)
	}
	if stdout != "" {
		t.Errorf("stdout = %q, want empty", stdout)
	}
	if want := "spmeta: Web mode is not available in this version\n"; stderr != want {
		t.Errorf("stderr = %q, want %q", stderr, want)
	}
}
