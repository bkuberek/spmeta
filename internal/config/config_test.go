package config_test

import (
	"errors"
	"io/fs"
	"math"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/bkuberek/spmeta/internal/config"
)

const (
	home        = "/home/u"
	defaultConf = "/home/u/.config/spmeta/config.toml"
	defaultTok  = "/home/u/.config/spmeta/token.json"
)

func ptr[T any](v T) *T { return &v }

func inf() float64 { return math.Inf(1) }

// testEnv builds an Env over an in-memory environment and file set. files
// maps a path to its content; reads records every path requested.
func testEnv(vars, files map[string]string) (env config.Env, reads *[]string) {
	reads = new([]string)
	return config.Env{
		Getenv: func(k string) string { return vars[k] },
		Home:   home,
		ReadFile: func(p string) ([]byte, error) {
			*reads = append(*reads, p)
			if c, ok := files[p]; ok {
				return []byte(c), nil
			}
			return nil, &fs.PathError{Op: "open", Path: p, Err: fs.ErrNotExist}
		},
	}, reads
}

func fixture(t *testing.T, name string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func load(t *testing.T, flags config.Flags, vars, files map[string]string) config.Settings {
	t.Helper()
	env, _ := testEnv(vars, files)
	s, err := config.Load(flags, env)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	return s
}

func TestLoadDefaults(t *testing.T) {
	s := load(t, config.Flags{}, nil, nil)
	want := config.Settings{
		Source:      "auto",
		Format:      "text",
		WebInterval: 3 * time.Second,
		ConfigPath:  defaultConf,
		TokenPath:   defaultTok,
	}
	if !reflect.DeepEqual(s, want) {
		t.Errorf("Load() = %+v, want %+v", s, want)
	}
}

func TestLoadPrecedence(t *testing.T) {
	const confPath = defaultConf
	tests := []struct {
		name  string
		flags config.Flags
		vars  map[string]string
		file  string
		check func(config.Settings) bool
	}{
		{"source from file", config.Flags{}, nil, `default_source = "local"`, func(s config.Settings) bool { return s.Source == "local" }},
		{"source env over file", config.Flags{}, map[string]string{"SPMETA_SOURCE": "web"}, `default_source = "local"`, func(s config.Settings) bool { return s.Source == "web" }},
		{"source flag over env", config.Flags{Source: ptr("local")}, map[string]string{"SPMETA_SOURCE": "web"}, `default_source = "auto"`, func(s config.Settings) bool { return s.Source == "local" }},

		{"format from file", config.Flags{}, nil, `default_format = "json"`, func(s config.Settings) bool { return s.Format == "json" }},
		{"waybar from file", config.Flags{}, nil, `default_format = "waybar"`, func(s config.Settings) bool { return s.Format == "waybar" }},
		{"waybar from env", config.Flags{}, map[string]string{"SPMETA_FORMAT": "waybar"}, "", func(s config.Settings) bool { return s.Format == "waybar" }},
		{"waybar from flag", config.Flags{Format: ptr("waybar")}, nil, "", func(s config.Settings) bool { return s.Format == "waybar" }},
		{"format env over file", config.Flags{}, map[string]string{"SPMETA_FORMAT": "json"}, `default_format = "text"`, func(s config.Settings) bool { return s.Format == "json" }},
		{"format flag over env", config.Flags{Format: ptr("text")}, map[string]string{"SPMETA_FORMAT": "json"}, `default_format = "json"`, func(s config.Settings) bool { return s.Format == "text" }},

		{"template from file", config.Flags{}, nil, `template = "{title}"`, func(s config.Settings) bool { return s.Template == "{title}" }},
		{"template env over file", config.Flags{}, map[string]string{"SPMETA_TEMPLATE": "{artist}"}, `template = "{title}"`, func(s config.Settings) bool { return s.Template == "{artist}" }},
		{"template flag over env", config.Flags{Template: ptr("{album}")}, map[string]string{"SPMETA_TEMPLATE": "{artist}"}, `template = "{title}"`, func(s config.Settings) bool { return s.Template == "{album}" }},

		{"player from file", config.Flags{}, nil, `player = "mpd"`, func(s config.Settings) bool { return s.Player == "mpd" }},
		{"player env over file", config.Flags{}, map[string]string{"SPMETA_PLAYER": "spotifyd"}, `player = "mpd"`, func(s config.Settings) bool { return s.Player == "spotifyd" }},
		{"player flag over env", config.Flags{Player: ptr("vlc")}, map[string]string{"SPMETA_PLAYER": "spotifyd"}, `player = "mpd"`, func(s config.Settings) bool { return s.Player == "vlc" }},

		{"client id from file", config.Flags{}, nil, `client_id = "abc"`, func(s config.Settings) bool { return s.ClientID == "abc" }},
		{"client id env over file", config.Flags{}, map[string]string{"SPMETA_CLIENT_ID": "env"}, `client_id = "abc"`, func(s config.Settings) bool { return s.ClientID == "env" }},
		{"client id flag over env", config.Flags{ClientID: ptr("flag")}, map[string]string{"SPMETA_CLIENT_ID": "env"}, `client_id = "abc"`, func(s config.Settings) bool { return s.ClientID == "flag" }},

		{"interval from file", config.Flags{}, nil, `interval = 2.5`, func(s config.Settings) bool { return s.Interval == 2500*time.Millisecond }},
		{"interval integer in file", config.Flags{}, nil, `interval = 2`, func(s config.Settings) bool { return s.Interval == 2*time.Second }},
		{"interval flag over file", config.Flags{Watch: true, Interval: ptr(0.5)}, nil, `interval = 2.5`, func(s config.Settings) bool { return s.Interval == 500*time.Millisecond }},
		{"web interval from file", config.Flags{}, nil, `web_interval = 5`, func(s config.Settings) bool { return s.WebInterval == 5*time.Second }},

		{"empty env counts as unset", config.Flags{}, map[string]string{"SPMETA_SOURCE": "", "SPMETA_PLAYER": ""}, "default_source = \"local\"\nplayer = \"mpd\"", func(s config.Settings) bool { return s.Source == "local" && s.Player == "mpd" }},
		{"all keys from fixture", config.Flags{}, nil, fixture(t, "full.toml"), func(s config.Settings) bool {
			return s.ClientID == "0123456789abcdef0123456789abcdef" && s.Source == "local" && s.Format == "template" &&
				s.Template == "{artist} - {title}" && s.Player == "spotifyd" &&
				s.Interval == 2500*time.Millisecond && s.WebInterval == 4*time.Second
		}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			s := load(t, tc.flags, tc.vars, map[string]string{confPath: tc.file})
			if !tc.check(s) {
				t.Errorf("unexpected settings: %+v", s)
			}
		})
	}
}

