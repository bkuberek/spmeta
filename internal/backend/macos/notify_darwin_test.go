//go:build darwin

package macos

import (
	"bufio"
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

const (
	// privatePrefix keeps the test's notifications out of every real app's
	// namespace; the tests never post a Spotify notification.
	privatePrefix   = "dev.spmeta.test."
	orphanHelperEnv = "SPMETA_ORPHAN_HELPER"
	// orphanHeartbeat is the listener heartbeat in the orphan test, short so
	// the test takes well under a second.
	orphanHeartbeat = 200 * time.Millisecond
)

func skipWithoutOsascript(t *testing.T) {
	t.Helper()
	if _, err := os.Stat(osascriptBin); err != nil {
		t.Skipf("no %s: %v", osascriptBin, err)
	}
}

func privateName() string {
	return privatePrefix + strings.ToLower(rand.Text())
}

// failUnlessCI fails the test, or skips it on CI, where the runner may have
// no user session to deliver distributed notifications.
func failUnlessCI(t *testing.T, format string, args ...any) {
	t.Helper()
	msg := fmt.Sprintf(format, args...)
	if os.Getenv("CI") != "" {
		t.Skip(msg + " (skipped because CI is set: the runner may not deliver distributed notifications)")
	}
	t.Fatal(msg)
}

// TestListenerReceivesDistributedNotifications runs the real listener with
// osascript, observing a private random name, and posts that name from a
// second osascript process. It sends no Apple events and touches no app.
func TestListenerReceivesDistributedNotifications(t *testing.T) {
	skipWithoutOsascript(t)
	name := privateName()
	b := New(ExecRunner{}, time.Now)
	b.notification = name
	b.readyTimeout = 10 * time.Second
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	wake, err := b.Changes(ctx)
	switch {
	case errors.Is(err, errNotReady):
		failUnlessCI(t, "Changes: %v", err)
	case err != nil:
		t.Fatalf("Changes: %v", err)
	}
	// Post again every 250 ms until the wake arrives, in case a slow runner
	// delivers late.
	deadline := time.After(10 * time.Second)
	for received := false; !received; {
		post(t, name)
		select {
		case _, ok := <-wake:
			if !ok {
				t.Fatal("the change channel closed before the notification arrived")
			}
			received = true
		case <-time.After(250 * time.Millisecond):
		case <-deadline:
			failUnlessCI(t, "no wake within 10s of posting %s", name)
		}
	}

	cancel()
	waitClosed(t, wake)
}

// post sends the distributed notification name from another process.
func post(t *testing.T, name string) {
	t.Helper()
	if !strings.HasPrefix(name, privatePrefix) {
		t.Fatalf("refusing to post %q outside %s", name, privatePrefix)
	}
	quoted, err := json.Marshal(name)
	if err != nil {
		t.Fatal(err)
	}
	script := "ObjC.import('Foundation');\n" +
		"$.NSDistributedNotificationCenter.defaultCenter" +
		".postNotificationNameObjectUserInfoDeliverImmediately(" + string(quoted) + ", $(), $(), true);\n"
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if out, err := exec.CommandContext(ctx, osascriptBin, "-l", "JavaScript", "-e", script).CombinedOutput(); err != nil {
		t.Fatalf("post %s: %v: %s", name, err, out)
	}
}

// TestOrphanHelper is the body of the short-lived parent process that
// TestListenerDiesWithItsParent starts and kills; it does nothing in a normal
// run. It starts the real listener on a private name with a short heartbeat,
// prints the listener's process ID, and waits to be killed.
func TestOrphanHelper(t *testing.T) {
	name := os.Getenv(orphanHelperEnv)
	if name == "" {
		t.Skip("helper for TestListenerDiesWithItsParent")
	}
	b := New(ExecRunner{}, time.Now)
	b.notification = name
	b.heartbeat = orphanHeartbeat
	var cmd *exec.Cmd
	b.listen = func() *exec.Cmd {
		cmd = listenerCommand()
		return cmd
	}
	if _, err := b.Changes(context.Background()); err != nil {
		fmt.Println("error:", err)
		os.Exit(1)
	}
	fmt.Printf("pid=%d\n", cmd.Process.Pid)
	time.Sleep(time.Hour)
}

// TestListenerDiesWithItsParent kills the process that started a real
// listener, as a SIGKILL or SIGHUP of spmeta would, and checks that the
// listener exits on its own within a few heartbeats instead of running on as
// an orphan.
func TestListenerDiesWithItsParent(t *testing.T) {
	skipWithoutOsascript(t)
	parent := exec.Command(os.Args[0], "-test.run=^TestOrphanHelper$")
	parent.Env = append(os.Environ(), orphanHelperEnv+"="+privateName())
	out, err := parent.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := parent.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = parent.Process.Kill()
		_ = parent.Wait()
	})
	pid := listenerPID(t, out)
	t.Cleanup(func() {
		if t.Failed() {
			_ = syscall.Kill(pid, syscall.SIGKILL) // do not leave a surviving listener behind
		}
	})

	_ = parent.Process.Kill()
	_ = parent.Wait()
	limit := 10 * orphanHeartbeat
	deadline := time.Now().Add(limit)
	for syscall.Kill(pid, 0) == nil {
		if time.Now().After(deadline) {
			t.Fatalf("listener %d still runs %v after its parent was killed", pid, limit)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

// listenerPID returns the listener process ID that TestOrphanHelper prints.
func listenerPID(t *testing.T, out io.Reader) int {
	t.Helper()
	lines := make(chan string, 1)
	go func() {
		defer close(lines)
		sc := bufio.NewScanner(out)
		for sc.Scan() {
			if l := sc.Text(); strings.HasPrefix(l, "pid=") || strings.HasPrefix(l, "error:") {
				lines <- l
				return
			}
		}
	}()
	select {
	case l := <-lines:
		pid, err := strconv.Atoi(strings.TrimPrefix(l, "pid="))
		if err != nil {
			t.Fatalf("the helper did not start the listener: %q", l)
		}
		return pid
	case <-time.After(stuck):
		t.Fatal("the helper printed no listener process ID")
	}
	return 0
}
