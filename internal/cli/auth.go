package cli

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"log/slog"
	"os"
	"runtime"
	"strings"
	"time"

	"github.com/bkuberek/spmeta/internal/auth"
	"github.com/bkuberek/spmeta/internal/backend"
	"github.com/bkuberek/spmeta/internal/config"
)

const (
	// expiryWarning is how long before the estimated end of the refresh
	// token's life auth status starts to warn.
	expiryWarning = 14 * 24 * time.Hour

	dateLayout   = "2006-01-02"
	maxPort      = 65535
	revokeURL    = "https://www.spotify.com/account/apps/"
	statusIndent = 24
)

// setupText is printed when auth login has no Client ID. Its %s is where to
// set client_id.
const setupText = `spmeta: no Spotify Client ID is configured.

Reading playback from other devices needs your own Spotify app. One-time setup:
  1. Sign in to https://developer.spotify.com/dashboard with an account that has
     Premium, and create an app with the Web API enabled.
  2. Add the redirect URI http://127.0.0.1:8974/callback.
  3. In the app's User Management, add the Spotify account you listen with,
     even if it is the account that owns the app.
  4. Copy the Client ID and run: spmeta auth login --client-id <id>
     Or set client_id in %s (or SPMETA_CLIENT_ID) and run: spmeta auth login
`

func runAuth(ctx context.Context, args []string, stdout, stderr io.Writer, opts options) int {
	if len(args) == 0 {
		return usageError(stderr, "auth needs a command: login, logout or status")
	}
	switch args[0] {
	case "login":
		return runLogin(ctx, args[1:], stdout, stderr, opts)
	case "logout":
		return runLogout(args[1:], stdout, stderr, opts)
	case "status":
		return runStatus(args[1:], stdout, stderr, opts)
	case "-h", "-help", "--help":
		printAuthUsage(stdout)
		return exitOK
	default:
		return usageError(stderr, "unknown auth command %q", args[0])
	}
}

// parseAuthFlags parses the flags of one auth command. done is true when the
// command is finished, with exit code code: help was printed, or the
// arguments are a usage error.
func parseAuthFlags(set *flag.FlagSet, command string, args []string, stdout, stderr io.Writer) (code int, done bool) {
	set.SetOutput(io.Discard)
	if err := set.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			printAuthUsage(stdout)
			return exitOK, true
		}
		if name, ok := strings.CutPrefix(err.Error(), "flag provided but not defined: -"); ok {
			return usageError(stderr, "unknown flag --%s", name), true
		}
		return usageError(stderr, "%v", err), true
	}
	if set.NArg() > 0 {
		return usageError(stderr, "unexpected argument %q for auth %s", set.Arg(0), command), true
	}
	return 0, false
}

// loadAuthSettings resolves the configuration for an auth command. Like the
// root command, it prints configuration warnings to stderr.
func loadAuthSettings(flags config.Flags, opts options, stderr io.Writer) (config.Settings, error) {
	settings, err := config.Load(flags, opts.env)
	if err != nil {
		return settings, err
	}
	for _, w := range settings.Warnings {
		_, _ = io.WriteString(stderr, warningPrefix+w+"\n")
	}
	return settings, nil
}

// tokenFile returns where the token lives, or an error when no location is
// known.
func tokenFile(s config.Settings) (string, error) {
	if s.TokenPath == "" {
		return "", &backend.Error{Kind: backend.KindUnexpected, Message: "no location is known for the token file; set XDG_CONFIG_HOME or HOME."}
	}
	return s.TokenPath, nil
}

// failAuth prints err like the root command does and returns its exit code.
func failAuth(stderr io.Writer, log *slog.Logger, err error) int {
	var be *backend.Error
	if errors.As(err, &be) && be.Err != nil {
		log.Debug("cause", "error", be.Err)
	}
	printError(stderr, err)
	return backend.ExitCode(backend.KindOf(err))
}

// loginPort is the --port default: DefaultLoginPort unless a test replaced it.
func (o options) loginPort() int {
	if o.defaultLoginPort != 0 {
		return o.defaultLoginPort
	}
	return auth.DefaultLoginPort
}

func runLogin(ctx context.Context, args []string, stdout, stderr io.Writer, opts options) int {
	set := flag.NewFlagSet("spmeta auth login", flag.ContinueOnError)
	clientID := set.String("client-id", "", "")
	port := set.Int("port", opts.loginPort(), "")
	noBrowser := set.Bool("no-browser", false, "")
	debug := set.Bool("debug", false, "")
	configFile := set.String("config", "", "")
	if code, done := parseAuthFlags(set, "login", args, stdout, stderr); done {
		return code
	}
	if *port < 0 || *port > maxPort {
		return usageError(stderr, "invalid value %d for --port: want 0 to %d", *port, maxPort)
	}
	flags := config.Flags{Debug: *debug}
	set.Visit(func(f *flag.Flag) {
		switch f.Name {
		case "client-id":
			flags.ClientID = clientID
		case "config":
			flags.Config = configFile
		}
	})
	settings, err := loadAuthSettings(flags, opts, stderr)
	if err != nil {
		return usageError(stderr, "%v", err)
	}
	id := strings.TrimSpace(settings.ClientID)
	if id == "" {
		conf := settings.ConfigPath
		if conf == "" {
			conf = "config.toml"
		}
		_, _ = fmt.Fprintf(stderr, setupText, conf)
		return exitUsage
	}
	path, err := tokenFile(settings)
	if err != nil {
		return failAuth(stderr, slog.New(slog.DiscardHandler), err)
	}

	log := debugLogger(*debug, stderr)
	log.Debug("login", "token", path, "port", *port, "browser", !*noBrowser)
	open := opts.browser
	if open == nil {
		open = auth.OpenBrowser
	}
	if *noBrowser {
		open = nil
	}
	_, err = auth.Login(ctx, auth.LoginOptions{
		ClientID:  id,
		Port:      *port,
		Path:      path,
		Endpoints: opts.endpoints,
		Open:      open,
		Now:       opts.now,
		Sleep:     opts.sleep,
		Stderr:    stderr,
		Log:       log,
	})
	if err != nil {
		return failAuth(stderr, log, err)
	}
	_, _ = fmt.Fprintf(stdout, "Logged in. Token stored at %s.\n", path)
	return exitOK
}

