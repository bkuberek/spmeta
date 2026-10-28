# spmeta

`spmeta` prints the track Spotify is playing right now: one line of text, JSON, or your own template. It is built for status bars, scripts, and overlays, and runs on macOS and Linux as a single binary.

```console
$ spmeta
▶ Amazone — Alvan · Home (2:00 / 3:58)
```

`spmeta` is an independent project and is not affiliated with, endorsed by, or sponsored by Spotify. "Spotify" is a trademark of Spotify AB.

Status: pre-release. There is no published release yet, so build it from source.

## Install

```bash
go install github.com/bkuberek/spmeta/cmd/spmeta@latest
```

This needs Go 1.26 or newer. Homebrew (`brew install bkuberek/tap/spmeta`) and `.deb` and `.rpm` packages come with the first release.

## Usage

```bash
spmeta                                       # one line of text
spmeta --format json                         # one JSON object
spmeta --template '{artist} - {title}'       # your own line
spmeta --watch                               # print a line on every change
spmeta doctor                                # check what works, and why not
```

`spmeta` reads the Spotify client running on this machine. It never launches Spotify.

| Flag | Default | Description |
| --- | --- | --- |
| `--format text\|json\|template` | `text` | Output format. `--template` alone selects `template` |
| `--template STR` | none | Template using `{field}` placeholders, for example `{state_icon} {artist} - {title}` |
| `--watch` | off | Keep running; print a line only when the track, state, or device changes |
| `--interval SECONDS` | 1 | Poll interval for `--watch`; minimum 0.5; only with `--watch` |
| `--player NAME` | none | Linux: use this MPRIS player (see [Linux](#linux)) |
| `--source auto\|local\|web` | `auto` | Where to read; `web` is [upcoming](#web-mode-upcoming) |
| `--quiet` | off | Print nothing; only set the exit code |
| `--debug` | off | Write diagnostics to stderr; never tokens |
| `--config PATH` | see below | Use this configuration file |

The full flag list, the template fields, and the environment variables are in [docs/design/cli.md](docs/design/cli.md). Defaults can live in `~/.config/spmeta/config.toml` (or `$XDG_CONFIG_HOME/spmeta/config.toml`) and in `SPMETA_*` variables; flags win over both.

## Output formats

**Text** (default) prints one line. A part with no value is left out with its separator.

```text
▶ Amazone — Alvan · Home (2:00 / 3:58)
```

The icon is `▶` playing, `⏸` paused, or `■` stopped.

**JSON** prints one object per line with a stable set of keys, validated by [schema/spmeta.schema.json](schema/spmeta.schema.json). Fields are described in [docs/design/data-model.md](docs/design/data-model.md).

```json
{"schema_version":1,"source":"macos","state":"playing","type":"track","title":"Amazone","artists":["Alvan"],"artist":"Alvan","album":"Home","album_artist":"Alvan","track_number":4,"disc_number":1,"duration_ms":238341,"position_ms":120165,"uri":"spotify:track:4brK3gpPjXArNUlU1hBXTe","url":"https://open.spotify.com/track/4brK3gpPjXArNUlU1hBXTe","artwork_url":"https://i.scdn.co/image/ab67616d0000b273a9ab54d90005cfd439f821e8","isrc":null,"explicit":null,"device":null,"fetched_at":"2026-10-27T17:40:00Z"}
```

**Template** renders one line from `{field}` placeholders. `{{` and `}}` give literal braces, and a missing value renders empty. An unknown field is an error before anything is read.

When nothing is playing, text and template output are empty, JSON prints `null`, and the exit code is 3.

## Exit codes

| Code | Meaning |
| --- | --- |
| 0 | An item is playing, paused, or loaded; output written. Also: watch mode stopped by SIGINT or SIGTERM; successful `auth`, `doctor`, `version`, `help` |
| 1 | Unexpected error: unparseable source output, network failure, timeout, Web 5xx after one retry |
| 2 | Usage error |
| 3 | A source is reachable but nothing is loaded, or no Web device is active |
| 4 | No source available: Spotify not running, no MPRIS player, no session bus, not logged in to Web, unsupported platform |
| 5 | Web authentication required: refresh failed, token file unreadable, login aborted or timed out. Run `spmeta auth login` |
| 6 | Permission denied: macOS Automation not granted, or Web 403 (not on the app's allowlist, or the app owner's Premium lapsed) |
| 7 | Rate limited by the Web API and gave up |

Codes 5 and 7, and the Web causes of the others, come with [Web mode](#web-mode-upcoming).

Every non-zero exit prints `spmeta: <message>` on stderr, sometimes followed by a `hint:` line. `--quiet` suppresses both, except for usage errors.

## Status bars and scripts

These assume `spmeta` is on `PATH`.

**tmux** shows `Artist - Title`, refreshed every 5 seconds, and nothing when idle:

```bash
# ~/.tmux.conf
set -g status-interval 5
set -g status-right '#(spmeta --template "{state_icon} {artist} - {title}" 2>/dev/null)'
```

**Waybar** updates a custom module on each change. Waybar hides a module whose line is empty, which is what `spmeta` prints when nothing plays:

```json
"custom/spotify": {
  "exec": "spmeta --watch --template '{state_icon} {artist} - {title}'",
  "max-length": 60,
  "escape": true
}
```

**SketchyBar** polls from a plugin script. The first run needs the macOS permission described [below](#macos): run `spmeta doctor` once from a terminal and allow SketchyBar when macOS asks.

```bash
#!/usr/bin/env bash
# ~/.config/sketchybar/plugins/spotify.sh
label="$(spmeta --template '{artist} – {title}' 2>/dev/null)"
sketchybar --set "$NAME" label="$label" drawing="$([ -n "$label" ] && echo on || echo off)"
```

**Shell scripts** can branch on the exit code:

```bash
if line="$(spmeta --template '{artist} - {title}')"; then
  echo "Now playing: $line"
else
  case $? in
    3) echo "Spotify is idle" ;;
    4) echo "Spotify is not running" ;;
    *) echo "spmeta failed" ;;
  esac
fi
```

The Waybar JSON return type and more examples are in [docs/design/cli.md](docs/design/cli.md#integration-examples).

## macOS

`spmeta` asks Spotify.app for the current track through AppleScript, so macOS requires a one-time Automation permission for the app that runs `spmeta` (Terminal, iTerm, SketchyBar, and so on).

1. Start Spotify.
2. Run `spmeta doctor` in a terminal. macOS shows a prompt; click OK. `doctor` waits up to 120 s for your answer.
3. The `macos` line should read `ok`.

```text
spmeta 0.1.0 (darwin/arm64)
config  ok: /Users/user/.config/spmeta/config.toml not found, using defaults
macos   ok: Spotify is running; Automation permission granted
web     unavailable: Web mode is not available in this version
```

Troubleshooting:

| Symptom | Fix |
| --- | --- |
| Exit 6, "macOS did not allow this app to control Spotify" | Open System Settings > Privacy & Security > Automation, find the app that runs `spmeta`, and turn on Spotify. Then run `spmeta doctor` |
| Exit 1, "Spotify did not respond within 2s" on the first run | macOS is probably waiting for you to answer the prompt. Run `spmeta doctor` |
| Exit 4, "Spotify is not running" | Start Spotify. `spmeta` never launches it |
| Exit 3 with Spotify open | Nothing is loaded yet; press play |
| Works in a terminal, not in a status bar | The status bar app is a different app to macOS. Allow it in the Automation list |

`spmeta doctor` exits 4 and names the failing check when no source works. Add `--debug` to see the cause of each failure.

## Linux

`spmeta` reads Spotify clients that speak MPRIS over the D-Bus session bus. With no `--player`, it tries these in order and uses the first one that is running. If several instances of that player run, the one that is playing wins:

1. `spotify` (the desktop client)
2. `spotifyd`
3. `ncspot`
4. `spotify_player`

Browsers and other MPRIS players are ignored unless you name them. `--player NAME` (or `player` in the config file, or `SPMETA_PLAYER`) selects a player by bus name, such as `spotifyd` or `spotifyd.instance1234`.

`spmeta` needs a session bus: run it inside a desktop session, or start one with `dbus-run-session`. It never autolaunches one. `spmeta doctor` lists the players it finds and marks the one it would read:

```text
spmeta 0.1.0 (linux/amd64)
config  ok: /home/user/.config/spmeta/config.toml not found, using defaults
mpris   ok: reading spotify
        spotify (Playing, selected)
        spotifyd.instance1234 (Paused)
web     unavailable: Web mode is not available in this version
```

The Spotify desktop client often reports its playback position as 0. `spmeta` then leaves `position_ms` null instead of showing a wrong time.

## Web mode (upcoming)

A later release will add `--source web`, which reads playback on any of your devices, such as a phone, through the Spotify Web API. It needs a one-time `spmeta auth login` with your own Spotify app. Until then `spmeta` reads only this machine, and `--source web` exits 4 with "Web mode is not available in this version."

## More documentation

Requirements, research, decisions, and the design specs are in [docs/README.md](docs/README.md).

## License

[MIT](LICENSE)
