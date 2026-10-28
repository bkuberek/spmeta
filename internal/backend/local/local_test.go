//go:build darwin

package local

import (
	"testing"

	"github.com/bkuberek/spmeta/internal/model"
)

func TestNewReturnsMacOSBackend(t *testing.T) {
	t.Parallel()
	if got := New(Settings{}).Source(); got != model.SourceMacOS {
		t.Errorf("Source() = %q, want %q", got, model.SourceMacOS)
	}
}
