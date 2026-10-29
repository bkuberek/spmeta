package web

import (
	"context"
	"flag"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/bkuberek/spmeta/internal/auth"
)

var update = flag.Bool("update", false, "rewrite golden files")

var fetchedAt = time.Date(2026, 10, 27, 17, 40, 0, 0, time.UTC)

const (
	accessToken    = "access-SECRET-1"
	refreshedToken = "access-SECRET-2"
	testVersion    = "1.2.3"
)

func ptr[T any](v T) *T { return &v }

func fixture(t *testing.T, name string) []byte {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	return data
}

// goldenFixtures are served as 200 responses and compared with
// testdata/golden/<name>.json.
func goldenFixtures() []string {
	return []string{
		"player-track",
		"player-track-paused",
		"player-episode",
		"player-ad",
		"player-local-file",
		"player-images-no-dimensions",
		"player-no-isrc",
	}
}

func checkGolden(t *testing.T, name string, got []byte) {
	t.Helper()
	file := filepath.Join("testdata", "golden", name+".json")
	if *update {
		if err := os.WriteFile(file, got, 0o644); err != nil {
			t.Fatalf("update %s: %v", file, err)
		}
		return
	}
	want, err := os.ReadFile(file)
	if err != nil {
		t.Fatalf("read golden (run with -update to create): %v", err)
	}
	if string(want) != string(got) {
		t.Errorf("%s mismatch\n got: %s\nwant: %s", file, got, want)
	}
}

// fakeTokens is a TokenSource that hands out accessToken, and refreshedToken
// after a forced call.
type fakeTokens struct {
	err   error // returned by every call when set
	errOn int   // when nonzero, only the call with this number (1-based) fails
	calls []bool
}

func (f *fakeTokens) Token(_ context.Context, force bool) (string, error) {
	f.calls = append(f.calls, force)
	if f.err != nil && (f.errOn == 0 || f.errOn == len(f.calls)) {
		return "", f.err
	}
	if force {
		return refreshedToken, nil
	}
	return accessToken, nil
}

// fakeSleep records the waits instead of sleeping.
type fakeSleep struct{ waits []time.Duration }

func (f *fakeSleep) sleep(_ context.Context, d time.Duration) error {
	f.waits = append(f.waits, d)
	return nil
}

// reply is one scripted response.
type reply struct {
	status int
	body   []byte
	header map[string]string
}

func ok(body []byte) reply { return reply{status: http.StatusOK, body: body} }

func status(code int) reply { return reply{status: code} }

func withRetryAfter(code int, seconds string, body []byte) reply {
	return reply{status: code, body: body, header: map[string]string{"Retry-After": seconds}}
}

// seenRequest is what the scripted server recorded about one request.
type seenRequest struct {
	method, path, query string
	header              http.Header
}

// script is an httptest server that answers each request with the next reply.
// A request past the end of the script fails the test.
type script struct {
	srv *httptest.Server

	mu   sync.Mutex
	seen []seenRequest
}

func newScript(t *testing.T, replies ...reply) *script {
	t.Helper()
	s := &script{}
	s.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		s.mu.Lock()
		s.seen = append(s.seen, seenRequest{r.Method, r.URL.Path, r.URL.RawQuery, r.Header.Clone()})
		n := len(s.seen)
		s.mu.Unlock()
		if n > len(replies) {
			t.Errorf("request %d has no scripted reply", n)
			w.WriteHeader(http.StatusTeapot)
			return
		}
		rep := replies[n-1]
		for k, v := range rep.header {
			w.Header().Set(k, v)
		}
		w.WriteHeader(rep.status)
		_, _ = w.Write(rep.body)
	}))
	t.Cleanup(s.srv.Close)
	return s
}

func (s *script) requests() []seenRequest {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]seenRequest(nil), s.seen...)
}

// rig is a Backend wired to a scripted server with fake clocks and tokens.
type rig struct {
	*Backend
	tokens *fakeTokens
	sleeps *fakeSleep
}

func newRig(s *script) *rig {
	tokens := &fakeTokens{}
	sleeps := &fakeSleep{}
	return &rig{
		Backend: &Backend{
			Tokens:     tokens,
			Endpoints:  auth.Endpoints{API: s.srv.URL},
			HTTPClient: s.srv.Client(),
			Now:        func() time.Time { return fetchedAt },
			Sleep:      sleeps.sleep,
			Version:    testVersion,
		},
		tokens: tokens,
		sleeps: sleeps,
	}
}
