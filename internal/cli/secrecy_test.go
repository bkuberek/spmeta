package cli_test

import (
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

const (
	rotatedAccess  = "BQD-rotated-access-token"
	rotatedRefresh = "AQD-rotated-refresh-token"
)

// TestDebugOutputHasNoSecrets runs a fake login, reads that need no refresh, a
// refresh, a forced refresh after a 401, and a doctor run, all with --debug,
// against fake Spotify services that try to echo secrets back in error
// bodies. No token, authorization code or PKCE verifier may reach stdout or
// stderr, and --debug must still say something.
func TestDebugOutputHasNoSecrets(t *testing.T) {
	s := newWebSetup(t)
	secrets := []string{authCode, authAccess, authRefresh, rotatedAccess, rotatedRefresh, savedAccess, savedRefresh}
	check := func(step, stdout, stderr string, wantDebug string) {
		t.Helper()
		if !strings.Contains(stderr, "level=DEBUG") || !strings.Contains(stderr, wantDebug) {
			t.Errorf("%s: --debug stderr lacks %q:\n%s", step, wantDebug, stderr)
		}
		for _, secret := range secrets {
			if strings.Contains(stdout, secret) || strings.Contains(stderr, secret) {
				t.Errorf("%s: a secret value (%d bytes, starting %q) reached the output:\nstdout: %s\nstderr: %s",
					step, len(secret), secret[:4], stdout, stderr)
			}
		}
	}

	// Login: the code exchange sends the authorization code and the verifier,
	// and gets the first tokens back.
	code, stdout, stderr := s.run(t, "login", "--client-id", authClientID, "--debug")
	if code != 0 {
		t.Fatalf("login: exit = %d, stderr %q", code, stderr)
	}
	verifier := s.lastForm().Get("code_verifier")
	if verifier == "" {
		t.Fatal("login: the token request carried no code_verifier")
	}
	secrets = append(secrets, verifier)
	check("login", stdout, stderr, "login")

	// Read with a fresh token: the Authorization header carries the access token.
	code, stdout, stderr = s.read(t, pausedLocal(), "--source", "web", "--format", "json", "--debug")
	if code != 0 {
		t.Fatalf("read: exit = %d, stderr %q", code, stderr)
	}
	check("read", stdout, stderr, "path=/v1/me/player")

	// Refresh: the saved token has expired, so the refresh token is sent and
	// rotated tokens come back.
	s.tokenReply = func(w http.ResponseWriter, _ *http.Request) {
		respond(w, 200, nil, `{"access_token":"`+rotatedAccess+`","token_type":"Bearer","expires_in":3600,"refresh_token":"`+rotatedRefresh+`"}`)
	}
	s.now = authNow.Add(2 * time.Hour)
	before := s.tokenReqs.Load()
	code, stdout, stderr = s.read(t, pausedLocal(), "--source", "web", "--format", "json", "--debug")
	if code != 0 || s.tokenReqs.Load() != before+1 {
		t.Fatalf("refresh: exit = %d, token requests = %d (was %d), stderr %q", code, s.tokenReqs.Load(), before, stderr)
	}
	if got := s.lastForm().Get("refresh_token"); got != authRefresh {
		t.Fatalf("refresh: sent refresh_token %q, want the one from login", got)
	}
	check("refresh", stdout, stderr, "path=/api/token")

	// A 401 forces another refresh; the API then rejects the new token too and
	// echoes it in the error body.
	var n atomic.Int32
	s.reply = func(w http.ResponseWriter, _ int) {
		if n.Add(1) == 1 {
			respond(w, 401, nil, `{"error":{"status":401,"message":"The access token expired: `+rotatedAccess+`"}}`)
			return
		}
		respond(w, 403, nil, `{"error":{"status":403,"message":"Forbidden for `+rotatedAccess+`"}}`)
	}
	code, stdout, stderr = s.read(t, pausedLocal(), "--source", "web", "--debug")
	if code != 6 {
		t.Fatalf("401 then 403: exit = %d, want 6 (stderr %q)", code, stderr)
	}
	check("forced refresh and refusal", stdout, stderr, "status=401")

	// A refused refresh whose description echoes the refresh token.
	s.tokenReply = func(w http.ResponseWriter, _ *http.Request) {
		respond(w, 400, nil, `{"error":"invalid_grant","error_description":"Invalid refresh token `+rotatedRefresh+`"}`)
	}
	s.now = s.now.Add(2 * time.Hour)
	code, stdout, stderr = s.read(t, pausedLocal(), "--source", "web", "--debug")
	if code != 5 {
		t.Fatalf("refused refresh: exit = %d, want 5 (stderr %q)", code, stderr)
	}
	check("refused refresh", stdout, stderr, "path=/api/token")

	// Doctor reads once and logs its causes.
	code, stdout, stderr = s.doctor(t, grantedMac(), "--debug")
	if code != 0 {
		t.Fatalf("doctor: exit = %d, stderr %q", code, stderr)
	}
	check("doctor", stdout, stderr, "name=web")
}
