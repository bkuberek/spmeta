# Research: macOS AppleScript access to Spotify.app

Date: October 27, 2026

This note records what Spotify.app exposes through AppleScript, what a live probe on this Mac returned, and what that means for the macOS backend. The design that follows from it is in [backend-macos.md](../design/backend-macos.md).

## Question

Which properties does Spotify.app expose, in which units and formats, how do you read them without launching a closed app, how fast is a read, and how does macOS Automation permission behave?

## Evidence levels

- **Observed live**: returned by Spotify.app 1.3.4.258 on macOS (Darwin 25.5.0, arm64) on October 27, 2026. This is the highest-authority evidence for this backend.
- **Verified**: stated in the scripting dictionary or a linked Apple source.
- **Reported**: community sources, not tested here.
- **Inferred**: a conclusion drawn from the above.

## Scripting dictionary (verified)

The scripting dictionary of Spotify.app 1.3.4.258 defines:

- Application properties: `current track`, `sound volume`, `player state` (`stopped`, `playing`, `paused`), `player position` (real, documented as seconds), `repeating`, `shuffling`.
- Track properties: `artist`, `album`, `disc number`, `duration` (integer, documented as seconds), `played count`, `track number`, `starred`, `popularity`, `id` ("The ID of the item"), `name`, `artwork url`, `artwork` (deprecated, never set), `album artist`, `spotify url` ("The URL of the track").

The dictionary's unit for `duration` is wrong; see the live probe.

## Live probe (observed live)

The probe ran a read-only script that first checks `pgrep -x Spotify` and exits without probing when Spotify is not running. The script targets `application id "com.spotify.client"` and prints one `key=value` line per property.

Track under test: "Amazone" by Alvan, album "Home", length shown by Spotify as 3:58.

Raw output:

```text
state=playing
name=Amazone
artist=Alvan
album=Home
album_artist=Alvan
duration=238341
player_position=120.156997680664
position_ms=120165
id=spotify:track:4brK3gpPjXArNUlU1hBXTe
spotify_url=spotify:track:4brK3gpPjXArNUlU1hBXTe
artwork_url=https://i.scdn.co/image/ab67616d0000b273a9ab54d90005cfd439f821e8
track_number=4
disc_number=1
```

`player_position` and `position_ms` were read a few milliseconds apart, which explains the 8 ms difference.

### Findings

1. `duration` is in **milliseconds** (238341 ms is 3:58). The dictionary's "seconds" is wrong.
2. `player position` is a real number of **seconds** with a fractional part. `(player position * 1000) as integer` yields milliseconds and avoids locale-dependent decimal separators.
3. `id` and `spotify url` both return the URI `spotify:track:<id>`. Neither is a web URL. The web URL must be derived: `https://open.spotify.com/track/<id>`.
4. `artwork url` is `https://i.scdn.co/image/<hash>`.
5. A script that names a variable `st` does not compile: `syntax error: Expected expression but found "st". (-2741)`. `st` is reserved as an ordinal suffix. The PRD's original script had this bug.
6. `tell application id "com.spotify.client"` works; the bundle ID was confirmed with `mdls`.
7. A warm `osascript` round trip against Spotify takes 150 to 170 ms over 5 runs. A no-target `osascript -e 'return 1'` takes about 46 ms, so the Apple event dominates.
8. The first call ever took about 12.9 s. The likely cause is the macOS Automation consent prompt waiting for the user; this is unconfirmed.

### Second probe: the design's read script (observed live)

On October 27, 2026, with Spotify already running (checked with `pgrep -x Spotify` first), the exact read script from [backend-macos.md](../design/backend-macos.md) compiled with `osacompile` and ran with `osascript -` (script on stdin). It exited 0 and printed one record; U+001F separators are shown as `|`:

```text
OK|playing|novacaine (addicted to you) - sped up|Lofuu|thinkin bout you (novacaine deluxe)|Lofuu|171605|65309|spotify:track:3L7RbxQx1JaMAs9sbvyRGV|spotify:track:3L7RbxQx1JaMAs9sbvyRGV|https://i.scdn.co/image/ab67616d0000b2739761c398f06544abaecf9dc2|1|1
```

