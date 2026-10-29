package cli

import (
	"log/slog"
	"strings"
	"time"

	"github.com/bkuberek/spmeta/internal/auth"
	"github.com/bkuberek/spmeta/internal/backend"
	"github.com/bkuberek/spmeta/internal/backend/web"
	"github.com/bkuberek/spmeta/internal/config"
	"github.com/bkuberek/spmeta/internal/doctor"
	"github.com/bkuberek/spmeta/internal/resolve"
)

// webBackend returns the Web backend the resolver reads, or nil when there is
// no saved login. One-shot reads wait out a short Retry-After inside the read;
// watch mode leaves that wait to the watch loop. In auto watch mode the
// backend is throttled to web_interval, because the local interval is shorter.
func webBackend(s config.Settings, o options, info BuildInfo, log *slog.Logger) backend.Backend {
	retryWait := web.OneShotRetryWait
	if s.Watch {
		retryWait = 0
	}
	b := newWeb(s, o, info, log, retryWait)
	if b == nil || !s.Watch || s.Source != string(resolve.ModeAuto) {
		return b
	}
	return resolve.Throttle(b, s.WebInterval, o.now)
}

// newWeb builds the Web backend over the token file, or returns nil when the
// file does not exist: without a login Web is off, and nothing is requested.
// A file that exists but is unusable still gets a backend, so that reading it
// reports the problem (exit 5). A backend set with WithWebBackend wins.
func newWeb(s config.Settings, o options, info BuildInfo, log *slog.Logger, retryWait time.Duration) backend.Backend {
	if o.web != nil {
		return o.web
	}
	if s.TokenPath == "" {
		return nil
	}
	tok, err := auth.ReadToken(s.TokenPath)
	if err != nil && backend.KindOf(err) == backend.KindNotAvailable {
		return nil
	}
	if err == nil {
		noteClientID(log, s.ClientID, tok.ClientID)
	}
	return &web.Backend{
		Tokens: &auth.TokenSource{
			Path: s.TokenPath, Endpoints: o.endpoints, Now: o.now, Sleep: o.sleep, Log: log,
		},
		Endpoints:    o.endpoints,
		Now:          o.now,
		Sleep:        o.sleep,
		Log:          log,
		MaxRetryWait: retryWait,
		Version:      info.Version,
	}
}

// noteClientID logs when the configured Client ID is not the one the login
// used. Refreshing always uses the saved one, so the difference matters only
// to a user who changed apps and has not logged in again. Client IDs are not
// secrets; no token is involved.
func noteClientID(log *slog.Logger, configured, saved string) {
	configured = strings.TrimSpace(configured)
	if configured == "" || configured == saved {
		return
	}
	log.Debug("configured client ID differs from the saved login; refreshing keeps using the saved one; run `spmeta auth login` to switch",
		"configured", configured, "saved", saved)
}

// webReport tells doctor about the Web setup. Its Reader makes the one Web
// request doctor is allowed, without waiting for rate limits.
func webReport(s config.Settings, o options, info BuildInfo, log *slog.Logger) doctor.WebReport {
	r := doctor.WebReport{TokenPath: s.TokenPath}
	b := newWeb(s, o, info, log, 0)
	if b == nil {
		return r
	}
	r.Reader = b
	if tok, err := auth.ReadToken(s.TokenPath); err == nil {
		r.RefreshExpires = tok.RefreshExpiresAt()
	}
	if mode, ok := fileMode(s.TokenPath); ok && mode&0o077 != 0 {
		r.WideMode = true
	}
	return r
}
