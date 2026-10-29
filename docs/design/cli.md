# CLI

This document defines the complete command-line surface of `spmeta`: commands, flags, output formats, the template syntax, exit codes, environment variables, the configuration file, and integration examples. Everything here is public contract ([ADR 0001](../decisions/0001-audience-build-vs-wrap.md)); changing it follows [ADR 0008](../decisions/0008-versioning-and-release.md).

## Synopsis

```text
spmeta [--source auto|local|web] [--format text|json|template] [--template STR]
       [--player NAME] [--watch] [--interval SECONDS] [--quiet] [--debug] [--config PATH]
spmeta auth login [--client-id ID] [--port PORT] [--no-browser] [--debug] [--config PATH]
spmeta auth logout [--config PATH]
spmeta auth status [--config PATH]
spmeta doctor [--debug] [--config PATH]
spmeta version
spmeta --version
spmeta help [COMMAND]
```

Parsing uses the Go `flag` package ([ADR 0006](../decisions/0006-dependencies.md)): `-flag` and `--flag` are equivalent, values are given as `--flag value` or `--flag=value`, flags must precede any positional argument, and the only short flag is `-h`. The first argument selects a subcommand when it is `auth`, `doctor`, `version`, or `help`; otherwise all arguments belong to the root read command, which accepts no positional arguments.

The `auth` subcommands exist from task T12 and `--source web` from task T13 onward ([ADR 0004](../decisions/0004-web-api-built-last.md)). In a build without Web, `--source web` exits 4 with "Web mode is not available in this version."

## Root command: read the current item

The root command reads the current item once, prints it, and exits; with `--watch` it keeps running.

| Flag | Values | Default | Description |
| --- | --- | --- | --- |
| `--source` | `auto`, `local`, `web` | `auto` | Source selection ([ADR 0005](../decisions/0005-auto-source-resolution.md)) |
| `--format` | `text`, `json`, `template` | `text` | Output format. Passing `--template` on the command line without `--format` selects `template` |
| `--template` | string | none | Template for `--format template` (see Template syntax) |
| `--player` | string | none | MPRIS player name or bus-name suffix (Linux). Ignored by other backends, with a `--debug` note |
| `--watch` | flag | off | Keep running; print a line only when the item or state changes |
| `--interval` | seconds, decimal allowed | 1 (local), `web_interval` (web) | Poll interval for `--watch`. Minimum 0.5 for local, 1 for web. Only valid with `--watch` |
| `--quiet` | flag | off | Print nothing on stdout or stderr; only set the exit code. Not valid with `--watch` |
| `--debug` | flag | off | Write diagnostics to stderr; never tokens or codes |
| `--config` | path | see Configuration file | Read this configuration file instead of the default |
| `--version` | flag | | Same as `spmeta version` |
| `-h`, `--help` | flag | | Print usage and exit 0 |

Usage errors exit 2 and print the problem plus `Run 'spmeta --help' for usage.` to stderr. These are usage errors:

- An unknown flag or command, or a positional argument on the root command.
- A value outside the allowed set for `--source` or `--format`.
- `--format template` with no template from any layer.
- A template that does not parse or names an unknown field.
- `--interval` below the minimum, `--interval` without `--watch`, or `--quiet` with `--watch`.
- An invalid value in an environment variable or the configuration file, or a configuration file that is not valid TOML. The message names the variable or file and key. Every layer is validated, so an invalid value fails even when a higher-precedence layer overrides it.
- A configuration file named by `--config` or `SPMETA_CONFIG` that does not exist or cannot be read.

## Output formats

### Text

The default format prints one line:

```text
▶ Amazone — Alvan · Home (2:00 / 3:58)
```

The line is assembled from parts; a part whose fields are null is omitted with its separator:

