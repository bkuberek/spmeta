// Package doctor reports which sources spmeta can read and why the others
// cannot. See docs/design/cli.md, section doctor.
package doctor

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"strings"
	"time"

	"github.com/bkuberek/spmeta/internal/backend"
	"github.com/bkuberek/spmeta/internal/backend/macos"
	"github.com/bkuberek/spmeta/internal/backend/mpris"
	"github.com/bkuberek/spmeta/internal/model"
)

// Status words that start each source line.
const (
	statusOK          = "ok"
	statusUnavailable = "unavailable"
	statusNotConfig   = "not configured"
	statusDenied      = "denied"
	statusError       = "error"
)

const (
	nameWidth          = 8 // width of the check-name column
	continuationIndent = "        "
	hintAgain          = "run 'spmeta doctor' again"
	unknownHostApp     = "the app that runs spmeta"
	permissionWait     = "120 s" // macos.CheckPermission waits this long for the consent prompt
	dateLayout         = "2006-01-02"
)

// MacOSChecker is the part of the macOS backend doctor uses.
type MacOSChecker interface {
	CheckPermission(ctx context.Context, beforePrompt func()) macos.PermissionResult
}

// PlayerLister is the part of the MPRIS backend doctor uses.
type PlayerLister interface {
	Players(ctx context.Context) ([]mpris.PlayerInfo, error)
}

// WebReader is the part of the Web backend doctor uses. Doctor discards the
// item: it only needs to know whether the request worked.
type WebReader interface {
	Read(ctx context.Context) (model.NowPlaying, error)
}

// WebReport describes the Web setup. The caller fills it in from the token
// file, which doctor does not read.
type WebReport struct {
	Reader         WebReader // nil when there is no token file: Web is not configured
	TokenPath      string
	RefreshExpires time.Time // estimated end of the saved login; zero when unknown
	WideMode       bool      // the token file allows group or other access
}

// ConfigReport describes how the configuration was resolved. The caller fills
// it in from config.Load, which doctor does not import.
type ConfigReport struct {
	Path     string   // empty when no location is known
	Found    bool     // the file exists and was read
	Warnings []string // unknown keys
	Err      error    // the configuration could not be loaded
}

// Options are everything Run needs from the outside world.
type Options struct {
	Version  string // empty means "dev"
	OS, Arch string
	Config   ConfigReport
	MacOS    MacOSChecker // nil when the platform has no macOS backend
	MPRIS    PlayerLister // nil when the platform has no MPRIS backend
	Web      WebReport
	// HostApp names the app macOS asks about (see package hostapp); nil or ""
	// means unknown. Run calls it at most once, and only when a message needs
	// the name.
	HostApp func(ctx context.Context) string

	Out    io.Writer    // the report
	Notice io.Writer    // progress notes shown while a check waits
	Log    *slog.Logger // debug lines with the causes; nil discards
}

// Run writes the report and returns nil when at least one source is usable,
// else a KindNotAvailable error for the caller to print. It never prints the
// current track and never launches Spotify. With a token file it makes one Web
// request.
func Run(ctx context.Context, o Options) error {
	r := &reporter{o: o, log: o.Log}
	if r.log == nil {
		r.log = slog.New(slog.DiscardHandler)
	}
	version := o.Version
	if version == "" {
		version = "dev"
	}
	r.printf("spmeta %s (%s/%s)\n", version, o.OS, o.Arch)
	r.config(o.Config)

	usable := false
	if o.MacOS != nil {
		usable = r.macOS(ctx) || usable
	}
	if o.MPRIS != nil {
		usable = r.mpris(ctx) || usable
	}
	if o.MacOS == nil && o.MPRIS == nil {
		r.line("local", statusUnavailable, "this platform is not supported; spmeta reads Spotify on macOS and Linux")
	}
	usable = r.web(ctx) || usable

	if usable {
		return nil
	}
	return &backend.Error{
		Kind:    backend.KindNotAvailable,
		Message: "no source is available",
		Hint:    "Fix the failing check above, then run `spmeta doctor` again.",
	}
}

type reporter struct {
	o   Options
	log *slog.Logger
	app *string // memoized host app name
}

// hostApp returns the name macOS shows for the app that runs spmeta, looked up
// on first use.
func (r *reporter) hostApp(ctx context.Context) string {
	if r.app == nil {
		name := ""
		if r.o.HostApp != nil {
			name = r.o.HostApp(ctx)
		}
		if name == "" {
			name = unknownHostApp
		}
		r.app = &name
	}
	return *r.app
}

func (r *reporter) printf(format string, args ...any) {
	_, _ = fmt.Fprintf(r.o.Out, format, args...)
}

// line prints one check result and logs it under --debug, with the cause when
// there is one.
func (r *reporter) line(name, status, detail string, cause ...error) {
	if detail == "" {
		r.printf("%-*s%s\n", nameWidth, name, status)
	} else {
		r.printf("%-*s%s: %s\n", nameWidth, name, status, detail)
	}
	attrs := []any{"name", name, "status", status}
	for _, c := range cause {
		if c != nil {
			attrs = append(attrs, "cause", c)
		}
	}
	r.log.Debug("check", attrs...)
}

