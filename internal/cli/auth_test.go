package cli_test

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"io/fs"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/bkuberek/spmeta/internal/auth"
	"github.com/bkuberek/spmeta/internal/cli"
	"github.com/bkuberek/spmeta/internal/config"
)

const (
	authClientID = "0123456789abcdef0123456789abcdef"
	authCode     = "AQD-authorization-code"
	authAccess   = "BQD-new-access-token"
	authRefresh  = "AQD-new-refresh-token"
	notLoggedIn  = "spmeta: not logged in to the Spotify Web API\nhint: Run `spmeta auth login`.\n"
)

// authNow is the injected clock of the auth tests.
var authNow = time.Date(2026, 10, 28, 9, 0, 0, 0, time.UTC)

// authSetup is a home directory for the token file, a fake accounts service
// and a browser that follows the authorization URL back to the callback.
type authSetup struct {
	home      string
	vars      map[string]string
	files     map[string]string
	accounts  *httptest.Server
	tokenReqs atomic.Int32
	mu        sync.Mutex
	form      url.Values // the last token request
	visit     func(t *testing.T, authURL string)
	opened    []string
	sleep     func(context.Context, time.Duration) error
	waited    []time.Duration
	// realDefaultPort leaves the login port at its production default;
	// otherwise run swaps in a free port so parallel tests do not collide.
	realDefaultPort bool
}

func newAuthSetup(t *testing.T) *authSetup {
	t.Helper()
	s := &authSetup{home: t.TempDir(), vars: map[string]string{}, files: map[string]string{}}
	s.accounts = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		s.mu.Lock()
		s.form = r.PostForm
		s.mu.Unlock()
		s.tokenReqs.Add(1)
		_, _ = io.WriteString(w, `{"access_token":"`+authAccess+`","token_type":"Bearer","expires_in":3600,"refresh_token":"`+authRefresh+`","scope":"user-read-playback-state"}`)
	}))
	t.Cleanup(s.accounts.Close)
	s.visit = func(t *testing.T, authURL string) { callAuth(t, authURL, url.Values{"code": {authCode}}) }
	return s
}

func (s *authSetup) lastForm() url.Values {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.form
}

func (s *authSetup) tokenPath() string {
	return filepath.Join(s.home, ".config", "spmeta", "token.json")
}

func (s *authSetup) env() config.Env {
	return config.Env{
		Home:   s.home,
		Getenv: func(k string) string { return s.vars[k] },
		ReadFile: func(p string) ([]byte, error) {
			if c, ok := s.files[p]; ok {
				return []byte(c), nil
			}
			return nil, &fs.PathError{Op: "open", Path: p, Err: fs.ErrNotExist}
		},
	}
}

// run executes spmeta with the fake accounts service and browser.
func (s *authSetup) run(t *testing.T, args ...string) (code int, stdout, stderr string) {
	t.Helper()
	var out, errOut bytes.Buffer
	opts := []cli.Option{
		cli.WithEnv(s.env()),
		cli.WithClock(func() time.Time { return authNow }),
		cli.WithAuthEndpoints(auth.Endpoints{Accounts: s.accounts.URL}),
		cli.WithBrowserOpener(func(u string) error {
			s.opened = append(s.opened, u)
			s.visit(t, u)
			return nil
		}),
	}
	if !s.realDefaultPort {
		opts = append(opts, cli.WithDefaultLoginPort(freeLoginPort(t)))
	}
	if s.sleep != nil {
		opts = append(opts, cli.WithSleep(func(ctx context.Context, d time.Duration) error {
			s.waited = append(s.waited, d)
			return s.sleep(ctx, d)
		}))
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	code = cli.Run(ctx, append([]string{"auth"}, args...), &out, &errOut, cli.BuildInfo{Version: "dev"}, opts...)
	return code, out.String(), errOut.String()
}

func freeLoginPort(t *testing.T) int {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = ln.Close() }()
	return ln.Addr().(*net.TCPAddr).Port
}

