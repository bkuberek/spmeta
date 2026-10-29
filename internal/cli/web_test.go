package cli_test

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/bkuberek/spmeta/internal/auth"
	"github.com/bkuberek/spmeta/internal/backend"
	"github.com/bkuberek/spmeta/internal/backend/macos"
	"github.com/bkuberek/spmeta/internal/cli"
	"github.com/bkuberek/spmeta/internal/model"
)

const (
	savedAccess  = "BQD-saved-access-token"
	savedRefresh = "AQD-saved-refresh-token"
	webVersion   = "1.2.3"

	// playerPlaying is a GET /v1/me/player body for a track on a phone.
	playerPlaying = `{"is_playing":true,"progress_ms":120165,"currently_playing_type":"track",` +
		`"device":{"name":"Phone","type":"Smartphone","volume_percent":50},` +
		`"item":{"type":"track","name":"Amazone","uri":"spotify:track:4brK3gpPjXArNUlU1hBXTe","duration_ms":238341,` +
		`"artists":[{"name":"Alvan"}],"album":{"name":"Home","artists":[{"name":"Alvan"}],"images":[]}}}`
)

// webSetup extends authSetup with a fake Web API and a clock that the
// injected sleep advances. Nothing leaves the process.
type webSetup struct {
	*authSetup
	api     *httptest.Server
	apiReqs atomic.Int32
	// reply answers the nth (1-based) API request; the default is playerPlaying.
	reply func(w http.ResponseWriter, n int)
	// now is the injected clock.
	now time.Time

	apiMu   sync.Mutex
	headers []http.Header
	targets []string
}

func newWebSetup(t *testing.T) *webSetup {
	t.Helper()
	s := &webSetup{authSetup: newAuthSetup(t), now: authNow}
	s.reply = func(w http.ResponseWriter, _ int) { respond(w, http.StatusOK, nil, playerPlaying) }
	s.api = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		s.apiMu.Lock()
		s.headers = append(s.headers, r.Header.Clone())
		s.targets = append(s.targets, r.URL.RequestURI())
		s.apiMu.Unlock()
		s.reply(w, int(s.apiReqs.Add(1)))
	}))
	t.Cleanup(s.api.Close)
	return s
}

// respond writes a canned response.
func respond(w http.ResponseWriter, status int, header map[string]string, body string) {
	for k, v := range header {
		w.Header().Set(k, v)
	}
	w.WriteHeader(status)
	_, _ = io.WriteString(w, body)
}

// saveLogin writes a token file whose access token expires in expiresIn.
func (s *webSetup) saveLogin(t *testing.T, expiresIn time.Duration) {
	t.Helper()
	err := auth.WriteToken(s.tokenPath(), auth.Token{
		ClientID: authClientID, AccessToken: savedAccess, RefreshToken: savedRefresh,
		Scope:        "user-read-playback-state",
		ExpiresAt:    authNow.Add(expiresIn),
		AuthorizedAt: authNow.Add(-24 * time.Hour),
	})
	if err != nil {
		t.Fatal(err)
	}
}

func (s *webSetup) options(local backend.Backend) []cli.Option {
	return []cli.Option{
		cli.WithBackend(local),
		cli.WithEnv(s.env()),
		cli.WithClock(func() time.Time { return s.now }),
		cli.WithAuthEndpoints(auth.Endpoints{Accounts: s.accounts.URL, API: s.api.URL}),
		cli.WithSleep(func(ctx context.Context, d time.Duration) error {
			s.waited = append(s.waited, d)
			s.now = s.now.Add(d)
			if s.sleep != nil {
				return s.sleep(ctx, d)
			}
			return nil
		}),
	}
}

// read runs the root command against the fake services.
func (s *webSetup) read(t *testing.T, local backend.Backend, args ...string) (code int, stdout, stderr string) {
	t.Helper()
	return s.readCtx(context.Background(), t, local, args...)
}

