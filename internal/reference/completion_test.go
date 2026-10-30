package reference_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/bkuberek/spmeta/internal/reference"
)

func script(t *testing.T, shell string) string {
	t.Helper()
	text, err := reference.Completion(shell)
	if err != nil {
		t.Fatalf("Completion(%q): %v", shell, err)
	}
	return text
}

// writeScript saves the completion script for shell and returns its path.
func writeScript(t *testing.T, shell string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "spmeta."+shell)
	if err := os.WriteFile(path, []byte(script(t, shell)), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

// lookShell returns the path of shell, skipping the test when it is not
// installed.
func lookShell(t *testing.T, shell string) string {
	t.Helper()
	path, err := exec.LookPath(shell)
	if err != nil {
		t.Skipf("%s is not installed", shell)
	}
	return path
}

func TestShells(t *testing.T) {
	if got, want := reference.Shells(), []string{"bash", "zsh", "fish"}; !slices.Equal(got, want) {
		t.Errorf("Shells() = %q, want %q", got, want)
	}
}

func TestCompletionNamesEveryCommandFlagAndValue(t *testing.T) {
	for _, shell := range reference.Shells() {
		t.Run(shell, func(t *testing.T) {
			text := script(t, shell)
			for _, c := range reference.Commands() {
				if !strings.Contains(text, c.Name) {
					t.Errorf("script lacks command %q", c.Name)
				}
				for _, f := range c.Flags {
					if !strings.Contains(text, f.Name) {
						t.Errorf("script lacks flag %s --%s", c.Path, f.Name)
					}
					for _, v := range f.Values {
						if !strings.Contains(text, v) {
							t.Errorf("script lacks value %q of %s --%s", v, c.Path, f.Name)
						}
					}
				}
				for _, a := range c.Args {
					if !strings.Contains(text, a) {
						t.Errorf("script lacks argument %q of %s", a, c.Path)
					}
				}
			}
		})
	}
}

func TestCompletionUnknownShell(t *testing.T) {
	_, err := reference.Completion("tcsh")
	if err == nil || !strings.Contains(err.Error(), `"tcsh"`) || !strings.Contains(err.Error(), "bash, zsh or fish") {
		t.Errorf("Completion(tcsh) error = %v, want one naming the shell and the supported ones", err)
	}
}

func TestCompletionScriptsParse(t *testing.T) {
	check := map[string][]string{
		"bash": {"-n"},
		"zsh":  {"-n"},
		"fish": {"--no-execute"},
	}
	for _, shell := range reference.Shells() {
		t.Run(shell, func(t *testing.T) {
			bin := lookShell(t, shell)
			args := append(slices.Clone(check[shell]), writeScript(t, shell))
			if out, err := exec.Command(bin, args...).CombinedOutput(); err != nil {
				t.Errorf("%s %s: %v\n%s", shell, strings.Join(args, " "), err, out)
			}
		})
	}
}

// completionCase is a command line, split into words with the cursor at the
// end of the last word, and the candidates a shell should offer.
type completionCase struct {
	name  string
	words []string
	want  []string
}

// completionCases hold for every shell.
var completionCases = []completionCase{
	{"commands", []string{"spmeta", ""}, []string{"auth", "doctor", "completion", "version", "help"}},
	{"root flags by prefix", []string{"spmeta", "--fo"}, []string{"--format"}},
	{"format values", []string{"spmeta", "--format", ""}, []string{"text", "json", "template", "waybar"}},
	{"source values by prefix", []string{"spmeta", "--watch", "--source", "l"}, []string{"local"}},
	{"no command after a root flag", []string{"spmeta", "--watch", ""}, nil},
	{"no candidates for a free value", []string{"spmeta", "--template", ""}, nil},
	{"auth commands", []string{"spmeta", "auth", ""}, []string{"login", "logout", "status"}},
	{"login flags", []string{"spmeta", "auth", "login", "--"}, []string{"--client-id", "--port", "--no-browser", "--debug", "--config", "--help"}},
	{"status flags", []string{"spmeta", "auth", "status", "--"}, []string{"--config", "--help"}},
	{"doctor flags", []string{"spmeta", "doctor", "--"}, []string{"--debug", "--config", "--help"}},
	{"shells", []string{"spmeta", "completion", ""}, []string{"bash", "zsh", "fish"}},
	{"help topics", []string{"spmeta", "help", ""}, []string{"auth", "doctor", "completion"}},
	{"nothing after a help topic", []string{"spmeta", "help", "auth", ""}, nil},
}

func TestBashCompletionCandidates(t *testing.T) {
	bash := lookShell(t, "bash")
	path := writeScript(t, "bash")
	// Bash splits --flag=value into three words.
	cases := append(slices.Clone(completionCases),
		completionCase{"value after an equals sign", []string{"spmeta", "--format", "=", "w"}, []string{"waybar"}},
		completionCase{"config files", []string{"spmeta", "doctor", "--config", "set"}, []string{"settings.toml"}},
	)
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "settings.toml"), nil, 0o600); err != nil {
		t.Fatal(err)
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			quoted := make([]string, len(tc.words))
			for i, w := range tc.words {
				quoted[i] = "'" + w + "'"
			}
			driver := `source "$1"; COMP_WORDS=(` + strings.Join(quoted, " ") + `); COMP_CWORD=` + strconv.Itoa(len(tc.words)-1) +
				`; _spmeta; printf '%s\n' "${COMPREPLY[@]}"`
			cmd := exec.Command(bash, "--noprofile", "--norc", "-c", driver, "bash", path)
			cmd.Dir = dir
			out, err := cmd.CombinedOutput()
			if err != nil {
				t.Fatalf("bash: %v\n%s", err, out)
			}
			assertCandidates(t, string(out), tc.want)
		})
	}
}

func TestFishCompletionCandidates(t *testing.T) {
	fish := lookShell(t, "fish")
	path := writeScript(t, "fish")
	for _, tc := range completionCases {
		t.Run(tc.name, func(t *testing.T) {
			line := strings.Join(tc.words, " ")
			out, err := exec.Command(fish, "--no-config", "-c", `source $argv[1]; complete --do-complete=$argv[2]`, path, line).CombinedOutput()
			if err != nil {
				t.Fatalf("fish: %v\n%s", err, out)
			}
			// fish prints "candidate<TAB>description" and completes a long
			// flag as the whole word.
			var got []string
			for l := range strings.Lines(string(out)) {
				got = append(got, strings.SplitN(strings.TrimSuffix(l, "\n"), "\t", 2)[0])
			}
			assertCandidates(t, strings.Join(got, "\n"), tc.want)
		})
	}
}

func assertCandidates(t *testing.T, out string, want []string) {
	t.Helper()
	got := strings.Fields(out)
	slices.Sort(got)
	want = slices.Clone(want)
	slices.Sort(want)
	if !slices.Equal(got, want) {
		t.Errorf("candidates = %q, want %q", got, want)
	}
}