// callAuth plays the browser: it follows the redirect URI of authURL with params.
func callAuth(t *testing.T, authURL string, params url.Values) {
	t.Helper()
	u, err := url.Parse(authURL)
	if err != nil {
		t.Errorf("authorization URL: %v", err)
		return
	}
	q := u.Query()
	target, err := url.Parse(q.Get("redirect_uri"))
	if err != nil {
		t.Errorf("redirect_uri: %v", err)
		return
	}
	params.Set("state", q.Get("state"))
	target.RawQuery = params.Encode()
	resp, err := http.Get(target.String())
	if err != nil {
		t.Errorf("callback: %v", err)
		return
	}
	_ = resp.Body.Close()
}

func (s *authSetup) writeToken(t *testing.T, mutate func(*auth.Token)) {
	t.Helper()
	tok := auth.Token{
		ClientID:     authClientID,
		AccessToken:  "AQD-stored-access-token",
		RefreshToken: "AQD-stored-refresh-token",
		Scope:        "user-read-playback-state",
		ExpiresAt:    time.Date(2026, 10, 27, 18, 40, 0, 0, time.UTC),
		AuthorizedAt: time.Date(2026, 10, 27, 17, 40, 0, 0, time.UTC),
	}
	if mutate != nil {
		mutate(&tok)
	}
	if err := auth.WriteToken(s.tokenPath(), tok); err != nil {
		t.Fatal(err)
	}
}

func TestAuthLoginWithoutClientIDPrintsSetup(t *testing.T) {
	t.Parallel()
	s := newAuthSetup(t)
	code, stdout, stderr := s.run(t, "login")
	if code != 2 {
		t.Fatalf("exit = %d, want 2", code)
	}
	if stdout != "" {
		t.Errorf("stdout = %q, want empty", stdout)
	}
	for _, want := range []string{
		"no Spotify Client ID",
		"https://developer.spotify.com/dashboard",
		"http://127.0.0.1:8974/callback",
		"even if it is the account that owns the app",
		"spmeta auth login --client-id",
		"client_id",
	} {
		if !strings.Contains(stderr, want) {
			t.Errorf("stderr lacks %q:\n%s", want, stderr)
		}
	}
	if len(s.opened) != 0 || s.tokenReqs.Load() != 0 {
		t.Errorf("a login without a Client ID opened %v and sent %d token requests", s.opened, s.tokenReqs.Load())
	}
}

func TestAuthLoginClientIDSources(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name  string
		setup func(*authSetup)
		args  []string
	}{
		{"flag", func(*authSetup) {}, []string{"--client-id", authClientID}},
		{"environment", func(s *authSetup) { s.vars["SPMETA_CLIENT_ID"] = authClientID }, nil},
		{"configuration file", func(s *authSetup) {
			s.files[filepath.Join(s.home, ".config", "spmeta", "config.toml")] = `client_id = "` + authClientID + `"` + "\n"
		}, nil},
		{"surrounding whitespace is dropped", func(s *authSetup) { s.vars["SPMETA_CLIENT_ID"] = " " + authClientID + "\n" }, nil},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			s := newAuthSetup(t)
			tc.setup(s)
			code, _, stderr := s.run(t, append([]string{"login"}, tc.args...)...)
			if code != 0 {
				t.Fatalf("exit = %d, stderr %q", code, stderr)
			}
			tok, err := auth.ReadToken(s.tokenPath())
			if err != nil || tok.ClientID != authClientID {
				t.Errorf("stored token = %v, %v; want the Client ID %s", tok, err, authClientID)
			}
		})
	}
}

func TestAuthLoginDefaultsToPort8974(t *testing.T) {
	// Not parallel: it binds the fixed default port.
	ln, err := net.Listen("tcp4", "127.0.0.1:8974")
	if err != nil {
		t.Skipf("port 8974 is not free: %v", err)
	}
	_ = ln.Close()
	s := newAuthSetup(t)
	s.realDefaultPort = true
	code, _, stderr := s.run(t, "login", "--client-id", authClientID)
	if code != 0 {
		t.Fatalf("exit = %d, stderr %q", code, stderr)
	}
	if len(s.opened) != 1 || !strings.Contains(s.opened[0], url.QueryEscape("http://127.0.0.1:8974/callback")) {
		t.Errorf("opened %v, want a redirect_uri of http://127.0.0.1:8974/callback", s.opened)
	}
}

