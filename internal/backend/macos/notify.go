package macos

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/bkuberek/spmeta/internal/backend"
)

const (
	// spotifyNotification is the distributed notification Spotify.app posts
	// when the track or the playback state changes. Spotify does not document
	// it.
	spotifyNotification  = "com.spotify.client.PlaybackStateChanged"
	listenerReadyTimeout = 2 * time.Second
	listenerHeartbeat    = 5 * time.Second
	lineReady            = "ready"
	lineChanged          = "changed"
	// stderrLimit bounds the listener's stderr kept for the start error.
	stderrLimit = 512
)

// Changes implements backend.Notifier. It starts one osascript process that
// runs the listener script and keeps it until ctx ends. The listener only
// observes Spotify's distributed notification and sends no Apple events, so
// it never launches Spotify and needs no Automation permission; it therefore
// runs without the process guard, even while Spotify is not running, and
// notifications arrive once Spotify starts.
//
// Changes returns once the listener prints "ready". When the listener exits
// first or does not print it within the ready timeout, Changes kills and
// reaps it and returns an error, and watch mode polls. The channel receives a
// value for each "changed" line; any other line is ignored, so a stray line
// costs no read. The channel closes when the listener exits.
func (b *Backend) Changes(ctx context.Context) (<-chan struct{}, error) {
	cmd := b.listen()
	cmd.Stdin = strings.NewReader(listenerScript(b.notification, b.heartbeat))
	stderr := &tail{limit: stderrLimit}
	cmd.Stderr = stderr
	cmd.WaitDelay = waitDelay
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, fmt.Errorf("start the change listener: %w", err)
	}
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("start the change listener: %w", err)
	}
	kill := func() { _ = cmd.Process.Kill() }
	stopKill := context.AfterFunc(ctx, kill)

	// The reader forwards stdout lines until the listener exits, then reaps
	// it. scanErr, waitErr, and stderr are complete once lines closes.
	lines := make(chan string)
	var scanErr, waitErr error
	go func() {
		defer close(lines)
		sc := bufio.NewScanner(stdout)
		for sc.Scan() {
			lines <- sc.Text()
		}
		scanErr = sc.Err()
		stopKill()
		kill() // stdout ended or broke: make sure the listener is gone
		waitErr = cmd.Wait()
	}()

	timer := time.NewTimer(b.readyTimeout)
	defer timer.Stop()
	for {
		select {
		case line, ok := <-lines:
			switch {
			case !ok && scanErr != nil:
				return nil, fmt.Errorf("read the change listener: %w", scanErr)
			case !ok:
				return nil, exitedEarly(waitErr, stderr)
			case line == lineReady:
				return backend.Coalesce(lines, func(l string) bool { return l == lineChanged }), nil
			}
		case <-timer.C:
			kill()
			drain(lines)
			return nil, fmt.Errorf("%w within %s", errNotReady, b.readyTimeout)
		case <-ctx.Done():
			drain(lines) // ctx ending killed the listener
			return nil, fmt.Errorf("start the change listener: %w", ctx.Err())
		}
	}
}

var errNotReady = errors.New("the change listener was not ready")

// drain discards lines until the reader closes the channel, which it does
// after reaping the listener.
func drain(lines <-chan string) {
	for range lines {
	}
}

// exitedEarly describes a listener that exited before printing "ready",
// with the last line of its stderr, such as a script error from osascript.
func exitedEarly(waitErr error, stderr *tail) error {
	status := "exit status 0"
	if waitErr != nil {
		status = waitErr.Error()
	}
	return fmt.Errorf("the change listener exited before it was ready (%s)%s", status, stderr.lastLine())
}

// tail keeps the last limit bytes written to it.
type tail struct {
	limit int
	buf   []byte
}

func (t *tail) Write(p []byte) (int, error) {
	t.buf = append(t.buf, p...)
	if over := len(t.buf) - t.limit; over > 0 {
		t.buf = t.buf[over:]
	}
	return len(p), nil
}

// lastLine returns ": " and the last nonempty line written, or "" when there
// is none.
func (t *tail) lastLine() string {
	b := bytes.TrimRight(t.buf, " \t\r\n")
	if i := bytes.LastIndexByte(b, '\n'); i >= 0 {
		b = b[i+1:]
	}
	if len(b) == 0 {
		return ""
	}
	return ": " + string(b)
}
