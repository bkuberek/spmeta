# Architecture

`spmeta` is a single Go binary. A thin CLI parses flags and configuration, hands the read to a resolver, and the resolver asks the backends in order and returns one normalized `NowPlaying` object to a formatter. This document defines the package layout, the backend interface and typed errors, the resolver algorithm, the watch loop, configuration precedence, and dependency boundaries.

![spmeta architecture: three backends behind one resolver](../assets/architecture.webp)

The diagram shows the original PRD naming; the structure is unchanged.

## Package layout

The module path is `github.com/bkuberek/spmeta`. All packages except `cmd/spmeta` are internal.

```text
cmd/spmeta/
  main.go                 # version vars (set by -ldflags), signal context, os.Exit(cli.Run(...))
internal/cli/
  cli.go                  # Run: dispatch to read, auth, doctor, completion, version, help
  read.go                 # root command: flags, resolver, formatter, watch
  auth.go                 # auth login|logout|status
  web.go                  # Web backend over the token file, throttle, doctor report
  doctor.go               # doctor command
  completion.go           # completion bash|zsh|fish
  usage.go                # help output from internal/reference, UsageError (exit 2)
  exit.go                 # error printing and exit-code mapping
internal/reference/
  reference.go            # commands, flags, values, exit codes: the source for help, completions, man page
  help.go                 # help pages
  completion.go           # static bash, zsh, and fish completion scripts
  man.go                  # spmeta(1) in roff
internal/docgen/
  main.go                 # release-time generator: completions and man page into dist-extras/
internal/model/
  nowplaying.go           # NowPlaying, Device, Source, State, ItemType
  normalize.go            # Normalize, ParseURI, OpenURL
  duration.go             # FormatDuration
internal/format/
  json.go                 # one-line JSON, "null" for nothing playing
  text.go                 # default text line
  template.go             # {field} mini-syntax: Parse, Render
  waybar.go               # Waybar custom-module JSON with Pango escaping
internal/config/
  paths.go                # XDG config dir, config and token paths
  config.go               # TOML file, env, flag merge into Settings
internal/backend/
  backend.go              # Backend interface
  notifier.go             # optional Notifier interface, Coalesce
  errors.go               # Error, Kind, ExitCode, Worse
internal/backend/macos/
  runner.go               # Runner interface, exec implementation, listener command
  script.go               # the read AppleScript (constant), the change listener script
  parse.go                # output parser
  macos.go                # Backend implementation, permission check for doctor
  notify.go               # Changes: change notifications for watch mode
internal/backend/mpris/
  bus.go                  # Bus interface, Properties type
  select.go               # player matching and selection
  mapping.go              # Metadata to NowPlaying, trackid normalization
  mpris.go                # Backend implementation, position memory
  notify.go               # Changes: change notifications for watch mode
  dbus_linux.go           # godbus adapter and signal subscription (//go:build linux)
internal/backend/local/
  local_darwin.go         # New() returns the macOS backend
  local_linux.go          # New() returns the MPRIS backend
  local_other.go          # New() returns a backend that always reports NotAvailable
internal/backend/web/
  client.go               # HTTP client, base URL injection, retries
  mapping.go              # /me/player JSON to NowPlaying
  web.go                  # Backend implementation
internal/auth/
  pkce.go                 # verifier, challenge, state
  login.go                # loopback server, authorize URL, code exchange
  browser.go              # OpenBrowser: open (macOS) or xdg-open (Linux)
  token.go                # token file read and atomic write (0600)
  refresh.go              # refresh logic with file lock
  lock_unix.go            # flock on token.json.lock
internal/resolve/
  resolve.go              # source resolution (auto, local, web), change notifications
  throttle.go             # minimum interval between Web reads in watch mode
internal/watch/
  watch.go                # polling loop with wakes, change detection, emission
internal/doctor/
  doctor.go               # collects source checks into a report
schema/
  schema.go               # embeds the schema for tests in other packages
  spmeta.schema.json      # JSON Schema for the JSON output
testdata/ per package     # golden files and fixtures
```

