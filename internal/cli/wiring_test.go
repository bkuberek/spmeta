package cli_test

import (
	"bytes"
	"context"
	"runtime"
	"strings"
	"testing"

	"github.com/bkuberek/spmeta/internal/backend"
	"github.com/bkuberek/spmeta/internal/backend/local"
	"github.com/bkuberek/spmeta/internal/cli"
	"github.com/bkuberek/spmeta/internal/config"
	"github.com/bkuberek/spmeta/internal/model"
)

var errNotRunning = &backend.Error{Kind: backend.KindNotAvailable, Message: "Spotify is not running"}

type webFake struct{ fakeBackend }

func (*webFake) Source() model.Source { return model.SourceWeb }

func playingOn(src model.Source, state model.State) model.NowPlaying {
	np := playing()
	np.Source, np.State = src, state
	return np
}

func runOpts(t *testing.T, opts []cli.Option, args ...string) (code int, stdout, stderr string) {
	t.Helper()
	var out, errOut bytes.Buffer
	opts = append([]cli.Option{cli.WithEnv(config.Env{})}, opts...)
	code = cli.Run(context.Background(), args, &out, &errOut, cli.BuildInfo{Version: "dev"}, opts...)
	return code, out.String(), errOut.String()
}

func TestSourceSelection(t *testing.T) {
	tests := []struct {
		name       string
		args       []string
		local      *fakeBackend
		web        *webFake
		wantCode   int
		wantSource string
		wantLocal  int
		wantWeb    int
	}{
		{"auto local playing skips web", nil, &fakeBackend{np: playing()}, &webFake{fakeBackend{np: playingOn(model.SourceWeb, model.StatePlaying)}}, 0, `"source":"macos"`, 1, 0},
		{"auto prefers web playing over local paused", nil, &fakeBackend{np: playingOn(model.SourceMacOS, model.StatePaused)}, &webFake{fakeBackend{np: playingOn(model.SourceWeb, model.StatePlaying)}}, 0, `"source":"web"`, 1, 1},
		{"auto falls back to web when local errors", nil, &fakeBackend{err: errNotRunning}, &webFake{fakeBackend{np: playingOn(model.SourceWeb, model.StatePaused)}}, 0, `"source":"web"`, 1, 1},
		{"local ignores web", []string{"--source", "local"}, &fakeBackend{np: playingOn(model.SourceMacOS, model.StatePaused)}, &webFake{fakeBackend{np: playingOn(model.SourceWeb, model.StatePlaying)}}, 0, `"source":"macos"`, 1, 0},
		{"web ignores local", []string{"--source", "web"}, &fakeBackend{np: playing()}, &webFake{fakeBackend{np: playingOn(model.SourceWeb, model.StatePaused)}}, 0, `"source":"web"`, 0, 1},
		{"auto without web returns the local error", nil, &fakeBackend{err: errNotRunning}, nil, 4, "", 1, 0},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			opts := []cli.Option{cli.WithBackend(tc.local)}
			if tc.web != nil {
				opts = append(opts, cli.WithWebBackend(tc.web))
			}
			code, stdout, stderr := runOpts(t, opts, append([]string{"--format", "json"}, tc.args...)...)
			if code != tc.wantCode {
				t.Fatalf("exit = %d, want %d (stderr %q)", code, tc.wantCode, stderr)
			}
			if !strings.Contains(stdout, tc.wantSource) {
				t.Errorf("stdout = %q, want it to contain %q", stdout, tc.wantSource)
			}
			if tc.local.calls != tc.wantLocal {
				t.Errorf("local calls = %d, want %d", tc.local.calls, tc.wantLocal)
			}
			if tc.web != nil && tc.web.calls != tc.wantWeb {
				t.Errorf("web calls = %d, want %d", tc.web.calls, tc.wantWeb)
			}
		})
	}
}

func TestAutoErrorPrecedenceExitCode(t *testing.T) {
	opts := []cli.Option{
		cli.WithBackend(&fakeBackend{err: errNotRunning}),
		cli.WithWebBackend(&webFake{fakeBackend{err: &backend.Error{Kind: backend.KindAuthRequired, Message: "login expired", Hint: "run `spmeta auth login`"}}}),
	}
	code, _, stderr := runOpts(t, opts)
	if code != 5 || stderr != "spmeta: login expired\nhint: run `spmeta auth login`\n" {
		t.Errorf("exit=%d stderr=%q, want 5 with the auth error", code, stderr)
	}
}

func TestLocalFactoryReceivesPlayer(t *testing.T) {
	var got local.Settings
	factory := func(s local.Settings) backend.Backend {
		got = s
		return &fakeBackend{np: playing()}
	}
	code, _, stderr := runOpts(t, []cli.Option{cli.WithLocalFactory(factory)}, "--player", "spotifyd")
	if code != 0 {
		t.Fatalf("exit = %d, stderr %q", code, stderr)
	}
	if got.Player != "spotifyd" {
		t.Errorf("Player = %q, want spotifyd", got.Player)
	}
}

func TestDebugReportsSourcesTried(t *testing.T) {
	opts := []cli.Option{
		cli.WithBackend(&fakeBackend{np: playingOn(model.SourceMacOS, model.StatePaused)}),
		cli.WithWebBackend(&webFake{fakeBackend{np: playingOn(model.SourceWeb, model.StatePlaying)}}),
	}
	code, _, stderr := runOpts(t, opts, "--debug")
	if code != 0 {
		t.Fatalf("exit = %d, stderr %q", code, stderr)
	}
	for _, want := range []string{"source=local", "source=web", "paused", "playing"} {
		if !strings.Contains(stderr, want) {
			t.Errorf("stderr lacks %q:\n%s", want, stderr)
		}
	}
}

func TestWithoutDebugNothingIsLogged(t *testing.T) {
	_, _, stderr := runOpts(t, []cli.Option{cli.WithBackend(&fakeBackend{np: playing()})})
	if stderr != "" {
		t.Errorf("stderr = %q, want empty", stderr)
	}
}

func TestDebugNotesIgnoredPlayer(t *testing.T) {
	opts := []cli.Option{cli.WithBackend(&fakeBackend{np: playing()})}
	_, _, stderr := runOpts(t, opts, "--debug", "--player", "spotifyd")
	got := strings.Contains(stderr, "player ignored")
	if want := runtime.GOOS != "linux"; got != want {
		t.Errorf("ignored-player note present = %v, want %v on %s:\n%s", got, want, runtime.GOOS, stderr)
	}
}
