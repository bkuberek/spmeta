package cli_test

import (
	"bytes"
	"context"
	"errors"
	"io/fs"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/bkuberek/spmeta/internal/backend"
	"github.com/bkuberek/spmeta/internal/backend/local"
	"github.com/bkuberek/spmeta/internal/backend/macos"
	"github.com/bkuberek/spmeta/internal/backend/mpris"
	"github.com/bkuberek/spmeta/internal/cli"
	"github.com/bkuberek/spmeta/internal/config"
)

// macFake stands in for the macOS backend: it can be read and can check the
// Automation permission.
type macFake struct {
	fakeBackend
	result  macos.PermissionResult
	prompts bool
	checks  int
}

func (m *macFake) CheckPermission(_ context.Context, beforePrompt func()) macos.PermissionResult {
	m.checks++
	if m.prompts && beforePrompt != nil {
		beforePrompt()
	}
	return m.result
}

// linuxFake stands in for the MPRIS backend.
type linuxFake struct {
	fakeBackend
	players []mpris.PlayerInfo
}

func (l *linuxFake) Players(context.Context) ([]mpris.PlayerInfo, error) { return l.players, nil }

func doctorEnv(files map[string]string, vars map[string]string) config.Env {
	return config.Env{
		Home:   "/home/u",
		Getenv: func(k string) string { return vars[k] },
		ReadFile: func(p string) ([]byte, error) {
			if s, ok := files[p]; ok {
				return []byte(s), nil
			}
			return nil, fs.ErrNotExist
		},
	}
}

func runDoctor(t *testing.T, b backend.Backend, env config.Env, args ...string) (code int, stdout, stderr string) {
	t.Helper()
	var out, errOut bytes.Buffer
	code = cli.Run(context.Background(), append([]string{"doctor"}, args...), &out, &errOut,
		cli.BuildInfo{Version: "0.1.0"}, cli.WithBackend(b), cli.WithEnv(env))
	return code, out.String(), errOut.String()
}

func grantedMac() *macFake {
	return &macFake{result: macos.PermissionResult{State: macos.PermissionGranted}}
}

func TestDoctorMacOSReport(t *testing.T) {
	mac := grantedMac()
	code, stdout, stderr := runDoctor(t, mac, doctorEnv(nil, nil))
	if code != 0 {
		t.Fatalf("exit = %d, want 0 (stderr %q)", code, stderr)
	}
	lines := strings.Split(strings.TrimSuffix(stdout, "\n"), "\n")
	want := []string{
		"config  ok: /home/u/.config/spmeta/config.toml not found, using defaults",
		"macos   ok: Spotify is running; Automation permission granted",
		"web     not configured: run 'spmeta auth login' to see playback on other devices (optional)",
	}
	if !strings.HasPrefix(lines[0], "spmeta 0.1.0 (") {
		t.Errorf("header = %q", lines[0])
	}
	if got := lines[1:]; strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Errorf("report:\n%s\nwant:\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
	if stderr != "" {
		t.Errorf("stderr = %q, want empty", stderr)
	}
	if mac.checks != 1 || mac.calls != 0 {
		t.Errorf("checks = %d, reads = %d, want 1 and 0: doctor never reads the track", mac.checks, mac.calls)
	}
}

func TestDoctorLinuxReport(t *testing.T) {
	b := &linuxFake{players: []mpris.PlayerInfo{{Name: "spotify", Status: "Playing", Selected: true}}}
	code, stdout, stderr := runDoctor(t, b, doctorEnv(nil, nil))
	if code != 0 {
		t.Fatalf("exit = %d, want 0 (stderr %q)", code, stderr)
	}
	for _, want := range []string{"mpris   ok: reading spotify\n", "        spotify (Playing, selected)\n"} {
		if !strings.Contains(stdout, want) {
			t.Errorf("stdout lacks %q:\n%s", want, stdout)
		}
	}
}

func TestDoctorExitCodes(t *testing.T) {
	tests := []struct {
		name       string
		b          backend.Backend
		wantCode   int
		wantStderr string
	}{
		{"usable source", grantedMac(), 0, ""},
		{
			"permission denied",
			&macFake{result: macos.PermissionResult{State: macos.PermissionDenied}},
			4, "spmeta: no source is available\nhint: Fix the failing check above, then run `spmeta doctor` again.\n",
		},
		{
			"spotify not running",
			&macFake{result: macos.PermissionResult{State: macos.PermissionSpotifyNotRunning}},
			4, "spmeta: no source is available\nhint: Fix the failing check above, then run `spmeta doctor` again.\n",
		},
		{
			"backend without diagnostics",
			&fakeBackend{},
			4, "spmeta: no source is available\nhint: Fix the failing check above, then run `spmeta doctor` again.\n",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			code, stdout, stderr := runDoctor(t, tc.b, doctorEnv(nil, nil))
			if code != tc.wantCode {
				t.Errorf("exit = %d, want %d", code, tc.wantCode)
			}
			if stderr != tc.wantStderr {
				t.Errorf("stderr = %q, want %q", stderr, tc.wantStderr)
			}
			if stdout == "" {
				t.Error("doctor printed no report")
			}
		})
	}
}

