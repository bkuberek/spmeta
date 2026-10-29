package auth_test

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/bkuberek/spmeta/internal/auth"
	"github.com/bkuberek/spmeta/internal/backend"
)

const authCode = "AQD-authorization-code"

var redirectPattern = regexp.MustCompile(`^http://127\.0\.0\.1:[0-9]+/callback$`)

// authorizeParams parses the authorization URL that the opener receives.
func authorizeParams(t *testing.T, raw string) (*url.URL, url.Values) {
	t.Helper()
	u, err := url.Parse(raw)
	if err != nil {
		t.Fatalf("authorization URL %q: %v", raw, err)
	}
	return u, u.Query()
}

// hitCallback plays the browser: it sends method to path on the redirect URI
// of authURL with the valid state plus extra. An extra key with no values
// removes that parameter.
func hitCallback(t *testing.T, authURL, method, path string, extra url.Values) (status int, body string) {
	t.Helper()
	_, q := authorizeParams(t, authURL)
	target, err := url.Parse(q.Get("redirect_uri"))
	if err != nil {
		t.Fatalf("redirect_uri: %v", err)
	}
	params := url.Values{"state": {q.Get("state")}}
	for k, v := range extra {
		if len(v) == 0 {
			params.Del(k)
			continue
		}
		params[k] = v
	}
	target.Path, target.RawQuery = path, params.Encode()
	req, err := http.NewRequestWithContext(context.Background(), method, target.String(), http.NoBody)
	if err != nil {
		t.Fatal(err)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("%s %s: %v", method, path, err)
	}
	defer func() { _ = resp.Body.Close() }()
	data, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, string(data)
}

// loginRig wires Login to a fake accounts server and a token file in a
// temporary directory. open is what the user's browser does with the URL.
type loginRig struct {
	path   string
	acc    *accounts
	stderr bytes.Buffer
	log    bytes.Buffer
	opts   auth.LoginOptions

	mu       sync.Mutex
	authURLs []string
}

func newLoginRig(t *testing.T, h http.HandlerFunc, open func(authURL string)) *loginRig {
	t.Helper()
	r := &loginRig{path: filepath.Join(t.TempDir(), "spmeta", "token.json"), acc: newAccounts(t, h)}
	r.opts = auth.LoginOptions{
		ClientID:   sampleToken().ClientID,
		Path:       r.path,
		Endpoints:  auth.Endpoints{Accounts: r.acc.srv.URL},
		HTTPClient: r.acc.srv.Client(),
		Open: func(u string) error {
			r.mu.Lock()
			r.authURLs = append(r.authURLs, u)
			r.mu.Unlock()
			if open != nil {
				open(u)
			}
			return nil
		},
		Now:    func() time.Time { return epoch },
		Stderr: &r.stderr,
		Log:    slog.New(slog.NewTextHandler(&r.log, &slog.HandlerOptions{Level: slog.LevelDebug})),
	}
	return r
}

func (r *loginRig) login(t *testing.T) (auth.Token, error) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), stuckGuard)
	defer cancel()
	return auth.Login(ctx, r.opts)
}

func (r *loginRig) stored(t *testing.T) auth.Token {
	t.Helper()
	tok, err := auth.ReadToken(r.path)
	if err != nil {
		t.Fatal(err)
	}
	return tok
}

func (r *loginRig) onlyAuthURL(t *testing.T) string {
	t.Helper()
	r.mu.Lock()
	defer r.mu.Unlock()
	if len(r.authURLs) != 1 {
		t.Fatalf("opener called %d times, want 1", len(r.authURLs))
	}
	return r.authURLs[0]
}

// approve is a browser on which the user grants access.
func approve(t *testing.T) func(string) {
	return func(authURL string) {
		if status, _ := hitCallback(t, authURL, http.MethodGet, "/callback", url.Values{"code": {authCode}}); status != http.StatusOK {
			t.Errorf("callback status = %d, want 200", status)
		}
	}
}

