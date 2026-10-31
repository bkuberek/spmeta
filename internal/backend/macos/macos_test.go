package macos

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/bkuberek/spmeta/internal/backend"
	"github.com/bkuberek/spmeta/internal/model"
)

var fetchedAt = time.Date(2026, 10, 27, 17, 40, 0, 0, time.UTC)

func ptr[T any](v T) *T { return &v }

func fixture(t *testing.T, name string) []byte {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	return data
}

// fakeRunner is a Runner that never touches the machine.
type fakeRunner struct {
	running    bool
	runningErr error
	stdout     []byte
	stderr     []byte
	scriptErr  error
	block      bool

	pgrepCalls  int
	scriptCalls int
	lastScript  string
}

func (f *fakeRunner) ProcessRunning(_ context.Context, name string) (bool, error) {
	f.pgrepCalls++
	if name != "Spotify" {
		return false, errors.New("unexpected process name " + name)
	}
	return f.running, f.runningErr
}

func (f *fakeRunner) RunScript(ctx context.Context, script string) ([]byte, []byte, error) {
	f.scriptCalls++
	f.lastScript = script
	if f.block {
		<-ctx.Done()
		return nil, nil, ctx.Err()
	}
	return f.stdout, f.stderr, f.scriptErr
}

func newBackend(r Runner) *Backend {
	b := New(r, func() time.Time { return fetchedAt })
	// Tests never run ps: the host app is unknown unless a test sets it.
	b.hostApp = func(context.Context) string { return "" }
	return b
}

func wantBase() model.NowPlaying {
	return model.NowPlaying{
		SchemaVersion: 1,
		Source:        model.SourceMacOS,
		State:         model.StatePaused,
		Type:          model.ItemTrack,
		Title:         ptr("So Cold - Remix"),
		Artists:       []string{"Breaking Benjamin"},
		Artist:        ptr("Breaking Benjamin"),
		Album:         ptr("We Are Not Alone"),
		AlbumArtist:   ptr("Breaking Benjamin"),
		TrackNumber:   ptr(1),
		DiscNumber:    ptr(1),
		DurationMS:    ptr(int64(273440)),
		PositionMS:    ptr(int64(108905)),
		URI:           ptr("spotify:track:4BJyt25nburVwbnESDeIc7"),
		URL:           ptr("https://open.spotify.com/track/4BJyt25nburVwbnESDeIc7"),
		ArtworkURL:    ptr("https://i.scdn.co/image/ab67616d0000b2734500b37eb7046f29233099a0"),
		FetchedAt:     fetchedAt,
	}
}

