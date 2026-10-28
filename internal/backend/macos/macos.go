// Package macos reads Spotify.app through AppleScript run by osascript. See
// docs/design/backend-macos.md.
package macos

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/bkuberek/spmeta/internal/backend"
	"github.com/bkuberek/spmeta/internal/model"
)

const (
	processName    = "Spotify"
	readTimeout    = 2 * time.Second
	processTimeout = time.Second
	// permissionTimeout bounds the doctor check, which may wait on the macOS
	// consent prompt.
	permissionTimeout = 120 * time.Second
	msgNotRunning     = "Spotify is not running."
	hintDoctor        = "Run `spmeta doctor`."
	hintFirstRun      = "If this is the first run, macOS may be waiting for you to allow Automation access: run `spmeta doctor`."
	hintAutomation    = "Allow it in System Settings > Privacy & Security > Automation, under the app that runs spmeta (for example Terminal), then run `spmeta doctor`."
	codeNotAuthorized = -1743
	codeNotRunning    = -600
	codeTimedOut      = -1712
)

var errNumber = regexp.MustCompile(`\((-?\d+)\)\s*$`)

// Backend reads the current item from Spotify.app.
type Backend struct {
	runner      Runner
	now         func() time.Time
	readTimeout time.Duration
	// permissionTimeout bounds CheckPermission.
	permissionTimeout time.Duration
}

// New returns a Backend that runs programs through r and stamps results with
// now.
func New(r Runner, now func() time.Time) *Backend {
	return &Backend{runner: r, now: now, readTimeout: readTimeout, permissionTimeout: permissionTimeout}
}

// Source identifies this backend.
func (b *Backend) Source() model.Source { return model.SourceMacOS }

// Read returns the current item. It never launches Spotify: the script runs
// only after the process check finds Spotify running.
func (b *Backend) Read(ctx context.Context) (model.NowPlaying, error) {
	ctx, cancel := context.WithTimeout(ctx, b.readTimeout)
	defer cancel()

	if err := b.checkRunning(ctx); err != nil {
		return model.NowPlaying{}, err
	}
	stdout, stderr, err := b.runner.RunScript(ctx, readScript)
	if err != nil {
		return model.NowPlaying{}, b.scriptError(ctx, stderr, err)
	}
	np, err := parseOutput(stdout)
	if err != nil {
		return model.NowPlaying{}, err
	}
	np.FetchedAt = b.now()
	return model.Normalize(np), nil
}

func (b *Backend) checkRunning(ctx context.Context) error {
	pctx, cancel := context.WithTimeout(ctx, processTimeout)
	defer cancel()
	running, err := b.runner.ProcessRunning(pctx, processName)
	switch {
	case err != nil:
		return &backend.Error{
			Kind:    backend.KindUnexpected,
			Source:  model.SourceMacOS,
			Message: "could not check whether Spotify is running",
			Err:     err,
		}
	case !running:
		return errNotRunning()
	}
	return nil
}

// scriptError maps a failed osascript run. Messages are localized by macOS,
// so only the error number in the last stderr line is matched.
func (b *Backend) scriptError(ctx context.Context, stderr []byte, cause error) *backend.Error {
	fail := func(kind backend.Kind, msg, hint string) *backend.Error {
		return &backend.Error{Kind: kind, Source: model.SourceMacOS, Message: msg, Hint: hint, Err: cause}
	}
	switch ctxErr := ctx.Err(); {
	case errors.Is(ctxErr, context.DeadlineExceeded):
		return fail(backend.KindUnexpected, fmt.Sprintf("Spotify did not respond within %s.", b.readTimeout), hintFirstRun)
	case ctxErr != nil:
		return fail(backend.KindUnexpected, "read cancelled", "")
	}

	n, ok := errorNumber(stderr)
	switch {
	case !ok:
		return fail(backend.KindUnexpected, "osascript failed.", "")
	case n == codeNotAuthorized:
		return fail(backend.KindPermissionDenied, "macOS did not allow this app to control Spotify.", hintAutomation)
	case n == codeNotRunning:
		return fail(backend.KindNotAvailable, msgNotRunning, "")
	case n == codeTimedOut:
		return fail(backend.KindUnexpected, "Spotify did not respond.", hintDoctor)
	default:
		return fail(backend.KindUnexpected, fmt.Sprintf("osascript failed (error %d).", n), "")
	}
}

func errorNumber(stderr []byte) (int, bool) {
	lines := strings.Split(strings.TrimRight(string(stderr), " \t\r\n"), "\n")
	m := errNumber.FindStringSubmatch(lines[len(lines)-1])
	if m == nil {
		return 0, false
	}
	n, err := strconv.Atoi(m[1])
	return n, err == nil
}

// permissionScript sends the smallest Apple event that needs Automation
// permission. The caller has checked that Spotify is running.
const permissionScript = `tell application id "com.spotify.client" to return (player state as string)`

// PermissionState is the outcome of CheckPermission.
type PermissionState int

// Outcomes of CheckPermission.
const (
	PermissionGranted           PermissionState = iota + 1 // Spotify answered the Apple event
	PermissionDenied                                       // macOS refused (error -1743)
	PermissionSpotifyNotRunning                            // nothing was sent: the check cannot run without launching Spotify
	PermissionNoAnswer                                     // no reply within the timeout, usually an unanswered prompt
	PermissionFailed                                       // anything else; see Message and Err
)

// PermissionResult is what CheckPermission reports to doctor.
type PermissionResult struct {
	State   PermissionState
	Message string // PermissionFailed only: one user-facing line
	Err     error  // PermissionFailed only: the cause, for --debug
}

// CheckPermission tells whether macOS lets this process control Spotify. It
// sends an Apple event only when Spotify is already running, so it never
// launches it. The event can block on the consent prompt, so it gets the long
// permission timeout; beforePrompt, when not nil, runs just before the event
// is sent so the caller can tell the user to expect the prompt.
func (b *Backend) CheckPermission(ctx context.Context, beforePrompt func()) PermissionResult {
	if err := b.checkRunning(ctx); err != nil {
		var be *backend.Error
		if errors.As(err, &be) && be.Kind == backend.KindNotAvailable {
			return PermissionResult{State: PermissionSpotifyNotRunning}
		}
		return failed(err.Error(), err)
	}
	if beforePrompt != nil {
		beforePrompt()
	}
	pctx, cancel := context.WithTimeout(ctx, b.permissionTimeout)
	defer cancel()
	_, stderr, err := b.runner.RunScript(pctx, permissionScript)
	if err == nil {
		return PermissionResult{State: PermissionGranted}
	}
	switch ctxErr := ctx.Err(); {
	case ctxErr != nil:
		return failed("check cancelled.", ctxErr)
	case pctx.Err() != nil:
		return PermissionResult{State: PermissionNoAnswer}
	}
	n, ok := errorNumber(stderr)
	switch {
	case !ok:
		return failed("osascript failed.", err)
	case n == codeNotAuthorized:
		return PermissionResult{State: PermissionDenied}
	case n == codeNotRunning:
		return PermissionResult{State: PermissionSpotifyNotRunning}
	case n == codeTimedOut:
		return PermissionResult{State: PermissionNoAnswer}
	default:
		return failed(fmt.Sprintf("osascript failed (error %d).", n), err)
	}
}

func failed(msg string, cause error) PermissionResult {
	return PermissionResult{State: PermissionFailed, Message: msg, Err: cause}
}