func exchange(t *testing.T) http.HandlerFunc { return respond(200, fixture(t, "token-exchange.json")) }

func TestLoginSuccess(t *testing.T) {
	t.Parallel()
	r := newLoginRig(t, exchange(t), approve(t))
	tok, err := r.login(t)
	if err != nil {
		t.Fatalf("Login = %v", err)
	}

	want := auth.Token{
		Version:      1,
		ClientID:     sampleToken().ClientID,
		AccessToken:  newAccess,
		TokenType:    "Bearer",
		RefreshToken: newRefresh,
		Scope:        "user-read-playback-state",
		ExpiresAt:    epoch.Add(time.Hour),
		AuthorizedAt: epoch,
	}
	if tok != want {
		t.Errorf("returned token = %v, want %v", tok, want)
	}
	if got := r.stored(t); got != want {
		t.Errorf("stored token = %v, want %v", got, want)
	}
	if n := r.acc.requests.Load(); n != 1 {
		t.Errorf("token requests = %d, want 1", n)
	}
}

func TestLoginAuthorizationURL(t *testing.T) {
	t.Parallel()
	r := newLoginRig(t, exchange(t), approve(t))
	if _, err := r.login(t); err != nil {
		t.Fatalf("Login = %v", err)
	}
	raw := r.onlyAuthURL(t)
	u, q := authorizeParams(t, raw)

	if base := r.acc.srv.URL + "/authorize"; u.Scheme+"://"+u.Host+u.Path != base {
		t.Errorf("authorization URL = %s, want it under %s", raw, base)
	}
	for key, want := range map[string]string{
		"client_id":             sampleToken().ClientID,
		"response_type":         "code",
		"code_challenge_method": "S256",
		"scope":                 "user-read-playback-state",
	} {
		if got := q.Get(key); got != want {
			t.Errorf("%s = %q, want %q", key, got, want)
		}
	}
	if len(q.Get("state")) != 22 || len(q.Get("code_challenge")) != 43 {
		t.Errorf("state %q and code_challenge %q have the wrong length", q.Get("state"), q.Get("code_challenge"))
	}
	if want := "Open this URL to log in:\n" + raw + "\n"; r.stderr.String() != want {
		t.Errorf("stderr = %q, want %q", r.stderr.String(), want)
	}
}

func TestLoginTokenRequestShape(t *testing.T) {
	t.Parallel()
	r := newLoginRig(t, exchange(t), approve(t))
	if _, err := r.login(t); err != nil {
		t.Fatalf("Login = %v", err)
	}
	_, q := authorizeParams(t, r.onlyAuthURL(t))

	got := r.acc.lastRequest()
	if got.method != http.MethodPost || got.path != "/api/token" || got.contentType != "application/x-www-form-urlencoded" {
		t.Errorf("request = %s %s (%s), want POST /api/token as a form", got.method, got.path, got.contentType)
	}
	for key, want := range map[string]string{
		"grant_type":   "authorization_code",
		"code":         authCode,
		"redirect_uri": q.Get("redirect_uri"),
		"client_id":    sampleToken().ClientID,
	} {
		if g := got.form.Get(key); g != want {
			t.Errorf("%s = %q, want %q", key, g, want)
		}
	}
	if len(got.form) != 5 {
		t.Errorf("form has %d fields, want 5", len(got.form))
	}
	// The verifier is the one whose S256 challenge was sent to the browser.
	if v := got.form.Get("code_verifier"); len(v) != 64 || auth.Challenge(v) != q.Get("code_challenge") {
		t.Errorf("code_verifier %q does not match code_challenge %q", v, q.Get("code_challenge"))
	}
}