func runLogout(args []string, stdout, stderr io.Writer, opts options) int {
	path, code, ok := authTokenFile("logout", args, stdout, stderr, opts)
	if !ok {
		return code
	}
	switch err := os.Remove(path); {
	case err == nil:
		_, _ = fmt.Fprintf(stdout, "Logged out. Removed %s.\n", path)
	case errors.Is(err, fs.ErrNotExist):
		_, _ = fmt.Fprintf(stdout, "Not logged in. No token file at %s.\n", path)
	default:
		return failAuth(stderr, slog.New(slog.DiscardHandler), &backend.Error{
			Kind: backend.KindUnexpected, Message: "could not remove the token file.", Err: err,
		})
	}
	_, _ = fmt.Fprintf(stdout, "Spotify has no token-revocation endpoint. To revoke access, visit %s.\n", revokeURL)
	return exitOK
}

func runStatus(args []string, stdout, stderr io.Writer, opts options) int {
	path, code, ok := authTokenFile("status", args, stdout, stderr, opts)
	if !ok {
		return code
	}
	tok, err := auth.ReadToken(path)
	if err != nil {
		// An unusable file counts as not logged in here, not as a failed
		// refresh: nothing was attempted.
		printError(stderr, err)
		return backend.ExitCode(backend.KindNotAvailable)
	}
	now := time.Now
	if opts.now != nil {
		now = opts.now
	}
	printStatus(stdout, tok, path, now())
	return exitOK
}

// authTokenFile parses the flags of logout and status, which take only
// --config, and returns the token file. When ok is false the command is
// finished with exit code code.
func authTokenFile(command string, args []string, stdout, stderr io.Writer, opts options) (path string, code int, ok bool) {
	set := flag.NewFlagSet("spmeta auth "+command, flag.ContinueOnError)
	configFile := set.String("config", "", "")
	if code, done := parseAuthFlags(set, command, args, stdout, stderr); done {
		return "", code, false
	}
	var flags config.Flags
	set.Visit(func(f *flag.Flag) {
		if f.Name == "config" {
			flags.Config = configFile
		}
	})
	settings, err := loadAuthSettings(flags, opts, stderr)
	if err != nil {
		return "", usageError(stderr, "%v", err), false
	}
	path, err = tokenFile(settings)
	if err != nil {
		return "", failAuth(stderr, slog.New(slog.DiscardHandler), err), false
	}
	return path, exitOK, true
}

// printStatus describes the stored login. It never prints the tokens.
func printStatus(w io.Writer, tok auth.Token, path string, now time.Time) {
	row := func(label, value string) { _, _ = fmt.Fprintf(w, "%-*s%s\n", statusIndent, label+":", value) }
	refreshEnds := tok.RefreshExpiresAt().UTC()

	_, _ = io.WriteString(w, "Logged in.\n")
	row("Client ID", tok.ClientID)
	row("Scope", tok.Scope)
	row("Access token expires", tok.ExpiresAt.UTC().Format(time.RFC3339))
	row("Authorized", tok.AuthorizedAt.UTC().Format(time.RFC3339))
	row("Refresh token expires", "about "+refreshEnds.Format(dateLayout)+" (6 months after authorization)")

	mode, hasMode := fileMode(path)
	if hasMode {
		row("Token file", fmt.Sprintf("%s (mode %04o)", path, mode))
	} else {
		row("Token file", path)
	}

	switch left := refreshEnds.Sub(now); {
	case left < 0:
		_, _ = fmt.Fprintf(w, "Warning: the saved login probably expired on %s; run 'spmeta auth login'.\n", refreshEnds.Format(dateLayout))
	case left <= expiryWarning:
		_, _ = fmt.Fprintf(w, "Warning: run 'spmeta auth login' before %s.\n", refreshEnds.Format(dateLayout))
	}
	if hasMode && mode&0o077 != 0 {
		_, _ = fmt.Fprintf(w, "Warning: other users can access the token file. Run: chmod 600 '%s'\n", path)
	}
}

// fileMode returns the permission bits of path. It reports false where they
// mean nothing (Windows) or the file cannot be examined.
func fileMode(path string) (fs.FileMode, bool) {
	if runtime.GOOS == "windows" {
		return 0, false
	}
	info, err := os.Stat(path)
	if err != nil {
		return 0, false
	}
	return info.Mode().Perm(), true
}
