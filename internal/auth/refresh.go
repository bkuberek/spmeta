package auth

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/bkuberek/spmeta/internal/backend"
)

const (
	defaultAccounts   = "https://accounts.spotify.com"
	defaultAPI        = "https://api.spotify.com"
	tokenEndpointPath = "/api/token"

	tokenTimeout    = 10 * time.Second
	maxResponseBody = 1 << 20

	lockFileSuffix = ".lock"
	lockTimeout    = 10 * time.Second
	lockPollEvery  = 100 * time.Millisecond

	msgInvalidGrant = "Spotify rejected the saved login (refresh tokens expire 6 months after login, or access was revoked)."
)

// errLockHeld is what tryLock returns while another holder has the lock.
var errLockHeld = errors.New("token lock is held by another process")

// Endpoints are the base URLs of the two Spotify services. Tests point them at
// httptest servers.
type Endpoints struct {
	Accounts string // default "https://accounts.spotify.com"
	API      string // default "https://api.spotify.com"
}

// WithDefaults returns e with the production URL in each empty field.
func (e Endpoints) WithDefaults() Endpoints {
	if e.Accounts == "" {
		e.Accounts = defaultAccounts
	}
	if e.API == "" {
		e.API = defaultAPI
	}
	return e
}

func (e Endpoints) tokenURL() string {
	return strings.TrimRight(e.WithDefaults().Accounts, "/") + tokenEndpointPath
}

// TokenSource returns valid access tokens from the token file at Path and
// refreshes them when they are about to expire. Only Path is required; every
// other zero value selects the production behavior.
type TokenSource struct {
	Path       string
	Endpoints  Endpoints
	HTTPClient *http.Client // nil uses http.DefaultClient
	Now        func() time.Time
	// Sleep waits between attempts to take the file lock; nil sleeps for real.
	Sleep func(ctx context.Context, d time.Duration) error
	Log   *slog.Logger // debug lines without secrets; nil discards
}

func (s *TokenSource) now() time.Time {
	if s.Now != nil {
		return s.Now()
	}
	return time.Now()
}

func (s *TokenSource) sleep(ctx context.Context, d time.Duration) error {
	if s.Sleep != nil {
		return s.Sleep(ctx, d)
	}
	return sleepFor(ctx, d)
}

// sleepFor waits for d or until ctx ends.
func sleepFor(ctx context.Context, d time.Duration) error {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}

func (s *TokenSource) endpoint() tokenEndpoint {
	return newTokenEndpoint(s.Endpoints, s.HTTPClient, s.Log)
}

// newTokenEndpoint fills the defaults of a nil client and a nil logger.
func newTokenEndpoint(e Endpoints, client *http.Client, log *slog.Logger) tokenEndpoint {
	if client == nil {
		client = http.DefaultClient
	}
	if log == nil {
		log = slog.New(slog.DiscardHandler)
	}
	return tokenEndpoint{url: e.tokenURL(), client: client, log: log}
}

// Token returns an access token that stays valid for more than 60 s. It
// refreshes the token first when it is closer to expiry than that, or always
// when force is set (after the API answered 401). A missing token file is
// KindNotAvailable; a rejected refresh token, such as after the 6-month
// expiry or a revoked grant, is KindAuthRequired; transport failures are
// KindUnexpected.
//
// Refreshing takes an exclusive lock on the token file's .lock sibling, then
// re-reads the file: a process that waited finds the token its peer refreshed
// and sends no request of its own.
func (s *TokenSource) Token(ctx context.Context, force bool) (string, error) {
	tok, err := ReadToken(s.Path)
	if err != nil {
		return "", err
	}
	if !force && tok.fresh(s.now()) {
		return tok.AccessToken, nil
	}

	release, err := s.acquire(ctx)
	if err != nil {
		return "", err
	}
	defer release()

	cur, err := ReadToken(s.Path)
	if err != nil {
		return "", err
	}
	if cur.AccessToken != tok.AccessToken && cur.fresh(s.now()) {
		return cur.AccessToken, nil
	}
	return s.refresh(ctx, cur)
}

// acquire polls for the lock every 100 ms for up to 10 s.
func (s *TokenSource) acquire(ctx context.Context) (func(), error) {
	path := s.Path + lockFileSuffix
	deadline := s.now().Add(lockTimeout)
	for {
		release, err := tryLock(path)
		switch {
		case err == nil:
			return release, nil
		case !errors.Is(err, errLockHeld):
			return nil, unexpected("could not lock the token file.", err)
		case !s.now().Before(deadline):
			return nil, unexpected("gave up waiting for another spmeta process to refresh the login.", nil)
		}
		if err := s.sleep(ctx, lockPollEvery); err != nil {
			return nil, unexpected("cancelled while waiting to refresh the login.", err)
		}
	}
}

func (s *TokenSource) refresh(ctx context.Context, cur Token) (string, error) {
	// The Client ID stored at login is used, whatever the configuration says
	// now: the refresh token is bound to the app that issued it.
	resp, err := s.endpoint().post(ctx, url.Values{
		"grant_type":    {"refresh_token"},
		"refresh_token": {cur.RefreshToken},
		"client_id":     {cur.ClientID},
	})
	var refused *tokenError
	switch {
	case errors.As(err, &refused):
		return "", refreshRefused(refused)
	case err != nil:
		return "", err
	}
	next := resp.applyTo(cur, s.now())
	if err := WriteToken(s.Path, next); err != nil {
		return "", err
	}
	return next.AccessToken, nil
}

