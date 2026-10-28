//go:build linux

package local

import (
	"time"

	"github.com/bkuberek/spmeta/internal/backend"
	"github.com/bkuberek/spmeta/internal/backend/mpris"
)

// New returns the Linux backend, which reads MPRIS players over the session
// bus.
func New(s Settings) backend.Backend {
	return mpris.New(mpris.Dial, s.Player, time.Now)
}
