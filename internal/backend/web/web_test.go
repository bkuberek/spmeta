package web

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/bkuberek/spmeta/internal/auth"
	"github.com/bkuberek/spmeta/internal/backend"
	"github.com/bkuberek/spmeta/internal/format"
	"github.com/bkuberek/spmeta/internal/model"
)

var (
	_ backend.Backend = (*Backend)(nil)
	_ TokenSource     = (*auth.TokenSource)(nil)
)

func asError(t *testing.T, err error) *backend.Error {
	t.Helper()
	var be *backend.Error
	if !errors.As(err, &be) {
		t.Fatalf("err = %v (%T), want *backend.Error", err, err)
	}
	return be
}

// wantKind checks the kind, the source, and a message fragment.
func wantKind(t *testing.T, err error, kind backend.Kind, msg string) {
	t.Helper()
	be := asError(t, err)
	if be.Kind != kind || be.Source != model.SourceWeb || !strings.Contains(be.Message, msg) {
		t.Fatalf("err = {kind %v source %q message %q}, want kind %v source web message containing %q",
			be.Kind, be.Source, be.Message, kind, msg)
	}
}

func TestSource(t *testing.T) {
	if got := (&Backend{}).Source(); got != model.SourceWeb {
		t.Errorf("Source = %q, want web", got)
	}
}

func TestReadSendsTheDocumentedRequest(t *testing.T) {
	s := newScript(t, ok(fixture(t, "player-track.json")))
	r := newRig(s)

	np, err := r.Read(context.Background())
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if np.Source != model.SourceWeb || np.FetchedAt != fetchedAt {
		t.Errorf("source %q fetched_at %v, want web at the injected time", np.Source, np.FetchedAt)
	}
	reqs := s.requests()
	if len(reqs) != 1 {
		t.Fatalf("%d requests, want 1", len(reqs))
	}
	got := reqs[0]
	if got.method != http.MethodGet || got.path != "/v1/me/player" || got.query != "additional_types=episode" {
		t.Errorf("request = %s %s?%s", got.method, got.path, got.query)
	}
	wantHeaders := map[string]string{
		"Authorization": "Bearer " + accessToken,
		"Accept":        "application/json",
		"User-Agent":    "spmeta/1.2.3 (+https://github.com/bkuberek/spmeta)",
	}
	for k, want := range wantHeaders {
		if v := got.header.Get(k); v != want {
			t.Errorf("header %s = %q, want %q", k, v, want)
		}
	}
	if len(r.tokens.calls) != 1 || r.tokens.calls[0] {
		t.Errorf("token calls (force flags) = %v, want one non-forced call", r.tokens.calls)
	}
}

func TestUserAgentDefaultsToDev(t *testing.T) {
	s := newScript(t, ok(fixture(t, "player-track.json")))
	r := newRig(s)
	r.Version = ""
	if _, err := r.Read(context.Background()); err != nil {
		t.Fatal(err)
	}
	if got := s.requests()[0].header.Get("User-Agent"); got != "spmeta/dev (+https://github.com/bkuberek/spmeta)" {
		t.Errorf("User-Agent = %q", got)
	}
}

// recordingTransport answers every request with 204 and keeps the URLs.
type recordingTransport struct{ urls []string }

func (r *recordingTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	r.urls = append(r.urls, req.URL.String())
	return &http.Response{
		StatusCode: http.StatusNoContent,
		Header:     http.Header{},
		Body:       http.NoBody,
		Request:    req,
	}, nil
}

func TestOnlyTheConfiguredBaseURLIsContacted(t *testing.T) {
	tests := []struct {
		name string
		api  string
		want string
	}{
		{"injected", "http://127.0.0.1:9999", "http://127.0.0.1:9999/v1/me/player?additional_types=episode"},
		{"trailing slash", "http://127.0.0.1:9999/", "http://127.0.0.1:9999/v1/me/player?additional_types=episode"},
		{"production default", "", "https://api.spotify.com/v1/me/player?additional_types=episode"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rt := &recordingTransport{}
			b := &Backend{
				Tokens:     &fakeTokens{},
				Endpoints:  auth.Endpoints{API: tt.api},
				HTTPClient: &http.Client{Transport: rt},
			}
			_, err := b.Read(context.Background())
			wantKind(t, err, backend.KindNothingPlaying, "")
			if len(rt.urls) != 1 || rt.urls[0] != tt.want {
				t.Errorf("contacted %v, want exactly [%s]", rt.urls, tt.want)
			}
		})
	}
}

