# macOS backend

The macOS backend reads Spotify.app through AppleScript run by `/usr/bin/osascript` ([ADR 0007](../decisions/0007-macos-applescript-access.md)). This document is the implementation spec: the process guard, the exact script, the output format and parser, error mapping, timeouts, and edge cases. Evidence for every rule is in [macos-applescript.md](../research/macos-applescript.md); "probe" below refers to the live probe of Spotify.app 1.3.4.258.

## Package

`internal/backend/macos`. The parser and backend carry no build tags, so their tests run on Linux CI too. Only `internal/backend/local/local_darwin.go` wires this backend into the binary.

```go
type Runner interface {
	// ProcessRunning reports whether a process with exactly this name runs.
	ProcessRunning(ctx context.Context, name string) (bool, error)
	// RunScript runs /usr/bin/osascript with the script on stdin.
	RunScript(ctx context.Context, script string) (stdout, stderr []byte, err error)
}

func New(r Runner, now func() time.Time) *Backend
func (b *Backend) Source() model.Source // "macos"
func (b *Backend) Read(ctx context.Context) (model.NowPlaying, error)

// CheckPermission is used by doctor. It never launches Spotify. beforePrompt,
// when not nil, runs just before the Apple event is sent.
func (b *Backend) CheckPermission(ctx context.Context, beforePrompt func()) PermissionResult

// Changes implements backend.Notifier for watch mode (see Change notifications).
func (b *Backend) Changes(ctx context.Context) (<-chan struct{}, error)
```

`ExecRunner` is the production `Runner`:

- `ProcessRunning` runs `/usr/bin/pgrep -x <name>`. Exit status 0 means running, 1 means not running, anything else is an error.
- `RunScript` runs `exec.CommandContext(ctx, "/usr/bin/osascript", "-")` with the script on stdin, captures stdout and stderr separately, and sets `cmd.WaitDelay = 100 * time.Millisecond` so a killed process does not hang the read.

## Read sequence

1. Derive a 2 s context from the caller's context.
2. Call `ProcessRunning(ctx, "Spotify")` (1 s timeout). If false, return `NotAvailable`: "Spotify is not running." No Apple event is sent.
3. Run the read script. Map errors as described in Error mapping.
4. Parse stdout as described in Output format.
5. Build a raw `NowPlaying` with `source: macos`, set `fetched_at` from `now()`, and return `model.Normalize` of it.

## Read script

The script below is the exact constant `readScript` in `script.go`. Rules it follows: it targets the bundle ID, it checks `running` before the `tell` block, no variable is named `st`, each field read is wrapped in `try`, `missing value` becomes an empty string, the position is converted to integer milliseconds inside the script, and fields are separated by character id 31.

```applescript
on txt(v)
	if v is missing value then return ""
	return v as string
end txt

if not (running of application id "com.spotify.client") then return "NOT_RUNNING"

tell application id "com.spotify.client"
	set pstate to (player state as string)
	if pstate is "stopped" then return "STOPPED"
	set sep to (character id 31)
	set t to missing value
	try
		set t to current track
	end try
	if t is missing value then return "NO_TRACK"

	set vName to ""
	try
		set vName to my txt(name of t)
	end try
	set vArtist to ""
	try
		set vArtist to my txt(artist of t)
	end try
	set vAlbum to ""
	try
		set vAlbum to my txt(album of t)
	end try
	set vAlbumArtist to ""
	try
		set vAlbumArtist to my txt(album artist of t)
	end try
	set vDuration to ""
	try
		set vDuration to my txt(duration of t)
	end try
	set vPosition to ""
	try
		set vPosition to my txt((player position * 1000) as integer)
	end try
	set vID to ""
	try
		set vID to my txt(id of t)
	end try
	set vSpotifyURL to ""
	try
		set vSpotifyURL to my txt(spotify url of t)
	end try
	set vArtwork to ""
	try
		set vArtwork to my txt(artwork url of t)
	end try
	set vTrackNumber to ""
	try
		set vTrackNumber to my txt(track number of t)
	end try
	set vDiscNumber to ""
	try
		set vDiscNumber to my txt(disc number of t)
	end try

	return "OK" & sep & pstate & sep & vName & sep & vArtist & sep & vAlbum & sep & vAlbumArtist & sep & vDuration & sep & vPosition & sep & vID & sep & vSpotifyURL & sep & vArtwork & sep & vTrackNumber & sep & vDiscNumber
end tell
```

