package cli_test

import (
	"strings"
	"testing"
)

func TestCompletionPrintsTheScript(t *testing.T) {
	for shell, marker := range map[string]string{
		"bash": "complete -F _spmeta spmeta",
		"zsh":  "#compdef spmeta",
		"fish": "complete -c spmeta",
	} {
		t.Run(shell, func(t *testing.T) {
			code, stdout, stderr := run(t, "completion", shell)
			if code != 0 || stderr != "" {
				t.Fatalf("exit %d, stderr %q; want 0 and nothing on stderr", code, stderr)
			}
			if !strings.Contains(stdout, marker) {
				t.Errorf("stdout lacks %q:\n%s", marker, stdout)
			}
		})
	}
}

func TestCompletionUsageErrors(t *testing.T) {
	for _, tc := range []struct {
		name string
		args []string
		want string
	}{
		{"no shell", []string{"completion"}, "completion needs a shell: bash, zsh or fish"},
		{"unknown shell", []string{"completion", "tcsh"}, `unsupported shell "tcsh" (want bash, zsh or fish)`},
		{"two shells", []string{"completion", "bash", "zsh"}, `unexpected argument "zsh" for completion`},
		{"unknown flag", []string{"completion", "--bogus"}, "unknown flag --bogus"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			code, stdout, stderr := run(t, tc.args...)
			if code != 2 || stdout != "" || !strings.Contains(stderr, tc.want) || !strings.Contains(stderr, usageHint) {
				t.Errorf("exit %d, stdout %q, stderr %q; want a usage error with %q", code, stdout, stderr, tc.want)
			}
		})
	}
}

func TestCompletionHelp(t *testing.T) {
	for _, args := range [][]string{{"help", "completion"}, {"completion", "-h"}, {"completion", "--help"}} {
		t.Run(strings.Join(args, " "), func(t *testing.T) {
			code, stdout, stderr := run(t, args...)
			if code != 0 || stderr != "" {
				t.Fatalf("exit %d, stderr %q; want 0 and nothing on stderr", code, stderr)
			}
			if !strings.Contains(stdout, "spmeta completion bash|zsh|fish") {
				t.Errorf("stdout lacks the completion usage:\n%s", stdout)
			}
		})
	}
}
