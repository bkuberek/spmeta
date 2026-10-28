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

	"github.com/bkuberek/spmeta/internal/backend"
	"github.com/bkuberek/spmeta/internal/backend/macos"
	"github.com/bkuberek/spmeta/internal/backend/mpris"
)

// Status words that start each source line.
const (
	statusOK          = "ok"
	statusUnavailable = "unavailable"
	statusDenied      = "denied"
	statusError       = "error"
)

const (
	nameWidth          = 8 // width of the check-name column
	continuationIndent = "        "
	hintAgain          = "run 'spmeta doctor' again"
	unknownTerminal    = "the app that runs spmeta"
	permissionWait     = "120 s" // macos.CheckPermission waits this long for the consent prompt
)

// MacOSChecker is the part of the macOS backend doctor uses.
type MacOSChecker interface {
	CheckPermission(ctx context.Context, beforePrompt func()) macos.PermissionResult
}

// PlayerLister is the part of the MPRIS backend doctor uses.
type PlayerLister interface {
	Players(ctx context.Context) ([]mpris.PlayerInfo, error)
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
	Terminal string       // TERM_PROGRAM, to name the app macOS asks about

	Out    io.Writer    // the report
	Notice io.Writer    // progress notes shown while a check waits
	Log    *slog.Logger // debug lines with the causes; nil discards
}

// Run writes the report and returns nil when at least one source is usable,
// else a KindNotAvailable error for the caller to print. It never reads the
// current track and never launches Spotify.
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
	// Web is not built yet, so it can never make a source usable.
	r.line("web", statusUnavailable, "Web mode is not available in this version")

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
	terminal := terminalName(r.o.Terminal)
	notice := func() {
		if r.o.Notice != nil {
			_, _ = fmt.Fprintf(r.o.Notice, "macOS may ask to allow %s to control Spotify; click OK.\n", terminal)
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
			terminal, hintAgain))
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

// terminalName turns TERM_PROGRAM into the name macOS shows in its Automation
// list, as far as it can be guessed.
func terminalName(termProgram string) string {
	switch termProgram {
	case "":
		return unknownTerminal
	case "Apple_Terminal":
		return "Terminal"
	default:
		return strings.TrimSuffix(termProgram, ".app")
	}
}
