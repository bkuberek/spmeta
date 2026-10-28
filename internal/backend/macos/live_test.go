//go:build darwin

package macos

import (
	"context"
	"os"
	"testing"
	"time"
)

func skipUnlessLive(t *testing.T) {
	t.Helper()
	if os.Getenv("SPMETA_LIVE") != "1" {
		t.Skip("set SPMETA_LIVE=1 to run against this machine")
	}
}

func TestExecRunnerProcessRunningUnknownName(t *testing.T) {
	skipUnlessLive(t)
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	running, err := ExecRunner{}.ProcessRunning(ctx, "spmeta-no-such-process")
	if err != nil {
		t.Fatalf("ProcessRunning: %v", err)
	}
	if running {
		t.Error("ProcessRunning reported a process that does not exist")
	}
}

func TestLiveReadWhenSpotifyRuns(t *testing.T) {
	skipUnlessLive(t)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	running, err := ExecRunner{}.ProcessRunning(ctx, "Spotify")
	if err != nil || !running {
		t.Skip("Spotify is not running")
	}
	np, err := New(ExecRunner{}, time.Now).Read(ctx)
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if np.URI == nil {
		t.Error("live read returned no uri")
	}
}