func TestReadParsesFixtures(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		fixture string
		mutate  func(*model.NowPlaying)
	}{
		{
			name:    "live paused capture",
			fixture: "track-paused.txt",
			mutate:  func(*model.NowPlaying) {},
		},
		{
			name:    "live playing capture",
			fixture: "track-playing.txt",
			mutate: func(np *model.NowPlaying) {
				np.State = model.StatePlaying
				np.Title = ptr("The Red")
				np.Artists = []string{"Chevelle"}
				np.Artist = ptr("Chevelle")
				np.Album = ptr("Wonder What's Next (Expanded Edition)")
				np.AlbumArtist = ptr("Chevelle")
				np.TrackNumber = ptr(5)
				np.DurationMS = ptr(int64(238400))
				np.PositionMS = ptr(int64(29985))
				np.URI = ptr("spotify:track:1gyee1JuFFiP476LQpRMYU")
				np.URL = ptr("https://open.spotify.com/track/1gyee1JuFFiP476LQpRMYU")
				np.ArtworkURL = ptr("https://i.scdn.co/image/ab67616d0000b273bd444869348f556430c21890")
			},
		},
		{
			name:    "second live probe",
			fixture: "track-playing-2.txt",
			mutate: func(np *model.NowPlaying) {
				np.State = model.StatePlaying
				np.Title = ptr("novacaine (addicted to you) - sped up")
				np.Artists = []string{"Lofuu"}
				np.Artist = ptr("Lofuu")
				np.Album = ptr("thinkin bout you (novacaine deluxe)")
				np.AlbumArtist = ptr("Lofuu")
				np.DurationMS = ptr(int64(171605))
				np.PositionMS = ptr(int64(65309))
				np.URI = ptr("spotify:track:3L7RbxQx1JaMAs9sbvyRGV")
				np.URL = ptr("https://open.spotify.com/track/3L7RbxQx1JaMAs9sbvyRGV")
				np.ArtworkURL = ptr("https://i.scdn.co/image/ab67616d0000b2739761c398f06544abaecf9dc2")
			},
		},
		{
			name:    "episode",
			fixture: "episode.txt",
			mutate: func(np *model.NowPlaying) {
				np.State = model.StatePlaying
				np.Type = model.ItemEpisode
				np.Title = ptr("Episode 12: Hello")
				np.Artists = []string{"Some Show"}
				np.Artist = ptr("Some Show")
				np.Album = ptr("Some Show")
				np.AlbumArtist = nil
				np.TrackNumber = nil
				np.DiscNumber = nil
				np.DurationMS = ptr(int64(2400000))
				np.PositionMS = ptr(int64(60000))
				np.URI = ptr("spotify:episode:7makk4oTQel546B0PZlDM5")
				np.URL = ptr("https://open.spotify.com/episode/7makk4oTQel546B0PZlDM5")
				np.ArtworkURL = ptr("https://i.scdn.co/image/ab6765630000ba8a0000000000000000000000ee")
			},
		},
		{
			name:    "ad",
			fixture: "ad.txt",
			mutate: func(np *model.NowPlaying) {
				np.State = model.StatePlaying
				np.Type = model.ItemAd
				np.Title = ptr("Spotify")
				np.Artists = []string{}
				np.Artist = nil
				np.Album = nil
				np.AlbumArtist = nil
				np.TrackNumber = nil
				np.DiscNumber = nil
				np.DurationMS = ptr(int64(30000))
				np.PositionMS = ptr(int64(5000))
				np.URI = ptr("spotify:ad:000000000000000000000000")
				np.URL = nil
				np.ArtworkURL = nil
			},
		},
		{
			name:    "local file",
			fixture: "local-file.txt",
			mutate: func(np *model.NowPlaying) {
				np.Title = ptr("Title")
				np.Artists = []string{"Artist"}
				np.Artist = ptr("Artist")
				np.Album = ptr("Album")
				np.AlbumArtist = nil
				np.TrackNumber = nil
				np.DiscNumber = nil
				np.DurationMS = ptr(int64(215000))
				np.PositionMS = ptr(int64(1000))
				np.URI = ptr("spotify:local:Artist:Album:Title:215")
				np.URL = nil
				np.ArtworkURL = nil
			},
		},
		{
			name:    "separators in title",
			fixture: "separators-in-title.txt",
			mutate: func(np *model.NowPlaying) {
				np.State = model.StatePlaying
				np.Title = ptr("Tab\there | pipe\nnewline")
				np.Artists = []string{"Artist, With Comma"}
				np.Artist = ptr("Artist, With Comma")
				np.Album = ptr("Album")
				np.AlbumArtist = ptr("Album Artist")
				np.TrackNumber = ptr(2)
				np.DurationMS = ptr(int64(200000))
				np.PositionMS = ptr(int64(1000))
				np.URI = ptr("spotify:track:4brK3gpPjXArNUlU1hBXTe")
				np.URL = ptr("https://open.spotify.com/track/4brK3gpPjXArNUlU1hBXTe")
				np.ArtworkURL = ptr("https://i.scdn.co/image/abc")
			},
		},
		{
			name:    "missing fields",
			fixture: "missing-fields.txt",
			mutate: func(np *model.NowPlaying) {
				np.State = model.StatePlaying
				np.Title = ptr("Only Title")
				np.Artists = []string{"Only Artist"}
				np.Artist = ptr("Only Artist")
				np.Album = nil
				np.AlbumArtist = nil
				np.TrackNumber = nil
				np.DiscNumber = nil
				np.DurationMS = ptr(int64(180000))
				np.PositionMS = ptr(int64(0))
				np.URI = ptr("spotify:track:4brK3gpPjXArNUlU1hBXTe")
				np.URL = ptr("https://open.spotify.com/track/4brK3gpPjXArNUlU1hBXTe")
				np.ArtworkURL = nil
			},
		},
		{
			name:    "bad integers become null",
			fixture: "bad-integer.txt",
			mutate: func(np *model.NowPlaying) {
				np.State = model.StatePlaying
				np.Title = ptr("Bad Numbers")
				np.Artists = []string{"Artist"}
				np.Artist = ptr("Artist")
				np.Album = ptr("Album")
				np.AlbumArtist = ptr("Artist")
				np.TrackNumber = nil
				np.DiscNumber = ptr(1)
				np.DurationMS = nil
				np.PositionMS = nil
				np.URI = ptr("spotify:track:4brK3gpPjXArNUlU1hBXTe")
				np.URL = ptr("https://open.spotify.com/track/4brK3gpPjXArNUlU1hBXTe")
				np.ArtworkURL = nil
			},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			want := wantBase()
			tc.mutate(&want)
			r := &fakeRunner{running: true, stdout: fixture(t, tc.fixture)}
			got, err := newBackend(r).Read(context.Background())
			if err != nil {
				t.Fatalf("Read: %v", err)
			}
			if !reflect.DeepEqual(got, want) {
				t.Errorf("Read mismatch\n got: %s\nwant: %s", dump(got), dump(want))
			}
		})
	}
}