func (s *webSetup) readCtx(ctx context.Context, t *testing.T, local backend.Backend, args ...string) (code int, stdout, stderr string) {
	t.Helper()
	var out, errOut bytes.Buffer
	code = cli.Run(ctx, args, &out, &errOut, cli.BuildInfo{Version: webVersion}, s.options(local)...)
	return code, out.String(), errOut.String()
}

func (s *webSetup) doctor(t *testing.T, local backend.Backend, args ...string) (code int, stdout, stderr string) {
	t.Helper()
	return s.read(t, local, append([]string{"doctor"}, args...)...)
}

func (s *webSetup) requests() (api, accounts int) {
	return int(s.apiReqs.Load()), int(s.tokenReqs.Load())
}

// firstRequest returns the headers and target of the first API request.
func (s *webSetup) firstRequest(t *testing.T) (http.Header, string) {
	t.Helper()
	s.apiMu.Lock()
	defer s.apiMu.Unlock()
	if len(s.headers) == 0 {
		t.Fatal("the API received no request")
	}
	return s.headers[0], s.targets[0]
}

func pausedLocal() *fakeBackend {
	return &fakeBackend{np: playingOn(model.SourceMacOS, model.StatePaused)}
}

func TestWebAutoPrefersPlayingWebOverPausedLocal(t *testing.T) {
	s := newWebSetup(t)
	s.saveLogin(t, time.Hour)
	local := pausedLocal()
	code, stdout, stderr := s.read(t, local, "--format", "json")
	if code != 0 || stderr != "" {
		t.Fatalf("exit = %d, stderr %q, want 0 and empty", code, stderr)
	}
	for _, want := range []string{`"source":"web"`, `"state":"playing"`, `"title":"Amazone"`, `"name":"Phone"`} {
		if !strings.Contains(stdout, want) {
			t.Errorf("stdout lacks %q:\n%s", want, stdout)
		}
	}
	if local.calls != 1 {
		t.Errorf("local reads = %d, want 1", local.calls)
	}
	if api, accounts := s.requests(); api != 1 || accounts != 0 {
		t.Errorf("API requests = %d, token requests = %d, want 1 and 0 (the saved token is fresh)", api, accounts)
	}
	h, target := s.firstRequest(t)
	if got := h.Get("Authorization"); got != "Bearer "+savedAccess {
		t.Errorf("Authorization header = %q, want the saved access token", got)
	}
	if got, want := h.Get("User-Agent"), "spmeta/"+webVersion+" (+https://github.com/bkuberek/spmeta)"; got != want {
		t.Errorf("User-Agent = %q, want %q", got, want)
	}
	if want := "/v1/me/player?additional_types=episode"; target != want {
		t.Errorf("request target = %q, want %q", target, want)
	}
}

func TestWebLocalPlayingSkipsWeb(t *testing.T) {
	s := newWebSetup(t)
	s.saveLogin(t, time.Hour)
	code, stdout, _ := s.read(t, &fakeBackend{np: playing()}, "--format", "json")
	if code != 0 || !strings.Contains(stdout, `"source":"macos"`) {
		t.Errorf("exit = %d, stdout %q, want the local item", code, stdout)
	}
	if api, accounts := s.requests(); api+accounts != 0 {
		t.Errorf("requests = %d API, %d token, want none while local plays", api, accounts)
	}
}

