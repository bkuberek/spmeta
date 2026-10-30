package cli_test

import (
	"strings"
	"testing"
)

func TestHelpListsEveryFormat(t *testing.T) {
	code, stdout, _ := run(t, "--help")
	if code != 0 {
		t.Fatalf("exit = %d, want 0", code)
	}
	if want := "--format text|json|template|waybar"; !strings.Contains(stdout, want) {
		t.Errorf("help is missing %q:\n%s", want, stdout)
	}
}

func TestHelpListsWatchFlags(t *testing.T) {
	_, stdout, _ := run(t, "--help")
	for _, want := range []string{"--watch", "--interval SECONDS"} {
		if !strings.Contains(stdout, want) {
			t.Errorf("help is missing %q:\n%s", want, stdout)
		}
	}
}

func TestHelpListsCompletion(t *testing.T) {
	_, stdout, _ := run(t, "help")
	if want := "spmeta completion bash|zsh|fish"; !strings.Contains(stdout, want) {
		t.Errorf("help is missing %q:\n%s", want, stdout)
	}
}
