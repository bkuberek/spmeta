// Package resolve chooses which source answers a read, following ADR 0005.
package resolve

import (
	"context"
	"log/slog"

	"github.com/bkuberek/spmeta/internal/backend"
	"github.com/bkuberek/spmeta/internal/model"
)

// Mode selects which sources a read consults.
type Mode string

// Modes accepted by --source.
const (
	ModeAuto  Mode = "auto"
	ModeLocal Mode = "local"
	ModeWeb   Mode = "web"
)

// Message and hint for --source web without a login. They match `spmeta auth
// status`, which reports the same condition.
const (
	msgNotLoggedIn = "not logged in to the Spotify Web API"
	hintLogin      = "Run `spmeta auth login`."
)

// Resolver reads the current item from the sources its Mode allows.
type Resolver struct {
	Mode  Mode
	Local backend.Backend // never nil
	Web   backend.Backend // nil when there is no saved login
	// Logger receives one debug line per source read. Nil discards them.
	Logger *slog.Logger
}

// Static reports an error that every Read would return for the whole run,
// such as --source web without a saved login, so a caller can fail fast
// instead of polling. It returns nil when reads may succeed.
func (r *Resolver) Static() error {
	if r.Mode == ModeWeb && r.Web == nil {
		return &backend.Error{Kind: backend.KindNotAvailable, Source: model.SourceWeb, Message: msgNotLoggedIn, Hint: hintLogin}
	}
	return nil
}

// Read returns the current item per ADR 0005, or the highest-precedence error
// among the sources it tried.
func (r *Resolver) Read(ctx context.Context) (model.NowPlaying, error) {
	switch r.Mode {
	case ModeAuto:
		return r.readAuto(ctx)
	case ModeLocal:
		return r.read(ctx, "local", r.Local)
	case ModeWeb:
		if err := r.Static(); err != nil {
			r.log().Debug("skipping source", "source", "web", "reason", "not configured")
			return model.NowPlaying{}, err
		}
		return r.read(ctx, "web", r.Web)
	default:
		return model.NowPlaying{}, &backend.Error{Kind: backend.KindUnexpected, Message: "unknown source mode " + string(r.Mode)}
	}
}

func (r *Resolver) readAuto(ctx context.Context) (model.NowPlaying, error) {
	l, errL := r.read(ctx, "local", r.Local)
	if errL == nil && l.State == model.StatePlaying {
		return l, nil
	}
	var w model.NowPlaying
	var errW error
	if r.Web != nil {
		w, errW = r.read(ctx, "web", r.Web)
		if errW == nil && w.State == model.StatePlaying {
			return w, nil
		}
	} else {
		r.log().Debug("skipping source", "source", "web", "reason", "not configured")
	}
	switch {
	case errL == nil:
		return l, nil
	case r.Web != nil && errW == nil:
		return w, nil
	default:
		return model.NowPlaying{}, backend.Worse(errL, errW)
	}
}

func (r *Resolver) read(ctx context.Context, name string, b backend.Backend) (model.NowPlaying, error) {
	np, err := b.Read(ctx)
	if err != nil {
		r.log().Debug("source read", "source", name, "backend", b.Source(), "kind", kindName(backend.KindOf(err)), "error", err)
		return model.NowPlaying{}, err
	}
	r.log().Debug("source read", "source", name, "backend", b.Source(), "state", np.State)
	return np, nil
}

func (r *Resolver) log() *slog.Logger {
	if r.Logger == nil {
		return slog.New(slog.DiscardHandler)
	}
	return r.Logger
}

func kindName(k backend.Kind) string {
	switch k {
	case backend.KindNothingPlaying:
		return "nothing_playing"
	case backend.KindNotAvailable:
		return "not_available"
	case backend.KindAuthRequired:
		return "auth_required"
	case backend.KindPermissionDenied:
		return "permission_denied"
	case backend.KindRateLimited:
		return "rate_limited"
	default:
		return "unexpected"
	}
}