func TestNothingPlaying(t *testing.T) {
	tests := []struct {
		name  string
		reply reply
	}{
		{"204 no content", status(http.StatusNoContent)},
		{"unknown type with null item", ok(fixture(t, "player-unknown-null-item.json"))},
		{"track type with null item", ok([]byte(`{"is_playing":true,"item":null,"currently_playing_type":"track"}`))},
		{"episode type with null item", ok([]byte(`{"is_playing":true,"item":null,"currently_playing_type":"episode"}`))},
		{"json null body", ok([]byte(`null`))},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := newRig(newScript(t, tt.reply))
			_, err := r.Read(context.Background())
			wantKind(t, err, backend.KindNothingPlaying, "nothing is playing")
		})
	}
}

func TestRefreshesAndRetriesOnce(t *testing.T) {
	s := newScript(t, reply{status: http.StatusUnauthorized, body: fixture(t, "error-401.json")}, ok(fixture(t, "player-track.json")))
	r := newRig(s)

	np, err := r.Read(context.Background())
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if np.Title == nil || *np.Title != "Amazone" {
		t.Errorf("title = %v, want the track from the retry", np.Title)
	}
	if want := []bool{false, true}; fmt.Sprint(r.tokens.calls) != fmt.Sprint(want) {
		t.Errorf("token calls (force flags) = %v, want %v", r.tokens.calls, want)
	}
	reqs := s.requests()
	if len(reqs) != 2 {
		t.Fatalf("%d requests, want 2", len(reqs))
	}
	if got := reqs[1].header.Get("Authorization"); got != "Bearer "+refreshedToken {
		t.Errorf("retry Authorization = %q, want the refreshed token", got)
	}
}

func TestSecond401IsAuthRequired(t *testing.T) {
	s := newScript(t, status(http.StatusUnauthorized), status(http.StatusUnauthorized))
	r := newRig(s)

	_, err := r.Read(context.Background())
	wantKind(t, err, backend.KindAuthRequired, "rejected the access token")
	be := asError(t, err)
	if be.Hint != "Run `spmeta auth login`." {
		t.Errorf("hint = %q", be.Hint)
	}
	if n := len(s.requests()); n != 2 {
		t.Errorf("%d requests, want 2 (one retry only)", n)
	}
	if want := []bool{false, true}; fmt.Sprint(r.tokens.calls) != fmt.Sprint(want) {
		t.Errorf("token calls = %v, want exactly one forced refresh", r.tokens.calls)
	}
}

func TestTokenSourceErrorsPassThrough(t *testing.T) {
	notLoggedIn := &backend.Error{Kind: backend.KindNotAvailable, Message: "not logged in to the Spotify Web API", Hint: "Run `spmeta auth login`."}
	rejected := &backend.Error{Kind: backend.KindAuthRequired, Message: "Spotify rejected the saved login"}
	tests := []struct {
		name      string
		replies   []reply
		tokens    fakeTokens
		want      *backend.Error
		wantCalls int
	}{
		{"first token call fails", nil, fakeTokens{err: notLoggedIn}, notLoggedIn, 0},
		{"forced refresh fails", []reply{status(http.StatusUnauthorized)}, fakeTokens{err: rejected, errOn: 2}, rejected, 1},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := newScript(t, tt.replies...)
			r := newRig(s)
			r.tokens.err, r.tokens.errOn = tt.tokens.err, tt.tokens.errOn

			_, err := r.Read(context.Background())
			if !errors.Is(err, tt.want) {
				t.Fatalf("err = %v, want the token source's error", err)
			}
			if n := len(s.requests()); n != tt.wantCalls {
				t.Errorf("%d requests, want %d", n, tt.wantCalls)
			}
		})
	}
}

func TestTokenSourceForeignErrorIsUnexpected(t *testing.T) {
	r := newRig(newScript(t))
	r.tokens.err = errors.New("disk on fire")
	_, err := r.Read(context.Background())
	wantKind(t, err, backend.KindUnexpected, "could not get an access token")
}

func TestForbiddenCarriesTheHint(t *testing.T) {
	r := newRig(newScript(t, reply{status: http.StatusForbidden, body: fixture(t, "error-403.json")}))

	_, err := r.Read(context.Background())
	wantKind(t, err, backend.KindPermissionDenied, "Spotify refused access (403): User not registered in the Developer Dashboard")
	be := asError(t, err)
	for _, want := range []string{"allowlist", "Premium"} {
		if !strings.Contains(be.Hint, want) {
			t.Errorf("hint %q does not mention %q", be.Hint, want)
		}
	}
}