func TestWebIsNeverCalledWithoutATokenFile(t *testing.T) {
	tests := []struct {
		name       string
		local      *fakeBackend
		args       []string
		wantCode   int
		wantStderr string
	}{
		{"auto with local paused returns local", pausedLocal(), nil, 0, ""},
		{"auto with local unavailable returns its error", &fakeBackend{err: errNotRunning}, nil, 4, "spmeta: Spotify is not running\n"},
		{"--source web says not logged in", pausedLocal(), []string{"--source", "web"}, 4, notLoggedIn},
		{"--source web in watch mode says it at once", pausedLocal(), []string{"--watch", "--source", "web"}, 4, notLoggedIn},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			s := newWebSetup(t)
			code, _, stderr := s.read(t, tc.local, tc.args...)
			if code != tc.wantCode || stderr != tc.wantStderr {
				t.Errorf("exit = %d, stderr %q, want %d and %q", code, stderr, tc.wantCode, tc.wantStderr)
			}
			if api, accounts := s.requests(); api+accounts != 0 {
				t.Errorf("requests = %d API, %d token, want none: Web is off without a token file", api, accounts)
			}
		})
	}
}

func TestWebExitCodes(t *testing.T) {
	tests := []struct {
		name     string
		prepare  func(t *testing.T, s *webSetup)
		reply    func(w http.ResponseWriter, n int)
		args     []string
		wantCode int
		wantErr  string
		wantAPI  int
	}{
		{
			name: "403 is permission denied",
			reply: func(w http.ResponseWriter, _ int) {
				respond(w, 403, nil, `{"error":{"status":403,"message":"Forbidden"}}`)
			},
			wantCode: 6, wantErr: "Spotify refused access (403)", wantAPI: 1,
		},
		{
			name:     "204 outranks an unavailable local source",
			reply:    func(w http.ResponseWriter, _ int) { respond(w, 204, nil, "") },
			wantCode: 3, wantErr: "nothing is playing", wantAPI: 1,
		},
		{
			name: "429 with a long Retry-After is rate limited",
			reply: func(w http.ResponseWriter, _ int) {
				respond(w, 429, map[string]string{"Retry-After": "30"}, `{}`)
			},
			wantCode: 7, wantErr: "rate limited by Spotify; retry after 30 s", wantAPI: 1,
		},
		{
			name:     "5xx is retried once, then unexpected",
			reply:    func(w http.ResponseWriter, _ int) { respond(w, 500, nil, `{}`) },
			wantCode: 1, wantErr: "unexpected HTTP status 500", wantAPI: 2,
		},
		{
			name:     "a second 401 needs a new login",
			reply:    func(w http.ResponseWriter, _ int) { respond(w, 401, nil, `{}`) },
			wantCode: 5, wantErr: "Spotify rejected the access token even after a refresh", wantAPI: 2,
		},
		{
			name: "a refused refresh needs a new login",
			prepare: func(t *testing.T, s *webSetup) {
				s.saveLogin(t, -time.Hour)
				s.tokenReply = func(w http.ResponseWriter, _ *http.Request) {
					respond(w, 400, nil, `{"error":"invalid_grant","error_description":"Refresh token revoked"}`)
				}
			},
			wantCode: 5, wantErr: "hint: Run `spmeta auth login`.", wantAPI: 0,
		},
		{
			name: "an unreadable token file needs a new login",
			prepare: func(t *testing.T, s *webSetup) {
				writeFile(t, s.tokenPath(), "not json")
			},
			args:     []string{"--source", "web"},
			wantCode: 5, wantErr: "hint: Run `spmeta auth login`.", wantAPI: 0,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			s := newWebSetup(t)
			if tc.prepare != nil {
				tc.prepare(t, s)
			} else {
				s.saveLogin(t, time.Hour)
			}
			if tc.reply != nil {
				s.reply = tc.reply
			}
			code, stdout, stderr := s.read(t, &fakeBackend{err: errNotRunning}, tc.args...)
			if code != tc.wantCode {
				t.Errorf("exit = %d, want %d (stderr %q)", code, tc.wantCode, stderr)
			}
			if !strings.Contains(stderr, tc.wantErr) {
				t.Errorf("stderr = %q, want it to contain %q", stderr, tc.wantErr)
			}
			if tc.wantCode != 3 && stdout != "" {
				t.Errorf("stdout = %q, want empty", stdout)
			}
			if api, _ := s.requests(); api != tc.wantAPI {
				t.Errorf("API requests = %d, want %d", api, tc.wantAPI)
			}
		})
	}
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestWebRefreshesAnExpiredTokenWithTheSavedClientID(t *testing.T) {
	s := newWebSetup(t)
	s.saveLogin(t, -time.Minute)
	s.vars["SPMETA_CLIENT_ID"] = "ffffffffffffffffffffffffffffffff"
	code, stdout, stderr := s.read(t, pausedLocal(), "--source", "web", "--format", "json")
	if code != 0 {
		t.Fatalf("exit = %d, stderr %q", code, stderr)
	}
	if !strings.Contains(stdout, `"source":"web"`) {
		t.Errorf("stdout = %q, want the Web item", stdout)
	}
	if got := s.lastForm().Get("client_id"); got != authClientID {
		t.Errorf("refresh client_id = %q, want the one saved at login", got)
	}
	if h, _ := s.firstRequest(t); h.Get("Authorization") != "Bearer "+authAccess {
		t.Errorf("Authorization header = %q, want the refreshed access token", h.Get("Authorization"))
	}
}