func dump(np model.NowPlaying) string {
	b, err := np.MarshalJSON()
	if err != nil {
		return err.Error()
	}
	return string(b)
}

func TestReadURIFallsBackToSpotifyURL(t *testing.T) {
	t.Parallel()
	const sep = "\x1f"
	out := strings.Join([]string{
		"OK", "playing", "T", "A", "Al", "A", "1000", "10",
		"not-a-uri", "spotify:track:4brK3gpPjXArNUlU1hBXTe", "", "1", "1",
	}, sep) + "\n"
	r := &fakeRunner{running: true, stdout: []byte(out)}
	got, err := newBackend(r).Read(context.Background())
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if got.URI == nil || *got.URI != "spotify:track:4brK3gpPjXArNUlU1hBXTe" {
		t.Errorf("URI = %v, want fallback to spotify url field", got.URI)
	}
}

func TestReadSendsReadScript(t *testing.T) {
	t.Parallel()
	r := &fakeRunner{running: true, stdout: fixture(t, "track-playing.txt")}
	if _, err := newBackend(r).Read(context.Background()); err != nil {
		t.Fatalf("Read: %v", err)
	}
	if r.lastScript != readScript {
		t.Error("RunScript did not receive readScript")
	}
}

func TestReadResultKinds(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		fixture  string
		wantKind backend.Kind
		wantMsg  string
	}{
		{"stopped", "stopped.txt", backend.KindNothingPlaying, ""},
		{"no track", "no-track.txt", backend.KindNothingPlaying, ""},
		{"quit between check and script", "not-running.txt", backend.KindNotAvailable, "Spotify is not running."},
		{"wrong field count", "wrong-field-count.txt", backend.KindUnexpected, "unexpected output from osascript"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			r := &fakeRunner{running: true, stdout: fixture(t, tc.fixture)}
			_, err := newBackend(r).Read(context.Background())
			assertError(t, err, tc.wantKind, tc.wantMsg, "")
		})
	}
}