| Part | Content | Omitted when |
| --- | --- | --- |
| State icon | `▶` playing, `⏸` paused, `■` stopped | Never |
| Title | ` {title}`; for an ad with no title, ` Advertisement`; otherwise ` (no title)` | Never |
| Artist | ` — {artist}` | `artist` is null |
| Album | ` · {album}` | `album` is null |
| Time | ` ({position} / {duration})`, or ` ({duration})` when position is null | `duration_ms` is null |

Text and template output replace each newline, carriage return, and tab in field values with one space and drop other control characters, so one item is always one line. JSON keeps values unchanged.

When nothing is playing, text output is empty (no newline) and the exit code is 3.

### JSON

`--format json` prints the `NowPlaying` object on one line followed by a newline, as defined in [data-model.md](./data-model.md). When nothing is playing, it prints `null` followed by a newline and exits 3. In watch mode, the output is NDJSON: one object or `null` per line.

### Template

`--format template --template STR` renders one line per item.

#### Template syntax

The template uses a `{field}` mini-syntax, not Go `text/template`.

- `{name}` is replaced by the value of field `name`.
- `{{` produces a literal `{` and `}}` produces a literal `}`.
- A null value renders as an empty string.
- Integers render in decimal, booleans as `true` or `false`.
- There are no format specifiers, conditionals, or functions.
- The template is parsed before any backend is read. An unknown field name, an unclosed `{`, or a stray `}` is a usage error (exit 2) that names the position.

Available fields:

| Field | Value |
| --- | --- |
| `schema_version`, `source`, `state`, `type` | As in JSON |
| `title`, `artist`, `album`, `album_artist` | As in JSON |
| `artists` | Same as `artist` (joined with `", "`) |
| `track_number`, `disc_number` | As in JSON |
| `duration_ms`, `position_ms` | As in JSON |
| `duration`, `position` | Pre-formatted: `m:ss` below one hour, `h:mm:ss` at one hour or more |
| `uri`, `url`, `artwork_url`, `isrc`, `explicit` | As in JSON |
| `device` | `device.name` |
| `device_type` | `device.type` |
| `state_icon` | `▶`, `⏸`, or `■` |
| `fetched_at` | As in JSON |

When nothing is playing, template output is empty and the exit code is 3.

#### Why not Go text/template

The `{field}` syntax was chosen over Go `text/template` for four reasons:

1. It keeps the PRD's documented semantics: `{artist} - {title}`, missing fields render empty, and `position` and `duration` are pre-formatted.
2. It is shell-friendly: `--template '{artist} - {title}'` needs no `{{.Artist}}` quoting.
3. `text/template` prints `<no value>` for missing map keys and reports errors only at execution time; the mini-syntax validates field names before reading.
4. Conditionals and functions would make templates a second API surface to keep stable. Scripts that need logic use `--format json` with `jq`.

## Exit codes

| Code | Meaning |
| --- | --- |
| 0 | An item is playing, paused, or loaded; output written. Also: watch mode stopped by SIGINT or SIGTERM; successful `auth`, `doctor`, `version`, `help` |
| 1 | Unexpected error: unparseable source output, network failure, timeout, Web 5xx after one retry |
| 2 | Usage error (see Root command) |
| 3 | A source is reachable but nothing is loaded, or no Web device is active |
| 4 | No source available: Spotify not running, no MPRIS player, no session bus, not logged in to Web, unsupported platform |
| 5 | Web authentication required: refresh failed, token file unreadable, login aborted or timed out. Run `spmeta auth login` |
| 6 | Permission denied: macOS Automation not granted, or Web 403 (not on the app's allowlist, or the app owner's Premium lapsed) |
| 7 | Rate limited by the Web API and gave up |

Every non-zero exit prints one `spmeta: <message>` line on stderr, optionally followed by a `hint: <next step>` line; this includes exit 3, while stdout stays as described under Output formats. `--quiet` suppresses both lines, except for usage errors (exit 2).

With `--source auto`, when no source returns an item, the exit code comes from the highest-precedence error: 6, then 5, then 7, then 1, then 3, then 4 ([ADR 0005](../decisions/0005-auto-source-resolution.md)).