func TestWebOneShotWaitsOutARetryAfterOfTenSeconds(t *testing.T) {
	s := newWebSetup(t)
	s.saveLogin(t, time.Hour)
	s.reply = func(w http.ResponseWriter, n int) {
		if n == 1 {
			respond(w, 429, map[string]string{"Retry-After": "10"}, `{}`)
			return
		}
		respond(w, 200, nil, playerPlaying)
	}
	code, stdout, stderr := s.read(t, pausedLocal(), "--source", "web", "--format", "json")
	if code != 0 || !strings.Contains(stdout, `"source":"web"`) {
		t.Fatalf("exit = %d, stdout %q, stderr %q, want the Web item", code, stdout, stderr)
	}
	if api, _ := s.requests(); api != 2 {
		t.Errorf("API requests = %d, want 2", api)
	}
	if len(s.waited) != 1 || s.waited[0] != 10*time.Second {
		t.Errorf("waited %v, want one wait of 10s", s.waited)
	}
}

func TestWebOneShotGivesUpOnARetryAfterOfElevenSeconds(t *testing.T) {
	s := newWebSetup(t)
	s.saveLogin(t, time.Hour)
	s.reply = func(w http.ResponseWriter, _ int) { respond(w, 429, map[string]string{"Retry-After": "11"}, `{}`) }
	code, _, stderr := s.read(t, pausedLocal(), "--source", "web")
	if code != 7 || !strings.Contains(stderr, "retry after 11 s") {
		t.Errorf("exit = %d, stderr %q, want 7 naming the wait", code, stderr)
	}
	if api, _ := s.requests(); api != 1 || len(s.waited) != 0 {
		t.Errorf("API requests = %d, waits %v, want 1 and none", api, s.waited)
	}
}

// Watch mode leaves the wait to the watch loop. A sleep that fails would make
// a Web read that waits inside report "read cancelled"; the loop ignores it.
func TestWebWatchLeavesRateLimitWaitsToTheLoop(t *testing.T) {
	s := newWebSetup(t)
	s.saveLogin(t, time.Hour)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	s.sleep = func(context.Context, time.Duration) error { return errors.New("sleep failed") }
	s.reply = func(w http.ResponseWriter, n int) {
		if n == 1 {
			respond(w, 429, map[string]string{"Retry-After": "2"}, `{}`)
			return
		}
		respond(w, 200, nil, playerPlaying)
		if n == 3 {
			cancel()
		}
	}
	code, stdout, stderr := s.readCtx(ctx, t, pausedLocal(), "--watch", "--source", "web")
	if code != 0 {
		t.Fatalf("exit = %d, stderr %q", code, stderr)
	}
	if stderr != "" {
		t.Errorf("stderr = %q, want empty: the read waited inside itself", stderr)
	}
	if want := "▶ Amazone — Alvan · Home (2:00 / 3:58)\n"; stdout != want {
		t.Errorf("stdout = %q, want one line %q", stdout, want)
	}
	if len(s.waited) == 0 || s.waited[0] != 2*time.Second {
		t.Errorf("waited %v, want the loop to sleep the Retry-After first", s.waited)
	}
}

