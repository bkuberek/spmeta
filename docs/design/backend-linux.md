# Linux backend (MPRIS)

The Linux backend reads MPRIS players over the session D-Bus with `github.com/godbus/dbus/v5` ([ADR 0006](../decisions/0006-dependencies.md)). This document is the implementation spec: bus discovery, the exact D-Bus calls, player selection, metadata mapping, the Spotify desktop position rule, error mapping, timeouts, and the integration test. Evidence is in [linux-mpris.md](../research/linux-mpris.md).

## Package

`internal/backend/mpris`. Only `dbus_linux.go` (`//go:build linux`) imports godbus; selection and mapping are plain Go and are unit-tested on both CI operating systems.

```go
// Properties holds the result of Properties.GetAll with D-Bus variants
// converted to plain Go values (see Value conversion).
type Properties map[string]any

type Bus interface {
	ListNames(ctx context.Context) ([]string, error)
	GetAll(ctx context.Context, busName string) (Properties, error)
	Close() error
}

type DialFunc func(ctx context.Context) (Bus, error)

func New(dial DialFunc, player string, now func() time.Time) *Backend
func (b *Backend) Source() model.Source // "mpris"
func (b *Backend) Read(ctx context.Context) (model.NowPlaying, error)

// Players is used by doctor: every MPRIS player found, with the one Read would select.
func (b *Backend) Players(ctx context.Context) ([]PlayerInfo, error)
```

`PlayerInfo` has `Name` (the bus name suffix), `Status` (`PlaybackStatus`, empty when the player did not answer), and `Selected`. `DialFunc` receives the context of the whole read, which stays live until `Read` returns, so an adapter may tie the connection to it; `Read` itself bounds the dial by the 500 ms connect timeout and closes a connection that arrives late. A `*backend.Error` returned by the dial passes through unchanged; any other dial error becomes `NotAvailable` "could not reach the D-Bus session bus".

`dbus_linux.go` provides `Dial`, the production `DialFunc`. `local_linux.go` calls `mpris.New(mpris.Dial, settings.Player, time.Now)`.

## Session bus discovery

`Dial` resolves the address itself, so it never triggers godbus's `dbus-launch` autolaunch:

1. If `DBUS_SESSION_BUS_ADDRESS` is non-empty and not `autolaunch:`, use it.
2. Otherwise, if `$XDG_RUNTIME_DIR/bus` exists and is a socket, use `unix:path=$XDG_RUNTIME_DIR/bus`.
3. Otherwise, return `NotAvailable`: "no D-Bus session bus found", hint "run spmeta inside a desktop session, or start one with `dbus-run-session`."

Then connect with `dbus.Connect(address, dbus.WithContext(ctx))`, which authenticates and sends `Hello`. A connection error is `NotAvailable` with the same hint. Each `Read` opens one connection and closes it before returning, so a restarted bus never leaves a stale connection.

The godbus names in this document (`Connect`, `WithContext`, `BusObject`, `Object`, `CallWithContext`, `Store`, `Variant.Value`) were checked against `go doc` and the source of v5.2.4 in task T7. `Connect` authenticates and sends `Hello`, and cancelling the context passed to `WithContext` closes the connection.

## D-Bus calls

| Purpose | Destination | Object path | Method | Arguments | Reply |
| --- | --- | --- | --- | --- | --- |
| List names | `org.freedesktop.DBus` | `/org/freedesktop/DBus` | `org.freedesktop.DBus.ListNames` | none | `as` |
| Read player | `org.mpris.MediaPlayer2.<suffix>` | `/org/mpris/MediaPlayer2` | `org.freedesktop.DBus.Properties.GetAll` | `"org.mpris.MediaPlayer2.Player"` | `a{sv}` |

```go
var names []string
err := conn.BusObject().CallWithContext(ctx, "org.freedesktop.DBus.ListNames", 0).Store(&names)

var props map[string]dbus.Variant
err = conn.Object(busName, "/org/mpris/MediaPlayer2").
	CallWithContext(ctx, "org.freedesktop.DBus.Properties.GetAll", 0, "org.mpris.MediaPlayer2.Player").
	Store(&props)
```

Properties used: `PlaybackStatus` (`s`), `Metadata` (`a{sv}`), and `Position` (`x`, microseconds).

### Value conversion

The adapter converts each `dbus.Variant` to plain Go values recursively before returning `Properties`:

