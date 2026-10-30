package reference_test

import (
	"strings"
	"testing"

	"github.com/bkuberek/spmeta/internal/reference"
)

func help(t *testing.T, topic string) string {
	t.Helper()
	text, ok := reference.Help(topic)
	if !ok {
		t.Fatalf("Help(%q) found no help page", topic)
	}
	return text
}

func TestRootHelpListsEveryRootFlag(t *testing.T) {
	text := help(t, "")
	for _, f := range reference.Root().Flags {
		if !strings.Contains(text, "--"+f.Name) {
			t.Errorf("root help lacks --%s:\n%s", f.Name, text)
		}
	}
	for _, want := range []string{
		"      --watch ",
		"      --interval SECONDS ",
		"      --format text|json|template|waybar  Output format (default text)\n",
		"  -h, --help ",
		"Environment: SPMETA_SOURCE, SPMETA_FORMAT, SPMETA_TEMPLATE, SPMETA_PLAYER, SPMETA_CONFIG.\n",
	} {
		if !strings.Contains(text, want) {
			t.Errorf("root help lacks %q:\n%s", want, text)
		}
	}
}

func TestRootHelpListsEveryCommand(t *testing.T) {
	text := help(t, "spmeta")
	for _, want := range []string{
		"  spmeta [flags]\n",
		"  spmeta auth login|logout|status\n",
		"  spmeta doctor [--debug] [--config PATH]\n",
		"  spmeta completion bash|zsh|fish\n",
		"  spmeta version\n",
		"  spmeta help [auth|doctor|completion]\n",
	} {
		if !strings.Contains(text, want) {
			t.Errorf("root help lacks usage line %q:\n%s", want, text)
		}
	}
	for _, c := range reference.Root().Commands {
		if !strings.Contains(text, "\n  "+c.Name+" ") {
			t.Errorf("root help does not list command %q:\n%s", c.Name, text)
		}
	}
}

func TestAuthHelpMergesTheFlagsOfItsCommands(t *testing.T) {
	text := help(t, "auth")
	for _, want := range []string{
		"  spmeta auth login [--client-id ID] [--port PORT] [--no-browser] [--debug] [--config PATH]\n",
		"  spmeta auth logout [--config PATH]\n",
		"  spmeta auth status [--config PATH]\n",
		"      --port PORT     Loopback port for the browser callback; 0 picks any free port (login only; default 8974)\n",
		"      --no-browser    Print the login URL instead of opening a browser (login only)\n",
		"      --config PATH   Read this configuration file instead of the default\n",
		"  -h, --help          Print this help\n",
	} {
		if !strings.Contains(text, want) {
			t.Errorf("auth help lacks %q:\n%s", want, text)
		}
	}
	if strings.Count(text, "--config PATH  ") != 1 {
		t.Errorf("auth help lists --config more than once:\n%s", text)
	}
}

func TestDoctorHelp(t *testing.T) {
	want := `spmeta doctor reports which sources spmeta can read and why the others cannot.

Usage:
  spmeta doctor [--debug] [--config PATH]

Flags:
      --debug        Write diagnostics to stderr, never tokens or codes
      --config PATH  Read this configuration file instead of the default
  -h, --help         Print this help

Environment: SPMETA_CONFIG.
On macOS it may wait up to 120 s while you answer the Automation prompt.
With a Web login (spmeta auth login) it makes one request to the Spotify Web API.
Exit status: 0 when at least one source is usable, 4 when none is.
`
	if got := help(t, "doctor"); got != want {
		t.Errorf("doctor help:\n%s\nwant:\n%s", got, want)
	}
}

func TestCompletionHelpShowsHowToLoadTheScript(t *testing.T) {
	text := help(t, "completion")
	for _, want := range []string{"spmeta completion bash|zsh|fish", `eval "$(spmeta completion bash)"`, "spmeta completion fish | source"} {
		if !strings.Contains(text, want) {
			t.Errorf("completion help lacks %q:\n%s", want, text)
		}
	}
}

func TestHelpWithoutAPage(t *testing.T) {
	for _, topic := range []string{"version", "help", "login", "bogus"} {
		if _, ok := reference.Help(topic); ok {
			t.Errorf("Help(%q) found a page, want none", topic)
		}
	}
}
