package doctor_test

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"strings"
	"testing"

	"github.com/bkuberek/spmeta/internal/backend"
	"github.com/bkuberek/spmeta/internal/backend/macos"
	"github.com/bkuberek/spmeta/internal/backend/mpris"
	"github.com/bkuberek/spmeta/internal/doctor"
)

const (
	headerLine = "spmeta 0.1.0 (darwin/arm64)"
	webLine    = "web     unavailable: Web mode is not available in this version"
)

// fakeMacOS answers CheckPermission with a canned result. prompts says whether
// the check got far enough to show the macOS consent notice.
type fakeMacOS struct {
	result  macos.PermissionResult
	prompts bool
	calls   int
}

func (f *fakeMacOS) CheckPermission(_ context.Context, beforePrompt func()) macos.PermissionResult {
	f.calls++
	if f.prompts && beforePrompt != nil {
		beforePrompt()
	}
	return f.result
}

type fakePlayers struct {
	players []mpris.PlayerInfo
	err     error
}

func (f fakePlayers) Players(context.Context) ([]mpris.PlayerInfo, error) {
	return f.players, f.err
}

func okConfig() doctor.ConfigReport {
	return doctor.ConfigReport{Path: "/home/u/.config/spmeta/config.toml", Found: true}
}

// report runs doctor with one buffer for stdout and the notice stream, so the
// order of both is visible, and returns the lines and Run's error.
func report(t *testing.T, o doctor.Options) (lines []string, err error) {
	t.Helper()
	var buf bytes.Buffer
	o.Out, o.Notice = &buf, &buf
	if o.Version == "" {
		o.Version, o.OS, o.Arch = "0.1.0", "darwin", "arm64"
	}
	if o.Config.Path == "" && o.Config.Err == nil {
		o.Config = okConfig()
	}
	err = doctor.Run(context.Background(), o)
	return strings.Split(strings.TrimSuffix(buf.String(), "\n"), "\n"), err
}

func TestRunHeaderAndWebAreAlwaysReported(t *testing.T) {
	lines, _ := report(t, doctor.Options{})
	if lines[0] != headerLine {
		t.Errorf("first line = %q, want %q", lines[0], headerLine)
	}
	if got := lines[len(lines)-1]; got != webLine {
		t.Errorf("last line = %q, want %q", got, webLine)
	}
}

func TestRunHeaderDefaultsToDev(t *testing.T) {
	var buf bytes.Buffer
	_ = doctor.Run(context.Background(), doctor.Options{Out: &buf, Notice: &buf, OS: "linux", Arch: "amd64", Config: okConfig()})
	if got, _, _ := strings.Cut(buf.String(), "\n"); got != "spmeta dev (linux/amd64)" {
		t.Errorf("header = %q", got)
	}
}

func TestRunConfigLine(t *testing.T) {
	tests := []struct {
		name string
		cfg  doctor.ConfigReport
		want []string
	}{
		{
			"file found",
			doctor.ConfigReport{Path: "/p/config.toml", Found: true},
			[]string{"config  ok: /p/config.toml"},
		},
		{
			"default location without a file",
			doctor.ConfigReport{Path: "/p/config.toml"},
			[]string{"config  ok: /p/config.toml not found, using defaults"},
		},
		{
			"no location known",
			doctor.ConfigReport{},
			[]string{"config  ok: no configuration location known, using defaults"},
		},
		{
			"unknown keys are listed",
			doctor.ConfigReport{Path: "/p/config.toml", Found: true, Warnings: []string{`unknown key "a" in /p/config.toml (ignored)`, `unknown key "b" in /p/config.toml (ignored)`}},
			[]string{
				"config  ok: /p/config.toml",
				`        unknown key "a" in /p/config.toml (ignored)`,
				`        unknown key "b" in /p/config.toml (ignored)`,
			},
		},
		{
			"invalid file",
			doctor.ConfigReport{Err: errors.New("config file /p/config.toml is not valid TOML: line 1")},
			[]string{"config  error: config file /p/config.toml is not valid TOML: line 1"},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var buf bytes.Buffer
			o := doctor.Options{Out: &buf, Notice: &buf, Version: "0.1.0", OS: "darwin", Arch: "arm64", Config: tc.cfg}
			// A zero ConfigReport means "no location known"; report() would replace it.
			_ = doctor.Run(context.Background(), o)
			lines := strings.Split(buf.String(), "\n")[1:]
			if got := lines[:len(tc.want)]; strings.Join(got, "\n") != strings.Join(tc.want, "\n") {
				t.Errorf("config lines:\n%s\nwant:\n%s", strings.Join(got, "\n"), strings.Join(tc.want, "\n"))
			}
		})
	}
}

