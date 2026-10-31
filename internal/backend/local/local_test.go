//go:build darwin

package local

import (
	"testing"

	"github.com/bkuberek/spmeta/internal/backend"
	"github.com/bkuberek/spmeta/internal/model"
)

func TestNewReturnsMacOSBackend(t *testing.T) {
	t.Parallel()
	if got := New(Settings{}).Source(); got != model.SourceMacOS {
		t.Errorf("Source() = %q, want %q", got, model.SourceMacOS)
	}
}

func TestNewAnnouncesChanges(t *testing.T) {
	t.Parallel()
	if _, ok := New(Settings{}).(backend.Notifier); !ok {
		t.Error("the macOS backend must implement backend.Notifier so watch mode reads changes at once")
	}
}
