//go:build darwin

package hostapp

import (
	"context"
	"os"
	"strings"
	"testing"
)

// TestSnapshotRunsTheRealPs runs /bin/ps, which only reads the process table.
func TestSnapshotRunsTheRealPs(t *testing.T) {
	out, err := snapshot(context.Background())
	if err != nil {
		t.Fatalf("snapshot: %v", err)
	}
	if !strings.Contains(out, " ") || strings.TrimSpace(out) == "" {
		t.Fatalf("snapshot = %q, want the process table", out)
	}
	// The walk must cope with the real format; the result depends on the
	// terminal running the tests, so only the call is checked.
	_ = FromProcesses(out, os.Getppid())
	if got := Resolve(context.Background(), "", nil); strings.TrimSpace(got) != got {
		t.Errorf("Resolve = %q, want no surrounding space", got)
	}
}
