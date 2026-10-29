package auth

import (
	"context"
	"crypto/subtle"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"strings"
	"syscall"
	"time"
	"unicode"

	"github.com/bkuberek/spmeta/internal/backend"
)

const (
	authorizePath = "/authorize"
	callbackPath  = "/callback"
	loginScope    = "user-read-playback-state"

	loginTimeout   = 180 * time.Second
	shutdownGrace  = time.Second
	headerTimeout  = 5 * time.Second
	maxDescription = 200

	hintAppSettings = "Check the Client ID, and that %s is a redirect URI of your Spotify app."
)

// DefaultLoginPort is the loopback port Login listens on unless told
// otherwise. The Spotify dashboard rejects a redirect URI without a port, so
// users register http://127.0.0.1:8974/callback.
const DefaultLoginPort = 8974

// LoginOptions configures Login. ClientID and Path are required; every other
// zero value selects the production behavior.
type LoginOptions struct {
	ClientID string
	// Port is the loopback port to listen on; 0 picks a free one, which only
	// works for an app that registered a redirect URI without a port.
	Port       int
	Path       string // the token file to write
	Endpoints  Endpoints
	HTTPClient *http.Client // nil uses http.DefaultClient
	// Open shows the authorization URL in a browser. Nil only prints the URL
	// (--no-browser). A failure is logged and ignored.
	Open func(url string) error
	Now  func() time.Time
	// Sleep waits for the 180 s callback timeout; nil sleeps for real.
	Sleep  func(ctx context.Context, d time.Duration) error
	Stderr io.Writer    // receives the authorization URL; nil discards
	Log    *slog.Logger // debug lines without secrets; nil discards
}

func (o LoginOptions) now() time.Time {
	if o.Now != nil {
		return o.Now()
	}
	return time.Now()
}

func (o LoginOptions) sleep(ctx context.Context, d time.Duration) error {
	if o.Sleep != nil {
		return o.Sleep(ctx, d)
	}
	return sleepFor(ctx, d)
}

func (e Endpoints) authorizeURL() string {
	return strings.TrimRight(e.WithDefaults().Accounts, "/") + authorizePath
}

// callbackResult is the first usable request to the callback: either an
// authorization code, or Spotify's refusal. reason is the sanitized OAuth
// error code and may be empty.
type callbackResult struct {
	code   string
	denied bool
	reason string
}

// Login runs the Authorization Code with PKCE flow (docs/design/backend-web.md,
// Login flow): it serves the redirect URI on 127.0.0.1, prints the
// authorization URL, waits up to 180 s for the browser to come back,
// exchanges the code, and stores the token at opts.Path.
//
// A refusal by the user, a timeout, a cancelled context and a refused
// exchange are KindAuthRequired. A busy port, a network failure and a
// failure to save the token are KindUnexpected. The authorization code, the
// verifier and the tokens never reach an error, the log or stderr.
func Login(ctx context.Context, opts LoginOptions) (Token, error) {
	log := opts.Log
	if log == nil {
		log = slog.New(slog.DiscardHandler)
	}
	verifier, state := NewVerifier(), NewState()

	ln, err := listenLoopback(opts.Port)
	if err != nil {
		return Token{}, err
	}
	// The same string goes into both requests: Spotify compares them byte for
	// byte.
	redirectURI := "http://" + ln.Addr().String() + callbackPath
	log.Debug("login listening", "redirect_uri", redirectURI)

	results := make(chan callbackResult, 1)
	srv := &http.Server{Handler: callbackHandler(state, results), ReadHeaderTimeout: headerTimeout}
	go func() { _ = srv.Serve(ln) }()
	defer shutdown(srv)

	authURL := opts.Endpoints.authorizeURL() + "?" + url.Values{
		"client_id":             {opts.ClientID},
		"response_type":         {"code"},
		"redirect_uri":          {redirectURI},
		"code_challenge_method": {"S256"},
		"code_challenge":        {Challenge(verifier)},
		"state":                 {state},
		"scope":                 {loginScope},
	}.Encode()
	if opts.Stderr != nil {
		_, _ = fmt.Fprintf(opts.Stderr, "Open this URL to log in:\n%s\n", authURL)
	}
	if opts.Open != nil {
		if err := opts.Open(authURL); err != nil {
			log.Debug("could not open the browser", "error", err)
		}
	}

	res, err := awaitCallback(ctx, opts, results)
	if err != nil {
		return Token{}, err
	}
	if res.denied {
		return Token{}, authFailed(withDetail("Spotify authorization failed", res.reason))
	}
	return exchangeCode(ctx, opts, log, redirectURI, verifier, res.code)
}

// listenLoopback listens on 127.0.0.1 only, so no other machine can reach the
// callback.
func listenLoopback(port int) (net.Listener, error) {
	ln, err := net.Listen("tcp4", fmt.Sprintf("127.0.0.1:%d", port))
	switch {
	case err == nil:
		return ln, nil
	case addrInUse(err):
		return nil, unexpected(fmt.Sprintf("port %d is in use.", port), err)
	default:
		return nil, unexpected("could not listen on the loopback interface.", err)
	}
}