func TestRunMacOS(t *testing.T) {
	tests := []struct {
		name     string
		result   macos.PermissionResult
		terminal string
		want     string
		usable   bool
	}{
		{
			"granted",
			macos.PermissionResult{State: macos.PermissionGranted},
			"",
			"macos   ok: Spotify is running; Automation permission granted",
			true,
		},
		{
			"denied names the terminal",
			macos.PermissionResult{State: macos.PermissionDenied},
			"Apple_Terminal",
			"macos   denied: macOS did not allow Terminal to control Spotify. Allow it in System Settings > Privacy & Security > Automation, then run 'spmeta doctor' again.",
			false,
		},
		{
			"denied without a terminal",
			macos.PermissionResult{State: macos.PermissionDenied},
			"",
			"macos   denied: macOS did not allow the app that runs spmeta to control Spotify. Allow it in System Settings > Privacy & Security > Automation, then run 'spmeta doctor' again.",
			false,
		},
		{
			"spotify not running",
			macos.PermissionResult{State: macos.PermissionSpotifyNotRunning},
			"",
			"macos   unavailable: Spotify is not running, so permission cannot be checked without launching it. Start Spotify and run 'spmeta doctor' again.",
			false,
		},
		{
			"no answer",
			macos.PermissionResult{State: macos.PermissionNoAnswer},
			"",
			"macos   error: no answer within 120 s. Look for a macOS permission prompt, answer it, and run 'spmeta doctor' again.",
			false,
		},
		{
			"other failure",
			macos.PermissionResult{State: macos.PermissionFailed, Message: "osascript failed (error -9999)."},
			"",
			"macos   error: osascript failed (error -9999)",
			false,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			f := &fakeMacOS{result: tc.result}
			lines, err := report(t, doctor.Options{MacOS: f, Terminal: tc.terminal})
			if !contains(lines, tc.want) {
				t.Errorf("report lacks line %q:\n%s", tc.want, strings.Join(lines, "\n"))
			}
			if usable := err == nil; usable != tc.usable {
				t.Errorf("usable = %v, want %v (err %v)", usable, tc.usable, err)
			}
			if f.calls != 1 {
				t.Errorf("CheckPermission called %d times, want 1", f.calls)
			}
		})
	}
}

func TestRunMacOSNotice(t *testing.T) {
	t.Run("shown before the macos line", func(t *testing.T) {
		f := &fakeMacOS{result: macos.PermissionResult{State: macos.PermissionGranted}, prompts: true}
		lines, _ := report(t, doctor.Options{MacOS: f, Terminal: "iTerm.app"})
		notice := indexOf(lines, "macOS may ask to allow iTerm to control Spotify; click OK.")
		macosLine := indexOfPrefix(lines, "macos   ")
		if notice < 0 || macosLine < 0 || notice > macosLine {
			t.Errorf("notice at %d, macos line at %d:\n%s", notice, macosLine, strings.Join(lines, "\n"))
		}
	})
	t.Run("unknown terminal", func(t *testing.T) {
		f := &fakeMacOS{result: macos.PermissionResult{State: macos.PermissionGranted}, prompts: true}
		lines, _ := report(t, doctor.Options{MacOS: f})
		if !contains(lines, "macOS may ask to allow the app that runs spmeta to control Spotify; click OK.") {
			t.Errorf("notice missing:\n%s", strings.Join(lines, "\n"))
		}
	})
	t.Run("not shown when the check never prompts", func(t *testing.T) {
		f := &fakeMacOS{result: macos.PermissionResult{State: macos.PermissionSpotifyNotRunning}}
		lines, _ := report(t, doctor.Options{MacOS: f})
		if indexOfPrefix(lines, "macOS may ask") >= 0 {
			t.Errorf("unexpected notice:\n%s", strings.Join(lines, "\n"))
		}
	})
}