func TestForbiddenServerTextIsFiltered(t *testing.T) {
	tests := []struct {
		name string
		body string
		want string
	}{
		{"no body", ``, "Spotify refused access (403)."},
		{"not json", `<html>nope</html>`, "Spotify refused access (403)."},
		{"empty message", `{"error":{"message":""}}`, "Spotify refused access (403)."},
		{"multiple lines", `{"error":{"message":"a\nb"}}`, "Spotify refused access (403)."},
		{"too long", `{"error":{"message":"` + strings.Repeat("x", 201) + `"}}`, "Spotify refused access (403)."},
		{"echoes the token", `{"error":{"message":"bad ` + accessToken + `"}}`, "Spotify refused access (403)."},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := newRig(newScript(t, reply{status: http.StatusForbidden, body: []byte(tt.body)}))
			_, err := r.Read(context.Background())
			if be := asError(t, err); be.Kind != backend.KindPermissionDenied || be.Message != tt.want {
				t.Errorf("err = {kind %v message %q}, want permission denied %q", be.Kind, be.Message, tt.want)
			}
		})
	}
}

func TestRateLimitWithinMaxRetryWaitSleepsAndRetries(t *testing.T) {
	s := newScript(t, withRetryAfter(http.StatusTooManyRequests, "2", nil), ok(fixture(t, "player-track.json")))
	r := newRig(s)
	r.MaxRetryWait = 10 * time.Second

	if _, err := r.Read(context.Background()); err != nil {
		t.Fatalf("Read: %v", err)
	}
	if want := []time.Duration{2 * time.Second}; fmt.Sprint(r.sleeps.waits) != fmt.Sprint(want) {
		t.Errorf("sleeps = %v, want %v", r.sleeps.waits, want)
	}
	if n := len(s.requests()); n != 2 {
		t.Errorf("%d requests, want 2", n)
	}
	if len(r.tokens.calls) != 1 {
		t.Errorf("token calls = %v, want the same token reused", r.tokens.calls)
	}
}

func TestRateLimitAtTheLimitStillWaits(t *testing.T) {
	r := newRig(newScript(t, withRetryAfter(http.StatusTooManyRequests, "10", nil), ok(fixture(t, "player-track.json"))))
	r.MaxRetryWait = 10 * time.Second
	if _, err := r.Read(context.Background()); err != nil {
		t.Fatalf("Read: %v", err)
	}
	if want := []time.Duration{10 * time.Second}; fmt.Sprint(r.sleeps.waits) != fmt.Sprint(want) {
		t.Errorf("sleeps = %v, want %v", r.sleeps.waits, want)
	}
}

func TestRateLimitBeyondMaxRetryWaitReturnsAtOnce(t *testing.T) {
	s := newScript(t, withRetryAfter(http.StatusTooManyRequests, "30", fixture(t, "error-429-quota.json")))
	r := newRig(s)
	r.MaxRetryWait = 10 * time.Second

	_, err := r.Read(context.Background())
	wantKind(t, err, backend.KindRateLimited, "rate limited by Spotify; retry after 30 s (QUOTA_EXCEEDED)")
	be := asError(t, err)
	if be.RetryAfter != 30*time.Second {
		t.Errorf("RetryAfter = %v, want 30s", be.RetryAfter)
	}
	if len(r.sleeps.waits) != 0 || len(s.requests()) != 1 {
		t.Errorf("sleeps %v, requests %d; want no sleep and one request", r.sleeps.waits, len(s.requests()))
	}
}

func TestRateLimitWithoutQuotaReasonHasNoSuffix(t *testing.T) {
	r := newRig(newScript(t, withRetryAfter(http.StatusTooManyRequests, "30", []byte(`{"error":{"status":429}}`))))
	_, err := r.Read(context.Background())
	if be := asError(t, err); be.Message != "rate limited by Spotify; retry after 30 s" {
		t.Errorf("message = %q", be.Message)
	}
}

func TestQuotaReasonInsideTheErrorObject(t *testing.T) {
	r := newRig(newScript(t, withRetryAfter(http.StatusTooManyRequests, "30", []byte(`{"error":{"status":429,"reason":"QUOTA_EXCEEDED"}}`))))
	_, err := r.Read(context.Background())
	if be := asError(t, err); !strings.HasSuffix(be.Message, " (QUOTA_EXCEEDED)") {
		t.Errorf("message = %q, want the QUOTA_EXCEEDED suffix", be.Message)
	}
}