Unit tests live next to the code (`*_test.go`). Fixtures live in each package's `testdata/` directory.

## Backend interface

Every source implements one interface.

```go
package backend

type Backend interface {
	// Source identifies the backend in output and errors.
	Source() model.Source
	// Read returns the current item, already normalized, or a *Error.
	// It must honor ctx cancellation and deadlines.
	Read(ctx context.Context) (model.NowPlaying, error)
}
```

A backend that can announce changes also implements the optional `Notifier` interface. Watch mode uses it to read at once instead of waiting for the next poll (see Watch loop).

```go
type Notifier interface {
	// Changes starts listening and returns a channel that receives a value
	// when the item may have changed, with at most one value pending. The
	// channel closes when ctx ends or the listener stops. An error means
	// nothing was started. Changes may run concurrently with Read.
	Changes(ctx context.Context) (<-chan struct{}, error)
}

// Coalesce turns each value from in that keep accepts (all when nil) into a
// wake on a channel holding at most one pending wake; it closes after in.
func Coalesce[T any](in <-chan T, keep func(T) bool) <-chan struct{}
```

| Backend | Notifier |
| --- | --- |
| MPRIS | Yes: `PropertiesChanged` and `NameOwnerChanged` signals ([backend-linux.md](./backend-linux.md#change-notifications)) |
| macOS | Yes: Spotify's `com.spotify.client.PlaybackStateChanged` distributed notification, observed by a JXA listener ([backend-macos.md](./backend-macos.md#change-notifications)) |
| Web | No; the Web API sends no notifications |

Backends receive their collaborators through constructors so tests can inject fakes:

| Backend | Constructor | Injected collaborators |
| --- | --- | --- |
| macOS | `macos.New(r Runner, now func() time.Time)` | `Runner` runs `pgrep` and `osascript` |
| MPRIS | `mpris.New(dial func(ctx) (Bus, error), player string, now func() time.Time)` | `Bus` lists names and reads properties |
| Web | `&web.Backend{Tokens, Endpoints, HTTPClient, Now, Sleep, Log, MaxRetryWait, Version}` | `*http.Client`, API base URL, token source |

## Typed errors and exit codes

Backends return `*backend.Error`. The CLI maps the kind to an exit code. Usage errors are produced by the CLI, not by backends.

```go
type Kind int

const (
	KindUnexpected Kind = iota + 1
	KindNothingPlaying
	KindNotAvailable
	KindAuthRequired
	KindPermissionDenied
	KindRateLimited
)

type Error struct {
	Kind       Kind
	Source     model.Source  // empty when not tied to a source
	Message    string        // one line, user-facing, no secrets
	Hint       string        // next step for the user; may be empty
	RetryAfter time.Duration // KindRateLimited only
	Err        error         // wrapped cause; logged only under --debug
}

func (e *Error) Error() string
func (e *Error) Unwrap() error

// KindOf returns the Kind of a *Error found with errors.As, or KindUnexpected.
func KindOf(err error) Kind

// ExitCode maps a Kind to the process exit code.
func ExitCode(k Kind) int

// Worse returns whichever error has the higher precedence (see the resolver).
func Worse(a, b error) error
```

| Kind | Exit code | Typical cause |
| --- | --- | --- |
| (success) | 0 | An item is playing, paused, or loaded |
| `KindUnexpected` | 1 | Unparseable output, network failure, timeout, 5xx |
| (usage error) | 2 | Bad flag, unknown template field, invalid configuration value |
| `KindNothingPlaying` | 3 | Source reachable, nothing loaded, or no active Web device |
| `KindNotAvailable` | 4 | App not running, no MPRIS player, no session bus, not logged in |
| `KindAuthRequired` | 5 | Refresh failed (`invalid_grant`), token file unreadable, login aborted |
| `KindPermissionDenied` | 6 | macOS Automation denied (-1743), Web 403 |
| `KindRateLimited` | 7 | Web 429 beyond the one-shot retry budget |

On failure, the CLI prints to stderr:

```text
spmeta: <Message>
hint: <Hint>
```

The `hint:` line is omitted when `Hint` is empty. `--quiet` suppresses both lines; usage errors (exit 2) are always printed. `--debug` adds the wrapped cause.

## Resolver

`internal/resolve` implements [ADR 0005](../decisions/0005-auto-source-resolution.md).

```go
type Mode string // "auto", "local", "web"

type Resolver struct {
	Mode  Mode
	Local backend.Backend // never nil; local_other.go returns an always-NotAvailable backend
	Web   backend.Backend // nil when there is no token file

	Logger *slog.Logger // debug line per source read; nil discards
}

func (r *Resolver) Read(ctx context.Context) (model.NowPlaying, error)

// Changes implements backend.Notifier: in auto and local modes it starts the
// local backend's notifications; in web mode, or when the local backend has
// none, it returns an error.
func (r *Resolver) Changes(ctx context.Context) (<-chan struct{}, error)
```

Algorithm for `auto`:

```text
L, errL := Local.Read(ctx)
if errL == nil && L.State == playing        -> return L
if Web != nil:
    W, errW := Web.Read(ctx)
    if errW == nil && W.State == playing    -> return W
if errL == nil                              -> return L   # paused or stopped with an item
if Web != nil && errW == nil                -> return W   # paused on another device
return Worse(errL, errW)
```

For `local`, return `Local.Read`. For `web`, return `Web.Read`; when `Web` is nil, return `NotAvailable` with the message "not logged in to the Spotify Web API" and the hint "Run `spmeta auth login`." (the wording of `auth status`). `Static()` reports the same error for a whole run, so watch mode exits 4 at once.

Error precedence for `Worse`, highest first: `KindPermissionDenied`, `KindAuthRequired`, `KindRateLimited`, `KindUnexpected`, `KindNothingPlaying`, `KindNotAvailable`. A nil error has the lowest precedence. When both errors have the same kind, the local error wins.

Each backend read runs under its own timeout (see Timeouts). The resolver itself adds no deadline. Under `--debug` the resolver logs one line per source read (source, backend, state or error kind and message) and one line when it skips Web; backends do not log.

## Watch loop

`internal/watch` implements FR-10.

```go
type Reader interface {
	Read(ctx context.Context) (model.NowPlaying, error)
}

type Options struct {
	Interval time.Duration                    // local poll interval
	Wake     <-chan struct{}                  // change notifications; nil polls only
	Emit     func(np *model.NowPlaying) error // nil np means nothing playing
	// Sleep waits d, or less when ctx ends or a value arrives on wake (nil
	// when the wait must run its full length; a closed wake never ends it).
	// Injected for tests.
	Sleep  func(ctx context.Context, d time.Duration, wake <-chan struct{}) error
	Errorf func(format string, args ...any) // one stderr line per call; nil discards
}

// Run polls until ctx is cancelled or a fatal error occurs. It returns nil on
// cancellation and the fatal *backend.Error otherwise, without printing it:
// the CLI prints the returned error once and exits with its code. A failing
// Emit also stops the loop with a KindUnexpected error.
func Run(ctx context.Context, r Reader, opts Options) error
```

Loop behavior:

1. Read immediately on start, then sleep `Interval` between reads. A value on `Wake` ends the sleep, so the next read starts at once, but never sooner than `MinWakeGap` (100 ms) after the previous one (see Change notifications).
2. Compute a change key for the result. Emit only when the key differs from the previous emitted key. The first result is always emitted.
3. Handle errors by kind:

   | Kind | Behavior |
   | --- | --- |
   | `KindNothingPlaying`, `KindNotAvailable` | Treated as "nothing"; emits `nil` once on transition |
   | `KindRateLimited` | No emission; sleep `RetryAfter` (or 5 s when unknown), then continue |
   | `KindUnexpected` | No emission; print the error once per distinct message; from the 5th consecutive failure, double the sleep for each further failure (never above 30 s, or the interval when longer); reset on any read that reaches a source |
   | `KindAuthRequired`, `KindPermissionDenied` | Fatal: return the error (the CLI prints it and exits 5 or 6) |

4. On SIGINT or SIGTERM, `main` cancels the context; `Run` returns nil and the process exits 0.

Change key: `source`, `state`, `type`, the item identity (`uri` when present, otherwise `title`, `artist`, and `album` joined with the unit separator), and `device.name`. Position and `fetched_at` are excluded, so progress alone never emits a line. A transition to nothing has the key `"none"`.

Emission per format:

| Format | Item | Nothing playing |
| --- | --- | --- |
| `text` | One text line | One empty line |
| `template` | One rendered line | One empty line |
| `json` | One JSON object line (NDJSON) | The line `null` |

Each line is written with one `Write` call to unbuffered stdout, so consumers see it immediately.

### Change notifications

`runWatch` in `internal/cli` checks whether the reader it watches, the resolver, implements `backend.Notifier`. If so, it calls `Changes` with a context that ends when the loop returns, and passes the channel as `Options.Wake`. Starting is best-effort: on an error, the loop polls only, and `--debug` logs one line with the reason. When the loop returns, `runWatch` cancels that context and waits up to 1 s for the channel to close, because `main` exits the process right after: a notifier that runs a process, such as the macOS listener, closes its channel only once that process is gone. In auto and local modes the resolver delegates to the local backend: the MPRIS backend on Linux, and the macOS backend's listener process on macOS. The Web API sends no notifications, so `--source web` polls as before. The design for both platforms is recorded in [ADR 0010](../decisions/0010-event-driven-watch.md).

After a read that reached a source (an item, nothing playing, or not available), the loop sleeps in two parts: first `MinWakeGap` (100 ms), which no wake ends, then the rest of `Interval`, which a wake ends. A player that floods notifications therefore causes at most one read per 100 ms, each on a fresh D-Bus connection or `osascript` run, instead of reads back to back. The rules:

| Situation | Behavior |
| --- | --- |
| A wake arrives during the rest of the sleep | The sleep ends and the next read starts at once |
| A wake arrives during a read or during the gap | The next read starts when the gap ends, 100 ms after the previous read |
| Several wakes are pending | They are drained together and cost one read |
| A wake arrives during a sleep after an error (`RateLimited`, `Unexpected`) | Ignored; the `RetryAfter` and backoff sleeps run their full length, and the next read covers the change |
| The wake channel closes | The current sleep runs to its end; the loop then polls only, at `Interval`, for the rest of the run |

`--interval` keeps its meaning and defaults: it is the pause between reads when nothing announces a change, and polling still finds changes that no notification announces, such as a Web device change. A wake never reads Web more often than `web_interval`, because the throttle in auto mode returns the cached Web result. The loop starts no goroutines; the default `Sleep` selects on the timer, `ctx`, and the wake channel, and stops selecting on the wake channel once it closes.

### Polling intervals

- Local sources: `--interval` (default 1 s, minimum 0.5 s).
- Web source with `--source web`: `--interval` when given, otherwise `web_interval` (default 3 s); minimum 1 s.
- `auto`: the local backend is read every `--interval`. The Web backend is wrapped in `resolve.Throttle(web, webInterval, now)`, which returns the previous Web result (or error) when called again within `webInterval`. Because the resolver skips Web while local is playing, Web is polled only while local is not playing.

## Timeouts

| Operation | Timeout | On timeout |
| --- | --- | --- |
| macOS `pgrep` | 1 s | `KindUnexpected` |
| macOS `osascript` read | 2 s | `KindUnexpected`, hint "run `spmeta doctor`" |
| macOS permission check in `doctor` | 120 s | Reported as "no answer" |
| MPRIS connect plus `ListNames` | 500 ms each | `KindNotAvailable` |
| MPRIS `GetAll` per player | 500 ms | Player skipped |
| MPRIS overall read | 2 s | `KindUnexpected` |
| MPRIS signal subscription (watch mode) | 500 ms connect, 500 ms `AddMatch` | Watch mode polls only |
| macOS change listener start (watch mode) | 2 s until the listener prints `ready` | Listener killed; watch mode polls only |
| Change notifications stop, when watch mode returns | 1 s for the channel to close | `spmeta` exits anyway; on macOS the listener's heartbeat ends it within 5 s |
| Web API request | 5 s | Retried once after 1 s, then `KindUnexpected` |
| Token request (exchange or refresh) | 10 s | `KindUnexpected` |
| Login callback wait | 180 s | `KindAuthRequired` |

## Configuration precedence

Settings come from four layers, highest first: flags, then environment variables (`SPMETA_*`), then the configuration file, then built-in defaults. `internal/config` merges them into one `Settings` value; the CLI never reads the environment or file directly. Keys, paths, and variable names are listed in [cli.md](./cli.md).

```go
type Settings struct {
	Source      string        // auto | local | web
	Format      string        // text | json | template
	Template    string
	Player      string
	Interval    time.Duration // zero means "use the source default"
	WebInterval time.Duration
	ClientID    string
	ConfigPath  string
	TokenPath   string
	Debug       bool
	Quiet       bool
	Watch       bool
}
```

`config.Load(args Flags, env func(string) string, readFile func(string) ([]byte, error)) (Settings, error)` takes its inputs as parameters so tests never touch the real environment or home directory.

## Logging and secrets

- `--debug` enables a `log/slog` text handler on stderr at debug level; otherwise logging is discarded.
- Debug output includes backend selection, the MPRIS player chosen, `osascript` exit status and stderr, HTTP method, path, status, and duration.
- Debug output never includes access tokens, refresh tokens, authorization codes, PKCE verifiers, `Authorization` headers, or token-endpoint bodies. Only the token package handles those values, and it never passes them to the logger. `internal/cli/secrecy_test.go` runs a fake login, reads, refreshes (also after a 401 and a refused refresh whose bodies echo tokens) and `doctor` with `--debug`, and asserts that no secret value appears in stdout or stderr.

## Dependency boundaries

Allowed imports between internal packages (an arrow means "may import"):

```text
cmd/spmeta        -> cli
cli               -> config, format, resolve, watch, doctor, auth, backend, backend/*, model
doctor            -> backend, backend/*, auth, config, model
watch             -> backend, model
resolve           -> backend, model
backend/local     -> backend/macos, backend/mpris
backend/web       -> auth, backend, model
backend/macos     -> backend, model
backend/mpris     -> backend, model
auth              -> backend (error kinds only)
format            -> model
config, model     -> standard library only (config also BurntSushi/toml)
```

Rules:

- Only `backend/mpris/dbus_linux.go` imports `github.com/godbus/dbus/v5`.
- Only `internal/config` imports `github.com/BurntSushi/toml`.
- Only `backend/web` and `auth` import `net/http`.
- Only `backend/macos` (and `auth`, for opening the browser) import `os/exec`.
- No package imports `internal/cli`.

## Platform selection

`internal/backend/local` chooses the local backend with build-constrained files: `local_darwin.go` returns the macOS backend, `local_linux.go` returns the MPRIS backend, and `local_other.go` (`//go:build !darwin && !linux`) returns a backend whose `Read` always returns `KindNotAvailable` with "unsupported platform." The macOS parser and the MPRIS mapping and selection code carry no build tags, so their unit tests run on both CI operating systems.

## Version information

`cmd/spmeta/main.go` declares `version`, `commit`, and `date` string variables, defaulting to `"dev"`, `""`, and `""`. Release builds set them with `-ldflags "-X main.version=... -X main.commit=... -X main.date=..."`. When `version` is `"dev"`, `main` falls back to `debug.ReadBuildInfo()`: the module version (for `go install ...@v0.1.0`) and the `vcs.revision` and `vcs.time` settings. `main` passes the result to `cli.Run` as a `BuildInfo` value.