func TestLoginRedirectURIIsByteIdentical(t *testing.T) {
	t.Parallel()
	r := newLoginRig(t, exchange(t), approve(t))
	if _, err := r.login(t); err != nil {
		t.Fatalf("Login = %v", err)
	}
	_, q := authorizeParams(t, r.onlyAuthURL(t))
	redirect := q.Get("redirect_uri")
	if !redirectPattern.MatchString(redirect) {
		t.Errorf("redirect_uri = %q, want http://127.0.0.1:<port>/callback", redirect)
	}
	if got := r.acc.lastRequest().form.Get("redirect_uri"); got != redirect {
		t.Errorf("token request redirect_uri = %q, authorization request had %q", got, redirect)
	}
}

func TestLoginUsesRequestedPort(t *testing.T) {
	t.Parallel()
	port := freePort(t)
	r := newLoginRig(t, exchange(t), approve(t))
	r.opts.Port = port
	if _, err := r.login(t); err != nil {
		t.Fatalf("Login = %v", err)
	}
	_, q := authorizeParams(t, r.onlyAuthURL(t))
	if want := fmt.Sprintf("http://127.0.0.1:%d/callback", port); q.Get("redirect_uri") != want {
		t.Errorf("redirect_uri = %q, want %q", q.Get("redirect_uri"), want)
	}
}

func TestDefaultLoginPortIsRegistrable(t *testing.T) {
	t.Parallel()
	if auth.DefaultLoginPort != 8974 {
		t.Errorf("DefaultLoginPort = %d, want 8974", auth.DefaultLoginPort)
	}
}

func freePort(t *testing.T) int {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = ln.Close() }()
	return ln.Addr().(*net.TCPAddr).Port
}

func TestLoginIgnoresUnusableCallbacks(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name       string
		method     string
		path       string
		extra      url.Values
		wantStatus int
		wantBody   string
	}{
		{"state mismatch", http.MethodGet, "/callback", url.Values{"state": {"forged"}, "code": {"forged-code"}}, 400, "State mismatch. Return to the terminal.\n"},
		{"state missing", http.MethodGet, "/callback", url.Values{"state": nil, "code": {"forged-code"}}, 400, "State mismatch. Return to the terminal.\n"},
		{"state mismatch with error", http.MethodGet, "/callback", url.Values{"state": {"forged"}, "error": {"access_denied"}}, 400, "State mismatch. Return to the terminal.\n"},
		{"other path", http.MethodGet, "/other", url.Values{"code": {"forged-code"}}, 404, "404 page not found\n"},
		{"other method", http.MethodPost, "/callback", url.Values{"code": {"forged-code"}}, 404, "404 page not found\n"},
		{"neither code nor error", http.MethodGet, "/callback", nil, 400, "Missing authorization code. Return to the terminal.\n"},
	}
	r := newLoginRig(t, exchange(t), func(authURL string) {
		for _, tc := range tests {
			status, body := hitCallback(t, authURL, tc.method, tc.path, tc.extra)
			if status != tc.wantStatus || body != tc.wantBody {
				t.Errorf("%s: response = %d %q, want %d %q", tc.name, status, body, tc.wantStatus, tc.wantBody)
			}
		}
		approve(t)(authURL)
	})
	if _, err := r.login(t); err != nil {
		t.Fatalf("Login = %v, want success after the ignored requests", err)
	}
	if got := r.acc.lastRequest().form.Get("code"); got != authCode {
		t.Errorf("exchanged code = %q, want the genuine one %q", got, authCode)
	}
	if n := r.acc.requests.Load(); n != 1 {
		t.Errorf("token requests = %d, want 1", n)
	}
}

func TestLoginSuccessPage(t *testing.T) {
	t.Parallel()
	r := newLoginRig(t, exchange(t), func(authURL string) {
		status, body := hitCallback(t, authURL, http.MethodGet, "/callback", url.Values{"code": {authCode}})
		if want := "spmeta is logged in. You can close this tab.\n"; status != 200 || body != want {
			t.Errorf("response = %d %q, want 200 %q", status, body, want)
		}
	})
	if _, err := r.login(t); err != nil {
		t.Fatalf("Login = %v", err)
	}
}