func TestReadUnexpectedOutput(t *testing.T) {
	t.Parallel()
	const sep = "\x1f"
	tests := []struct {
		name string
		out  string
	}{
		{"empty", ""},
		{"garbage", "hello\n"},
		{"unknown state", strings.Join([]string{"OK", "buffering", "T", "A", "Al", "A", "1", "1", "spotify:track:x", "", "", "1", "1"}, sep) + "\n"},
		{"too many fields", strings.Join([]string{"OK", "playing", "T", "A", "Al", "A", "1", "1", "spotify:track:x", "", "", "1", "1", "extra"}, sep) + "\n"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			r := &fakeRunner{running: true, stdout: []byte(tc.out)}
			_, err := newBackend(r).Read(context.Background())
			assertError(t, err, backend.KindUnexpected, "unexpected output from osascript", "")
		})
	}
}

func TestReadRecordWithoutIdentityIsNothingPlaying(t *testing.T) {
	t.Parallel()
	out := strings.Repeat("\x1f", 12)
	r := &fakeRunner{running: true, stdout: []byte("OK\x1fplaying" + out[:11] + "\n")}
	_, err := newBackend(r).Read(context.Background())
	assertError(t, err, backend.KindNothingPlaying, "", "")
}

func TestReadDoesNotRunScriptWhenSpotifyIsNotRunning(t *testing.T) {
	t.Parallel()
	r := &fakeRunner{running: false, stdout: fixture(t, "track-playing.txt")}
	_, err := newBackend(r).Read(context.Background())
	assertError(t, err, backend.KindNotAvailable, "Spotify is not running.", "")
	if r.scriptCalls != 0 {
		t.Errorf("RunScript called %d times, want 0", r.scriptCalls)
	}
}

func TestReadProcessCheckFailure(t *testing.T) {
	t.Parallel()
	r := &fakeRunner{runningErr: errors.New("pgrep: boom")}
	_, err := newBackend(r).Read(context.Background())
	assertError(t, err, backend.KindUnexpected, "could not check whether Spotify is running", "")
	if r.scriptCalls != 0 {
		t.Errorf("RunScript called %d times, want 0", r.scriptCalls)
	}
}

const (
	wantHintAutomation = "Allow it in System Settings > Privacy & Security > Automation, under the app that runs spmeta, then run `spmeta doctor`."
	wantHintDoctor     = "Run `spmeta doctor`."
	wantHintFirstRun   = "If this is the first run, macOS may be waiting for you to allow Automation access: run `spmeta doctor`."
)

func TestReadScriptFailures(t *testing.T) {
	t.Parallel()
	exitErr := errors.New("exit status 1")
	tests := []struct {
		name     string
		stderr   []byte
		wantKind backend.Kind
		wantMsg  string
		wantHint string
	}{
		{
			"automation denied", fixture(t, "stderr-1743.txt"),
			backend.KindPermissionDenied, "macOS did not allow this app to control Spotify.", wantHintAutomation,
		},
		{
			"application not running", fixture(t, "stderr-600.txt"),
			backend.KindNotAvailable, "Spotify is not running.", "",
		},
		{
			"apple event timed out", fixture(t, "stderr-1712.txt"),
			backend.KindUnexpected, "Spotify did not respond.", wantHintDoctor,
		},
		{
			"unknown error number", []byte("execution error: something odd. (-9999)\n"),
			backend.KindUnexpected, "osascript failed (error -9999).", "",
		},
		{
			"number on last line only", []byte("warning: ignore (-1743)\nexecution error: odd. (-9999)\n"),
			backend.KindUnexpected, "osascript failed (error -9999).", "",
		},
		{
			"no number", []byte("osascript: something broke\n"),
			backend.KindUnexpected, "osascript failed.", "",
		},
		{
			"empty stderr", nil,
			backend.KindUnexpected, "osascript failed.", "",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			r := &fakeRunner{running: true, stderr: tc.stderr, scriptErr: exitErr}
			_, err := newBackend(r).Read(context.Background())
			assertError(t, err, tc.wantKind, tc.wantMsg, tc.wantHint)
			var be *backend.Error
			if errors.As(err, &be) && !errors.Is(err, exitErr) {
				t.Errorf("error does not wrap the runner error: %v", err)
			}
		})
	}
}

