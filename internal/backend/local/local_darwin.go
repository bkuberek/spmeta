package local

import (
	"time"

	"github.com/bkuberek/spmeta/internal/backend"
	"github.com/bkuberek/spmeta/internal/backend/macos"
)

// New returns the macOS backend, which reads Spotify.app through AppleScript.
func New(Settings) backend.Backend {
	return macos.New(macos.ExecRunner{}, time.Now)
}
