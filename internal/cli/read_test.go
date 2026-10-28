package cli_test

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/bkuberek/spmeta/internal/backend"
	"github.com/bkuberek/spmeta/internal/cli"
	"github.com/bkuberek/spmeta/internal/config"
	"github.com/bkuberek/spmeta/internal/model"
)

const usageHint = "Run 'spmeta --help' for usage."

type fakeBackend struct {
	np    model.NowPlaying
	err   error
	calls int
}

func (f *fakeBackend) Source() model.Source { return model.SourceMacOS }

func (f *fakeBackend) Read(context.Context) (model.NowPlaying, error) {
	f.calls++
	return f.np, f.err
}

func ptr[T any](v T) *T { return &v }

func playing() model.NowPlaying {
	return model.NowPlaying{
		SchemaVersion: 1,
		Source:        model.SourceMacOS,
		State:         model.StatePlaying,
		Type:          model.ItemTrack,
		Title:         ptr("Amazone"),
		Artists:       []string{"Alvan"},
		Artist:        ptr("Alvan"),
		Album:         ptr("Home"),
		DurationMS:    ptr(int64(238341)),
		PositionMS:    ptr(int64(120165)),
		FetchedAt:     time.Date(2026, 10, 27, 17, 40, 0, 0, time.UTC),
	}
}

func runWith(t *testing.T, b backend.Backend, args ...string) (code int, stdout, stderr string) {
	t.Helper()
	var out, errOut bytes.Buffer
	code = cli.Run(context.Background(), args, &out, &errOut, cli.BuildInfo{Version: "dev"}, cli.WithBackend(b), cli.WithEnv(config.Env{}))
	return code, out.String(), errOut.String()
}

func TestReadFormats(t *testing.T) {
	tests := []struct {
		name string
		args []string
		want string
	}{
		{"default text", nil, "▶ Amazone — Alvan · Home (2:00 / 3:58)\n"},
		{"explicit text", []string{"--format", "text"}, "▶ Amazone — Alvan · Home (2:00 / 3:58)\n"},
		{"single dash", []string{"-format=text"}, "▶ Amazone — Alvan · Home (2:00 / 3:58)\n"},
		{"template format", []string{"--format", "template", "--template", "{artist} - {title}"}, "Alvan - Amazone\n"},
		{"template alone selects template", []string{"--template", "{state_icon} {title}"}, "▶ Amazone\n"},
		{"explicit json beats template", []string{"--format", "json", "--template", "{title}"}, ""},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			fake := &fakeBackend{np: playing()}
			code, stdout, stderr := runWith(t, fake, tc.args...)
			if code != 0 {
				t.Fatalf("exit = %d, want 0 (stderr %q)", code, stderr)
			}
			if stderr != "" {
				t.Errorf("stderr = %q, want empty", stderr)
			}
			if tc.want != "" && stdout != tc.want {
				t.Errorf("stdout = %q, want %q", stdout, tc.want)
			}
			if fake.calls != 1 {
				t.Errorf("backend calls = %d, want 1", fake.calls)
			}
		})
	}
}

func TestReadJSON(t *testing.T) {
	fake := &fakeBackend{np: playing()}
	code, stdout, _ := runWith(t, fake, "--format", "json")
	if code != 0 {
		t.Fatalf("exit = %d, want 0", code)
	}
	if !strings.HasPrefix(stdout, `{"schema_version":1,"source":"macos","state":"playing"`) ||
		strings.Count(stdout, "\n") != 1 || !strings.HasSuffix(stdout, "}\n") {
		t.Errorf("stdout = %q, want one JSON line", stdout)
	}
}