func TestReadDeniedHintNamesTheHostApp(t *testing.T) {
	t.Parallel()
	exitErr := errors.New("exit status 1")
	const want = "Allow it in System Settings > Privacy & Security > Automation, under iTerm, then run `spmeta doctor`."
	calls := 0
	b := newBackend(&fakeRunner{running: true, stderr: fixture(t, "stderr-1743.txt"), scriptErr: exitErr})
	b.hostApp = func(context.Context) string { calls++; return "iTerm" }
	_, err := b.Read(context.Background())
	assertError(t, err, backend.KindPermissionDenied, "macOS did not allow this app to control Spotify.", want)
	if calls != 1 {
		t.Errorf("host app resolved %d times, want 1", calls)
	}
}

func TestReadResolvesTheHostAppOnlyWhenDenied(t *testing.T) {
	t.Parallel()
	calls := 0
	b := newBackend(&fakeRunner{running: true, stdout: fixture(t, "track-playing.txt")})
	b.hostApp = func(context.Context) string { calls++; return "iTerm" }
	if _, err := b.Read(context.Background()); err != nil {
		t.Fatalf("Read: %v", err)
	}
	if calls != 0 {
		t.Errorf("host app resolved %d times on a successful read, want 0", calls)
	}
}

func TestReadDeadline(t *testing.T) {
	t.Parallel()
	r := &fakeRunner{running: true, block: true}
	b := newBackend(r)
	b.readTimeout = 20 * time.Millisecond
	start := time.Now()
	_, err := b.Read(context.Background())
	assertError(t, err, backend.KindUnexpected, "Spotify did not respond within 20ms.", wantHintFirstRun)
	if time.Since(start) > 2*time.Second {
		t.Error("Read did not honor the deadline")
	}
}

func TestReadHonorsCallerCancellation(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	r := &fakeRunner{running: true, block: true}
	_, err := newBackend(r).Read(ctx)
	if backend.KindOf(err) != backend.KindUnexpected {
		t.Fatalf("kind = %v, want unexpected (err %v)", backend.KindOf(err), err)
	}
	if !errors.Is(err, context.Canceled) {
		t.Errorf("error does not wrap context.Canceled: %v", err)
	}
}

func TestSource(t *testing.T) {
	t.Parallel()
	if got := newBackend(&fakeRunner{}).Source(); got != model.SourceMacOS {
		t.Errorf("Source() = %q, want %q", got, model.SourceMacOS)
	}
}

func TestScriptRules(t *testing.T) {
	t.Parallel()
	for _, want := range []string{
		`application id "com.spotify.client"`,
		`running of application id "com.spotify.client"`,
		`character id 31`,
		`(player position * 1000) as integer`,
	} {
		if !strings.Contains(readScript, want) {
			t.Errorf("readScript missing %q", want)
		}
	}
	for _, banned := range []string{`application "Spotify"`, "set st ", " st to "} {
		if strings.Contains(readScript, banned) {
			t.Errorf("readScript contains %q", banned)
		}
	}
}

func assertError(t *testing.T, err error, kind backend.Kind, msg, hint string) {
	t.Helper()
	var be *backend.Error
	if !errors.As(err, &be) {
		t.Fatalf("error = %v (%T), want *backend.Error", err, err)
	}
	if be.Kind != kind {
		t.Errorf("Kind = %v, want %v (message %q)", be.Kind, kind, be.Message)
	}
	if be.Source != model.SourceMacOS {
		t.Errorf("Source = %q, want %q", be.Source, model.SourceMacOS)
	}
	if msg != "" && be.Message != msg {
		t.Errorf("Message = %q, want %q", be.Message, msg)
	}
	if be.Hint != hint {
		t.Errorf("Hint = %q, want %q", be.Hint, hint)
	}
}