func TestLoadFlagsPassThrough(t *testing.T) {
	s := load(t, config.Flags{Quiet: true, Debug: true}, nil, nil)
	if !s.Quiet || !s.Debug || s.Watch {
		t.Errorf("Quiet/Debug/Watch = %v/%v/%v, want true/true/false", s.Quiet, s.Debug, s.Watch)
	}
	s = load(t, config.Flags{Watch: true}, nil, nil)
	if !s.Watch {
		t.Error("Watch = false, want true")
	}
}

func TestLoadTemplateImpliesFormat(t *testing.T) {
	jsonLayers := map[string]string{"SPMETA_FORMAT": "json"}
	const jsonFile = `default_format = "json"`
	tests := []struct {
		name       string
		flags      config.Flags
		vars       map[string]string
		file       string
		wantFormat string
	}{
		{"flag template alone", config.Flags{Template: ptr("{title}")}, nil, "", "template"},
		{"flag template beats env format", config.Flags{Template: ptr("{title}")}, jsonLayers, "", "template"},
		{"flag template beats file format", config.Flags{Template: ptr("{title}")}, nil, jsonFile, "template"},
		{"explicit format flag wins", config.Flags{Template: ptr("{title}"), Format: ptr("json")}, nil, "", "json"},
		{"env template does not imply", config.Flags{}, map[string]string{"SPMETA_TEMPLATE": "{title}"}, "", "text"},
		{"file template does not imply", config.Flags{}, nil, `template = "{title}"`, "text"},
		{"env template keeps env format", config.Flags{}, map[string]string{"SPMETA_TEMPLATE": "{title}", "SPMETA_FORMAT": "json"}, "", "json"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			s := load(t, tc.flags, tc.vars, map[string]string{defaultConf: tc.file})
			if s.Format != tc.wantFormat {
				t.Errorf("Format = %q, want %q", s.Format, tc.wantFormat)
			}
		})
	}
}