func TestWebWatchAutoThrottlesWebToItsInterval(t *testing.T) {
	s := newWebSetup(t)
	s.saveLogin(t, time.Hour)
	s.reply = func(w http.ResponseWriter, _ int) { respond(w, 204, nil, "") }
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	// Local reads happen at 0 s, 1 s, 2 s and 3 s of the injected clock; the
	// 3 s web_interval allows Web reads at 0 s and 3 s only.
	local := &scriptedBackend{src: model.SourceMacOS, cancel: cancel, steps: []wstep{
		titled("A", model.StatePaused), titled("A", model.StatePaused),
		titled("A", model.StatePaused), titled("A", model.StatePaused),
	}}
	code, _, stderr := s.readCtx(ctx, t, local, "--watch")
	if code != 0 {
		t.Fatalf("exit = %d, stderr %q", code, stderr)
	}
	if api, _ := s.requests(); api != 2 {
		t.Errorf("API requests = %d over 4 local reads, want 2", api)
	}
}

func TestWebDebugNotesAClientIDThatDiffersFromTheSavedOne(t *testing.T) {
	const configured = "ffffffffffffffffffffffffffffffff"
	tests := []struct {
		name     string
		clientID string
		args     []string
		want     bool
	}{
		{"different", configured, []string{"--debug"}, true},
		{"same", authClientID, []string{"--debug"}, false},
		{"none configured", "", []string{"--debug"}, false},
		{"without --debug", configured, nil, false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			s := newWebSetup(t)
			s.saveLogin(t, time.Hour)
			s.vars["SPMETA_CLIENT_ID"] = tc.clientID
			_, _, stderr := s.read(t, pausedLocal(), tc.args...)
			got := strings.Contains(stderr, "differs from the saved login")
			if got != tc.want {
				t.Fatalf("note present = %v, want %v:\n%s", got, tc.want, stderr)
			}
			if tc.want {
				for _, id := range []string{configured, authClientID} {
					if !strings.Contains(stderr, id) {
						t.Errorf("note lacks client ID %s:\n%s", id, stderr)
					}
				}
			}
			for _, secret := range []string{savedAccess, savedRefresh} {
				if strings.Contains(stderr, secret) {
					t.Errorf("stderr leaks a token:\n%s", stderr)
				}
			}
		})
	}
}