func TestRateLimitInWatchModeNeverWaits(t *testing.T) {
	s := newScript(t, withRetryAfter(http.StatusTooManyRequests, "1", nil))
	r := newRig(s) // MaxRetryWait is zero: watch mode

	_, err := r.Read(context.Background())
	wantKind(t, err, backend.KindRateLimited, "retry after 1 s")
	be := asError(t, err)
	if be.RetryAfter != time.Second || len(r.sleeps.waits) != 0 || len(s.requests()) != 1 {
		t.Errorf("RetryAfter %v, sleeps %v, requests %d; want 1s, no sleep, one request", be.RetryAfter, r.sleeps.waits, len(s.requests()))
	}
}

func TestRateLimitGivesUpAfterThreeAttempts(t *testing.T) {
	s := newScript(t,
		withRetryAfter(http.StatusTooManyRequests, "1", nil),
		withRetryAfter(http.StatusTooManyRequests, "2", nil),
		withRetryAfter(http.StatusTooManyRequests, "3", nil),
	)
	r := newRig(s)
	r.MaxRetryWait = 10 * time.Second

	_, err := r.Read(context.Background())
	wantKind(t, err, backend.KindRateLimited, "retry after 3 s")
	be := asError(t, err)
	if be.RetryAfter != 3*time.Second {
		t.Errorf("RetryAfter = %v, want 3s", be.RetryAfter)
	}
	if want := []time.Duration{time.Second, 2 * time.Second}; fmt.Sprint(r.sleeps.waits) != fmt.Sprint(want) {
		t.Errorf("sleeps = %v, want %v", r.sleeps.waits, want)
	}
	if n := len(s.requests()); n != 3 {
		t.Errorf("%d requests, want 3", n)
	}
}