func TestLoadTemplateOrigin(t *testing.T) {
	tests := []struct {
		name  string
		flags config.Flags
		vars  map[string]string
		file  string
		want  string
	}{
		{"none", config.Flags{}, nil, "", ""},
		{"flag", config.Flags{Template: ptr("{title}")}, map[string]string{"SPMETA_TEMPLATE": "{album}"}, "", "--template"},
		{"env", config.Flags{}, map[string]string{"SPMETA_TEMPLATE": "{album}"}, `template = "{title}"`, "SPMETA_TEMPLATE"},
		{"file", config.Flags{}, nil, `template = "{title}"`, "template in " + defaultConf},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			s := load(t, tc.flags, tc.vars, map[string]string{defaultConf: tc.file})
			if s.TemplateOrigin != tc.want {
				t.Errorf("TemplateOrigin = %q, want %q", s.TemplateOrigin, tc.want)
			}
		})
	}
}

func TestLoadInvalid(t *testing.T) {
	tests := []struct {
		name  string
		flags config.Flags
		vars  map[string]string
		files map[string]string
		want  []string
	}{
		{"flag source", config.Flags{Source: ptr("xml")}, nil, nil, []string{"--source", `"xml"`, "auto, local or web"}},
		{"flag format", config.Flags{Format: ptr("xml")}, nil, nil, []string{"--format", `"xml"`, "text, json, template or waybar"}},
		{"env source", config.Flags{}, map[string]string{"SPMETA_SOURCE": "cloud"}, nil, []string{"SPMETA_SOURCE", `"cloud"`}},
		{"env format", config.Flags{}, map[string]string{"SPMETA_FORMAT": "xml"}, nil, []string{"SPMETA_FORMAT", `"xml"`}},
		{"env invalid even when flag overrides", config.Flags{Format: ptr("json")}, map[string]string{"SPMETA_FORMAT": "xml"}, nil, []string{"SPMETA_FORMAT"}},
		{"file source", config.Flags{}, nil, map[string]string{defaultConf: `default_source = "cloud"`}, []string{defaultConf, "default_source", `"cloud"`}},
		{"file format", config.Flags{}, nil, map[string]string{defaultConf: `default_format = "xml"`}, []string{defaultConf, "default_format"}},
		{"file string key wrong type", config.Flags{}, nil, map[string]string{defaultConf: `player = 5`}, []string{defaultConf, "player", "string"}},
		{"file client id wrong type", config.Flags{}, nil, map[string]string{defaultConf: `client_id = true`}, []string{defaultConf, "client_id"}},
		{"file interval wrong type", config.Flags{}, nil, map[string]string{defaultConf: fixture(t, "wrong_types.toml")}, []string{defaultConf, "interval", "number"}},
		{"file interval below minimum", config.Flags{}, nil, map[string]string{defaultConf: `interval = 0.4`}, []string{defaultConf, "interval", "0.5"}},
		{"file web interval below minimum", config.Flags{}, nil, map[string]string{defaultConf: `web_interval = 0.9`}, []string{defaultConf, "web_interval", "1"}},
		{"file interval not finite", config.Flags{}, nil, map[string]string{defaultConf: `interval = inf`}, []string{defaultConf, "interval"}},
		{"file interval nan", config.Flags{}, nil, map[string]string{defaultConf: `interval = nan`}, []string{defaultConf, "interval"}},
		{"malformed toml", config.Flags{}, nil, map[string]string{defaultConf: fixture(t, "malformed.toml")}, []string{defaultConf, "TOML"}},
		{"explicit config path in message", config.Flags{Config: ptr("/etc/x.toml")}, nil, map[string]string{"/etc/x.toml": `default_source = "cloud"`}, []string{"/etc/x.toml", "default_source"}},
		{"template format without template", config.Flags{Format: ptr("template")}, nil, nil, []string{"--template", "SPMETA_TEMPLATE"}},
		{"template format from env without template", config.Flags{}, map[string]string{"SPMETA_FORMAT": "template"}, nil, []string{"template"}},
		{"template format with empty flag template", config.Flags{Format: ptr("template"), Template: ptr("")}, nil, nil, []string{"--template"}},
		{"interval below local minimum", config.Flags{Watch: true, Interval: ptr(0.4)}, nil, nil, []string{"--interval", "0.5"}},
		{"interval below web minimum", config.Flags{Watch: true, Interval: ptr(0.9), Source: ptr("web")}, nil, nil, []string{"--interval", "1"}},
		{"interval without watch", config.Flags{Interval: ptr(2.0)}, nil, nil, []string{"--interval", "--watch"}},
		{"interval not finite", config.Flags{Watch: true, Interval: ptr(inf())}, nil, nil, []string{"--interval"}},
		{"quiet with watch", config.Flags{Quiet: true, Watch: true}, nil, nil, []string{"--quiet", "--watch"}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			env, _ := testEnv(tc.vars, tc.files)
			_, err := config.Load(tc.flags, env)
			if err == nil {
				t.Fatal("Load succeeded, want an error")
			}
			for _, w := range tc.want {
				if !strings.Contains(err.Error(), w) {
					t.Errorf("error %q does not contain %q", err, w)
				}
			}
		})
	}
}