func TestAuthLoginSuccess(t *testing.T) {
	t.Parallel()
	s := newAuthSetup(t)
	code, stdout, stderr := s.run(t, "login", "--client-id", authClientID)
	if code != 0 {
		t.Fatalf("exit = %d, stderr %q", code, stderr)
	}
	if want := "Logged in. Token stored at " + s.tokenPath() + ".\n"; stdout != want {
		t.Errorf("stdout = %q, want %q", stdout, want)
	}
	if len(s.opened) != 1 || stderr != "Open this URL to log in:\n"+s.opened[0]+"\n" {
		t.Errorf("stderr = %q, want the prompt and the URL %v", stderr, s.opened)
	}
	tok, err := auth.ReadToken(s.tokenPath())
	if err != nil || tok.AccessToken != authAccess || tok.RefreshToken != authRefresh || !tok.AuthorizedAt.Equal(authNow) {
		t.Errorf("stored token = %v, %v; want the exchanged one authorized at %v", tok, err, authNow)
	}
	if runtime.GOOS != "windows" {
		if info, err := os.Stat(s.tokenPath()); err != nil || info.Mode().Perm() != 0o600 {
			t.Errorf("token file = %v, %v; want mode 0600", info, err)
		}
	}
	if got := s.lastForm().Get("client_id"); got != authClientID {
		t.Errorf("token request client_id = %q, want %q", got, authClientID)
	}
	if _, err := os.Stat(filepath.Join(s.home, ".config", "spmeta", "config.toml")); err == nil {
		t.Error("login wrote config.toml; the Client ID belongs in the token file only")
	}
}

func TestAuthLoginKeepsSecretsOutOfOutput(t *testing.T) {
	t.Parallel()
	s := newAuthSetup(t)
	code, stdout, stderr := s.run(t, "login", "--client-id", authClientID, "--debug")
	if code != 0 {
		t.Fatalf("exit = %d, stderr %q", code, stderr)
	}
	secrets := []string{authCode, authAccess, authRefresh, s.lastForm().Get("code_verifier")}
	if secrets[3] == "" {
		t.Fatal("the token request carried no code_verifier")
	}
	for name, out := range map[string]string{"stdout": stdout, "stderr": stderr} {
		for _, secret := range secrets {
			if strings.Contains(out, secret) {
				t.Errorf("%s contains the secret %q:\n%s", name, secret, out)
			}
		}
	}
	if !strings.Contains(stderr, "/api/token") {
		t.Errorf("stderr lacks the debug line for the token request:\n%s", stderr)
	}
}

func TestAuthLoginFailures(t *testing.T) {
	t.Parallel()
	busy, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = busy.Close() })
	busyPort := busy.Addr().(*net.TCPAddr).Port

	tests := []struct {
		name       string
		setup      func(*authSetup)
		args       []string
		wantCode   int
		wantStderr string
		wantOpened bool
	}{
		{"access denied", func(s *authSetup) {
			s.visit = func(t *testing.T, u string) { callAuth(t, u, url.Values{"error": {"access_denied"}}) }
		}, nil, 5, "spmeta: Spotify authorization failed: access_denied\n", true},
		{"no answer within 180 s", func(s *authSetup) {
			s.visit = func(*testing.T, string) {}
			s.sleep = func(context.Context, time.Duration) error { return nil }
		}, nil, 5, "spmeta: no response from the browser within 180 s.\n", true},
		{"no browser", func(s *authSetup) {
			s.sleep = func(context.Context, time.Duration) error { return nil }
		}, []string{"--no-browser"}, 5, "spmeta: no response from the browser within 180 s.\n", false},
		{"port in use", nil, []string{"--port", fmt.Sprint(busyPort)}, 1, fmt.Sprintf("spmeta: port %d is in use.\n", busyPort), false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			s := newAuthSetup(t)
			if tc.setup != nil {
				tc.setup(s)
			}
			code, stdout, stderr := s.run(t, append([]string{"login", "--client-id", authClientID}, tc.args...)...)
			if code != tc.wantCode {
				t.Errorf("exit = %d, want %d (stderr %q)", code, tc.wantCode, stderr)
			}
			if stdout != "" {
				t.Errorf("stdout = %q, want empty", stdout)
			}
			if !strings.HasSuffix(stderr, tc.wantStderr) {
				t.Errorf("stderr = %q, want it to end with %q", stderr, tc.wantStderr)
			}
			for _, d := range s.waited {
				if d != 180*time.Second {
					t.Errorf("waited %v for the browser, want 180s", d)
				}
			}
			if got := len(s.opened) > 0; got != tc.wantOpened {
				t.Errorf("browser opened = %v, want %v", got, tc.wantOpened)
			}
			if _, err := os.Stat(s.tokenPath()); err == nil {
				t.Error("a failed login left a token file")
			}
		})
	}
}

