package macos

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/bkuberek/spmeta/internal/backend"
)

// Changes is what watch mode looks for on the local backend.
var _ backend.Notifier = (*Backend)(nil)

const (
	helperModeEnv    = "SPMETA_LISTENER_HELPER"
	helperNameEnv    = "SPMETA_LISTENER_NAME"
	testNotification = "dev.spmeta.test.helper"
	// stuck bounds every wait in these tests; reaching it is a failure.
	stuck = 10 * time.Second
)

// TestListenerHelper is the body of the fake listener processes the Changes
// tests start instead of osascript; it does nothing in a normal run. Like
// osascript, it reads the script from stdin until stdin closes. It exits 2
// when the script does not observe the expected notification name.
func TestListenerHelper(t *testing.T) {
	mode := os.Getenv(helperModeEnv)
	if mode == "" {
		t.Skip("helper for the Changes tests")
	}
	script, err := io.ReadAll(os.Stdin)
	if err != nil || !strings.Contains(string(script), os.Getenv(helperNameEnv)) {
		fmt.Fprintln(os.Stderr, "the script does not observe the expected notification")
		os.Exit(2)
	}
	switch mode {
	case "changes": // ready, then a change every 10 ms until killed
		fmt.Println(lineReady)
		for {
			fmt.Println(lineChanged)
			time.Sleep(10 * time.Millisecond)
		}
	case "idle": // ready, then nothing until killed
		fmt.Println(lineReady)
		time.Sleep(time.Hour)
	case "silent": // never ready
		time.Sleep(time.Hour)
	case "long": // a line too long for the reader, then nothing until killed
		fmt.Println(strings.Repeat("x", 70_000))
		time.Sleep(time.Hour)
	case "once": // ready, one change, then exit
		fmt.Println(lineReady)
		fmt.Println(lineChanged)
	case "noise": // ready, then lines that are not changes, then exit
		fmt.Println(lineReady)
		for _, line := range []string{"", lineReady, "Changed", "changed twice", "undefined"} {
			fmt.Println(line)
		}
	case "fail": // a script error: osascript reports it on stderr and exits 1
		fmt.Fprintln(os.Stderr, "osascript: starting")
		fmt.Fprintln(os.Stderr, "-:120:121: execution error: Error: SyntaxError (-2700)")
		os.Exit(1)
	}
	os.Exit(0)
}

// helper runs TestListenerHelper in a mode and keeps the command so tests
// can check how the process ended.
type helper struct {
	mode string
	cmd  *exec.Cmd
}

func (h *helper) command() *exec.Cmd {
	h.cmd = exec.Command(os.Args[0], "-test.run=^TestListenerHelper$")
	h.cmd.Env = append(os.Environ(), helperModeEnv+"="+h.mode, helperNameEnv+"="+testNotification)
	return h.cmd
}

// listening returns a backend whose listener is the helper in mode.
func listening(mode string) (*Backend, *helper) {
	h := &helper{mode: mode}
	b := newBackend(&fakeRunner{})
	b.listen = h.command
	b.notification = testNotification
	b.readyTimeout = stuck
	return b, h
}

// waitClosed fails unless ch closes within stuck, ignoring pending wakes.
func waitClosed(t *testing.T, ch <-chan struct{}) {
	t.Helper()
	deadline := time.After(stuck)
	for {
		select {
		case _, ok := <-ch:
			if !ok {
				return
			}
		case <-deadline:
			t.Fatal("the change channel did not close")
		}
	}
}

// reaped returns how the helper process ended, or fails when it was not
// started and reaped.
func reaped(t *testing.T, h *helper) *os.ProcessState {
	t.Helper()
	if h.cmd == nil || h.cmd.ProcessState == nil {
		t.Error("the listener process was not started and reaped")
		return nil
	}
	return h.cmd.ProcessState
}

// checkKilled fails unless the helper process was reaped after a kill signal.
func checkKilled(t *testing.T, h *helper) {
	t.Helper()
	if ps := reaped(t, h); ps != nil && ps.ExitCode() != -1 {
		t.Errorf("the listener ended with %v; want it killed", ps)
	}
}

func TestChangesWakesOnChangeLines(t *testing.T) {
	t.Parallel()
	b, _ := listening("changes")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	wake, err := b.Changes(ctx)
	if err != nil {
		t.Fatalf("Changes: %v", err)
	}
	for i := range 3 {
		select {
		case _, ok := <-wake:
			if !ok {
				t.Fatalf("the channel closed after %d wakes", i)
			}
		case <-time.After(stuck):
			t.Fatalf("no wake %d", i+1)
		}
	}
}

func TestChangesKillsTheListenerWhenContextEnds(t *testing.T) {
	t.Parallel()
	b, h := listening("idle")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	wake, err := b.Changes(ctx)
	if err != nil {
		t.Fatalf("Changes: %v", err)
	}
	cancel()
	waitClosed(t, wake)
	checkKilled(t, h)
}

