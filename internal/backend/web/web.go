// Package web reads the current item from the Spotify Web API. See
// docs/design/backend-web.md.
package web

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"time"

	"github.com/bkuberek/spmeta/internal/auth"
	"github.com/bkuberek/spmeta/internal/backend"
	"github.com/bkuberek/spmeta/internal/model"
)

const (
	// OneShotRetryWait is the MaxRetryWait of a one-shot read. Watch mode
	// leaves MaxRetryWait at zero.
	OneShotRetryWait = 10 * time.Second

	defaultVersion = "dev"
	hintLogin      = "Run `spmeta auth login`."
)

// TokenSource returns the access token for the next request. With force set
// it first refreshes the token, as after a 401. *auth.TokenSource implements
// it.
type TokenSource interface {
	Token(ctx context.Context, force bool) (string, error)
}

var _ TokenSource = (*auth.TokenSource)(nil)

// Backend reads GET /v1/me/player. Only Tokens is required; every other zero
// value selects the production behavior.
type Backend struct {
	Tokens TokenSource
	// Endpoints.API is the base URL of the Web API; the Accounts field is
	// not used here.
	Endpoints  auth.Endpoints
	HTTPClient *http.Client // nil uses http.DefaultClient
	Now        func() time.Time
	// Sleep waits before a retry; nil sleeps for real.
	Sleep func(ctx context.Context, d time.Duration) error
	Log   *slog.Logger // debug lines without secrets; nil discards
	// MaxRetryWait is the longest Retry-After the read waits out before
	// retrying a 429. Zero never waits, which is what watch mode wants.
	MaxRetryWait time.Duration
	Version      string // for the User-Agent; empty means "dev"
}

// Source identifies this backend.
func (b *Backend) Source() model.Source { return model.SourceWeb }

// Read returns the current item. The request has a 5 s timeout per attempt
// and the retries of the response table in docs/design/backend-web.md: one
// refresh on 401, one retry after 1 s on a 5xx or network error, and up to
// three attempts on 429 when Retry-After fits MaxRetryWait.
func (b *Backend) Read(ctx context.Context) (model.NowPlaying, error) {
	body, err := b.fetch(ctx)
	if err != nil {
		return model.NowPlaying{}, err
	}
	p, err := decode(body)
	if err != nil {
		return model.NowPlaying{}, err
	}
	np, err := mapPlayer(p)
	if err != nil {
		return model.NowPlaying{}, err
	}
	np.FetchedAt = b.now()
	return model.Normalize(np), nil
}

func (b *Backend) now() time.Time {
	if b.Now != nil {
		return b.Now()
	}
	return time.Now()
}

func (b *Backend) sleep(ctx context.Context, d time.Duration) error {
	if b.Sleep != nil {
		return b.Sleep(ctx, d)
	}
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}

func (b *Backend) log() *slog.Logger {
	if b.Log != nil {
		return b.Log
	}
	return slog.New(slog.DiscardHandler)
}

func (b *Backend) version() string {
	if b.Version != "" {
		return b.Version
	}
	return defaultVersion
}

// token asks the token source. Its *backend.Error values (not logged in, login
// expired) pass through; anything else is KindUnexpected.
func (b *Backend) token(ctx context.Context, force bool) (string, error) {
	tok, err := b.Tokens.Token(ctx, force)
	if err == nil {
		return tok, nil
	}
	var be *backend.Error
	if errors.As(err, &be) {
		return "", err
	}
	return "", unexpected("could not get an access token", err)
}

func unexpected(msg string, cause error) *backend.Error {
	return &backend.Error{Kind: backend.KindUnexpected, Source: model.SourceWeb, Message: msg, Err: cause}
}

func errNothingPlaying() *backend.Error {
	return &backend.Error{Kind: backend.KindNothingPlaying, Source: model.SourceWeb, Message: "nothing is playing"}
}