func TestRunMPRIS(t *testing.T) {
	both := []mpris.PlayerInfo{
		{Name: "spotify", Status: "Playing", Selected: true},
		{Name: "spotifyd.instance1", Status: "Paused"},
	}
	tests := []struct {
		name    string
		players fakePlayers
		want    []string
		usable  bool
	}{
		{
			"selected player and others",
			fakePlayers{players: both},
			[]string{
				"mpris   ok: reading spotify",
				"        spotify (Playing, selected)",
				"        spotifyd.instance1 (Paused)",
			},
			true,
		},
		{
			"unanswered player is flagged",
			fakePlayers{players: []mpris.PlayerInfo{{Name: "ncspot"}, {Name: "spotifyd", Status: "Stopped", Selected: true}}},
			[]string{
				"mpris   ok: reading spotifyd",
				"        ncspot (no answer)",
				"        spotifyd (Stopped, selected)",
			},
			true,
		},
		{
			"no players",
			fakePlayers{},
			[]string{"mpris   unavailable: no MPRIS player is running. Start Spotify, spotifyd, ncspot or spotify_player."},
			false,
		},
		{
			"only browsers",
			fakePlayers{players: []mpris.PlayerInfo{{Name: "firefox.instance1", Status: "Playing"}}},
			[]string{
				"mpris   unavailable: none of the players found is a Spotify client. Start one, or set 'player' in the configuration file.",
				"        firefox.instance1 (Playing)",
			},
			false,
		},
		{
			"no session bus",
			fakePlayers{err: &backend.Error{Kind: backend.KindNotAvailable, Message: "no D-Bus session bus found", Hint: "Run spmeta inside a desktop session, or with `dbus-run-session`."}},
			[]string{"mpris   unavailable: no D-Bus session bus found. Run spmeta inside a desktop session, or with `dbus-run-session`."},
			false,
		},
		{
			"timeout",
			fakePlayers{err: &backend.Error{Kind: backend.KindUnexpected, Message: "MPRIS read timed out"}},
			[]string{"mpris   error: MPRIS read timed out"},
			false,
		},
		{
			"plain error",
			fakePlayers{err: errors.New("boom")},
			[]string{"mpris   error: boom"},
			false,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			lines, err := report(t, doctor.Options{MPRIS: tc.players, OS: "linux", Arch: "amd64", Version: "0.1.0"})
			i := indexOfPrefix(lines, "mpris   ")
			if i < 0 || i+len(tc.want) > len(lines) || strings.Join(lines[i:i+len(tc.want)], "\n") != strings.Join(tc.want, "\n") {
				t.Errorf("report:\n%s\nwant block:\n%s", strings.Join(lines, "\n"), strings.Join(tc.want, "\n"))
			}
			if usable := err == nil; usable != tc.usable {
				t.Errorf("usable = %v, want %v (err %v)", usable, tc.usable, err)
			}
		})
	}
}

func TestRunUnsupportedPlatform(t *testing.T) {
	lines, err := report(t, doctor.Options{OS: "windows", Arch: "amd64", Version: "0.1.0"})
	if want := "local   unavailable: this platform is not supported; spmeta reads Spotify on macOS and Linux"; !contains(lines, want) {
		t.Errorf("report lacks %q:\n%s", want, strings.Join(lines, "\n"))
	}
	assertNoSource(t, err)
}

func TestRunExitMapsToNoSource(t *testing.T) {
	denied := &fakeMacOS{result: macos.PermissionResult{State: macos.PermissionDenied}}
	_, err := report(t, doctor.Options{MacOS: denied})
	assertNoSource(t, err)
}

func TestRunWebNeverMakesASourceUsable(t *testing.T) {
	_, err := report(t, doctor.Options{MPRIS: fakePlayers{}})
	assertNoSource(t, err)
}

func TestRunDebugLogsTheCause(t *testing.T) {
	var logs bytes.Buffer
	log := slog.New(slog.NewTextHandler(&logs, &slog.HandlerOptions{Level: slog.LevelDebug}))
	f := &fakeMacOS{result: macos.PermissionResult{State: macos.PermissionFailed, Message: "osascript failed.", Err: errors.New("exit status 1")}}
	lines, _ := report(t, doctor.Options{MacOS: f, Log: log})
	if !strings.Contains(logs.String(), "exit status 1") || !strings.Contains(logs.String(), "macos") {
		t.Errorf("debug log = %q, want the macos check and its cause", logs.String())
	}
	if strings.Contains(strings.Join(lines, "\n"), "exit status 1") {
		t.Errorf("the cause leaked into the report:\n%s", strings.Join(lines, "\n"))
	}
}

func TestRunWithoutLoggerDoesNotPanic(t *testing.T) {
	f := &fakeMacOS{result: macos.PermissionResult{State: macos.PermissionFailed, Message: "x", Err: errors.New("y")}}
	if _, err := report(t, doctor.Options{MacOS: f}); err == nil {
		t.Error("want an error when the only source fails")
	}
}

func assertNoSource(t *testing.T, err error) {
	t.Helper()
	var be *backend.Error
	if !errors.As(err, &be) || be.Kind != backend.KindNotAvailable {
		t.Fatalf("err = %v, want a not-available *backend.Error", err)
	}
	if backend.ExitCode(be.Kind) != 4 {
		t.Errorf("exit code = %d, want 4", backend.ExitCode(be.Kind))
	}
	if be.Message != "no source is available" || be.Hint == "" {
		t.Errorf("error = %+v, want message %q and a hint", be, "no source is available")
	}
}

func contains(lines []string, want string) bool { return indexOf(lines, want) >= 0 }

func indexOf(lines []string, want string) int {
	for i, l := range lines {
		if l == want {
			return i
		}
	}
	return -1
}

func indexOfPrefix(lines []string, prefix string) int {
	for i, l := range lines {
		if strings.HasPrefix(l, prefix) {
			return i
		}
	}
	return -1
}