// addrInUse reports whether err says the port is taken. Windows reports
// WSAEADDRINUSE (10048), which syscall.EADDRINUSE does not match there.
func addrInUse(err error) bool {
	const wsaEADDRINUSE = syscall.Errno(10048)
	return errors.Is(err, syscall.EADDRINUSE) || errors.Is(err, wsaEADDRINUSE)
}

// shutdown stops the callback server, giving a response in flight a second.
func shutdown(srv *http.Server) {
	ctx, cancel := context.WithTimeout(context.Background(), shutdownGrace)
	defer cancel()
	if err := srv.Shutdown(ctx); err != nil {
		_ = srv.Close()
	}
}

// callbackHandler answers the browser's requests per the Login flow table and
// passes the first usable one to results. Anything else is ignored so that a
// stray request cannot end the login.
func callbackHandler(state string, results chan<- callbackResult) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != callbackPath {
			http.NotFound(w, r)
			return
		}
		q := r.URL.Query()
		if subtle.ConstantTimeCompare([]byte(q.Get("state")), []byte(state)) != 1 {
			reply(w, http.StatusBadRequest, "State mismatch. Return to the terminal.")
			return
		}
		var res callbackResult
		switch {
		case q.Has("error"):
			reply(w, http.StatusOK, "Authorization was not granted. You can close this tab.")
			res = callbackResult{denied: true, reason: safeCode(q.Get("error"))}
		case q.Get("code") != "":
			reply(w, http.StatusOK, "spmeta is logged in. You can close this tab.")
			res = callbackResult{code: q.Get("code")}
		default:
			reply(w, http.StatusBadRequest, "Missing authorization code. Return to the terminal.")
			return
		}
		select {
		case results <- res:
		default:
		}
	})
}

func reply(w http.ResponseWriter, status int, text string) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.WriteHeader(status)
	_, _ = io.WriteString(w, text+"\n")
}

// awaitCallback waits for the browser, the 180 s timeout or cancellation.
func awaitCallback(ctx context.Context, opts LoginOptions, results <-chan callbackResult) (callbackResult, error) {
	timerCtx, stop := context.WithCancel(ctx)
	defer stop()
	timedOut := make(chan struct{})
	go func() {
		if opts.sleep(timerCtx, loginTimeout) == nil {
			close(timedOut)
		}
	}()
	select {
	case res := <-results:
		return res, nil
	case <-timedOut:
		return callbackResult{}, authFailed(fmt.Sprintf("no response from the browser within %.0f s.", loginTimeout.Seconds()))
	case <-ctx.Done():
		return callbackResult{}, authFailed("login was cancelled.")
	}
}

// exchangeCode trades the authorization code for tokens and stores them.
func exchangeCode(ctx context.Context, opts LoginOptions, log *slog.Logger, redirectURI, verifier, code string) (Token, error) {
	resp, err := newTokenEndpoint(opts.Endpoints, opts.HTTPClient, log).post(ctx, url.Values{
		"grant_type":    {"authorization_code"},
		"code":          {code},
		"redirect_uri":  {redirectURI},
		"client_id":     {opts.ClientID},
		"code_verifier": {verifier},
	})
	var refused *tokenError
	switch {
	case errors.As(err, &refused):
		return Token{}, loginRefused(refused, code, redirectURI)
	case err != nil:
		return Token{}, err
	case resp.RefreshToken == "":
		return Token{}, unexpected("the Spotify accounts service returned no refresh token.", nil)
	}
	now := opts.now()
	tok := resp.applyTo(Token{ClientID: opts.ClientID, AuthorizedAt: now}, now)
	if err := WriteToken(opts.Path, tok); err != nil {
		return Token{}, err
	}
	// The token as stored: defaults filled in and times cut to the second.
	return ReadToken(opts.Path)
}

// loginRefused maps a 400 from the code exchange. The description is server
// text, so it is shown only when it is one short printable line that does not
// repeat the authorization code.
func loginRefused(e *tokenError, code, redirectURI string) *backend.Error {
	d := e.Description
	if strings.Contains(d, code) || len(d) > maxDescription || strings.IndexFunc(d, func(r rune) bool { return !unicode.IsPrint(r) }) >= 0 {
		d = ""
	}
	if d == "" {
		d = e.Code
	}
	return &backend.Error{Kind: backend.KindAuthRequired, Message: withDetail("Spotify refused the login", d), Hint: fmt.Sprintf(hintAppSettings, redirectURI), Err: e}
}

func authFailed(msg string) *backend.Error {
	return &backend.Error{Kind: backend.KindAuthRequired, Message: msg}
}

// withDetail returns "msg: detail", or "msg." when there is no detail.
func withDetail(msg, detail string) string {
	if detail == "" {
		return msg + "."
	}
	return msg + ": " + detail
}
