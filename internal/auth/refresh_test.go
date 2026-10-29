package auth_test

import (
	"bytes"
	"context"
	"errors"
	"io"
	"log/slog"
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
	"github.com/bkuberek/spmeta/internal/backend"
)

const (
	newAccess  = "BQD-new-access-token"
	newRefresh = "AQD-new-refresh-token"
	refreshed  = "BQD-refreshed-access-token"
)

// stuckGuard only bounds how long a broken lock can hang a test; it is not
// used to order events.
const stuckGuard = 30 * time.Second

// epoch is the injected clock. The stored token expires at epoch plus the
// offset a test gives newRig.
var epoch = time.Date(2026, 10, 27, 17, 40, 0, 0, time.UTC)

type recorded struct {
	method, path, contentType string
	form                      url.Values
}

// accounts is a fake accounts.spotify.com that counts and records requests.
type accounts struct {
	srv      *httptest.Server
	requests atomic.Int32
	mu       sync.Mutex
	last     recorded
}

func newAccounts(t *testing.T, h http.HandlerFunc) *accounts {
	t.Helper()
	a := &accounts{}
	a.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		a.mu.Lock()
		a.last = recorded{r.Method, r.URL.Path, r.Header.Get("Content-Type"), r.PostForm}
		a.mu.Unlock()
		a.requests.Add(1)
		h(w, r)
	}))
	t.Cleanup(a.srv.Close)
	return a
}

func (a *accounts) lastRequest() recorded {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.last
}

func fixture(t *testing.T, name string) []byte {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func respond(status int, body []byte) http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_, _ = w.Write(body)
	}
}

// rig is a token file expiring expiresIn after epoch, a fake accounts server
// and a TokenSource wired to both.
type rig struct {
	path string
	acc  *accounts
	src  *auth.TokenSource
}

func newRig(t *testing.T, expiresIn time.Duration, h http.HandlerFunc) *rig {
	t.Helper()
	path := filepath.Join(t.TempDir(), "spmeta", "token.json")
	tok := sampleToken()
	tok.ExpiresAt = epoch.Add(expiresIn)
	if err := auth.WriteToken(path, tok); err != nil {
		t.Fatal(err)
	}
	acc := newAccounts(t, h)
	return &rig{path: path, acc: acc, src: &auth.TokenSource{
		Path:       path,
		Endpoints:  auth.Endpoints{Accounts: acc.srv.URL},
		HTTPClient: acc.srv.Client(),
		Now:        func() time.Time { return epoch },
	}}
}

func (r *rig) stored(t *testing.T) auth.Token {
	t.Helper()
	tok, err := auth.ReadToken(r.path)
	if err != nil {
		t.Fatal(err)
	}
	return tok
}

// assertNoSecrets fails when err or anything it wraps mentions a secret.
func assertNoSecrets(t *testing.T, err error, secrets ...string) {
	t.Helper()
	for e := err; e != nil; e = errors.Unwrap(e) {
		text := e.Error()
		var be *backend.Error
		if errors.As(e, &be) {
			text += be.Message + be.Hint
		}
		for _, s := range secrets {
			if strings.Contains(text, s) {
				t.Errorf("error %q contains the secret %q", text, s)
			}
		}
	}
}

var allSecrets = []string{storedAccess, storedRefresh, newAccess, newRefresh, refreshed}

func TestTokenRefreshWindow(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name      string
		expiresIn time.Duration
		want      string
		requests  int32
	}{
		{"61 s away is still fresh", 61 * time.Second, storedAccess, 0},
		{"exactly 60 s away refreshes", 60 * time.Second, refreshed, 1},
		{"30 s away refreshes", 30 * time.Second, refreshed, 1},
		{"expired refreshes", -time.Minute, refreshed, 1},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			r := newRig(t, tc.expiresIn, respond(200, fixture(t, "token-refresh-no-rotation.json")))
			got, err := r.src.Token(context.Background(), false)
			if err != nil || got != tc.want {
				t.Errorf("Token = %q, %v; want %q", got, err, tc.want)
			}
			if n := r.acc.requests.Load(); n != tc.requests {
				t.Errorf("token requests = %d, want %d", n, tc.requests)
			}
		})
	}
}

