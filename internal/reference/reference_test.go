package reference_test

import (
	"slices"
	"strings"
	"testing"

	"github.com/bkuberek/spmeta/internal/reference"
)

func TestCommandPathsAreUniqueAndStartWithSpmeta(t *testing.T) {
	seen := map[string]bool{}
	for _, c := range reference.Commands() {
		if !strings.HasPrefix(c.Path, "spmeta") {
			t.Errorf("command path %q does not start with spmeta", c.Path)
		}
		if seen[c.Path] {
			t.Errorf("command path %q appears twice", c.Path)
		}
		seen[c.Path] = true
	}
	for _, want := range []string{"spmeta", "spmeta auth", "spmeta auth login", "spmeta auth logout", "spmeta auth status", "spmeta doctor", "spmeta completion", "spmeta version", "spmeta help"} {
		if !seen[want] {
			t.Errorf("reference lacks command %q", want)
		}
	}
}

func TestFlagsAreWellFormed(t *testing.T) {
	for _, c := range reference.Commands() {
		names := map[string]bool{}
		for _, f := range c.Flags {
			where := c.Path + " --" + f.Name
			if names[f.Name] {
				t.Errorf("%s: flag listed twice", where)
			}
			names[f.Name] = true
			if f.Help == "" || strings.HasSuffix(f.Help, ".") {
				t.Errorf("%s: help %q must be one line without a final period", where, f.Help)
			}
			if f.Value != "" && len(f.Values) > 0 {
				t.Errorf("%s: has both a placeholder and a list of values", where)
			}
			if f.File && f.Value == "" {
				t.Errorf("%s: a file flag needs a placeholder", where)
			}
			if f.Builtin && f.Name != "help" {
				t.Errorf("%s: only help is built in", where)
			}
		}
	}
}

func TestCommandsHaveSummaries(t *testing.T) {
	for _, c := range reference.Commands() {
		if c.Summary == "" || strings.HasSuffix(c.Summary, ".") {
			t.Errorf("%s: summary %q must be one line without a final period", c.Path, c.Summary)
		}
	}
}

func TestTopicsAreTheCommandsWithAHelpPage(t *testing.T) {
	want := []string{"auth", "doctor", "completion"}
	if got := reference.Topics(); !slices.Equal(got, want) {
		t.Errorf("Topics() = %q, want %q", got, want)
	}
}

func TestExitCodesCoverZeroToSeven(t *testing.T) {
	codes := reference.ExitCodes()
	if len(codes) != 8 {
		t.Fatalf("got %d exit codes, want 8", len(codes))
	}
	for i, c := range codes {
		if c.Code != i || c.Meaning == "" {
			t.Errorf("exit code %d = %+v, want code %d with a meaning", i, c, i)
		}
	}
}