func TestReadErrorKinds(t *testing.T) {
	tests := []struct {
		name       string
		err        error
		code       int
		stderr     string
		jsonStdout string
	}{
		{"unexpected", &backend.Error{Kind: backend.KindUnexpected, Message: "cannot parse output", Hint: "run `spmeta doctor`"}, 1, "spmeta: cannot parse output\nhint: run `spmeta doctor`\n", ""},
		{"nothing playing", &backend.Error{Kind: backend.KindNothingPlaying, Message: "nothing is playing"}, 3, "spmeta: nothing is playing\n", "null\n"},
		{"not available", &backend.Error{Kind: backend.KindNotAvailable, Message: "Spotify is not running"}, 4, "spmeta: Spotify is not running\n", ""},
		{"auth required", &backend.Error{Kind: backend.KindAuthRequired, Message: "login expired", Hint: "run `spmeta auth login`"}, 5, "spmeta: login expired\nhint: run `spmeta auth login`\n", ""},
		{"permission denied", &backend.Error{Kind: backend.KindPermissionDenied, Message: "Automation not granted", Hint: "allow it in System Settings"}, 6, "spmeta: Automation not granted\nhint: allow it in System Settings\n", ""},
		{"rate limited", &backend.Error{Kind: backend.KindRateLimited, Message: "rate limited", RetryAfter: 30 * time.Second}, 7, "spmeta: rate limited\n", ""},
		{"untyped error", errors.New("boom"), 1, "spmeta: boom\n", ""},
		{"wrapped typed error", wrap(&backend.Error{Kind: backend.KindNotAvailable, Message: "no player"}), 4, "spmeta: no player\n", ""},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			code, stdout, stderr := runWith(t, &fakeBackend{err: tc.err})
			if code != tc.code {
				t.Errorf("exit = %d, want %d", code, tc.code)
			}
			if stdout != "" {
				t.Errorf("text stdout = %q, want empty", stdout)
			}
			if stderr != tc.stderr {
				t.Errorf("stderr = %q, want %q", stderr, tc.stderr)
			}

			code, stdout, _ = runWith(t, &fakeBackend{err: tc.err}, "--format", "json")
			if code != tc.code {
				t.Errorf("json exit = %d, want %d", code, tc.code)
			}
			if stdout != tc.jsonStdout {
				t.Errorf("json stdout = %q, want %q", stdout, tc.jsonStdout)
			}
		})
	}
}

func wrap(err error) error { return fmt.Errorf("reading: %w", err) }

func TestReadNothingPlayingTemplateIsEmpty(t *testing.T) {
	fake := &fakeBackend{err: &backend.Error{Kind: backend.KindNothingPlaying, Message: "nothing is playing"}}
	code, stdout, _ := runWith(t, fake, "--template", "{artist} - {title}")
	if code != 3 || stdout != "" {
		t.Errorf("exit = %d, stdout = %q, want 3 and empty", code, stdout)
	}
}

func TestQuietSilencesEverythingButUsageErrors(t *testing.T) {
	t.Run("success prints nothing", func(t *testing.T) {
		for _, args := range [][]string{{"--quiet"}, {"--quiet", "--format", "json"}} {
			code, stdout, stderr := runWith(t, &fakeBackend{np: playing()}, args...)
			if code != 0 || stdout != "" || stderr != "" {
				t.Errorf("%v: exit=%d stdout=%q stderr=%q, want 0 and nothing", args, code, stdout, stderr)
			}
		}
	})
	t.Run("errors keep exit code only", func(t *testing.T) {
		err := &backend.Error{Kind: backend.KindPermissionDenied, Message: "denied", Hint: "grant it"}
		code, stdout, stderr := runWith(t, &fakeBackend{err: err}, "--quiet", "--debug")
		if code != 6 || stdout != "" || stderr != "" {
			t.Errorf("exit=%d stdout=%q stderr=%q, want 6 and nothing", code, stdout, stderr)
		}
	})
	t.Run("nothing playing json is silent", func(t *testing.T) {
		err := &backend.Error{Kind: backend.KindNothingPlaying, Message: "nothing is playing"}
		code, stdout, stderr := runWith(t, &fakeBackend{err: err}, "--quiet", "--format", "json")
		if code != 3 || stdout != "" || stderr != "" {
			t.Errorf("exit=%d stdout=%q stderr=%q, want 3 and nothing", code, stdout, stderr)
		}
	})
	t.Run("usage errors are still printed", func(t *testing.T) {
		code, _, stderr := runWith(t, &fakeBackend{}, "--quiet", "--format", "xml")
		if code != 2 || !strings.Contains(stderr, usageHint) {
			t.Errorf("exit=%d stderr=%q, want 2 and usage hint", code, stderr)
		}
	})
	t.Run("no backend message is silenced", func(t *testing.T) {
		code, stdout, stderr := run(t, "--quiet")
		if code != 4 || stdout != "" || stderr != "" {
			t.Errorf("exit=%d stdout=%q stderr=%q, want 4 and nothing", code, stdout, stderr)
		}
	})
}

