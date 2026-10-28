//go:build !darwin && !linux

package local

import (
	"context"

	"github.com/bkuberek/spmeta/internal/backend"
	"github.com/bkuberek/spmeta/internal/model"
)

type unsupported struct{}

// New returns a backend that always reports an unsupported platform.
func New(Settings) backend.Backend { return unsupported{} }

func (unsupported) Source() model.Source { return "" }

func (unsupported) Read(context.Context) (model.NowPlaying, error) {
	return model.NowPlaying{}, &backend.Error{
		Kind:    backend.KindNotAvailable,
		Message: "unsupported platform.",
	}
}
