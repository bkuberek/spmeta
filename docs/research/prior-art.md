# Research: prior art

Date: October 27, 2026

This note surveys existing tools that report Spotify's current track, to decide whether `spmeta` should be built or whether wrapping existing tools is enough. The decision is [ADR 0001](../decisions/0001-audience-build-vs-wrap.md).

## Question

Does a tool already provide a stable, cross-platform (macOS and Linux) now-playing interface for Spotify with JSON output, watch mode, and documented exit codes?

## Findings

The survey was done at README and search level. Release dates, exit codes, and maintenance status beyond star counts were not verified.

| Tool | Platforms | Source of data | JSON | Watch | Status | Install |
| --- | --- | --- | --- | --- | --- | --- |
| [playerctl](https://github.com/altdesktop/playerctl) | Linux | MPRIS | Not documented; `--format` templates | `--follow` | About 3,000 stars | Distribution packages |
| [shpotify](https://github.com/hnarayanan/shpotify) | macOS | AppleScript | No | No | About 2,000 stars | `brew install shpotify` |
| [nowplaying-cli](https://github.com/kirtan-shah/nowplaying-cli) | macOS | MediaRemote | `get-raw` dictionary, not JSON | No | About 300 stars; broke on macOS 15.4 (issue 28); README credits the mediaremote-adapter fix | `brew install nowplaying-cli` (v2.1.0) |
| [media-control](https://github.com/ungive/media-control) with mediaremote-adapter | macOS | MediaRemote through `/usr/bin/perl` | Yes (`get`, `stream`) | Streams diffs | About 130 and 300 stars; early | `brew tap ungive/media-control` |
| [spotify-player](https://github.com/aome510/spotify-player) | macOS, Linux | Web API (PKCE), TUI | Search only | No | About 7,300 stars; active | Homebrew, Cargo |
| [spotify-tui](https://github.com/Rigellute/spotify-tui) | macOS, Linux | Web API | No | No | About 19,000 stars | Homebrew, Cargo |
| [spotify-cli-linux](https://github.com/pwittchen/spotify-cli-linux) | Linux | D-Bus | No | No | About 720 stars | pip |
| [sptlrx](https://github.com/raitonoberu/sptlrx) | All | Web API, MPRIS, MPD | No | Pipes lyrics | About 530 stars | Binaries |
| [spotify-cli (zach.dev)](https://spotify-cli.zach.dev/guide/getting-started.html) | macOS | Web API (PKCE) | Yes (`spotify now`); schema undocumented | Unknown | Unknown | Binary, Bun |

Not checked: `sp`, tmux and SketchyBar plugins, the crates.io `spotify-cli` 0.5.1, spotatui, and AUR packages.

## Gap

- No single tool covers macOS and Linux behind one interface.
- None documents a versioned JSON schema or exit codes.
- None combines local-first reads with a Web API fallback.
- None documents that it never launches a closed Spotify app.

## Verdict

`spmeta` is not a duplicate of any one tool. It is partly a composition of playerctl, an AppleScript or MediaRemote reader, and a Web API client. A shell wrapper around those works for personal use but is fragile across platforms and offers no contract. Building is justified only if the product is the contract: the schema, the exit codes, and the three-source fallback.

## Implications for the design

- The PRD cites playerctl and the Web API CLIs as prior art.
- `spmeta` adopts playerctl's dot-boundary player matching rather than inventing a new rule.
- The JSON schema, exit codes, and "never launch a closed Spotify" behavior are documented as public contract in [cli.md](../design/cli.md) and [data-model.md](../design/data-model.md).