This confirms the `on txt(v)` handler, the top-level `running` check, the `try` wrappers, the 13-field framing, and that the script compiles without the reserved `st`. Latency was not measured in this run.

### Not probed

These scenarios remain in the manual test matrix in [testing.md](../design/testing.md): podcast episode, ad, local file, paused, Automation denied (exact error text for -1743), Spotify quit (no launch), Spotify Connect to another device, and a hung app.

## Reported (not tested here)

- `duration` is milliseconds in practice despite the dictionary; `player position` is seconds. Source: [willcodefor.beer/asdur](https://willcodefor.beer/asdur). Now confirmed live.
- `spotify url` returns a `spotify:track:<id>` URI. Source: [MacScripter thread](https://www.macscripter.net/t/irc-script-spotify-shorten-url/65516). Now confirmed live.
- `id` returns the same URI. Source: [spotify-node-applescript](https://github.com/matthewnitschke/spotify-node-applescript). Now confirmed live.
- The application `running` property exists since Mac OS X 10.5 and does not need System Events. Source: [AppleScript application object](https://macosxautomation.com/applescript/features/appobject.html).
- A name-based `tell application "Spotify"` can resolve, and on some systems launch, the app at compile time; reports conflict. A process check before the Apple event, plus bundle-ID targeting, is the safer pattern.
- macOS attributes the Automation (TCC) prompt to the responsible process: Terminal, iTerm2, Ghostty, or the status-bar app that runs `spmeta`. Attribution under tmux, ssh, and launchd agents is unclear. Source: [Apple developer forums thread 693744](https://developer.apple.com/forums/thread/693744).
- Error codes: -1743 means not authorized to send Apple events; -600 means the application is not running.
- Since macOS 15.4, the private MediaRemote framework only serves `com.apple.*` bundle IDs. Source: [Keyboard Maestro forum](https://forum.keyboardmaestro.com/t/beware-upgrading-to-macos-15-4-if-you-need-now-playing-data/40285). `nowplaying-cli` behavior on 15.4 and later is unconfirmed; `ungive/mediaremote-adapter` works around the restriction through `/usr/bin/perl`.
- JXA uses the same TCC path as AppleScript and offers native JSON output.
- When Spotify plays on another device through Spotify Connect, `name` and `artist` may break. Source: [Spotify community thread](https://community.spotify.com/t5/Spotify-for-Developers/Connect-Recent-update-broke-AppleScript-for-Spotify-Connect/m-p/4785134/highlight/true).

## Unverified

- The ID formats for episodes, ads, and local files. The `spotify:ad:` prefix has no source.
- `AEDeterminePermissionToAutomateTarget` return codes (-1744 for "not determined") come from memory and were not checked.
- Whether `running of application id "com.spotify.client"` evaluated inside a script ever launches the app. The design keeps a process check in Go as the primary guard.

## Implications for the design

1. Check `pgrep -x Spotify` from Go before invoking `osascript`, and check `running` again inside the script.
2. Target `application id "com.spotify.client"`, never the name.
3. Never use a variable named `st`.
4. Separate fields with the ASCII unit separator (character id 31).
5. Wrap each field read in `try` so a missing value becomes an empty string; empty strings become `null` in the model.
6. Take `uri` from `id` (fallback `spotify url`) and derive `url` as `https://open.spotify.com/{type}/{id}`.
7. Read `duration` as milliseconds and compute `(player position * 1000) as integer` in the script.
8. Use a 2 s timeout for normal reads. The first call can block on the consent prompt, so `spmeta doctor` triggers and waits for permission with a long timeout, and a read timeout maps to an error whose hint says to run `spmeta doctor`.
9. Measured latency (about 160 ms) fits the 300 ms budget only with a runtime whose startup is negligible, which supports Go ([ADR 0002](../decisions/0002-language-go.md)).
10. MediaRemote is not an option for a third-party tool on macOS 15.4 and later ([ADR 0007](../decisions/0007-macos-applescript-access.md)).
