package main

import (
	"bytes"
	"compress/gzip"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/bkuberek/spmeta/internal/reference"
)

func TestRunWritesCompletionsAndManPage(t *testing.T) {
	dir := t.TempDir()
	var stderr bytes.Buffer
	if code := run([]string{"-version", "1.2.3", "-date", "2026-10-30T17:40:00Z", dir}, &stderr); code != 0 {
		t.Fatalf("exit %d: %s", code, stderr.String())
	}
	for shell, name := range map[string]string{"bash": "spmeta.bash", "zsh": "spmeta.zsh", "fish": "spmeta.fish"} {
		want, err := reference.Completion(shell)
		if err != nil {
			t.Fatal(err)
		}
		if got := readFile(t, filepath.Join(dir, "completions", name)); got != want {
			t.Errorf("completions/%s differs from the %s completion script", name, shell)
		}
	}
	page := readFile(t, filepath.Join(dir, "manpages", "spmeta.1"))
	if want := reference.ManPage(reference.ManInfo{Version: "1.2.3", Date: "2026-10-30"}); page != want {
		t.Errorf("manpages/spmeta.1 differs from the man page for version 1.2.3 of 2026-10-30")
	}
	if got := gunzip(t, filepath.Join(dir, "manpages", "spmeta.1.gz")); got != page {
		t.Errorf("manpages/spmeta.1.gz does not hold manpages/spmeta.1")
	}
}

func TestRunIsReproducible(t *testing.T) {
	args := func(dir string) []string { return []string{"-version", "1.2.3", "-date", "2026-10-30", dir} }
	first, second := t.TempDir(), t.TempDir()
	for _, dir := range []string{first, second} {
		if code := run(args(dir), io.Discard); code != 0 {
			t.Fatalf("exit %d", code)
		}
	}
	gz := filepath.Join("manpages", "spmeta.1.gz")
	if readFile(t, filepath.Join(first, gz)) != readFile(t, filepath.Join(second, gz)) {
		t.Error("two runs wrote different compressed man pages")
	}
}

func TestRunUsageErrors(t *testing.T) {
	for _, tc := range []struct {
		name string
		args []string
		want string
	}{
		{"no directory", nil, "usage: docgen"},
		{"two directories", []string{"a", "b"}, "usage: docgen"},
		{"bad date", []string{"-date", "yesterday", "out"}, `invalid -date "yesterday"`},
		{"unknown flag", []string{"-bogus", "out"}, "bogus"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var stderr bytes.Buffer
			if code := run(tc.args, &stderr); code != 2 || !strings.Contains(stderr.String(), tc.want) {
				t.Errorf("exit %d, stderr %q; want 2 and %q", code, stderr.String(), tc.want)
			}
		})
	}
}

func readFile(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func gunzip(t *testing.T, path string) string {
	t.Helper()
	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = f.Close() }()
	r, err := gzip.NewReader(f)
	if err != nil {
		t.Fatal(err)
	}
	b, err := io.ReadAll(r)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}