func TestAuthLoginUsageErrors(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		args []string
		want string
	}{
		{"port too large", []string{"login", "--client-id", authClientID, "--port", "70000"}, "--port"},
		{"port negative", []string{"login", "--client-id", authClientID, "--port", "-1"}, "--port"},
		{"port not a number", []string{"login", "--client-id", authClientID, "--port", "http"}, "port"},
		{"unknown flag", []string{"login", "--bogus"}, "--bogus"},
		{"positional argument", []string{"login", "extra"}, `"extra"`},
		{"status takes no client id", []string{"status", "--client-id", authClientID}, "--client-id"},
		{"no command", nil, "auth needs a command"},
		{"unknown command", []string{"frobnicate"}, `"frobnicate"`},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			s := newAuthSetup(t)
			code, stdout, stderr := s.run(t, tc.args...)
			if code != 2 || stdout != "" || !strings.Contains(stderr, tc.want) || !strings.Contains(stderr, usageHint) {
				t.Errorf("exit %d, stdout %q, stderr %q; want a usage error mentioning %q", code, stdout, stderr, tc.want)
			}
			if len(s.opened) != 0 {
				t.Error("a usage error still opened the browser")
			}
		})
	}
}

func TestAuthLoginInvalidConfigIsAUsageError(t *testing.T) {
	t.Parallel()
	s := newAuthSetup(t)
	s.vars["SPMETA_SOURCE"] = "bogus"
	code, _, stderr := s.run(t, "login", "--client-id", authClientID)
	if code != 2 || !strings.Contains(stderr, "SPMETA_SOURCE") {
		t.Errorf("exit %d, stderr %q; want a usage error naming SPMETA_SOURCE", code, stderr)
	}
}

func TestAuthWithoutTokenLocation(t *testing.T) {
	t.Parallel()
	for _, cmd := range []string{"logout", "status"} {
		t.Run(cmd, func(t *testing.T) {
			t.Parallel()
			var out, errOut bytes.Buffer
			code := cli.Run(context.Background(), []string{"auth", cmd}, &out, &errOut, cli.BuildInfo{}, cli.WithEnv(config.Env{}))
			if code != 1 || !strings.Contains(errOut.String(), "no location is known for the token file") {
				t.Errorf("exit %d, stderr %q; want exit 1 naming the missing location", code, errOut.String())
			}
		})
	}
}

func TestAuthLogout(t *testing.T) {
	t.Parallel()
	s := newAuthSetup(t)
	s.writeToken(t, nil)
	want := "Logged out. Removed " + s.tokenPath() + ".\n"
	for i, first := range []bool{true, false} {
		code, stdout, stderr := s.run(t, "logout")
		if code != 0 || stderr != "" {
			t.Fatalf("run %d: exit %d, stderr %q; want 0 and nothing on stderr", i+1, code, stderr)
		}
		if first && !strings.HasPrefix(stdout, want) {
			t.Errorf("first logout stdout = %q, want it to start with %q", stdout, want)
		}
		if !first && !strings.HasPrefix(stdout, "Not logged in. No token file at "+s.tokenPath()+".\n") {
			t.Errorf("second logout stdout = %q, want the not-logged-in note", stdout)
		}
		if !strings.Contains(stdout, "https://www.spotify.com/account/apps/") {
			t.Errorf("stdout = %q, want the revocation reminder", stdout)
		}
		if _, err := os.Stat(s.tokenPath()); err == nil {
			t.Errorf("run %d: the token file still exists", i+1)
		}
	}
}

