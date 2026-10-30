package cli_test

import (
	"bytes"
	"context"
	"io/fs"
	"strings"
	"testing"

	"github.com/bkuberek/spmeta/internal/backend"
	"github.com/bkuberek/spmeta/internal/cli"
	"github.com/bkuberek/spmeta/internal/config"
)

const (
	testHome    = "/home/u"
	defaultConf = "/home/u/.config/spmeta/config.toml"
	playingText = "▶ Amazone — Alvan · Home (2:00 / 3:58)\n"
)

// runConfigured runs the CLI over an in-memory environment and file set.
func runConfigured(t *testing.T, b backend.Backend, vars, files map[string]string, args ...string) (code int, stdout, stderr string) {
	t.Helper()
	env := config.Env{
		Getenv: func(k string) string { return vars[k] },
		Home:   testHome,
		ReadFile: func(p string) ([]byte, error) {
			if c, ok := files[p]; ok {
				return []byte(c), nil
			}
			return nil, &fs.PathError{Op: "open", Path: p, Err: fs.ErrNotExist}
		},
	}
	var out, errOut bytes.Buffer
	code = cli.Run(context.Background(), args, &out, &errOut, cli.BuildInfo{Version: "dev"}, cli.WithBackend(b), cli.WithEnv(env))
	return code, out.String(), errOut.String()
}

func TestConfigLayersDriveOutput(t *testing.T) {
	jsonFile := map[string]string{defaultConf: `default_format = "json"`}
	tests := []struct {
		name  string
		vars  map[string]string
		files map[string]string
		args  []string
		want  func(stdout string) bool
	}{
		{"file format", nil, jsonFile, nil, func(s string) bool { return strings.HasPrefix(s, "{") }},
		{"env over file", map[string]string{"SPMETA_FORMAT": "text"}, jsonFile, nil, func(s string) bool { return s == playingText }},
		{"flag over env", map[string]string{"SPMETA_FORMAT": "json"}, nil, []string{"--format", "text"}, func(s string) bool { return s == playingText }},
		{"env template with env format", map[string]string{"SPMETA_FORMAT": "template", "SPMETA_TEMPLATE": "{title}"}, nil, nil, func(s string) bool { return s == "Amazone\n" }},
		{"file template with file format", nil, map[string]string{defaultConf: "default_format = \"template\"\ntemplate = \"{artist}\""}, nil, func(s string) bool { return s == "Alvan\n" }},
		{"template flag beats json env", map[string]string{"SPMETA_FORMAT": "json"}, nil, []string{"--template", "{album}"}, func(s string) bool { return s == "Home\n" }},
		{"env template alone stays text", map[string]string{"SPMETA_TEMPLATE": "{title}"}, nil, nil, func(s string) bool { return s == playingText }},
		{"env waybar", map[string]string{"SPMETA_FORMAT": "waybar"}, nil, nil, func(s string) bool { return strings.HasPrefix(s, `{"text":"▶ Amazone`) }},
		{"file waybar", nil, map[string]string{defaultConf: `default_format = "waybar"`}, nil, func(s string) bool { return strings.HasPrefix(s, `{"text":"▶ Amazone`) }},
		{"config flag path", nil, map[string]string{"/etc/alt.toml": `default_format = "json"`}, []string{"--config", "/etc/alt.toml"}, func(s string) bool { return strings.HasPrefix(s, "{") }},
		{"config env path", map[string]string{"SPMETA_CONFIG": "/etc/alt.toml"}, map[string]string{"/etc/alt.toml": `default_format = "json"`, defaultConf: `default_format = "text"`}, nil, func(s string) bool { return strings.HasPrefix(s, "{") }},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			code, stdout, stderr := runConfigured(t, &fakeBackend{np: playing()}, tc.vars, tc.files, tc.args...)
			if code != 0 || stderr != "" {
				t.Fatalf("exit=%d stderr=%q, want 0 and empty", code, stderr)
			}
			if !tc.want(stdout) {
				t.Errorf("stdout = %q", stdout)
			}
		})
	}
}