func TestDoctorUnsupportedPlatformLine(t *testing.T) {
	_, stdout, _ := runDoctor(t, &fakeBackend{}, doctorEnv(nil, nil))
	want := "local   unavailable: this platform is not supported; spmeta reads Spotify on macOS and Linux\n"
	if !strings.Contains(stdout, want) {
		t.Errorf("stdout lacks %q:\n%s", want, stdout)
	}
}

func TestDoctorConfigFile(t *testing.T) {
	const path = "/home/u/.config/spmeta/config.toml"
	tests := []struct {
		name string
		env  config.Env
		args []string
		want string
	}{
		{
			"found",
			doctorEnv(map[string]string{path: `player = "spotifyd"`}, nil), nil,
			"config  ok: " + path + "\n",
		},
		{
			"unknown key",
			doctorEnv(map[string]string{path: `bogus = 1`}, nil), nil,
			"config  ok: " + path + "\n        unknown key \"bogus\" in " + path + " (ignored)\n",
		},
		{
			"invalid toml",
			doctorEnv(map[string]string{path: `not toml =`}, nil), nil,
			"config  error: config file " + path + " is not valid TOML",
		},
		{
			"invalid value",
			doctorEnv(nil, map[string]string{"SPMETA_SOURCE": "bogus"}), nil,
			`config  error: invalid value "bogus" for SPMETA_SOURCE`,
		},
		{
			"explicit file missing",
			doctorEnv(nil, nil),
			[]string{"--config", "/nope.toml"},
			"config  error: config file /nope.toml (from --config) does not exist\n",
		},
		{
			"explicit file",
			doctorEnv(map[string]string{"/etc/spmeta.toml": `player = "x"`}, nil),
			[]string{"--config", "/etc/spmeta.toml"},
			"config  ok: /etc/spmeta.toml\n",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			code, stdout, stderr := runDoctor(t, grantedMac(), tc.env, tc.args...)
			if code != 0 {
				t.Errorf("exit = %d, want 0: a configuration problem does not hide the sources (stderr %q)", code, stderr)
			}
			if !strings.Contains(stdout, tc.want) {
				t.Errorf("stdout lacks %q:\n%s", tc.want, stdout)
			}
			if !strings.Contains(stdout, "macos   ok:") {
				t.Errorf("sources were not checked:\n%s", stdout)
			}
		})
	}
}

func TestDoctorPassesPlayerToTheBackend(t *testing.T) {
	const path = "/home/u/.config/spmeta/config.toml"
	var got local.Settings
	factory := func(s local.Settings) backend.Backend {
		got = s
		return grantedMac()
	}
	env := doctorEnv(map[string]string{path: `player = "spotifyd"`}, nil)
	var out, errOut bytes.Buffer
	cli.Run(context.Background(), []string{"doctor"}, &out, &errOut, cli.BuildInfo{}, cli.WithLocalFactory(factory), cli.WithEnv(env))
	if got.Player != "spotifyd" {
		t.Errorf("Player = %q, want spotifyd", got.Player)
	}
}

func TestDoctorNoticeNamesTheTerminal(t *testing.T) {
	mac := grantedMac()
	mac.prompts = true
	env := doctorEnv(nil, map[string]string{"TERM_PROGRAM": "Apple_Terminal"})
	code, stdout, stderr := runDoctor(t, mac, env)
	if code != 0 {
		t.Fatalf("exit = %d (stderr %q)", code, stderr)
	}
	const notice = "macOS may ask to allow Terminal to control Spotify; click OK.\n"
	if stderr != notice {
		t.Errorf("stderr = %q, want %q", stderr, notice)
	}
	if strings.Contains(stdout, "click OK") {
		t.Errorf("the notice belongs on stderr, not in the report:\n%s", stdout)
	}
}

