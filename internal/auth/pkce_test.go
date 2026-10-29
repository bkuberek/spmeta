package auth_test

import (
	"regexp"
	"testing"

	"github.com/bkuberek/spmeta/internal/auth"
)

var urlSafe = regexp.MustCompile(`^[A-Za-z0-9_-]+$`)

func TestChallengeRFC7636Vector(t *testing.T) {
	t.Parallel()
	// RFC 7636 Appendix B.
	const (
		verifier  = "dBjftJeZ4CVP-mB92K27uhbUJU1p1r_wW1gFWFOEjXk"
		challenge = "E9Melhoa2OwvFrEMTJguCHaoeK1t8URWbuGJSstw-cM"
	)
	if got := auth.Challenge(verifier); got != challenge {
		t.Errorf("Challenge(%q) = %q, want %q", verifier, got, challenge)
	}
}

func TestNewVerifierLengthAndCharset(t *testing.T) {
	t.Parallel()
	seen := make(map[string]bool)
	for range 1000 {
		v := auth.NewVerifier()
		if len(v) != 64 || !urlSafe.MatchString(v) {
			t.Fatalf("verifier has length %d or characters outside [A-Za-z0-9_-]", len(v))
		}
		if seen[v] {
			t.Fatal("NewVerifier returned the same verifier twice")
		}
		seen[v] = true
		if c := auth.Challenge(v); len(c) != 43 || !urlSafe.MatchString(c) {
			t.Fatalf("challenge has length %d or characters outside [A-Za-z0-9_-]", len(c))
		}
	}
}

func TestNewStateLengthAndCharset(t *testing.T) {
	t.Parallel()
	seen := make(map[string]bool)
	for range 1000 {
		s := auth.NewState()
		if len(s) != 22 || !urlSafe.MatchString(s) {
			t.Fatalf("state has length %d or characters outside [A-Za-z0-9_-]", len(s))
		}
		if seen[s] {
			t.Fatal("NewState returned the same state twice")
		}
		seen[s] = true
	}
}
