// Package auth implements the Spotify Web API login: PKCE, the token file, and
// token refresh with a file lock. See docs/design/backend-web.md.
package auth

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"time"

	"github.com/bkuberek/spmeta/internal/backend"
)

const (
	tokenFileVersion = 1
	defaultTokenType = "Bearer"
	// refreshMargin is how far from expiry an access token still counts as
	// valid.
	refreshMargin = 60 * time.Second

	msgNotLoggedIn = "not logged in to the Spotify Web API"
	hintLogin      = "Run `spmeta auth login`."
)

// Token is the content of the token file. Its formatting methods redact the
// secrets so that a stray %v or log call cannot leak them.
type Token struct {
	Version      int       `json:"version"`
	ClientID     string    `json:"client_id"`
	AccessToken  string    `json:"access_token"`
	TokenType    string    `json:"token_type"`
	RefreshToken string    `json:"refresh_token"`
	Scope        string    `json:"scope"`
	ExpiresAt    time.Time `json:"expires_at"`
	AuthorizedAt time.Time `json:"authorized_at"`
}

// fresh reports whether the access token is valid for more than the refresh
// margin after now.
func (t Token) fresh(now time.Time) bool { return t.ExpiresAt.Sub(now) > refreshMargin }

// String omits the access and refresh tokens.
func (t Token) String() string {
	return fmt.Sprintf("auth.Token{client_id=%s expires_at=%s authorized_at=%s}",
		t.ClientID, t.ExpiresAt.Format(time.RFC3339), t.AuthorizedAt.Format(time.RFC3339))
}

// GoString makes %#v as safe as %v.
func (t Token) GoString() string { return t.String() }

// LogValue keeps the secrets out of slog output.
func (t Token) LogValue() slog.Value { return slog.StringValue(t.String()) }

// ReadToken reads and validates the token file. A missing file is
// KindNotAvailable ("not logged in"); a file that cannot be read or parsed is
// KindAuthRequired. A file with a wide mode is still used.
func ReadToken(path string) (Token, error) {
	data, err := os.ReadFile(path)
	switch {
	case errors.Is(err, fs.ErrNotExist):
		return Token{}, &backend.Error{Kind: backend.KindNotAvailable, Message: msgNotLoggedIn, Hint: hintLogin, Err: err}
	case err != nil:
		return Token{}, unusable("could not read the saved Spotify login.", err)
	}
	var t Token
	if err := json.Unmarshal(data, &t); err != nil {
		return Token{}, unusable("the saved Spotify login is not valid.", err)
	}
	switch {
	case t.Version != tokenFileVersion:
		return Token{}, unusable(fmt.Sprintf("the saved Spotify login has unsupported version %d.", t.Version), nil)
	case t.ClientID == "" || t.AccessToken == "" || t.RefreshToken == "":
		return Token{}, unusable("the saved Spotify login is incomplete.", nil)
	}
	return t, nil
}

func unusable(msg string, cause error) *backend.Error {
	return &backend.Error{Kind: backend.KindAuthRequired, Message: msg, Hint: hintLogin, Err: cause}
}

func unexpected(msg string, cause error) *backend.Error {
	return &backend.Error{Kind: backend.KindUnexpected, Message: msg, Err: cause}
}

// WriteToken stores t at path atomically: it writes a temporary file with mode
// 0600 in the same directory, syncs it, and renames it over path. The parent
// directory is created with mode 0700 when missing. The version is always
// written as the current one, and times are stored in UTC to the second.
func WriteToken(path string, t Token) error {
	if path == "" {
		return unexpected("no location is known for the token file.", nil)
	}
	t.Version = tokenFileVersion
	if t.TokenType == "" {
		t.TokenType = defaultTokenType
	}
	t.ExpiresAt = t.ExpiresAt.UTC().Truncate(time.Second)
	t.AuthorizedAt = t.AuthorizedAt.UTC().Truncate(time.Second)
	data, err := json.MarshalIndent(t, "", "  ")
	if err != nil {
		return unexpected("could not encode the Spotify login.", err)
	}
	data = append(data, '\n')

	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return unexpected("could not create the token directory.", err)
	}
	// CreateTemp opens the file with mode 0600.
	tmp, err := os.CreateTemp(dir, ".token-*.tmp")
	if err != nil {
		return unexpected("could not save the Spotify login.", err)
	}
	renamed := false
	defer func() {
		if !renamed {
			_ = tmp.Close()
			_ = os.Remove(tmp.Name())
		}
	}()
	if _, err := tmp.Write(data); err != nil {
		return unexpected("could not save the Spotify login.", err)
	}
	if err := tmp.Sync(); err != nil {
		return unexpected("could not save the Spotify login.", err)
	}
	if err := tmp.Close(); err != nil {
		return unexpected("could not save the Spotify login.", err)
	}
	if err := os.Rename(tmp.Name(), path); err != nil {
		return unexpected("could not save the Spotify login.", err)
	}
	renamed = true
	return nil
}
