package reference_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/bkuberek/spmeta/internal/reference"
)

var manInfo = reference.ManInfo{Version: "1.2.3", Date: "2026-10-30"}

func TestManPageHeader(t *testing.T) {
	page := reference.ManPage(manInfo)
	if want := `.TH SPMETA 1 2026-10-30 "spmeta 1.2.3" "User Commands"` + "\n"; !strings.Contains(page, want) {
		t.Errorf("man page lacks header %q:\n%s", want, page)
	}
	if !strings.Contains(page, ".SH NAME\nspmeta \\- print the track Spotify is currently playing\n") {
		t.Errorf("man page lacks the NAME line:\n%s", page)
	}
}

func TestManPageSections(t *testing.T) {
	page := reference.ManPage(manInfo)
	last := -1
	for _, section := range []string{"NAME", "SYNOPSIS", "DESCRIPTION", "COMMANDS", "OPTIONS", `"EXIT STATUS"`, "ENVIRONMENT", "FILES", "EXAMPLES", `"SEE ALSO"`} {
		i := strings.Index(page, "\n.SH "+section+"\n")
		if i < 0 {
			t.Errorf("man page lacks section %s", section)
			continue
		}
		if i < last {
			t.Errorf("section %s is out of order", section)
		}
		last = i
	}
}

func TestManPageDocumentsEveryCommandFlagAndExitCode(t *testing.T) {
	page := reference.ManPage(manInfo)
	for _, c := range reference.Commands() {
		if !strings.Contains(page, roffDashes(c.Path)) {
			t.Errorf("man page lacks command %q", c.Path)
		}
		for _, f := range c.Flags {
			if !strings.Contains(page, `\fB\-\-`+roffDashes(f.Name)+`\fR`) {
				t.Errorf("man page lacks flag %s --%s", c.Path, f.Name)
			}
			if f.Env != "" && !strings.Contains(page, ".B "+f.Env+"\n") {
				t.Errorf("ENVIRONMENT lacks %s", f.Env)
			}
		}
	}
	for _, e := range reference.ExitCodes() {
		if !strings.Contains(page, ".B "+strconv.Itoa(e.Code)+"\n") {
			t.Errorf("EXIT STATUS lacks code %d", e.Code)
		}
	}
}

func TestManPageIsASCII(t *testing.T) {
	for i, r := range reference.ManPage(manInfo) {
		if r > 0x7e || (r < 0x20 && r != '\n') {
			t.Fatalf("man page has character %q at byte %d; man pages must be plain ASCII", r, i)
		}
	}
}

func TestManPageWithoutVersion(t *testing.T) {
	page := reference.ManPage(reference.ManInfo{Date: "2026-10-30"})
	if want := `.TH SPMETA 1 2026-10-30 "spmeta" "User Commands"`; !strings.Contains(page, want) {
		t.Errorf("man page lacks header %q", want)
	}
}

func TestManPageLint(t *testing.T) {
	mandoc, err := exec.LookPath("mandoc")
	if err != nil {
		t.Skip("mandoc is not installed")
	}
	path := filepath.Join(t.TempDir(), "spmeta.1")
	if err := os.WriteFile(path, []byte(reference.ManPage(manInfo)), 0o600); err != nil {
		t.Fatal(err)
	}
	out, err := exec.Command(mandoc, "-T", "lint", "-W", "all", path).CombinedOutput()
	if err != nil || len(out) > 0 {
		t.Errorf("mandoc -T lint: %v\n%s", err, out)
	}
}

func TestManPageRendersWithGroff(t *testing.T) {
	groff, err := exec.LookPath("groff")
	if err != nil {
		t.Skip("groff is not installed")
	}
	cmd := exec.Command(groff, "-man", "-Tutf8", "-ww", "-z")
	cmd.Stdin = strings.NewReader(reference.ManPage(manInfo))
	if out, err := cmd.CombinedOutput(); err != nil || len(out) > 0 {
		t.Errorf("groff: %v\n%s", err, out)
	}
}

// roffDashes escapes hyphens the way the man page writes them.
func roffDashes(s string) string {
	return strings.ReplaceAll(s, "-", `\-`)
}