func TestUsageErrorsExitBeforeAnyRead(t *testing.T) {
	tests := []struct {
		name string
		args []string
		want string
	}{
		{"unknown template field", []string{"--template", "{nope}"}, `unknown field "nope" at position 1`},
		{"unclosed brace", []string{"--format", "template", "--template", "a {title"}, "unclosed '{' at position 3"},
		{"stray close", []string{"--template", "a}b"}, "unmatched '}' at position 2"},
		{"template format without template", []string{"--format", "template"}, "--template"},
		{"empty template", []string{"--format", "template", "--template", ""}, "--template"},
		{"invalid format", []string{"--format", "xml"}, `"xml"`},
		{"positional argument", []string{"extra"}, "extra"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			fake := &fakeBackend{np: playing()}
			code, stdout, stderr := runWith(t, fake, tc.args...)
			if code != 2 {
				t.Errorf("exit = %d, want 2", code)
			}
			if fake.calls != 0 {
				t.Errorf("backend read %d times, want 0", fake.calls)
			}
			if stdout != "" {
				t.Errorf("stdout = %q, want empty", stdout)
			}
			if !strings.Contains(stderr, tc.want) || !strings.HasSuffix(stderr, usageHint+"\n") {
				t.Errorf("stderr = %q, want %q and usage hint", stderr, tc.want)
			}
		})
	}
}

func TestInvalidTemplateBeatsMissingBackend(t *testing.T) {
	code, _, stderr := run(t, "--template", "{nope}")
	if code != 2 || !strings.Contains(stderr, "nope") {
		t.Errorf("exit=%d stderr=%q, want usage error 2", code, stderr)
	}
}

func TestDebugAddsCauseToStderr(t *testing.T) {
	err := &backend.Error{
		Kind: backend.KindUnexpected, Message: "cannot read Spotify",
		Err: errors.New("osascript exited with status 1"),
	}
	_, _, plain := runWith(t, &fakeBackend{err: err})
	if strings.Contains(plain, "osascript") {
		t.Errorf("stderr without --debug = %q, want no cause", plain)
	}
	_, stdout, debug := runWith(t, &fakeBackend{err: err}, "--debug")
	if !strings.Contains(debug, "spmeta: cannot read Spotify") || !strings.Contains(debug, "osascript exited with status 1") {
		t.Errorf("stderr with --debug = %q, want message and cause", debug)
	}
	if stdout != "" {
		t.Errorf("stdout = %q, want empty", stdout)
	}
}

func TestDebugOnSuccessKeepsStdoutClean(t *testing.T) {
	code, stdout, stderr := runWith(t, &fakeBackend{np: playing()}, "--debug")
	if code != 0 || stdout != "▶ Amazone — Alvan · Home (2:00 / 3:58)\n" {
		t.Errorf("exit=%d stdout=%q", code, stdout)
	}
	if !strings.Contains(stderr, "macos") {
		t.Errorf("stderr = %q, want a diagnostic naming the source", stderr)
	}
}

func TestHelpListsReadFlags(t *testing.T) {
	_, stdout, _ := run(t, "--help")
	for _, want := range []string{"--format", "--template", "--quiet", "--debug"} {
		if !strings.Contains(stdout, want) {
			t.Errorf("usage missing %s", want)
		}
	}
}