func TestTokenForceRefreshesFreshToken(t *testing.T) {
	t.Parallel()
	r := newRig(t, time.Hour, respond(200, fixture(t, "token-refresh-no-rotation.json")))
	got, err := r.src.Token(context.Background(), true)
	if err != nil || got != refreshed {
		t.Errorf("Token(force) = %q, %v; want %q", got, err, refreshed)
	}
	if n := r.acc.requests.Load(); n != 1 {
		t.Errorf("token requests = %d, want 1", n)
	}
}

func TestRefreshRequestShape(t *testing.T) {
	t.Parallel()
	r := newRig(t, 0, respond(200, fixture(t, "token-refresh-no-rotation.json")))
	// The stored Client ID is used, whatever the caller's configuration says.
	if _, err := r.src.Token(context.Background(), false); err != nil {
		t.Fatalf("Token = %v", err)
	}
	got := r.acc.lastRequest()
	if got.method != http.MethodPost || got.path != "/api/token" || got.contentType != "application/x-www-form-urlencoded" {
		t.Errorf("request = %s %s (%s), want POST /api/token as a form", got.method, got.path, got.contentType)
	}
	want := url.Values{
		"grant_type":    {"refresh_token"},
		"refresh_token": {storedRefresh},
		"client_id":     {sampleToken().ClientID},
	}
	if len(got.form) != len(want) {
		t.Errorf("form has %d fields, want %d", len(got.form), len(want))
	}
	for k := range want {
		if got.form.Get(k) != want.Get(k) {
			t.Errorf("form field %s differs from the expected value", k)
		}
	}
}

func TestRefreshWritesTokenFile(t *testing.T) {
	t.Parallel()
	r := newRig(t, 0, respond(200, fixture(t, "token-refresh-no-rotation.json")))
	if _, err := r.src.Token(context.Background(), false); err != nil {
		t.Fatalf("Token = %v", err)
	}
	before := sampleToken()
	got := r.stored(t)
	if got.AccessToken != refreshed || !got.ExpiresAt.Equal(epoch.Add(time.Hour)) {
		t.Errorf("access token or expiry not updated: %+v", got)
	}
	if !got.AuthorizedAt.Equal(before.AuthorizedAt) || got.ClientID != before.ClientID {
		t.Errorf("refresh changed authorized_at or client_id: %+v", got)
	}
}

func TestRefreshRotation(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name        string
		fixture     string
		wantRefresh string
		wantAccess  string
	}{
		{"kept when the response has none", "token-refresh-no-rotation.json", storedRefresh, refreshed},
		{"replaced when the response returns one", "token-exchange.json", newRefresh, newAccess},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			r := newRig(t, 0, respond(200, fixture(t, tc.fixture)))
			got, err := r.src.Token(context.Background(), false)
			if err != nil || got != tc.wantAccess {
				t.Fatalf("Token = %q, %v; want %q", got, err, tc.wantAccess)
			}
			if rt := r.stored(t).RefreshToken; rt != tc.wantRefresh {
				t.Errorf("stored refresh token = %q, want %q", rt, tc.wantRefresh)
			}
		})
	}
}

func TestRefreshInvalidGrantIsAuthRequired(t *testing.T) {
	t.Parallel()
	r := newRig(t, 0, respond(400, fixture(t, "token-invalid-grant.json")))
	_, err := r.src.Token(context.Background(), false)
	be := asBackend(t, err, backend.KindAuthRequired)
	if !strings.Contains(be.Message, "Spotify rejected the saved login") || !strings.Contains(be.Hint, "spmeta auth login") {
		t.Errorf("message %q, hint %q; want the revoked-login text and the login command", be.Message, be.Hint)
	}
	want := sampleToken()
	want.Version = 1
	want.ExpiresAt = epoch // as stored by newRig
	if got := r.stored(t); got != want {
		t.Errorf("a rejected refresh changed the token file: %+v", got)
	}
}

