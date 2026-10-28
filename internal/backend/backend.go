// Package backend defines the interface every Spotify source implements and
// the typed errors they return.
package backend

import (
	"context"

	"github.com/bkuberek/spmeta/internal/model"
)

// Backend reads the current item from one source.
type Backend interface {
	// Source identifies the backend in output and errors.
	Source() model.Source
	// Read returns the current item, already normalized, or a *Error.
	// It must honor ctx cancellation and deadlines.
	Read(ctx context.Context) (model.NowPlaying, error)
}