func TestAuthLogoutKeepsTheLockFile(t *testing.T) {
	t.Parallel()
	s := newAuthSetup(t)
	s.writeToken(t, nil)
	lock := s.tokenPath() + ".lock"
	if err := os.WriteFile(lock, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if code, _, stderr := s.run(t, "logout"); code != 0 {
		t.Fatalf("exit %d, stderr %q", code, stderr)
	}
	if _, err := os.Stat(lock); err != nil {
		t.Errorf("logout removed the lock file: %v", err)
	}
}

func TestAuthLogoutFailsWhenTheFileCannotBeRemoved(t *testing.T) {
	t.Parallel()
	s := newAuthSetup(t)
	// A directory at the token path cannot be removed with a plain delete.
	if err := os.MkdirAll(filepath.Join(s.tokenPath(), "child"), 0o700); err != nil {
		t.Fatal(err)
	}
	code, stdout, stderr := s.run(t, "logout")
	if code != 1 || stdout != "" || !strings.Contains(stderr, "could not remove the token file") {
		t.Errorf("exit %d, stdout %q, stderr %q; want exit 1 and a removal error", code, stdout, stderr)
	}
}

func TestAuthStatusLoggedIn(t *testing.T) {
	t.Parallel()
	s := newAuthSetup(t)
	s.writeToken(t, nil)
	code, stdout, stderr := s.run(t, "status")
	if code != 0 || stderr != "" {
		t.Fatalf("exit %d, stderr %q; want 0 and nothing on stderr", code, stderr)
	}
	want := "Logged in.\n" +
		"Client ID:              " + authClientID + "\n" +
		"Scope:                  user-read-playback-state\n" +
		"Access token expires:   2026-10-27T18:40:00Z\n" +
		"Authorized:             2026-10-27T17:40:00Z\n" +
		"Refresh token expires:  about 2027-04-27 (6 months after authorization)\n" +
		"Token file:             " + s.tokenPath()
	if runtime.GOOS == "windows" {
		want += "\n"
	} else {
		want += " (mode 0600)\n"
	}
	if stdout != want {
		t.Errorf("stdout:\n%s\nwant:\n%s", stdout, want)
	}
	if s.tokenReqs.Load() != 0 {
		t.Error("status contacted the accounts service")
	}
}

func TestAuthStatusNeverPrintsSecrets(t *testing.T) {
	t.Parallel()
	s := newAuthSetup(t)
	s.writeToken(t, nil)
	_, stdout, stderr := s.run(t, "status")
	for _, secret := range []string{"AQD-stored-access-token", "AQD-stored-refresh-token"} {
		if strings.Contains(stdout+stderr, secret) {
			t.Errorf("output contains the secret %q", secret)
		}
	}
}

func TestAuthStatusNotLoggedIn(t *testing.T) {
	t.Parallel()
	s := newAuthSetup(t)
	code, stdout, stderr := s.run(t, "status")
	if code != 4 || stdout != "" || stderr != notLoggedIn {
		t.Errorf("exit %d, stdout %q, stderr %q; want 4, nothing on stdout, %q", code, stdout, stderr, notLoggedIn)
	}
}

func TestAuthStatusUnusableTokenFile(t *testing.T) {
	t.Parallel()
	s := newAuthSetup(t)
	if err := os.MkdirAll(filepath.Dir(s.tokenPath()), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(s.tokenPath(), []byte("not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	code, stdout, stderr := s.run(t, "status")
	if code != 4 || stdout != "" || !strings.HasPrefix(stderr, "spmeta: the saved Spotify login is not valid.") || !strings.Contains(stderr, "hint: Run `spmeta auth login`.") {
		t.Errorf("exit %d, stdout %q, stderr %q; want exit 4 and the invalid-login error", code, stdout, stderr)
	}
}

func TestAuthStatusExpiryWarning(t *testing.T) {
	t.Parallel()
	const day = 24 * time.Hour
	// expiring authorizes so that the refresh token expires d after the clock.
	expiring := func(d time.Duration) time.Time { return authNow.Add(d).AddDate(0, -6, 0) }
	tests := []struct {
		name       string
		authorized time.Time
		want       string // empty means no warning
	}{
		{"just over 14 days left", expiring(14*day + time.Second), ""},
		{"exactly 14 days left", expiring(14 * day), "Warning: run 'spmeta auth login' before 2026-11-11.\n"},
		{"1 day left", expiring(day), "Warning: run 'spmeta auth login' before 2026-10-29.\n"},
		{"expired", expiring(-30 * day), "Warning: the saved login probably expired on 2026-09-28; run 'spmeta auth login'.\n"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			s := newAuthSetup(t)
			s.writeToken(t, func(tok *auth.Token) { tok.AuthorizedAt = tc.authorized })
			code, stdout, stderr := s.run(t, "status")
			if code != 0 || stderr != "" {
				t.Fatalf("exit %d, stderr %q; want 0 and nothing on stderr", code, stderr)
			}
			for _, line := range strings.SplitAfter(stdout, "\n") {
				if strings.HasPrefix(line, "Warning:") && line != tc.want {
					t.Errorf("warning = %q, want %q", line, tc.want)
				}
			}
			if got := strings.Contains(stdout, "Warning:"); got != (tc.want != "") {
				t.Errorf("warning present = %v, want %v:\n%s", got, tc.want != "", stdout)
			}
		})
	}
}

func TestAuthStatusModeWarning(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("file modes are not enforced on Windows")
	}
	t.Parallel()
	tests := []struct {
		mode os.FileMode
		warn bool
	}{
		{0o600, false},
		{0o400, false},
		{0o640, true},
		{0o644, true},
		{0o602, true},
	}
	for _, tc := range tests {
		t.Run(fmt.Sprintf("%04o", tc.mode), func(t *testing.T) {
			t.Parallel()
			s := newAuthSetup(t)
			s.writeToken(t, nil)
			if err := os.Chmod(s.tokenPath(), tc.mode); err != nil {
				t.Fatal(err)
			}
			code, stdout, _ := s.run(t, "status")
			if code != 0 {
				t.Fatalf("exit = %d, want 0: a file with a wide mode is still used", code)
			}
			if want := fmt.Sprintf("(mode %04o)", tc.mode); !strings.Contains(stdout, want) {
				t.Errorf("stdout lacks %q:\n%s", want, stdout)
			}
			chmod := "chmod 600 '" + s.tokenPath() + "'"
			if got := strings.Contains(stdout, "Warning: ") && strings.Contains(stdout, chmod); got != tc.warn {
				t.Errorf("mode warning with %q present = %v, want %v:\n%s", chmod, got, tc.warn, stdout)
			}
		})
	}
}

func TestAuthHelp(t *testing.T) {
	t.Parallel()
	for _, args := range [][]string{{"help", "auth"}, {"auth", "-h"}, {"auth", "--help"}, {"auth", "login", "-h"}, {"auth", "logout", "--help"}, {"auth", "status", "-h"}} {
		t.Run(strings.Join(args, " "), func(t *testing.T) {
			t.Parallel()
			code, stdout, stderr := run(t, args...)
			if code != 0 || stderr != "" {
				t.Fatalf("exit %d, stderr %q; want 0 and nothing on stderr", code, stderr)
			}
			for _, want := range []string{"spmeta auth login", "spmeta auth logout", "spmeta auth status", "--client-id", "--port", "--no-browser"} {
				if !strings.Contains(stdout, want) {
					t.Errorf("usage lacks %q:\n%s", want, stdout)
				}
			}
		})
	}
}

func TestMainUsageListsAuth(t *testing.T) {
	t.Parallel()
	_, stdout, _ := run(t, "help")
	for _, want := range []string{"spmeta auth login|logout|status", "spmeta help auth"} {
		if !strings.Contains(stdout, want) {
			t.Errorf("usage lacks %q:\n%s", want, stdout)
		}
	}
}
