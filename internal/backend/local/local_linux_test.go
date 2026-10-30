//go:build linux

package local

import (
	"testing"

	"github.com/bkuberek/spmeta/internal/backend"
	"github.com/bkuberek/spmeta/internal/model"
)

func TestNewReturnsMPRISBackend(t *testing.T) {
	t.Parallel()
	if got := New(Settings{Player: "spotifyd"}).Source(); got != model.SourceMPRIS {
		t.Errorf("Source() = %q, want %q", got, model.SourceMPRIS)
	}
}

func TestNewAnnouncesChanges(t *testing.T) {
	t.Parallel()
	if _, ok := New(Settings{}).(backend.Notifier); !ok {
		t.Error("the Linux backend must implement backend.Notifier so watch mode reads changes at once")
	}
}