func TestRetryAfterParsing(t *testing.T) {
	tests := []struct {
		name   string
		header string
		want   time.Duration
	}{
		{"integer seconds", "7", 7 * time.Second},
		{"missing", "", 5 * time.Second},
		{"not a number", "soon", 5 * time.Second},
		{"http date", "Wed, 21 Oct 2026 07:28:00 GMT", 5 * time.Second},
		{"fraction", "1.5", 5 * time.Second},
		{"negative", "-3", 5 * time.Second},
		{"zero", "0", 5 * time.Second},
		{"absurdly large", "99999999999999999999", 5 * time.Second},
		{"large but valid", "86400000", 24 * time.Hour},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := newRig(newScript(t, withRetryAfter(http.StatusTooManyRequests, tt.header, nil)))
			_, err := r.Read(context.Background())
			wantKind(t, err, backend.KindRateLimited, "rate limited")
			if got := asError(t, err).RetryAfter; got != tt.want {
				t.Errorf("RetryAfter = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestServerErrorRetriesOnceAfterOneSecond(t *testing.T) {
	s := newScript(t, reply{status: http.StatusInternalServerError, body: fixture(t, "error-500.json")}, ok(fixture(t, "player-track.json")))
	r := newRig(s)

	if _, err := r.Read(context.Background()); err != nil {
		t.Fatalf("Read: %v", err)
	}
	if want := []time.Duration{time.Second}; fmt.Sprint(r.sleeps.waits) != fmt.Sprint(want) {
		t.Errorf("sleeps = %v, want %v", r.sleeps.waits, want)
	}
	if n := len(s.requests()); n != 2 {
		t.Errorf("%d requests, want 2", n)
	}
}

func TestServerErrorTwiceIsUnexpected(t *testing.T) {
	s := newScript(t, status(http.StatusServiceUnavailable), status(http.StatusBadGateway))
	r := newRig(s)

	_, err := r.Read(context.Background())
	wantKind(t, err, backend.KindUnexpected, "unexpected HTTP status 502 from the Spotify Web API")
	if n := len(s.requests()); n != 2 {
		t.Errorf("%d requests, want 2", n)
	}
}

func TestOtherStatusIsUnexpectedWithoutRetry(t *testing.T) {
	for _, code := range []int{http.StatusBadRequest, http.StatusNotFound, http.StatusTeapot} {
		t.Run(http.StatusText(code), func(t *testing.T) {
			s := newScript(t, status(code))
			r := newRig(s)
			_, err := r.Read(context.Background())
			wantKind(t, err, backend.KindUnexpected, fmt.Sprintf("unexpected HTTP status %d from the Spotify Web API", code))
			if n := len(s.requests()); n != 1 || len(r.sleeps.waits) != 0 {
				t.Errorf("%d requests, sleeps %v; want one request and no sleep", n, r.sleeps.waits)
			}
		})
	}
}

// flakyTransport fails the first n requests with err, then delegates.
type flakyTransport struct {
	fail  int
	err   error
	next  http.RoundTripper
	calls int
}

func (f *flakyTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	f.calls++
	if f.calls <= f.fail {
		return nil, f.err
	}
	return f.next.RoundTrip(req)
}

func TestNetworkErrorRetriesOnceAfterOneSecond(t *testing.T) {
	s := newScript(t, ok(fixture(t, "player-track.json")))
	r := newRig(s)
	ft := &flakyTransport{fail: 1, err: errors.New("connection reset"), next: s.srv.Client().Transport}
	r.HTTPClient = &http.Client{Transport: ft}

	if _, err := r.Read(context.Background()); err != nil {
		t.Fatalf("Read: %v", err)
	}
	if want := []time.Duration{time.Second}; fmt.Sprint(r.sleeps.waits) != fmt.Sprint(want) || ft.calls != 2 {
		t.Errorf("sleeps %v, attempts %d; want [1s] and 2", r.sleeps.waits, ft.calls)
	}
}

func TestNetworkErrorTwiceIsUnexpected(t *testing.T) {
	r := newRig(newScript(t))
	ft := &flakyTransport{fail: 2, err: errors.New("connection refused")}
	r.HTTPClient = &http.Client{Transport: ft}

	_, err := r.Read(context.Background())
	wantKind(t, err, backend.KindUnexpected, "could not reach the Spotify Web API")
	be := asError(t, err)
	if be.Err == nil || ft.calls != 2 {
		t.Errorf("cause %v, attempts %d; want the cause kept and 2 attempts", be.Err, ft.calls)
	}
}

// deadlineTransport records the request deadline and fails as a timeout would.
type deadlineTransport struct {
	deadlines []time.Duration
}

func (d *deadlineTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	if dl, ok := req.Context().Deadline(); ok {
		d.deadlines = append(d.deadlines, time.Until(dl))
	}
	return nil, context.DeadlineExceeded
}

func TestEachAttemptHasAFiveSecondTimeout(t *testing.T) {
	r := newRig(newScript(t))
	dt := &deadlineTransport{}
	r.HTTPClient = &http.Client{Transport: dt}

	_, err := r.Read(context.Background())
	wantKind(t, err, backend.KindUnexpected, "did not answer within 5 s")
	if len(dt.deadlines) != 2 {
		t.Fatalf("%d attempts with a deadline, want 2 (one retry)", len(dt.deadlines))
	}
	for i, left := range dt.deadlines {
		if left <= 4*time.Second || left > 5*time.Second {
			t.Errorf("attempt %d has %v left, want just under 5 s", i+1, left)
		}
	}
}

func TestCancelledContextStopsWithoutRetry(t *testing.T) {
	s := newScript(t)
	r := newRig(s)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := r.Read(ctx)
	wantKind(t, err, backend.KindUnexpected, "read cancelled")
	if len(r.sleeps.waits) != 0 || len(s.requests()) != 0 {
		t.Errorf("sleeps %v, requests %d; want none", r.sleeps.waits, len(s.requests()))
	}
}

func TestCancelledWhileWaiting(t *testing.T) {
	s := newScript(t, status(http.StatusInternalServerError))
	r := newRig(s)
	r.Sleep = func(context.Context, time.Duration) error { return context.Canceled }

	_, err := r.Read(context.Background())
	wantKind(t, err, backend.KindUnexpected, "read cancelled")
	if n := len(s.requests()); n != 1 {
		t.Errorf("%d requests, want 1", n)
	}
}

func TestMalformedBodyIsUnexpected(t *testing.T) {
	for name, body := range map[string]string{
		"not json":  `not json`,
		"empty":     ``,
		"wrong typ": `{"is_playing":"yes"}`,
		"truncated": `{"is_playing":true,"item":{"name":`,
	} {
		t.Run(name, func(t *testing.T) {
			r := newRig(newScript(t, ok([]byte(body))))
			_, err := r.Read(context.Background())
			wantKind(t, err, backend.KindUnexpected, "unexpected response from the Spotify Web API")
		})
	}
}

func TestBodyIsCappedAtOneMebibyte(t *testing.T) {
	// Without the cap this is a valid body for nothing playing.
	body := `{"is_playing":true,"padding":"` + strings.Repeat("a", 1<<20+1024) + `"}`
	r := newRig(newScript(t, ok([]byte(body))))

	_, err := r.Read(context.Background())
	wantKind(t, err, backend.KindUnexpected, "unexpected response from the Spotify Web API")
}

func TestGoldenJSON(t *testing.T) {
	for _, name := range goldenFixtures() {
		t.Run(name, func(t *testing.T) {
			r := newRig(newScript(t, ok(fixture(t, name+".json"))))
			np, err := r.Read(context.Background())
			if err != nil {
				t.Fatalf("Read: %v", err)
			}
			got, err := format.JSON(&np)
			if err != nil {
				t.Fatal(err)
			}
			checkGolden(t, name, got)
		})
	}
}

// TestSecretsNeverReachErrorsOrLogs drives the backend through every response
// class. The server echoes the Authorization header in its error message, as a hostile or buggy
// server could.
func TestSecretsNeverReachErrorsOrLogs(t *testing.T) {
	echo := func(code int) reply {
		return reply{status: code, body: []byte(`{"error":{"message":"you sent ` + accessToken + ` and ` + refreshedToken + `"}}`)}
	}
	tests := []struct {
		name    string
		replies []reply
	}{
		{"ok", []reply{ok(fixture(t, "player-track.json"))}},
		{"no content", []reply{status(http.StatusNoContent)}},
		{"refresh then ok", []reply{echo(http.StatusUnauthorized), ok(fixture(t, "player-track.json"))}},
		{"second 401", []reply{echo(http.StatusUnauthorized), echo(http.StatusUnauthorized)}},
		{"forbidden", []reply{echo(http.StatusForbidden)}},
		{"rate limited", []reply{withRetryAfter(http.StatusTooManyRequests, "30", echo(0).body)}},
		{"server error", []reply{echo(http.StatusInternalServerError), echo(http.StatusInternalServerError)}},
		{"other status", []reply{echo(http.StatusTeapot)}},
		{"bad body", []reply{ok([]byte(accessToken))}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var logs bytes.Buffer
			r := newRig(newScript(t, tt.replies...))
			r.Log = slog.New(slog.NewTextHandler(&logs, &slog.HandlerOptions{Level: slog.LevelDebug}))

			_, err := r.Read(context.Background())
			seen := logs.String()
			if err != nil {
				seen += fmt.Sprintf("%v %+v", err, err)
				for e := err; e != nil; e = errors.Unwrap(e) {
					seen += e.Error()
				}
				be := asError(t, err)
				seen += be.Message + be.Hint
			}
			for _, secret := range []string{accessToken, refreshedToken, "Bearer"} {
				if strings.Contains(seen, secret) {
					t.Errorf("%q leaked into errors or logs:\n%s", secret, seen)
				}
			}
		})
	}
}

func TestDebugLogRecordsMethodHostPathStatusDuration(t *testing.T) {
	var logs bytes.Buffer
	s := newScript(t, ok(fixture(t, "player-track.json")))
	r := newRig(s)
	r.Log = slog.New(slog.NewTextHandler(&logs, &slog.HandlerOptions{Level: slog.LevelDebug}))

	if _, err := r.Read(context.Background()); err != nil {
		t.Fatal(err)
	}
	line := logs.String()
	host := strings.TrimPrefix(s.srv.URL, "http://")
	for _, want := range []string{"method=GET", "host=" + host, "path=/v1/me/player", "status=200", "duration="} {
		if !strings.Contains(line, want) {
			t.Errorf("log %q lacks %q", line, want)
		}
	}
	for _, banned := range []string{"additional_types", "Amazone", "Alvan"} {
		if strings.Contains(line, banned) {
			t.Errorf("log %q must not contain %q", line, banned)
		}
	}
}

func TestRedirectsAreNotFollowed(t *testing.T) {
	elsewhere := newScript(t)
	s := newScript(t, reply{status: http.StatusFound, header: map[string]string{"Location": elsewhere.srv.URL + "/v1/me/player"}})
	r := newRig(s)

	_, err := r.Read(context.Background())
	wantKind(t, err, backend.KindUnexpected, "unexpected HTTP status 302 from the Spotify Web API")
	if n := len(elsewhere.requests()); n != 0 {
		t.Errorf("the redirect target received %d requests, want 0", n)
	}
}