func TestRefreshFailures(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name   string
		status int
		body   string
		kind   backend.Kind
	}{
		{"other 400 error", 400, `{"error":"invalid_client","error_description":"Invalid client"}`, backend.KindAuthRequired},
		{"400 without JSON", 400, "oops", backend.KindAuthRequired},
		{"400 description echoing the token", 400, `{"error":"invalid_request","error_description":"bad ` + storedRefresh + `"}`, backend.KindAuthRequired},
		{"400 code echoing the token", 400, `{"error":"` + storedRefresh + `"}`, backend.KindAuthRequired},
		{"server error", 503, "unavailable " + storedAccess, backend.KindUnexpected},
		{"unexpected status", 429, "", backend.KindUnexpected},
		{"redirect", 302, "", backend.KindUnexpected},
		{"200 that is not JSON", 200, "<html>" + newAccess, backend.KindUnexpected},
		{"200 without an access token", 200, `{"expires_in":3600}`, backend.KindUnexpected},
		{"200 without an expiry", 200, `{"access_token":"` + newAccess + `"}`, backend.KindUnexpected},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			r := newRig(t, 0, respond(tc.status, []byte(tc.body)))
			_, err := r.src.Token(context.Background(), false)
			wantKind(t, err, tc.kind)
			assertNoSecrets(t, err, allSecrets...)
			if got := r.stored(t); got.AccessToken != storedAccess || got.RefreshToken != storedRefresh {
				t.Errorf("a failed refresh changed the token file")
			}
		})
	}
}

func TestRefreshNetworkErrorIsUnexpected(t *testing.T) {
	t.Parallel()
	r := newRig(t, 0, respond(200, nil))
	r.acc.srv.Close()
	_, err := r.src.Token(context.Background(), false)
	wantKind(t, err, backend.KindUnexpected)
	assertNoSecrets(t, err, allSecrets...)
}

func TestRefreshHonorsCancellation(t *testing.T) {
	t.Parallel()
	arrived := make(chan struct{})
	r := newRig(t, 0, func(_ http.ResponseWriter, req *http.Request) {
		close(arrived)
		<-req.Context().Done()
	})
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		_, err := r.src.Token(ctx, false)
		done <- err
	}()
	<-arrived
	cancel()
	err := <-done
	wantKind(t, err, backend.KindUnexpected)
	if !errors.Is(err, context.Canceled) {
		t.Errorf("err = %v, want it to wrap context.Canceled", err)
	}
}

// deadlineTransport records the deadline of the request context and answers
// with a fixed result, so the 10 s limit is checked without waiting for it.
type deadlineTransport struct {
	deadline time.Time
	err      error
}

func (d *deadlineTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	d.deadline, _ = req.Context().Deadline()
	if d.err != nil {
		return nil, d.err
	}
	return &http.Response{
		StatusCode: 200,
		Body:       io.NopCloser(strings.NewReader(`{"access_token":"x","expires_in":1}`)),
		Header:     http.Header{},
		Request:    req,
	}, nil
}

func TestRefreshRequestHasTenSecondDeadline(t *testing.T) {
	t.Parallel()
	r := newRig(t, 0, nil)
	tr := &deadlineTransport{}
	r.src.HTTPClient = &http.Client{Transport: tr}
	if _, err := r.src.Token(context.Background(), false); err != nil {
		t.Fatalf("Token = %v", err)
	}
	if left := time.Until(tr.deadline); left <= 9*time.Second || left > 10*time.Second {
		t.Errorf("request deadline is %v away, want about 10 s", left)
	}
}

func TestRefreshTimeoutIsUnexpected(t *testing.T) {
	t.Parallel()
	r := newRig(t, 0, nil)
	r.src.HTTPClient = &http.Client{Transport: &deadlineTransport{err: context.DeadlineExceeded}}
	_, err := r.src.Token(context.Background(), false)
	be := asBackend(t, err, backend.KindUnexpected)
	if !strings.Contains(be.Message, "did not answer") {
		t.Errorf("Message = %q, want the timeout text", be.Message)
	}
}