| D-Bus value | Go value |
| --- | --- |
| `dbus.ObjectPath` | `string` |
| `map[string]dbus.Variant` | `map[string]any` |
| nested `dbus.Variant` | its converted value |
| strings, string slices, integers, floats, booleans | unchanged |

`convert.go` implements this table without importing godbus: it opens any value with a `Value() any` method (`dbus.Variant`) and converts string-kind values (`dbus.ObjectPath`) and string-keyed maps by reflection, so it is unit-tested on every OS. `address.go` holds the session bus address resolution the same way.

Mapping helpers accept every shape players are known to send:

- `asString(v)`: `string`; otherwise not present.
- `asStrings(v)`: `[]string`, a single `string` (one-element list), or `[]any` of strings.
- `asInt64(v)`: any signed or unsigned integer type, or `float64` truncated toward zero.

## Player selection

Bus names are reduced to suffixes: for each name from `ListNames` that starts with `org.mpris.MediaPlayer2.`, the suffix is the rest of the name.

A name **matches** a suffix when the suffix equals the name, or the suffix starts with the name followed by `.` (playerctl's dot-boundary rule). `spotify` therefore matches `spotify` but not `spotifyd`, and `spotifyd` matches `spotifyd.instance1234`.

Algorithm:

```text
suffixes := sorted MPRIS suffixes from ListNames
if --player NAME is set:
    entries := [NAME]
else:
    entries := ["spotify", "spotifyd", "ncspot", "spotify_player"]
for each entry in entries:
    candidates := suffixes matching entry, in sorted order
    results := GetAll for each candidate; drop candidates whose GetAll fails or times out
    if results is not empty:
        return the first result whose PlaybackStatus is "Playing", else the first result
if --player was set and no suffix matched: NotAvailable "no MPRIS player named NAME"
otherwise: NotAvailable "no Spotify MPRIS player found"
```

Browser players (`firefox.instance<PID>`, `chromium...`) are never selected unless named with `--player`. The full suffix (for example `--player spotifyd.instance1234`) also matches.

## Metadata mapping

`mapping.go` converts the selected player's `Properties` into a raw `NowPlaying` with `source: mpris`; `model.Normalize` finishes it.

| Field | Rule |
| --- | --- |
| `state` | `PlaybackStatus`: `Playing` to `playing`, `Paused` to `paused`, `Stopped` to `stopped`; any other value is `KindUnexpected` |
| `title` | `xesam:title` |
| `artists` | `asStrings(xesam:artist)` |
| `album` | `xesam:album` |
| `album_artist` | `asStrings(xesam:albumArtist)` joined with `", "` |
| `track_number` | `xesam:trackNumber` |
| `disc_number` | `xesam:discNumber` |
| `duration_ms` | `mpris:length` / 1000 |
| `position_ms` | `Position` / 1000, subject to the Spotify desktop rule |
| `uri` | Normalized `mpris:trackid`; else parsed from `xesam:url`; else `xesam:url` when it starts with `spotify:` |
| `url` | `xesam:url` when it starts with `https://open.spotify.com/`; else left null for `Normalize` to derive from `uri` |
| `artwork_url` | `mpris:artUrl`; a value starting with `https://open.spotify.com/image/` is rewritten to `https://i.scdn.co/image/` plus the same final path segment; non-HTTPS values become null |

### Track ID normalization

Apply the regular expression `(track|episode|ad)[/:]([A-Za-z0-9]+)$` to `mpris:trackid`; a match produces `spotify:<group 1>:<group 2>`.

| Input | Output | Client |
| --- | --- | --- |
| `/com/spotify/track/4brK3gpPjXArNUlU1hBXTe` | `spotify:track:4brK3gpPjXArNUlU1hBXTe` | Spotify desktop |
| `spotify:track:4brK3gpPjXArNUlU1hBXTe` (string) | same | Spotify desktop (reported) |
| `/spotify/track/4brK3gpPjXArNUlU1hBXTe` | `spotify:track:4brK3gpPjXArNUlU1hBXTe` | spotifyd |
| `/org/ncspot/spotify/track/4brK3gpPjXArNUlU1hBXTe` | `spotify:track:4brK3gpPjXArNUlU1hBXTe` | ncspot |
| `/com/spotify/episode/<id>` | `spotify:episode:<id>` | Spotify desktop (inferred) |
| `/org/mpris/MediaPlayer2/TrackList/NoTrack` | nothing loaded | Specification |

Parsing `xesam:url`: `https://open.spotify.com/(track|episode)/<id>` (query string ignored) produces `spotify:<type>:<id>`.

### Nothing playing

The read returns `NothingPlaying` when `mpris:trackid` is the `NoTrack` path, when `Metadata` is empty, or when `PlaybackStatus` is `Stopped` and both `uri` and `title` are null.

### Spotify desktop position rule

The Spotify desktop client has a long history of reporting `Position` 0. The backend keeps `lastPosition map[string]int64` and `trustedPosition map[string]bool`, keyed by bus name, for the life of the `Backend` (one process; watch mode reuses it).

- If the suffix is exactly `spotify`, the state is `playing`, `Position` is 0, and the bus name is not trusted, then `position_ms` is null.
- If a later read of the same bus name returns a nonzero `Position` different from the stored one, mark the bus name trusted.
- Every other player's `Position` is used as reported.

One-shot reads against Spotify desktop therefore report null whenever it reports 0. Whether current builds report a real position stays open in the manual matrix ([testing.md](./testing.md)).

## Error mapping

| Condition | Kind | Message | Hint |
| --- | --- | --- | --- |
| No session bus address | `NotAvailable` | no D-Bus session bus found | run spmeta inside a desktop session, or with `dbus-run-session` |
| Connect or `ListNames` fails or times out | `NotAvailable` | could not reach the D-Bus session bus | same as above |
| No matching player | `NotAvailable` | no Spotify MPRIS player found | start Spotify, spotifyd, ncspot, or spotify_player, or pass `--player` |
| `--player NAME` matches nothing | `NotAvailable` | no MPRIS player named NAME | run `spmeta doctor` to list players |
| All candidates' `GetAll` fail | `NotAvailable` | MPRIS players did not answer | none |
| `NoTrack`, empty metadata, or stopped with no item | `NothingPlaying` | | |
| Unknown `PlaybackStatus` value | `Unexpected` | unexpected PlaybackStatus "X" from PLAYER | none |
| Overall 2 s deadline exceeded | `Unexpected` | MPRIS read timed out | none |

## Timeouts

| Call | Timeout |
| --- | --- |
| Connect | 500 ms |
| `ListNames` | 500 ms |
| `GetAll`, per player | 500 ms; a timeout skips that player |
| Whole read | 2 s |

## Edge cases

| Case | Behavior | Evidence |
| --- | --- | --- |
| spotifyd before its first Spotify Connect session | No bus name yet: `NotAvailable` | spotifyd source |
| Several spotifyd instances | The one `Playing` wins; otherwise the lowest sorted suffix | Selection rule |
| Spotify desktop and spotifyd both running | Spotify desktop wins (earlier preference) even if paused | Selection rule; FR-5 |
| Spotify web player in a browser | Ignored unless `--player firefox` (or similar); Web mode covers it | PRD |
| spotify_player with no `mpris:trackid` | `uri` from `xesam:url` if present; otherwise null and `type: unknown` | spotify_player source |
| Flatpak or Snap Spotify | Assumed to use `org.mpris.MediaPlayer2.spotify` | Not checked |
| `mpris:length` sent as `uint64` or `double` | Accepted by `asInt64` | Defensive |
| `xesam:artist` sent as one string | Accepted by `asStrings` | Defensive |

## Integration test

Task T7 adds `internal/backend/mpris/integration_test.go` with build tag `integration`. The test:

1. Connects to the session bus with `Dial`.
2. Exports an object at `/org/mpris/MediaPlayer2` with `prop.Export` from `github.com/godbus/dbus/v5/prop`, which serves `org.freedesktop.DBus.Properties.GetAll` for `org.mpris.MediaPlayer2.Player`. The fixture metadata uses real `dbus.Variant` and `dbus.ObjectPath` values.
3. Requests the name `org.mpris.MediaPlayer2.spotify` (and, in other cases, `org.mpris.MediaPlayer2.spotifyd.instance42`) with `dbus.NameFlagDoNotQueue`.
4. Runs `Backend.Read` and compares the JSON output with the golden files that the fake-bus tests use.

It also covers `Players`, an empty bus, an unreachable address, and no address at all (`NotAvailable`, no autolaunch).

CI runs it on `ubuntu-latest` after `sudo apt-get install -y dbus-daemon`:

```bash
dbus-run-session -- go test -race -count=1 -tags integration ./internal/backend/mpris/...
```