func TestLoginAccessDenied(t *testing.T) {
	t.Parallel()
	r := newLoginRig(t, exchange(t), func(authURL string) {
		status, body := hitCallback(t, authURL, http.MethodGet, "/callback", url.Values{"error": {"access_denied"}})
		if want := "Authorization was not granted. You can close this tab.\n"; status != 200 || body != want {
			t.Errorf("response = %d %q, want 200 %q", status, body, want)
		}
	})
	_, err := r.login(t)
	be := asBackend(t, err, backend.KindAuthRequired)
	if want := "Spotify authorization failed: access_denied"; be.Message != want {
		t.Errorf("message = %q, want %q", be.Message, want)
	}
	assertNoToken(t, r)
}

func TestLoginErrorCodeIsSanitized(t *testing.T) {
	t.Parallel()
	r := newLoginRig(t, exchange(t), func(authURL string) {
		hitCallback(t, authURL, http.MethodGet, "/callback", url.Values{"error": {"<b>Access Denied</b>\n"}})
	})
	_, err := r.login(t)
	be := asBackend(t, err, backend.KindAuthRequired)
	if want := "Spotify authorization failed."; be.Message != want {
		t.Errorf("message = %q, want %q", be.Message, want)
	}
}

func assertNoToken(t *testing.T, r *loginRig) {
	t.Helper()
	if n := r.acc.requests.Load(); n != 0 {
		t.Errorf("token requests = %d, want 0", n)
	}
	if _, err := os.Stat(r.path); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("token file after a failed login: Stat = %v, want not exist", err)
	}
}

func TestLoginTimesOutAfter180Seconds(t *testing.T) {
	t.Parallel()
	var waited []time.Duration
	r := newLoginRig(t, exchange(t), nil)
	r.opts.Sleep = func(_ context.Context, d time.Duration) error {
		waited = append(waited, d)
		return nil
	}
	_, err := r.login(t)
	be := asBackend(t, err, backend.KindAuthRequired)
	if want := "no response from the browser within 180 s."; be.Message != want {
		t.Errorf("message = %q, want %q", be.Message, want)
	}
	if len(waited) != 1 || waited[0] != 180*time.Second {
		t.Errorf("waited %v, want one wait of 180s", waited)
	}
	assertNoToken(t, r)
}

func TestLoginCancelled(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithCancel(context.Background())
	r := newLoginRig(t, exchange(t), func(string) { cancel() })
	_, err := auth.Login(ctx, r.opts)
	be := asBackend(t, err, backend.KindAuthRequired)
	if want := "login was cancelled."; be.Message != want {
		t.Errorf("message = %q, want %q", be.Message, want)
	}
	assertNoToken(t, r)
}

func TestLoginPortInUse(t *testing.T) {
	t.Parallel()
	busy, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = busy.Close() })
	port := busy.Addr().(*net.TCPAddr).Port

	r := newLoginRig(t, exchange(t), nil)
	r.opts.Port = port
	_, err = r.login(t)
	be := asBackend(t, err, backend.KindUnexpected)
	if want := fmt.Sprintf("port %d is in use.", port); be.Message != want {
		t.Errorf("message = %q, want %q", be.Message, want)
	}
	if len(r.authURLs) != 0 || r.stderr.Len() != 0 {
		t.Errorf("a busy port still printed %q and opened %v", r.stderr.String(), r.authURLs)
	}
	assertNoToken(t, r)
}

func TestLoginWithoutOpenerOnlyPrintsTheURL(t *testing.T) {
	t.Parallel()
	r := newLoginRig(t, exchange(t), nil)
	r.opts.Open = nil // --no-browser
	r.opts.Sleep = func(context.Context, time.Duration) error { return nil }
	if _, err := r.login(t); err == nil {
		t.Fatal("Login succeeded without any callback")
	}
	if !strings.HasPrefix(r.stderr.String(), "Open this URL to log in:\nhttp") {
		t.Errorf("stderr = %q, want the prompt and the URL", r.stderr.String())
	}
}