func (r *reporter) continuation(text string) {
	r.printf("%s%s\n", continuationIndent, text)
}

func (r *reporter) config(c ConfigReport) {
	switch {
	case c.Err != nil:
		r.line("config", statusError, c.Err.Error(), c.Err)
	case c.Path == "":
		r.line("config", statusOK, "no configuration location known, using defaults")
	case !c.Found:
		r.line("config", statusOK, c.Path+" not found, using defaults")
	default:
		r.line("config", statusOK, c.Path)
	}
	for _, w := range c.Warnings {
		r.continuation(w)
	}
}

func (r *reporter) macOS(ctx context.Context) bool {
	notice := func() {
		if r.o.Notice != nil {
			_, _ = fmt.Fprintf(r.o.Notice, "macOS may ask to allow %s to control Spotify; click OK.\n", r.hostApp(ctx))
		}
	}
	res := r.o.MacOS.CheckPermission(ctx, notice)
	switch res.State {
	case macos.PermissionGranted:
		r.line("macos", statusOK, "Spotify is running; Automation permission granted")
		return true
	case macos.PermissionDenied:
		r.line("macos", statusDenied, fmt.Sprintf(
			"macOS did not allow %s to control Spotify. Allow it in System Settings > Privacy & Security > Automation, then %s.",
			r.hostApp(ctx), hintAgain))
	case macos.PermissionSpotifyNotRunning:
		r.line("macos", statusUnavailable, fmt.Sprintf(
			"Spotify is not running, so permission cannot be checked without launching it. Start Spotify and %s.", hintAgain))
	case macos.PermissionNoAnswer:
		r.line("macos", statusError, fmt.Sprintf(
			"no answer within %s. Look for a macOS permission prompt, answer it, and %s.", permissionWait, hintAgain), res.Err)
	default:
		msg := strings.TrimSuffix(res.Message, ".")
		if msg == "" {
			msg = "the permission check failed"
		}
		r.line("macos", statusError, msg, res.Err)
	}
	return false
}

func (r *reporter) mpris(ctx context.Context) bool {
	players, err := r.o.MPRIS.Players(ctx)
	if err != nil {
		status := statusError
		if backend.KindOf(err) == backend.KindNotAvailable {
			status = statusUnavailable
		}
		r.line("mpris", status, describe(err), err)
		return false
	}
	selected := ""
	for _, p := range players {
		if p.Selected {
			selected = p.Name
		}
	}
	switch {
	case selected != "":
		r.line("mpris", statusOK, "reading "+selected)
	case len(players) == 0:
		r.line("mpris", statusUnavailable, "no MPRIS player is running. Start Spotify, spotifyd, ncspot or spotify_player.")
	default:
		r.line("mpris", statusUnavailable,
			"none of the players found is a Spotify client. Start one, or set 'player' in the configuration file.")
	}
	for _, p := range players {
		r.continuation(playerLine(p))
	}
	return selected != ""
}

func (r *reporter) web(ctx context.Context) bool {
	w := r.o.Web
	if w.Reader == nil {
		r.line("web", statusNotConfig, "run 'spmeta auth login' to see playback on other devices (optional)")
		return false
	}
	// Reaching Spotify is what counts: "no active device" is an answer.
	_, err := w.Reader.Read(ctx)
	usable := false
	switch {
	case err == nil:
		r.line("web", statusOK, "logged in; the Spotify Web API answered")
		usable = true
	case backend.KindOf(err) == backend.KindNothingPlaying:
		r.line("web", statusOK, "logged in; the Spotify Web API answered, but no device is active")
		usable = true
	case backend.KindOf(err) == backend.KindPermissionDenied:
		r.line("web", statusDenied, describe(err), err)
	case backend.KindOf(err) == backend.KindNotAvailable:
		r.line("web", statusUnavailable, describe(err), err)
	default:
		r.line("web", statusError, describe(err), err)
	}
	if !w.RefreshExpires.IsZero() {
		r.continuation("refresh token expires about " + w.RefreshExpires.UTC().Format(dateLayout))
	}
	if w.WideMode {
		r.continuation(fmt.Sprintf("other users can access the token file. Run: chmod 600 '%s'", w.TokenPath))
	}
	return usable
}

func playerLine(p mpris.PlayerInfo) string {
	switch {
	case p.Status == "":
		return p.Name + " (no answer)"
	case p.Selected:
		return fmt.Sprintf("%s (%s, selected)", p.Name, p.Status)
	default:
		return fmt.Sprintf("%s (%s)", p.Name, p.Status)
	}
}

// describe joins a backend error's message and hint into one sentence pair.
func describe(err error) string {
	var be *backend.Error
	if !errors.As(err, &be) {
		return err.Error()
	}
	msg := strings.TrimSuffix(be.Message, ".")
	if be.Hint == "" {
		return msg
	}
	return msg + ". " + be.Hint
}