In watch mode, the process exits only on a fatal error (5 or 6), a usage error (2), or a signal (0). Nothing playing, unavailable sources, rate limits, and transient errors keep it running. The one exception is a source that can never become available during the run, such as `--source web` in a build without Web: watch mode exits 4 at once instead of polling.

## Subcommands

### auth login

`spmeta auth login` runs the Authorization Code with PKCE flow in the browser and stores tokens ([backend-web.md](./backend-web.md)).

| Flag | Default | Description |
| --- | --- | --- |
| `--client-id` | from env or configuration | Spotify app Client ID |
| `--port` | 8974 | Loopback port for the callback. 0 picks any free port, which works only if the app registered a portless redirect URI; the dashboard currently rejects those |
| `--no-browser` | off | Do not open a browser; only print the URL |
| `--debug` | off | Write diagnostics to stderr; never tokens or codes |
| `--config` | see Configuration file | Read this configuration file instead of the default |

Behavior:

- `--port` outside 0 to 65535 is a usage error (exit 2). A port that is already in use exits 1 with "port N is in use."
- Without a Client ID from any layer, print the one-time setup steps (create an app, register `http://127.0.0.1:8974/callback`, add your account under User Management even if it owns the app, copy the Client ID) to stderr and exit 2. The Client ID is trimmed of surrounding whitespace.
- Print the authorization URL to stderr, open it with `open` (macOS) or `xdg-open` (Linux) unless `--no-browser`, and wait up to 180 s for the callback.
- On success, write the token file, print `Logged in. Token stored at <path>.`, and exit 0.
- If the user denies access, the callback times out, the code exchange is refused, or `spmeta` is interrupted while waiting, exit 5. A network failure or a 5xx from Spotify during the exchange exits 1.
- The Client ID passed with `--client-id` is stored in the token file so later refreshes do not depend on the flag. It is not written to `config.toml`.

### auth logout

`spmeta auth logout` deletes the token file and exits 0, also when no token file exists (it then prints `Not logged in. No token file at <path>.` instead of `Logged out. Removed <path>.`). It leaves `token.json.lock` in place and prints a reminder that access can be revoked at `https://www.spotify.com/account/apps/`, because Spotify offers no token-revocation endpoint. It exits 1 when the file exists but cannot be removed.

### auth status

`spmeta auth status` reads the token file without network access and prints:

```text
Logged in.
Client ID:              0123456789abcdef0123456789abcdef
Scope:                  user-read-playback-state
Access token expires:   2026-10-27T18:40:00Z
Authorized:             2026-10-27T17:40:00Z
Refresh token expires:  about 2027-04-27 (6 months after authorization)
Token file:             /home/user/.config/spmeta/token.json (mode 0600)
```

It exits 0 when a token file exists and parses, and 4 otherwise; a file that exists but cannot be read or parsed prints its error and also exits 4, because nothing was refreshed. The tokens themselves are never printed.

Warnings follow the table on stdout:

- When the estimated refresh-token expiry is 14 days away or less, `Warning: run 'spmeta auth login' before <date>.` Once that date has passed, `Warning: the saved login probably expired on <date>; run 'spmeta auth login'.`
- When the token file mode allows group or other access (any of the bits `0077`), `Warning: other users can access the token file. Run: chmod 600 '<path>'`.

On Windows the mode is not shown and the mode warning is not given, because Go does not report meaningful permission bits there.

`logout` and `status` accept only `--config`. All three commands print configuration warnings (unknown keys) to stderr and treat an invalid configuration as a usage error. When no home directory or `XDG_CONFIG_HOME` is known, they exit 1 with "no location is known for the token file".

### doctor

`spmeta doctor` reports which sources are available and why the others are not (FR-11). It may wait up to 120 s for the macOS Automation prompt, and it makes one Web request when a token file exists.