func TestChangesClosesWhenTheListenerExits(t *testing.T) {
	t.Parallel()
	b, h := listening("once")

	wake, err := b.Changes(context.Background())
	if err != nil {
		t.Fatalf("Changes: %v", err)
	}
	waitClosed(t, wake)
	reaped(t, h)
}

func TestChangesIgnoresOtherLines(t *testing.T) {
	t.Parallel()
	b, _ := listening("noise")

	wake, err := b.Changes(context.Background())
	if err != nil {
		t.Fatalf("Changes: %v", err)
	}
	select {
	case _, ok := <-wake:
		if ok {
			t.Error("woken by a line that is not a change")
		}
	case <-time.After(stuck):
		t.Fatal("the change channel did not close")
	}
}

func TestChangesStartErrors(t *testing.T) {
	t.Parallel()

	t.Run("not ready in time: killed", func(t *testing.T) {
		t.Parallel()
		b, h := listening("silent")
		b.readyTimeout = 200 * time.Millisecond
		if wake, err := b.Changes(context.Background()); wake != nil || err == nil || !strings.Contains(err.Error(), "not ready within 200ms") {
			t.Errorf("Changes = %v, %v; want nil and a ready timeout", wake, err)
		}
		checkKilled(t, h)
	})

	t.Run("context ends before ready: killed", func(t *testing.T) {
		t.Parallel()
		b, h := listening("silent")
		ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
		defer cancel()
		if wake, err := b.Changes(ctx); wake != nil || !errors.Is(err, context.DeadlineExceeded) {
			t.Errorf("Changes = %v, %v; want nil and %v", wake, err, context.DeadlineExceeded)
		}
		checkKilled(t, h)
	})

	t.Run("exits before ready: reaped, stderr reported", func(t *testing.T) {
		t.Parallel()
		b, h := listening("fail")
		wake, err := b.Changes(context.Background())
		if wake != nil || err == nil {
			t.Fatalf("Changes = %v, %v; want nil and an error", wake, err)
		}
		for _, want := range []string{"exited before it was ready", "exit status 1", "SyntaxError (-2700)"} {
			if !strings.Contains(err.Error(), want) {
				t.Errorf("error %q does not mention %q", err, want)
			}
		}
		if strings.Contains(err.Error(), "starting") {
			t.Errorf("error %q reports more than the last stderr line", err)
		}
		reaped(t, h)
	})

	t.Run("unreadable output before ready: killed, read error reported", func(t *testing.T) {
		t.Parallel()
		b, h := listening("long")
		wake, err := b.Changes(context.Background())
		if wake != nil || err == nil || !strings.Contains(err.Error(), "token too long") {
			t.Errorf("Changes = %v, %v; want nil and the read error", wake, err)
		}
		checkKilled(t, h)
	})

	t.Run("program missing", func(t *testing.T) {
		t.Parallel()
		b, _ := listening("")
		b.listen = func() *exec.Cmd { return exec.Command("/nonexistent/spmeta-osascript") }
		if wake, err := b.Changes(context.Background()); wake != nil || err == nil {
			t.Errorf("Changes = %v, %v; want nil and an error", wake, err)
		}
	})
}

func TestListenerScript(t *testing.T) {
	t.Parallel()
	quoted := func(s string) string {
		q, err := json.Marshal(s)
		if err != nil {
			t.Fatal(err)
		}
		return string(q)
	}

	t.Run("observes Spotify's notification by default", func(t *testing.T) {
		t.Parallel()
		b := newBackend(&fakeRunner{})
		if want := quoted("com.spotify.client.PlaybackStateChanged"); !strings.Contains(listenerScript(b.notification, b.heartbeat), want) {
			t.Errorf("the default script does not observe %s", want)
		}
	})

	t.Run("observes the given name as a string literal", func(t *testing.T) {
		t.Parallel()
		name := `dev.spmeta.test.a"b\c`
		if want := quoted(name); !strings.Contains(listenerScript(name, time.Second), want) {
			t.Errorf("the script does not observe %s", want)
		}
	})

	t.Run("sends no Apple events", func(t *testing.T) {
		t.Parallel()
		script := listenerScript(testNotification, time.Second)
		for _, apple := range []string{"Application(", "tell application"} {
			if strings.Contains(script, apple) {
				t.Errorf("the listener script contains %q, which would talk to an app", apple)
			}
		}
	})

	t.Run("ticks every heartbeat, by default every 5 s", func(t *testing.T) {
		t.Parallel()
		tests := []struct {
			heartbeat time.Duration
			want      string
		}{
			{newBackend(&fakeRunner{}).heartbeat, "TargetSelectorUserInfoRepeats(5, observer, 'onTick:'"},
			{1500 * time.Millisecond, "TargetSelectorUserInfoRepeats(1.5, observer, 'onTick:'"},
		}
		for _, tc := range tests {
			if script := listenerScript(testNotification, tc.heartbeat); !strings.Contains(script, tc.want) {
				t.Errorf("the script for a %v heartbeat does not schedule %s", tc.heartbeat, tc.want)
			}
		}
	})
}