func TestConfigErrorsAreUsageErrorsNamingTheSource(t *testing.T) {
	tests := []struct {
		name  string
		vars  map[string]string
		files map[string]string
		args  []string
		want  []string
	}{
		{"env format", map[string]string{"SPMETA_FORMAT": "xml"}, nil, nil, []string{"SPMETA_FORMAT", `"xml"`}},
		{"env source", map[string]string{"SPMETA_SOURCE": "cloud"}, nil, nil, []string{"SPMETA_SOURCE", `"cloud"`}},
		{"file value", nil, map[string]string{defaultConf: `default_source = "cloud"`}, nil, []string{defaultConf, "default_source"}},
		{"file not toml", nil, map[string]string{defaultConf: "= nope"}, nil, []string{defaultConf, "TOML"}},
		{"flag source", nil, nil, []string{"--source", "cloud"}, []string{"--source", `"cloud"`}},
		{"env template unknown field", map[string]string{"SPMETA_TEMPLATE": "{nope}", "SPMETA_FORMAT": "template"}, nil, nil, []string{"SPMETA_TEMPLATE", `unknown field "nope"`}},
		{"file template unknown field", nil, map[string]string{defaultConf: "default_format = \"template\"\ntemplate = \"{nope}\""}, nil, []string{"template in " + defaultConf, `unknown field "nope"`}},
		{"format template without template", map[string]string{"SPMETA_FORMAT": "template"}, nil, nil, []string{"--template"}},
		{"interval without watch", nil, nil, []string{"--interval", "2"}, []string{"--interval", "--watch"}},
		{"interval too small", nil, nil, []string{"--watch", "--interval", "0.1"}, []string{"--interval", "0.5"}},
		{"quiet with watch", nil, nil, []string{"--quiet", "--watch"}, []string{"--quiet", "--watch"}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			fake := &fakeBackend{np: playing()}
			code, stdout, stderr := runConfigured(t, fake, tc.vars, tc.files, tc.args...)
			if code != 2 || stdout != "" || fake.calls != 0 {
				t.Errorf("exit=%d stdout=%q reads=%d, want 2, empty, 0", code, stdout, fake.calls)
			}
			if !strings.HasSuffix(stderr, usageHint+"\n") {
				t.Errorf("stderr = %q, want usage hint", stderr)
			}
			for _, w := range tc.want {
				if !strings.Contains(stderr, w) {
					t.Errorf("stderr = %q, want %q", stderr, w)
				}
			}
		})
	}
}

func TestConfigErrorsSurviveQuiet(t *testing.T) {
	code, _, stderr := runConfigured(t, &fakeBackend{}, map[string]string{"SPMETA_FORMAT": "xml"}, nil, "--quiet")
	if code != 2 || !strings.Contains(stderr, "SPMETA_FORMAT") {
		t.Errorf("exit=%d stderr=%q, want usage error naming SPMETA_FORMAT", code, stderr)
	}
}

func TestUnknownConfigKeysWarn(t *testing.T) {
	files := map[string]string{defaultConf: "zeta = 1\nplayer = \"mpd\""}
	code, stdout, stderr := runConfigured(t, &fakeBackend{np: playing()}, nil, files)
	if code != 0 || stdout != playingText {
		t.Fatalf("exit=%d stdout=%q, want a normal read", code, stdout)
	}
	if !strings.Contains(stderr, `"zeta"`) || !strings.Contains(stderr, defaultConf) || strings.Count(stderr, "\n") != 1 {
		t.Errorf("stderr = %q, want exactly one warning naming the key and file", stderr)
	}
}

func TestUnknownConfigKeysWarningSilencedByQuiet(t *testing.T) {
	files := map[string]string{defaultConf: "zeta = 1"}
	code, stdout, stderr := runConfigured(t, &fakeBackend{np: playing()}, nil, files, "--quiet")
	if code != 0 || stdout != "" || stderr != "" {
		t.Errorf("exit=%d stdout=%q stderr=%q, want 0 and nothing", code, stdout, stderr)
	}
}

func TestMissingDefaultConfigFileIsSilent(t *testing.T) {
	code, stdout, stderr := runConfigured(t, &fakeBackend{np: playing()}, nil, nil)
	if code != 0 || stdout != playingText || stderr != "" {
		t.Errorf("exit=%d stdout=%q stderr=%q, want a normal silent read", code, stdout, stderr)
	}
}

func TestMissingExplicitConfigFileIsUsageError(t *testing.T) {
	fake := &fakeBackend{np: playing()}
	code, stdout, stderr := runConfigured(t, fake, nil, nil, "--config", "/nope.toml")
	if code != 2 || stdout != "" || !strings.Contains(stderr, "/nope.toml") {
		t.Errorf("exit=%d stdout=%q stderr=%q, want 2 naming the path", code, stdout, stderr)
	}
	if fake.calls != 0 {
		t.Errorf("backend read %d times, want 0", fake.calls)
	}
}

func TestRootWebWithoutLoginStillExits4WithSettings(t *testing.T) {
	fake := &fakeBackend{np: playing()}
	code, _, stderr := runConfigured(t, fake, nil, nil, "--source", "web", "--player", "mpd")
	if code != 4 || stderr != notLoggedIn {
		t.Errorf("exit=%d stderr=%q, want 4", code, stderr)
	}
	if fake.calls != 0 {
		t.Errorf("local read %d times, want 0", fake.calls)
	}
}

func TestHelpListsConfigFlags(t *testing.T) {
	_, stdout, _ := run(t, "--help")
	for _, flag := range []string{"--source", "--player", "--config"} {
		if !strings.Contains(stdout, flag) {
			t.Errorf("help does not list %s:\n%s", flag, stdout)
		}
	}
}