func TestDoctorDebug(t *testing.T) {
	failing := &macFake{result: macos.PermissionResult{
		State: macos.PermissionFailed, Message: "osascript failed.", Err: errCause,
	}}
	_, _, quiet := runDoctor(t, failing, doctorEnv(nil, nil))
	if want := "spmeta: no source is available\nhint: Fix the failing check above, then run `spmeta doctor` again.\n"; quiet != want {
		t.Errorf("without --debug stderr = %q, want only the exit-4 lines %q", quiet, want)
	}
	code, stdout, stderr := runDoctor(t, failing, doctorEnv(nil, nil), "--debug")
	if code != 4 {
		t.Fatalf("exit = %d, want 4", code)
	}
	if !strings.Contains(stderr, "level=DEBUG") || !strings.Contains(stderr, "cause") || !strings.Contains(stderr, "exit status 1") {
		t.Errorf("--debug stderr lacks the cause:\n%s", stderr)
	}
	if strings.Contains(stdout, "exit status 1") {
		t.Errorf("the cause leaked into stdout:\n%s", stdout)
	}
}

func TestDoctorUsageErrors(t *testing.T) {
	tests := []struct {
		name string
		args []string
		want string
	}{
		{"unknown flag", []string{"--bogus"}, "unknown flag --bogus"},
		{"quiet is not a doctor flag", []string{"--quiet"}, "unknown flag --quiet"},
		{"positional argument", []string{"extra"}, `unexpected argument "extra"`},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			mac := grantedMac()
			code, stdout, stderr := runDoctor(t, mac, doctorEnv(nil, nil), tc.args...)
			if code != 2 {
				t.Fatalf("exit = %d, want 2", code)
			}
			if stdout != "" || mac.checks != 0 {
				t.Errorf("stdout = %q, checks = %d, want neither", stdout, mac.checks)
			}
			if !strings.Contains(stderr, tc.want) || !strings.Contains(stderr, usageHint) {
				t.Errorf("stderr = %q, want %q and the usage hint", stderr, tc.want)
			}
		})
	}
}

func TestDoctorHelp(t *testing.T) {
	for _, args := range [][]string{{"doctor", "-h"}, {"doctor", "--help"}, {"help", "doctor"}} {
		t.Run(strings.Join(args, " "), func(t *testing.T) {
			mac := grantedMac()
			var out, errOut bytes.Buffer
			code := cli.Run(context.Background(), args, &out, &errOut, cli.BuildInfo{}, cli.WithBackend(mac), cli.WithEnv(config.Env{}))
			if code != 0 || errOut.Len() != 0 {
				t.Fatalf("exit = %d, stderr %q, want 0 and empty", code, errOut.String())
			}
			for _, want := range []string{"spmeta doctor [--debug] [--config PATH]", "--debug", "--config PATH", "120 s", "one request to the Spotify Web API"} {
				if !strings.Contains(out.String(), want) {
					t.Errorf("usage lacks %q:\n%s", want, out.String())
				}
			}
			if mac.checks != 0 {
				t.Error("help ran the checks")
			}
		})
	}
}

func TestRootUsageListsDoctor(t *testing.T) {
	_, stdout, _ := run(t, "help")
	for _, want := range []string{"spmeta doctor", "doctor    "} {
		if !strings.Contains(stdout, want) {
			t.Errorf("usage lacks %q:\n%s", want, stdout)
		}
	}
}

var errCause = errors.New("exit status 1")

func TestDoctorChecksWebWhenConfigIsInvalid(t *testing.T) {
	s := newWebSetup(t)
	s.saveLogin(t, time.Hour)
	s.files = map[string]string{filepath.Join(s.home, ".config", "spmeta", "config.toml"): "format = [\n"}
	_, stdout, _ := s.doctor(t, grantedMac())
	if !strings.Contains(stdout, "config  error:") {
		t.Errorf("stdout lacks the config error:\n%s", stdout)
	}
	if strings.Contains(stdout, "web not configured") || strings.Contains(stdout, "not configured") {
		t.Errorf("stdout reports Web as not configured despite a saved login:\n%s", stdout)
	}
	if !strings.Contains(stdout, "web     ok: logged in") {
		t.Errorf("stdout lacks the Web check result:\n%s", stdout)
	}
}
