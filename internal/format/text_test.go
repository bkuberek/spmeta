package format_test

import (
	"testing"

	"github.com/bkuberek/spmeta/internal/format"
	"github.com/bkuberek/spmeta/internal/model"
)

func TestTextGolden(t *testing.T) {
	for _, f := range fixtures() {
		t.Run(f.name, func(t *testing.T) {
			checkGolden(t, "text/"+f.name+".golden", []byte(format.Text(f.np)))
		})
	}
	t.Run("nothing", func(t *testing.T) {
		checkGolden(t, "text/nothing.golden", []byte(format.Text(nil)))
	})
}

func TestTextAlwaysOneLine(t *testing.T) {
	for _, f := range fixtures() {
		t.Run(f.name, func(t *testing.T) {
			for _, r := range format.Text(f.np) {
				if r < ' ' || (r >= 0x7f && r <= 0x9f) {
					t.Fatalf("control character %U in %q", r, format.Text(f.np))
				}
			}
		})
	}
}

func TestStateIcon(t *testing.T) {
	tests := []struct {
		state model.State
		want  string
	}{
		{model.StatePlaying, "▶"},
		{model.StatePaused, "⏸"},
		{model.StateStopped, "■"},
		{model.State("bogus"), "■"},
	}
	for _, tc := range tests {
		if got := format.StateIcon(tc.state); got != tc.want {
			t.Errorf("StateIcon(%q) = %q, want %q", tc.state, got, tc.want)
		}
	}
}