func TestDoctorWebReport(t *testing.T) {
	const notRunning = macos.PermissionSpotifyNotRunning
	tests := []struct {
		name     string
		prepare  func(t *testing.T, s *webSetup)
		reply    func(w http.ResponseWriter, n int)
		mac      macos.PermissionState
		wantCode int
		want     string
		wantAPI  int
	}{
		{
			name: "logged in and reachable", mac: macos.PermissionGranted, wantCode: 0, wantAPI: 1,
			want: "web     ok: logged in; the Spotify Web API answered\n        refresh token expires about 2027-04-27\n",
		},
		{
			name:  "logged in, no active device",
			reply: func(w http.ResponseWriter, _ int) { respond(w, 204, nil, "") },
			mac:   macos.PermissionGranted, wantCode: 0, wantAPI: 1,
			want: "web     ok: logged in; the Spotify Web API answered, but no device is active\n",
		},
		{
			name: "web alone is a usable source", mac: notRunning, wantCode: 0, wantAPI: 1,
			want: "web     ok: ",
		},
		{
			name: "login expired",
			prepare: func(t *testing.T, s *webSetup) {
				s.saveLogin(t, -time.Hour)
				s.tokenReply = func(w http.ResponseWriter, _ *http.Request) {
					respond(w, 400, nil, `{"error":"invalid_grant"}`)
				}
			},
			mac: macos.PermissionGranted, wantCode: 0, wantAPI: 0,
			want: "web     error: Spotify rejected the saved login (refresh tokens expire 6 months after login, or access was revoked). Run `spmeta auth login`.\n",
		},
		{
			name:  "forbidden",
			reply: func(w http.ResponseWriter, _ int) { respond(w, 403, nil, `{}`) },
			mac:   macos.PermissionGranted, wantCode: 0, wantAPI: 1,
			want: "web     denied: Spotify refused access (403). Your account may not be on the app's user allowlist",
		},
		{
			name:  "server error",
			reply: func(w http.ResponseWriter, _ int) { respond(w, 500, nil, `{}`) },
			mac:   macos.PermissionGranted, wantCode: 0, wantAPI: 2,
			want: "web     error: unexpected HTTP status 500 from the Spotify Web API\n",
		},
		{
			name:  "web failing and local unavailable",
			reply: func(w http.ResponseWriter, _ int) { respond(w, 403, nil, `{}`) },
			mac:   notRunning, wantCode: 4, wantAPI: 1,
			want: "web     denied: ",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			s := newWebSetup(t)
			if tc.prepare != nil {
				tc.prepare(t, s)
			} else {
				s.saveLogin(t, time.Hour)
			}
			if tc.reply != nil {
				s.reply = tc.reply
			}
			mac := &macFake{result: macos.PermissionResult{State: tc.mac}}
			code, stdout, stderr := s.doctor(t, mac)
			if code != tc.wantCode {
				t.Errorf("exit = %d, want %d (stderr %q)", code, tc.wantCode, stderr)
			}
			if !strings.Contains(stdout, tc.want) {
				t.Errorf("stdout lacks %q:\n%s", tc.want, stdout)
			}
			if api, _ := s.requests(); api != tc.wantAPI {
				t.Errorf("API requests = %d, want %d: doctor makes at most one Web read", api, tc.wantAPI)
			}
		})
	}
}

func TestDoctorWebWarnsAboutAWideTokenFile(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Go does not report meaningful permission bits on Windows")
	}
	s := newWebSetup(t)
	s.saveLogin(t, time.Hour)
	if err := os.Chmod(s.tokenPath(), 0o644); err != nil {
		t.Fatal(err)
	}
	_, stdout, _ := s.doctor(t, grantedMac())
	want := "        other users can access the token file. Run: chmod 600 '" + s.tokenPath() + "'\n"
	if !strings.Contains(stdout, want) {
		t.Errorf("stdout lacks %q:\n%s", want, stdout)
	}
}

func TestDoctorWebNeverPrintsTheTrack(t *testing.T) {
	s := newWebSetup(t)
	s.saveLogin(t, time.Hour)
	_, stdout, stderr := s.doctor(t, grantedMac(), "--debug")
	for _, leak := range []string{"Amazone", "Alvan", "Phone"} {
		if strings.Contains(stdout, leak) || strings.Contains(stderr, leak) {
			t.Errorf("doctor printed %q from the current track:\n%s\n%s", leak, stdout, stderr)
		}
	}
}

func TestWebPollIntervalIsStillWebIntervalForSourceWeb(t *testing.T) {
	s := newWebSetup(t)
	s.saveLogin(t, time.Hour)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	s.reply = func(w http.ResponseWriter, n int) {
		respond(w, 200, nil, playerPlaying)
		if n == 2 {
			cancel()
		}
	}
	code, _, stderr := s.readCtx(ctx, t, pausedLocal(), "--watch", "--source", "web")
	if code != 0 {
		t.Fatalf("exit = %d, stderr %q", code, stderr)
	}
	if len(s.waited) == 0 || s.waited[0] != 3*time.Second {
		t.Errorf("waited %v, want web_interval (3s) between Web reads", s.waited)
	}
}