// refreshRefused maps a 400 from the token endpoint. Only the error code is
// shown; the description is server text and is not echoed.
func refreshRefused(e *tokenError) *backend.Error {
	msg := msgInvalidGrant
	if e.Code != "invalid_grant" {
		msg = "Spotify refused to refresh the saved login"
		if e.Code != "" {
			msg += ": " + e.Code
		}
		msg += "."
	}
	return &backend.Error{Kind: backend.KindAuthRequired, Message: msg, Hint: hintLogin, Err: e}
}

// tokenResponse is the body of a 200 from POST /api/token.
type tokenResponse struct {
	AccessToken  string `json:"access_token"`
	TokenType    string `json:"token_type"`
	Scope        string `json:"scope"`
	ExpiresIn    int64  `json:"expires_in"` // seconds
	RefreshToken string `json:"refresh_token"`
}

// applyTo returns t updated with a refresh response. The access token and
// expiry always change; the scope and the refresh token change only when the
// response carries them, so a refresh without rotation keeps the saved one.
func (r tokenResponse) applyTo(t Token, now time.Time) Token {
	t.AccessToken = r.AccessToken
	t.ExpiresAt = now.Add(time.Duration(r.ExpiresIn) * time.Second)
	if r.Scope != "" {
		t.Scope = r.Scope
	}
	if r.RefreshToken != "" {
		t.RefreshToken = r.RefreshToken
	}
	return t
}

// tokenError is a 400 from POST /api/token: the server refused the grant. Code
// keeps only lowercase letters, digits and underscores, so it is safe to
// print. Description is server text.
type tokenError struct {
	Code        string
	Description string
}

func (e *tokenError) Error() string {
	if e.Code == "" {
		return "token request refused"
	}
	return "token request refused: " + e.Code
}

// tokenEndpoint posts forms to POST /api/token.
type tokenEndpoint struct {
	url    string
	client *http.Client
	log    *slog.Logger
}

// post sends form and decodes the answer. It returns a *tokenError for a 400
// and a *backend.Error of KindUnexpected for every other failure: no
// connection, timeout, cancellation, a status other than 200 and 400, and a
// 200 without a usable token. The request has a 10 s timeout and is never
// retried. Neither the form nor the response body reaches an error or the log.
func (e tokenEndpoint) post(ctx context.Context, form url.Values) (tokenResponse, error) {
	reqCtx, cancel := context.WithTimeout(ctx, tokenTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(reqCtx, http.MethodPost, e.url, strings.NewReader(form.Encode()))
	if err != nil {
		return tokenResponse{}, unexpected("could not build the token request.", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")

	start := time.Now()
	resp, err := e.client.Do(req)
	if err != nil {
		return tokenResponse{}, transportError(ctx, err)
	}
	defer func() { _ = resp.Body.Close() }()
	e.log.Debug("http", "method", req.Method, "host", req.URL.Host, "path", req.URL.Path,
		"status", resp.StatusCode, "duration", time.Since(start))

	body, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseBody))
	if err != nil {
		return tokenResponse{}, transportError(ctx, err)
	}
	switch resp.StatusCode {
	case http.StatusOK:
		return decodeTokenResponse(body)
	case http.StatusBadRequest:
		return tokenResponse{}, decodeTokenError(body)
	default:
		return tokenResponse{}, unexpected(fmt.Sprintf("unexpected HTTP status %d from the Spotify accounts service.", resp.StatusCode), nil)
	}
}

func transportError(ctx context.Context, err error) *backend.Error {
	switch {
	case ctx.Err() != nil:
		return unexpected("the token request was cancelled.", err)
	case errors.Is(err, context.DeadlineExceeded):
		return unexpected(fmt.Sprintf("Spotify did not answer the token request within %s.", tokenTimeout), err)
	default:
		return unexpected("could not reach the Spotify accounts service.", err)
	}
}

func decodeTokenResponse(body []byte) (tokenResponse, error) {
	var r tokenResponse
	if err := json.Unmarshal(body, &r); err != nil {
		return tokenResponse{}, unexpected("unexpected response from the Spotify accounts service.", err)
	}
	if r.AccessToken == "" || r.ExpiresIn <= 0 {
		return tokenResponse{}, unexpected("the Spotify accounts service returned an incomplete token.", nil)
	}
	return r, nil
}

// decodeTokenError tolerates any body: an unparsable one yields an empty code.
func decodeTokenError(body []byte) *tokenError {
	var raw struct {
		Error       string `json:"error"`
		Description string `json:"error_description"`
	}
	_ = json.Unmarshal(body, &raw)
	return &tokenError{Code: safeCode(raw.Error), Description: raw.Description}
}

// safeCode returns code when it looks like an OAuth error code (such as
// invalid_grant) and an empty string otherwise.
func safeCode(code string) string {
	if len(code) > 40 {
		return ""
	}
	for _, r := range code {
		if (r < 'a' || r > 'z') && (r < '0' || r > '9') && r != '_' {
			return ""
		}
	}
	return code
}