func TestLoadIntervalMinimumFollowsSource(t *testing.T) {
	flags := config.Flags{Watch: true, Interval: ptr(0.5)}
	for _, tc := range []struct {
		name   string
		source string
		vars   map[string]string
		ok     bool
	}{
		{"auto", "", nil, true},
		{"local", "local", nil, true},
		{"web flag", "web", nil, false},
		{"web env", "", map[string]string{"SPMETA_SOURCE": "web"}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := flags
			if tc.source != "" {
				f.Source = ptr(tc.source)
			}
			env, _ := testEnv(tc.vars, nil)
			_, err := config.Load(f, env)
			if (err == nil) != tc.ok {
				t.Errorf("Load error = %v, want ok=%v", err, tc.ok)
			}
		})
	}
}

func TestLoadUnknownKeysWarn(t *testing.T) {
	s := load(t, config.Flags{}, nil, map[string]string{defaultConf: fixture(t, "unknown_keys.toml")})
	if s.Player != "spotifyd" {
		t.Errorf("Player = %q, want known keys still applied", s.Player)
	}
	if len(s.Warnings) != 3 {
		t.Fatalf("Warnings = %q, want 3 (alpha, theme, zeta)", s.Warnings)
	}
	for i, key := range []string{"alpha", "theme", "zeta"} {
		w := s.Warnings[i]
		if !strings.Contains(w, `"`+key+`"`) || !strings.Contains(w, defaultConf) {
			t.Errorf("Warnings[%d] = %q, want key %q and path %q", i, w, key, defaultConf)
		}
	}
}

func TestLoadNoWarningsForKnownKeys(t *testing.T) {
	s := load(t, config.Flags{}, nil, map[string]string{defaultConf: fixture(t, "full.toml")})
	if len(s.Warnings) != 0 {
		t.Errorf("Warnings = %q, want none", s.Warnings)
	}
}

func TestLoadMissingDefaultFileIsFine(t *testing.T) {
	s := load(t, config.Flags{}, nil, nil)
	if s.Source != "auto" || len(s.Warnings) != 0 {
		t.Errorf("Settings = %+v, want defaults and no warnings", s)
	}
}

func TestLoadMissingExplicitFileIsAnError(t *testing.T) {
	for _, tc := range []struct {
		name   string
		flags  config.Flags
		vars   map[string]string
		origin string
	}{
		{"flag path", config.Flags{Config: ptr("/nope.toml")}, nil, "--config"},
		{"env path", config.Flags{}, map[string]string{"SPMETA_CONFIG": "/nope.toml"}, "SPMETA_CONFIG"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			env, _ := testEnv(tc.vars, nil)
			_, err := config.Load(tc.flags, env)
			if err == nil {
				t.Fatal("Load succeeded, want an error for a missing explicit config file")
			}
			for _, want := range []string{"/nope.toml", tc.origin} {
				if !strings.Contains(err.Error(), want) {
					t.Errorf("error %q does not name %q", err, want)
				}
			}
		})
	}
}

