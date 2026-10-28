//go:build linux

package local

import (
	"testing"

	"github.com/bkuberek/spmeta/internal/model"
)

func TestNewReturnsMPRISBackend(t *testing.T) {
	t.Parallel()
	if got := New(Settings{Player: "spotifyd"}).Source(); got != model.SourceMPRIS {
		t.Errorf("Source() = %q, want %q", got, model.SourceMPRIS)
	}
}
