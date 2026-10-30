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