func TestLoginIgnoresOpenerFailure(t *testing.T) {
	t.Parallel()
	r := newLoginRig(t, exchange(t), nil)
	r.opts.Open = func(u string) error {
		approve(t)(u)
		return errors.New("xdg-open: not found")
	}
	if _, err := r.login(t); err != nil {
		t.Fatalf("Login = %v, want a failure to open the browser to be ignored", err)
	}
	if !strings.Contains(r.log.String(), "xdg-open: not found") {
		t.Errorf("debug log = %q, want the opener failure", r.log.String())
	}
}

func TestLoginExchangeFailures(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name        string
		handler     func(*testing.T) http.HandlerFunc
		kind        backend.Kind
		wantMessage string
	}{
		{
			"refused with a description", func(t *testing.T) http.HandlerFunc { return respond(400, fixture(t, "token-login-refused.json")) },
			backend.KindAuthRequired, "Spotify refused the login: Invalid authorization code",
		},
		{
			"refused without a body", func(*testing.T) http.HandlerFunc { return respond(400, nil) },
			backend.KindAuthRequired, "Spotify refused the login.",
		},
		{"refusal that echoes the code", func(*testing.T) http.HandlerFunc {
			return respond(400, []byte(`{"error":"invalid_grant","error_description":"bad code `+authCode+`"}`))
		}, backend.KindAuthRequired, "Spotify refused the login: invalid_grant"},
		{
			"server error", func(*testing.T) http.HandlerFunc { return respond(503, nil) },
			backend.KindUnexpected, "unexpected HTTP status 503 from the Spotify accounts service.",
		},
		{"no refresh token", func(*testing.T) http.HandlerFunc {
			return respond(200, []byte(`{"access_token":"`+newAccess+`","token_type":"Bearer","expires_in":3600}`))
		}, backend.KindUnexpected, "the Spotify accounts service returned no refresh token."},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			r := newLoginRig(t, tc.handler(t), approve(t))
			_, err := r.login(t)
			be := asBackend(t, err, tc.kind)
			if be.Message != tc.wantMessage {
				t.Errorf("message = %q, want %q", be.Message, tc.wantMessage)
			}
			assertNoSecrets(t, err, authCode, newAccess)
			if _, err := os.Stat(r.path); !errors.Is(err, os.ErrNotExist) {
				t.Errorf("token file after a failed exchange: Stat = %v, want not exist", err)
			}
		})
	}
}

func TestLoginRefusedExchangeHintsAtTheAppSettings(t *testing.T) {
	t.Parallel()
	r := newLoginRig(t, respond(400, fixture(t, "token-login-refused.json")), approve(t))
	_, err := r.login(t)
	be := asBackend(t, err, backend.KindAuthRequired)
	_, q := authorizeParams(t, r.onlyAuthURL(t))
	if redirect := q.Get("redirect_uri"); !strings.Contains(be.Hint, redirect) || !strings.Contains(be.Hint, "Client ID") {
		t.Errorf("hint = %q, want it to name the Client ID and the redirect URI %q actually used", be.Hint, redirect)
	}
}

func TestLoginKeepsSecretsOutOfOutput(t *testing.T) {
	t.Parallel()
	r := newLoginRig(t, exchange(t), approve(t))
	if _, err := r.login(t); err != nil {
		t.Fatalf("Login = %v", err)
	}
	form := r.acc.lastRequest().form
	secrets := []string{authCode, newAccess, newRefresh, form.Get("code_verifier")}
	for name, out := range map[string]string{"stderr": r.stderr.String(), "debug log": r.log.String()} {
		for _, s := range secrets {
			if strings.Contains(out, s) {
				t.Errorf("%s contains the secret %q:\n%s", name, s, out)
			}
		}
	}
	if !strings.Contains(r.log.String(), "/api/token") {
		t.Errorf("debug log = %q, want the token request logged without its body", r.log.String())
	}
}