This exact script was compiled with `osacompile` and run against Spotify.app on October 27, 2026 while a track was playing; it returned an `OK` record with 13 fields (see "Second probe" in [macos-applescript.md](../research/macos-applescript.md)). Task T5 recorded a live paused capture as `track-paused.txt`; `track-playing.txt` is derived from it (marked synthetic) until a live capture is taken while a track plays.

Latency, measured on October 27, 2026 against Spotify.app 1.3.4.258: the full script takes 0.22–0.27 s per read (each property read is one Apple event, about 5 ms). Precompiling the script with `osacompile` saves nothing measurable, and fetching all fields at once with `properties of current track` is not supported by Spotify (error -10000, also from JXA), so the per-property script stays.

AppleScript integers are limited to 536,870,911; larger values become reals and render in exponent notation. Durations and positions stay far below that limit (149 hours); a value that does not parse as an integer becomes null.

## Output format

`osascript` prints the returned string followed by one newline. The parser removes exactly one trailing `\n` and then recognizes four forms:

| Output | Meaning | Result |
| --- | --- | --- |
| `NOT_RUNNING` | Spotify quit between the process check and the script | `NotAvailable`: "Spotify is not running." |
| `STOPPED` | `player state` is `stopped` | `NothingPlaying` |
| `NO_TRACK` | No current track | `NothingPlaying` |
| `OK` + 12 fields, separated by U+001F | A track is loaded | Parsed as below |

Anything else, or an `OK` record that does not split into exactly 13 parts, returns `KindUnexpected`: "unexpected output from osascript"; the raw output is logged only under `--debug`.

Fields of the `OK` record, in order:

| Index | Field | Mapping |
| --- | --- | --- |
| 0 | `OK` | Literal marker |
| 1 | player state | `playing` or `paused`; anything else is `KindUnexpected` |
| 2 | name | `title` |
| 3 | artist | `artists = [artist]`; never split on commas |
| 4 | album | `album` |
| 5 | album artist | `album_artist` |
| 6 | duration | `duration_ms`, base-10 integer, milliseconds |
| 7 | position | `position_ms`, base-10 integer, milliseconds |
| 8 | id | `uri` when it starts with `spotify:` |
| 9 | spotify url | `uri` fallback when field 8 does not start with `spotify:` |
| 10 | artwork url | `artwork_url` |
| 11 | track number | `track_number` |
| 12 | disc number | `disc_number` |

A numeric field that fails `strconv.ParseInt` becomes null and is logged under `--debug`. If `uri` is null and `title` is empty, the result is `NothingPlaying`. `type` and `url` are left empty for `model.Normalize`, which derives them from `uri` ([data-model.md](./data-model.md)).

## Error mapping

When `osascript` exits nonzero, the backend extracts the error number from the last stderr line with the regular expression `\((-?\d+)\)\s*$`. Messages are localized by macOS, so only the number is matched.

