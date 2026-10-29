package auth

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
)

const (
	verifierBytes = 48 // encodes to 64 characters, within RFC 7636's 43 to 128
	stateBytes    = 16
)

// NewVerifier returns a PKCE code verifier (RFC 7636): 48 random bytes,
// base64url-encoded without padding, which is 64 characters from
// [A-Za-z0-9_-]. crypto/rand.Read never returns an error.
func NewVerifier() string { return randomString(verifierBytes) }

// Challenge returns the S256 code challenge for verifier: the unpadded
// base64url encoding of its SHA-256 digest.
func Challenge(verifier string) string {
	sum := sha256.Sum256([]byte(verifier))
	return base64.RawURLEncoding.EncodeToString(sum[:])
}

// NewState returns an unguessable value for the OAuth state parameter: 16
// random bytes, base64url-encoded without padding.
func NewState() string { return randomString(stateBytes) }

func randomString(n int) string {
	b := make([]byte, n)
	_, _ = rand.Read(b)
	return base64.RawURLEncoding.EncodeToString(b)
}
