package model_test

import (
	"testing"

	"github.com/bkuberek/spmeta/internal/model"
)

func TestFormatDuration(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		ms   int64
		want string
	}{
		{"zero", 0, "0:00"},
		{"negative clamps to zero", -5, "0:00"},
		{"sub-second truncates", 999, "0:00"},
		{"one second", 1000, "0:01"},
		{"typical track", 238341, "3:58"},
		{"last second below an hour", 3599999, "59:59"},
		{"exactly one hour", 3600000, "1:00:00"},
		{"hours minutes seconds", 3723000, "1:02:03"},
		{"double digit hours", 36000000, "10:00:00"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := model.FormatDuration(tc.ms); got != tc.want {
				t.Errorf("FormatDuration(%d) = %q, want %q", tc.ms, got, tc.want)
			}
		})
	}
}