func TestCheckPermission(t *testing.T) {
	t.Parallel()
	exitErr := errors.New("exit status 1")
	tests := []struct {
		name       string
		runner     *fakeRunner
		want       PermissionState
		wantPrompt bool
		wantScript int
	}{
		{"granted", &fakeRunner{running: true, stdout: []byte("playing\n")}, PermissionGranted, true, 1},
		{"denied", &fakeRunner{running: true, stderr: fixture(t, "stderr-1743.txt"), scriptErr: exitErr}, PermissionDenied, true, 1},
		{"quit during the check", &fakeRunner{running: true, stderr: fixture(t, "stderr-600.txt"), scriptErr: exitErr}, PermissionSpotifyNotRunning, true, 1},
		{"apple event timed out", &fakeRunner{running: true, stderr: fixture(t, "stderr-1712.txt"), scriptErr: exitErr}, PermissionNoAnswer, true, 1},
		{"unknown error number", &fakeRunner{running: true, stderr: []byte("execution error: odd. (-9999)\n"), scriptErr: exitErr}, PermissionFailed, true, 1},
		{"not running", &fakeRunner{running: false}, PermissionSpotifyNotRunning, false, 0},
		{"process check fails", &fakeRunner{runningErr: errors.New("pgrep: boom")}, PermissionFailed, false, 0},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			prompts := 0
			got := newBackend(tc.runner).CheckPermission(context.Background(), func() { prompts++ })
			if got.State != tc.want {
				t.Errorf("State = %v, want %v (message %q)", got.State, tc.want, got.Message)
			}
			if (prompts == 1) != tc.wantPrompt || prompts > 1 {
				t.Errorf("notice shown %d times, want shown=%v", prompts, tc.wantPrompt)
			}
			if tc.runner.scriptCalls != tc.wantScript {
				t.Errorf("RunScript called %d times, want %d", tc.runner.scriptCalls, tc.wantScript)
			}
			if tc.want == PermissionFailed && got.Message == "" {
				t.Error("a failure carries no message")
			}
		})
	}
}

func TestCheckPermissionSendsMinimalScript(t *testing.T) {
	t.Parallel()
	r := &fakeRunner{running: true, stdout: []byte("paused\n")}
	newBackend(r).CheckPermission(context.Background(), nil)
	const want = `tell application id "com.spotify.client" to return (player state as string)`
	if strings.TrimSpace(r.lastScript) != want {
		t.Errorf("script = %q, want %q", r.lastScript, want)
	}
}

func TestCheckPermissionNoticeComesBeforeTheScript(t *testing.T) {
	t.Parallel()
	r := &fakeRunner{running: true, stdout: []byte("playing\n")}
	scriptsAtNotice := -1
	newBackend(r).CheckPermission(context.Background(), func() { scriptsAtNotice = r.scriptCalls })
	if scriptsAtNotice != 0 {
		t.Errorf("notice ran after %d scripts, want before the first", scriptsAtNotice)
	}
}

func TestCheckPermissionWaitsForTheConsentPrompt(t *testing.T) {
	t.Parallel()
	r := &fakeRunner{running: true, block: true}
	b := newBackend(r)
	b.permissionTimeout = 20 * time.Millisecond
	start := time.Now()
	got := b.CheckPermission(context.Background(), nil)
	if got.State != PermissionNoAnswer {
		t.Errorf("State = %v, want PermissionNoAnswer", got.State)
	}
	if time.Since(start) > 2*time.Second {
		t.Error("CheckPermission did not honor its own timeout")
	}
}

func TestCheckPermissionHonorsCallerCancellation(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	r := &fakeRunner{running: true, block: true}
	got := newBackend(r).CheckPermission(ctx, nil)
	if got.State != PermissionFailed || !errors.Is(got.Err, context.Canceled) {
		t.Errorf("result = %+v, want a failure wrapping context.Canceled", got)
	}
}

func TestPermissionTimeoutIsTwoMinutes(t *testing.T) {
	t.Parallel()
	if got := newBackend(&fakeRunner{}).permissionTimeout; got != 120*time.Second {
		t.Errorf("permissionTimeout = %v, want 120s", got)
	}
}