| Flag | Default | Description |
| --- | --- | --- |
| `--debug` | off | Write diagnostics, including the cause of each failed check, to stderr |
| `--config` | see Configuration file | Read this configuration file instead of the default |

`doctor` accepts no other flags and no positional arguments (exit 2). It has no `--quiet`.

```text
spmeta 0.1.0 (darwin/arm64)
config  ok: /Users/user/.config/spmeta/config.toml not found, using defaults
macos   ok: Spotify is running; Automation permission granted
web     not configured: run 'spmeta auth login' to see playback on other devices (optional)
```

Each line starts with the check name, padded to 8 columns, and one of `ok`, `unavailable`, `not configured`, `denied`, or `error`, followed by a colon, the reason and, where useful, the next step. The `config` line follows the same rule. A check may add detail lines indented by 8 spaces under its line: `config` lists each unknown key, and `mpris` lists each player. Checks per platform:

| Check | What it does |
| --- | --- |
| `config` | Resolves the path; parses the file; lists unknown keys. A configuration that cannot be loaded (invalid TOML or value, or a missing file named by `--config`) is reported as `error` with the message `config.Load` produces; it does not stop the other checks and does not change the exit code |
| `macos` (macOS) | Runs the process check. If Spotify is running, sends a minimal Apple event with a 120 s timeout and prints "macOS may ask to allow <terminal> to control Spotify; click OK." to stderr before sending. `<terminal>` comes from `TERM_PROGRAM` (`Apple_Terminal` as Terminal, a trailing `.app` dropped), or reads "the app that runs spmeta" when it is unset. Reports `ok` when granted, `denied` (with the System Settings path), or `error` when no answer arrives or the check fails. If Spotify is not running, reports `unavailable` because permission cannot be checked without launching it, and does not launch it |
| `mpris` (Linux) | Resolves the session bus address, lists MPRIS players one per detail line, marks the one `spmeta` would select (the `player` setting, else the usual preference order), and shows `no answer` for a player whose `GetAll` failed. `ok` needs a selected player; no bus, no players, or no Spotify client is `unavailable` |
| `local` (other platforms) | Reports `unavailable`: this platform is not supported |
| `web` | Reports the Client ID source, token file presence and mode, access-token expiry, and estimated refresh-token expiry; if a token file exists, performs one `GET /v1/me/player` and reports the outcome. In a build without Web it reports `unavailable: Web mode is not available in this version` and makes no request |

The report goes to stdout, one line per check as soon as the check finishes, so a long wait on the Automation prompt shows what was checked before it. `doctor` never reads the current track and never launches Spotify.

`doctor` exits 0 when at least one source is usable and 4 otherwise. A source is usable when it is reachable: macOS permission granted, or an MPRIS player selected. Web alone counts once it exists (T14). On exit 4 it prints `spmeta: no source is available` and a `hint:` line on stderr after the report.

### version

`spmeta version` and `spmeta --version` print one line and exit 0:

```text
spmeta 0.1.0 (commit 1a2b3c4, built 2026-10-27T17:40:00Z, go1.27.0, darwin/arm64)
```

The version comes from `-ldflags "-X main.version=..."` in release builds, or from Go build information for `go install` builds; a local `go build` prints `spmeta dev (...)`.

### help

`spmeta help` prints the synopsis; `spmeta help auth` and `spmeta help doctor` print the subcommand usage. `-h` on any command does the same; `auth login -h`, `auth logout -h` and `auth status -h` print the usage of `auth`.

## Environment variables

Environment variables override the configuration file; flags override both.

| Variable | Equivalent | Values |
| --- | --- | --- |
| `SPMETA_CONFIG` | `--config` | Path to the configuration file |
| `SPMETA_SOURCE` | `--source` | `auto`, `local`, `web` |
| `SPMETA_FORMAT` | `--format` | `text`, `json`, `template` |
| `SPMETA_TEMPLATE` | `--template` | Template string (does not change the format) |
| `SPMETA_PLAYER` | `--player` | Player name |
| `SPMETA_CLIENT_ID` | `--client-id` | Spotify Client ID |
| `XDG_CONFIG_HOME` | | Base configuration directory; used only when it is an absolute path |