| Condition | Kind | Message | Hint |
| --- | --- | --- | --- |
| `pgrep` reports not running | `NotAvailable` | Spotify is not running. | none |
| `pgrep` fails or times out | `Unexpected` | could not check whether Spotify is running | none |
| Error -1743 | `PermissionDenied` | macOS did not allow this app to control Spotify. | Allow it in System Settings > Privacy & Security > Automation, under the host app (see [Host application](#host-application)), or "the app that runs spmeta" when it is unknown, then run `spmeta doctor`. |
| Error -600 | `NotAvailable` | Spotify is not running. | none |
| Error -1712 (Apple event timed out) | `Unexpected` | Spotify did not respond. | Run `spmeta doctor`. |
| 2 s deadline exceeded | `Unexpected` | Spotify did not respond within 2s. | If this is the first run, macOS may be waiting for you to allow Automation access: run `spmeta doctor`. |
| Any other error number | `Unexpected` | osascript failed (error N). | none |
| Nonzero exit with no number | `Unexpected` | osascript failed. | none |

The exact stderr text for -1743 was not captured live; a typical form is `execution error: Not authorized to send Apple events to Spotify. (-1743)`. The fixture for it is marked synthetic.

## Permission check for doctor

`CheckPermission` runs only when `ProcessRunning` reports Spotify running. It calls `beforePrompt`, which `doctor` uses to print the prompt notice on stderr (see [cli.md](./cli.md)), then runs this script with a 120 s timeout:

```applescript
tell application id "com.spotify.client" to return (player state as string)
```

| Result | Report |
| --- | --- |
| Exit 0 | `ok`: Automation permission granted |
| Error -1743 | `denied`, with the System Settings path |
| Timeout | `error`: no answer within 120 s |
| Spotify not running | `unavailable`: cannot check permission without launching Spotify; start Spotify and run `spmeta doctor` again |

macOS attributes the prompt to the responsible process, so the permission belongs to one app: each terminal and each status bar such as SketchyBar has its own entry in System Settings. Allowing Terminal does not allow iTerm.

## Host application

`doctor` and the exit-6 hint name that app so the user can find the right entry. `internal/hostapp` resolves the name, best-effort, and only when a message needs it: a successful read never runs it.

1. **Process chain.** One `/bin/ps -axo pid=,ppid=,comm=` run (1 s limit) gives the process table. The walk starts at spmeta's parent and goes up through `ppid`, at most 64 hops, and stops at pid 1 (launchd), a missing pid, or a repeated pid.
2. **App bundle.** The first ancestor whose executable path contains `.app/Contents/MacOS/` names the host: the directory of the outermost `.app` in the path, without the suffix. Helpers nested in another app resolve to the outer app (`Visual Studio Code.app/.../Code Helper.app/...` is Visual Studio Code); `iTerm.app` is iTerm.
3. **No bundle.** When the walk reaches launchd without finding a bundle, the outermost ancestor that is not a shell or similar (`sh`, `bash`, `zsh`, `fish`, `dash`, `tmux`, `screen`, `login`, `sudo`, `env`, `go`, `spmeta`, and a few more) names the host, by executable name: `sketchybar` for a SketchyBar plugin. A tmux server is daemonized, so its chain holds only skipped names and gives no answer. A broken chain or a cycle gives no answer.
4. **`TERM_PROGRAM`.** When ps fails or the chain gives no answer: `Apple_Terminal` is Terminal, `iTerm.app` is iTerm, `ghostty` is Ghostty, `WarpTerminal` is Warp, `vscode` is Visual Studio Code, `WezTerm` is WezTerm, and `tmux` gives no answer because tmux hides the terminal, unless `LC_TERMINAL=iTerm2` (set by iTerm and kept inside tmux) names iTerm. Any other value is used as is without a trailing `.app`.
5. **Generic wording.** With no answer, messages say "the app that runs spmeta".

A `ps` failure falls back silently (a `--debug` line in `doctor`); it never changes an exit code. The unit tests feed fake `ps` output to the pure walker, and one darwin-only test runs the real `ps`.

## Change notifications

`Changes` lets watch mode read as soon as Spotify changes the track or the playback state, instead of waiting for the next poll ([ADR 0010](../decisions/0010-event-driven-watch.md)). It starts one long-lived `/usr/bin/osascript -l JavaScript -` process, writes the listener script to its stdin, closes stdin, and keeps the process until the watch context ends. It returns a channel that receives a value when a change may have happened and closes when the process exits.

The listener is a JavaScript for Automation (JXA) program. `listenerScript` builds it from the notification name, as a JSON string literal, and the heartbeat interval in seconds; production uses `com.spotify.client.PlaybackStateChanged` and a 5 s heartbeat:

```javascript
ObjC.import('Foundation');
ObjC.import('stdlib');

function say(line) {
	var data = $(line + '\n').dataUsingEncoding($.NSUTF8StringEncoding);
	if (!$.NSFileHandle.fileHandleWithStandardOutput.writeDataError(data, null)) {
		$.exit(0);
	}
}

ObjC.registerSubclass({
	name: 'SpmetaObserver',
	methods: {
		'onChange:': {
			types: ['void', ['id']],
			implementation: function (notification) { say('changed'); }
		},
		'onTick:': {
			types: ['void', ['id']],
			implementation: function (timer) { say('tick'); }
		}
	}
});

var observer = $.SpmetaObserver.alloc.init;
$.NSDistributedNotificationCenter.defaultCenter.addObserverSelectorNameObject(
	observer, 'onChange:', "com.spotify.client.PlaybackStateChanged", $());
$.NSTimer.scheduledTimerWithTimeIntervalTargetSelectorUserInfoRepeats(5, observer, 'onTick:', $(), true);
say('ready');
$.NSRunLoop.currentRunLoop.run;
```

Rules:

- The listener only observes a distributed notification. It sends no Apple events, so it never launches Spotify, needs no Automation permission, and triggers no consent prompt. It therefore runs without the process guard, and it starts even when Spotify is not running: notifications arrive once Spotify starts.
- The observer stays in a global variable because the notification center does not retain it. The script prints `ready` after it registers the observer, so no notification is lost between `ready` and the first change.
- `Changes` waits up to 2 s for `ready`. When the listener exits first or stays silent, `Changes` kills and reaps it and returns an error, and watch mode polls only. The error carries the exit status and the last line of the listener's stderr (at most 512 bytes are kept), or the read error when stdout could not be read, which `--debug` logs.
- After `ready`, each `changed` line becomes a wake through `backend.Coalesce`: at most one wake is pending, so a burst of notifications costs one read. Any other line, including `tick`, is ignored, so it costs no read; a missed change is still found by the next poll.
- The listener ignores the notification's `userInfo`. The read that follows takes a full snapshot with the read script, so one parser and one mapping serve both one-shot and watch reads.
- When the watch context ends, `Changes` kills the listener. It always waits for the process, so no zombie remains, and the channel closes after the process exits. Watch mode waits up to 1 s for that close before it returns, because `spmeta` exits right after, so a normal stop (SIGINT, SIGTERM, or a fatal error) leaves no listener behind. If the listener dies during the run, the channel closes and watch mode polls for the rest of the run; `Changes` does not restart it.
- The heartbeat covers the case where `spmeta` dies without stopping the listener, such as on SIGKILL or SIGHUP. Every 5 s the listener writes a `tick` line. Once `spmeta` is gone, nothing reads the pipe, so the write raises SIGPIPE and ends `osascript`; when SIGPIPE is ignored, the write fails and the script exits. An orphaned listener therefore exits within 5 s (about 3 s observed after SIGKILL and after SIGHUP). The ticks cost no measurable CPU: 0.0%, and 0.02 s of CPU time over 30 s.
- Spotify does not document `com.spotify.client.PlaybackStateChanged`. Other tools rely on it, but Spotify posting it has not been observed live on this Mac yet ([issue #8](https://github.com/bkuberek/spmeta/issues/8), item "Instant watch"). If Spotify stops posting it, the listener stays idle and watch mode keeps polling at `--interval`, without an error.

Each wake costs one read: the process check and the read script. The listener adds one `osascript` process per `--watch` run, measured on macOS 26 at 13 to 29 MB resident and 0.0% CPU while idle, and under 200 ms from start to `ready` (59 to 165 ms measured, slowest on a cold first run).

## Latency

The probe measured 150 to 170 ms for a warm `osascript` round trip. `pgrep` adds a few milliseconds (not measured). With negligible Go startup, a local read fits the 300 ms budget. The first call on a machine can block for many seconds on the consent prompt (about 12.9 s observed); the 2 s read timeout deliberately fails it with the `doctor` hint instead of hanging a status bar.

## Edge cases

| Case | Behavior | Evidence |
| --- | --- | --- |
| Spotify not running | `NotAvailable`; no Apple event sent; Spotify is not launched | Process check; in-script `running` check |
| Spotify quits during the read | `NOT_RUNNING` or error -600, both `NotAvailable`. A remaining race of a few milliseconds is covered by the manual matrix | Inferred |
| Paused | `state: paused`, exit 0 | Dictionary; manual matrix |
| Nothing loaded after launch | `STOPPED` or `NO_TRACK`, exit 3 | Inferred; manual matrix |
| Podcast episode | Expected `spotify:episode:<id>`, `type: episode`; `album` holds what the app reports | Unverified; manual matrix |
| Ad | Expected `spotify:ad:<id>`, `type: ad`, `url` null | Unverified; manual matrix |
| Local file | Expected `spotify:local:...`, `type: track`, `url` and `artwork_url` null | Unverified; manual matrix |
| Spotify Connect playing on another device | The app may report the remote item with broken name or artist; `spmeta` reports what the app returns | Reported; manual matrix |
| Hung app | 2 s timeout, exit 1 with the `doctor` hint | Manual matrix |
| Title containing tabs, pipes, or newlines | Preserved by the U+001F separator; text output replaces control characters | Golden test |
| Locale with decimal comma | Position is converted to an integer inside the script, so no decimal separator reaches the parser | Golden test |