func TestTokenMissingFileIsNotAvailable(t *testing.T) {
	t.Parallel()
	acc := newAccounts(t, respond(200, nil))
	src := &auth.TokenSource{
		Path:      filepath.Join(t.TempDir(), "token.json"),
		Endpoints: auth.Endpoints{Accounts: acc.srv.URL},
	}
	for _, force := range []bool{false, true} {
		be := asBackend(t, errorOf(src.Token(context.Background(), force)), backend.KindNotAvailable)
		if !strings.Contains(be.Hint, "spmeta auth login") {
			t.Errorf("Hint = %q, want the login command", be.Hint)
		}
	}
	if n := acc.requests.Load(); n != 0 {
		t.Errorf("token requests = %d, want 0", n)
	}
}

func TestEndpointsWithDefaults(t *testing.T) {
	t.Parallel()
	got := auth.Endpoints{}.WithDefaults()
	if got.Accounts != "https://accounts.spotify.com" || got.API != "https://api.spotify.com" {
		t.Errorf("defaults = %+v", got)
	}
	custom := auth.Endpoints{Accounts: "http://127.0.0.1:1", API: "http://127.0.0.1:2"}
	if got := custom.WithDefaults(); got != custom {
		t.Errorf("WithDefaults changed explicit endpoints: %+v", got)
	}
}

func TestRefreshDebugLogHasNoSecrets(t *testing.T) {
	t.Parallel()
	var buf bytes.Buffer
	r := newRig(t, 0, respond(200, fixture(t, "token-exchange.json")))
	r.src.Log = slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug}))
	if _, err := r.src.Token(context.Background(), false); err != nil {
		t.Fatalf("Token = %v", err)
	}
	log := buf.String()
	if !strings.Contains(log, "/api/token") || !strings.Contains(log, "status=200") {
		t.Errorf("debug log lacks the request line: %q", log)
	}
	for _, s := range allSecrets {
		if strings.Contains(log, s) {
			t.Errorf("debug log contains the secret %q", s)
		}
	}
}

// TestConcurrentRefreshersPerformOneRequest runs two sources over one token
// file, as two processes would. The first holds the file lock inside its HTTP
// request; the second is proven to be waiting for the lock (through its Sleep
// hook) before the first is released. It must then find the refreshed file and
// send no request of its own.
func TestConcurrentRefreshersPerformOneRequest(t *testing.T) {
	t.Parallel()
	body := fixture(t, "token-exchange.json")
	entered := make(chan struct{})
	release := make(chan struct{})
	var releaseOnce sync.Once
	unblock := func() { releaseOnce.Do(func() { close(release) }) }

	var first sync.Once
	r := newRig(t, 0, func(w http.ResponseWriter, req *http.Request) {
		first.Do(func() {
			close(entered)
			<-release
		})
		respond(200, body)(w, req)
	})
	t.Cleanup(unblock)

	waiting := make(chan time.Duration, 1)
	second := *r.src
	second.Sleep = func(ctx context.Context, d time.Duration) error {
		select {
		case waiting <- d:
		default:
		}
		select {
		case <-release:
		case <-ctx.Done():
			return ctx.Err()
		}
		runtime.Gosched()
		return nil
	}

	type result struct {
		token string
		err   error
	}
	run := func(src *auth.TokenSource) <-chan result {
		ch := make(chan result, 1)
		go func() {
			tok, err := src.Token(context.Background(), false)
			ch <- result{tok, err}
		}()
		return ch
	}

	firstDone := run(r.src)
	<-entered
	secondDone := run(&second)
	select {
	case d := <-waiting:
		if d != 100*time.Millisecond {
			t.Errorf("lock poll interval = %v, want 100 ms", d)
		}
	case <-time.After(stuckGuard):
		t.Fatal("the second refresher never waited for the lock")
	}
	unblock()

	for name, ch := range map[string]<-chan result{"first": firstDone, "second": secondDone} {
		if res := <-ch; res.err != nil || res.token != newAccess {
			t.Errorf("%s refresher = %q, %v; want the refreshed token", name, res.token, res.err)
		}
	}
	if n := r.acc.requests.Load(); n != 1 {
		t.Errorf("token requests = %d, want exactly 1", n)
	}
	if got := r.stored(t); got.AccessToken != newAccess || got.RefreshToken != newRefresh {
		t.Errorf("token file not refreshed: %+v", got)
	}
}