An empty variable counts as unset.

## Configuration file

The configuration file is optional. Its path is, in order of precedence: `--config`, `SPMETA_CONFIG`, `$XDG_CONFIG_HOME/spmeta/config.toml`, then `~/.config/spmeta/config.toml`. The same path rule applies on macOS and Linux; `spmeta` does not use `~/Library/Application Support`.

```toml
# ~/.config/spmeta/config.toml
client_id      = "0123456789abcdef0123456789abcdef"
default_source = "auto"            # auto | local | web
default_format = "template"        # text | json | template
template       = "{artist} - {title}"
player         = "spotifyd"
interval       = 1.0               # seconds, watch mode, local
web_interval   = 3.0               # seconds, watch mode, Web (minimum 1)
```

| Key | Type | Default | Equivalent |
| --- | --- | --- | --- |
| `client_id` | string | none | `--client-id`, `SPMETA_CLIENT_ID` |
| `default_source` | string | `auto` | `--source`, `SPMETA_SOURCE` |
| `default_format` | string | `text` | `--format`, `SPMETA_FORMAT` |
| `template` | string | none | `--template`, `SPMETA_TEMPLATE` |
| `player` | string | none | `--player`, `SPMETA_PLAYER` |
| `interval` | float, seconds | 1.0 | `--interval` |
| `web_interval` | float, seconds | 3.0 | none |

Unknown keys print one warning per key on stderr (suppressed by `--quiet`) and are otherwise ignored, so a newer configuration file does not break an older binary. A missing file at the default location is not an error; a missing file named explicitly by `--config` or `SPMETA_CONFIG` is a usage error, so a mistyped path is never silently ignored.

### Token file

The token file is `token.json` in the same directory as the default configuration file (`$XDG_CONFIG_HOME/spmeta/token.json`, else `~/.config/spmeta/token.json`); `--config` does not move it. The directory is created with mode 0700 and the file with mode 0600. Its format is in [backend-web.md](./backend-web.md).

## Integration examples

These examples assume `spmeta` is on `PATH`.

### tmux

Show `Artist - Title` in the status line, refreshed every 5 seconds; the segment is empty when nothing plays:

```bash
# ~/.tmux.conf
set -g status-interval 5
set -g status-right '#(spmeta --template "{state_icon} {artist} - {title}" 2>/dev/null)'
```

### Waybar

A custom module that updates on each change. Waybar hides a text module whose line is empty, which is what `spmeta` prints when nothing plays:

```json
"custom/spotify": {
  "exec": "spmeta --watch --template '{state_icon} {artist} - {title}'",
  "max-length": 60,
  "escape": true
}
```

For Waybar's JSON return type, build the JSON with `jq` instead of a template, so quotes in titles are escaped correctly:

```bash
spmeta --watch --format json | jq --unbuffered -c \
  'if . == null then {text: ""} else {text: "\(.artist) - \(.title)", class: .state, tooltip: .album} end'
```

### SketchyBar

A plugin script that updates an item's label. SketchyBar becomes the responsible process for the macOS Automation prompt, so run `spmeta doctor` once from a terminal and allow SketchyBar when macOS asks:

```bash
#!/usr/bin/env bash
# ~/.config/sketchybar/plugins/spotify.sh
label="$(spmeta --template '{artist} – {title}' 2>/dev/null)"
sketchybar --set "$NAME" label="$label" drawing="$([ -n "$label" ] && echo on || echo off)"
```

```bash
# ~/.config/sketchybar/sketchybarrc
sketchybar --add item spotify right \
           --set spotify update_freq=5 script="$HOME/.config/sketchybar/plugins/spotify.sh"
```

### Shell scripts

Branch on the exit code:

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