func TestLoadEmptyFileIsFine(t *testing.T) {
	s := load(t, config.Flags{}, nil, map[string]string{defaultConf: ""})
	if s.Source != "auto" {
		t.Errorf("Source = %q, want auto", s.Source)
	}
}

func TestLoadUnreadableFileNamesPath(t *testing.T) {
	env := config.Env{
		Getenv:   func(string) string { return "" },
		Home:     home,
		ReadFile: func(string) ([]byte, error) { return nil, errors.New("permission denied") },
	}
	_, err := config.Load(config.Flags{}, env)
	if err == nil || !strings.Contains(err.Error(), defaultConf) || !strings.Contains(err.Error(), "permission denied") {
		t.Errorf("Load error = %v, want path and cause", err)
	}
}

func TestLoadZeroEnvIsHermetic(t *testing.T) {
	t.Setenv("SPMETA_SOURCE", "web")
	s, err := config.Load(config.Flags{}, config.Env{})
	if err != nil {
		t.Fatal(err)
	}
	if s.Source != "auto" {
		t.Errorf("Source = %q, want auto: a zero Env must ignore the process environment", s.Source)
	}
	if s.ConfigPath != "" || s.TokenPath != "" {
		t.Errorf("paths = %q, %q, want empty without a home directory", s.ConfigPath, s.TokenPath)
	}
}

func TestLoadPaths(t *testing.T) {
	tests := []struct {
		name      string
		flags     config.Flags
		vars      map[string]string
		wantConf  string
		wantToken string
	}{
		{"home default", config.Flags{}, nil, defaultConf, defaultTok},
		{"absolute xdg", config.Flags{}, map[string]string{"XDG_CONFIG_HOME": "/x"}, "/x/spmeta/config.toml", "/x/spmeta/token.json"},
		{"relative xdg ignored", config.Flags{}, map[string]string{"XDG_CONFIG_HOME": "rel/dir"}, defaultConf, defaultTok},
		{"empty xdg ignored", config.Flags{}, map[string]string{"XDG_CONFIG_HOME": ""}, defaultConf, defaultTok},
		{"env config path", config.Flags{}, map[string]string{"SPMETA_CONFIG": "/e/c.toml", "XDG_CONFIG_HOME": "/x"}, "/e/c.toml", "/x/spmeta/token.json"},
		{"flag over env path", config.Flags{Config: ptr("/f/c.toml")}, map[string]string{"SPMETA_CONFIG": "/e/c.toml"}, "/f/c.toml", defaultTok},
		{"relative flag path kept", config.Flags{Config: ptr("c.toml")}, nil, "c.toml", defaultTok},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			// The chosen file exists: a missing explicit path is an error,
			// covered by TestLoadMissingExplicitFileIsAnError.
			env, reads := testEnv(tc.vars, map[string]string{tc.wantConf: ""})
			s, err := config.Load(tc.flags, env)
			if err != nil {
				t.Fatal(err)
			}
			if s.ConfigPath != tc.wantConf || s.TokenPath != tc.wantToken {
				t.Errorf("ConfigPath, TokenPath = %q, %q; want %q, %q", s.ConfigPath, s.TokenPath, tc.wantConf, tc.wantToken)
			}
			if !reflect.DeepEqual(*reads, []string{tc.wantConf}) {
				t.Errorf("read %q, want only %q", *reads, tc.wantConf)
			}
		})
	}
}

func TestLoadTokenPathWithoutHome(t *testing.T) {
	env := config.Env{Getenv: func(k string) string {
		return map[string]string{"XDG_CONFIG_HOME": "/x"}[k]
	}}
	s, err := config.Load(config.Flags{}, env)
	if err != nil {
		t.Fatal(err)
	}
	if s.TokenPath != "/x/spmeta/token.json" {
		t.Errorf("TokenPath = %q, want it derived from XDG_CONFIG_HOME alone", s.TokenPath)
	}
}

func TestOSEnv(t *testing.T) {
	t.Setenv("SPMETA_PLAYER", "from-process")
	env := config.OSEnv()
	if got := env.Getenv("SPMETA_PLAYER"); got != "from-process" {
		t.Errorf("Getenv = %q, want the process environment", got)
	}
	if env.ReadFile == nil {
		t.Error("ReadFile is nil")
	}
}
