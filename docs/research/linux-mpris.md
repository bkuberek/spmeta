# Research: Linux MPRIS over D-Bus

Date: October 27, 2026

This note records how Spotify clients on Linux expose playback over MPRIS, how existing tools pick a player, and what the Go D-Bus library offers. The design that follows from it is in [backend-linux.md](../design/backend-linux.md).

## Question

Which MPRIS bus names, properties, and value shapes do Spotify clients use, how reliable is `Position`, and how should `spmeta` select a player and test the backend without a real desktop?

## Evidence levels

- **Verified**: stated in a specification or in the client's source code at the linked URL.
- **Reported**: community reports, not confirmed in source or live.
- **Inferred**: a conclusion drawn from the verified items.

## Verified findings

### MPRIS specification

- `Position` has D-Bus type `x` (int64) in microseconds. Players do not emit `PropertiesChanged` when it advances, so clients must poll. Source: [MPRIS Player interface](http://specifications.freedesktop.org/mpris/latest/Player_Interface.html).
- `mpris:trackid` has type `o` (object path). The specification page does not state the unit of `mpris:length`; microseconds is the convention, and ncspot uses microseconds. Same source.

### Clients

| Client | Bus name | `mpris:trackid` shape | Notes | Source |
| --- | --- | --- | --- | --- |
| Spotify desktop | `org.mpris.MediaPlayer2.spotify` | `/com/spotify/track/<id>`; possibly the string `spotify:track:<id>` (reported) | `mpris:artUrl` on `open.spotify.com/image/...` | [Gentoo wiki](https://wiki.gentoo.org/wiki/Spotify) |
| spotifyd | `org.mpris.MediaPlayer2.spotifyd.instance<PID>`, requested after `SessionConnected` | `/spotify/track/<id>` | Extrapolates `Position` | [dbus_mpris.rs](https://raw.githubusercontent.com/Spotifyd/spotifyd/master/src/dbus_mpris.rs) |
| ncspot | `org.mpris.MediaPlayer2.ncspot.instance<PID>` | `/org/ncspot/spotify/track/<id>` | `Position` and length in microseconds; sets `xesam:url` | [mpris.rs](https://raw.githubusercontent.com/hrkfdn/ncspot/main/src/mpris.rs) |
| spotify_player | souvlaki `dbus_name` `spotify_player`; final bus name unconfirmed | none | | [media_control.rs](https://raw.githubusercontent.com/aome510/spotify-player/master/spotify_player/src/media_control.rs) |
| Firefox | `org.mpris.MediaPlayer2.firefox.instance<PID>` | n/a | | [Fedora discussion](https://discussion.fedoraproject.org/t/media-controls-mpris-in-firefox-81-flatpak/23522) |

Chromium's bus name was not verified.

### Player matching in playerctl

playerctl matches a requested name against the suffix after `org.mpris.MediaPlayer2.`: the suffix must equal the name, or start with the name followed by `.`. A plain prefix match would make `spotify` select `spotifyd`. Source: [playerctl-common.c](https://raw.githubusercontent.com/altdesktop/playerctl/master/playerctl/playerctl-common.c).

### CI runner

The Ubuntu 24.04 GitHub runner image lists the `dbus` package. `dbus-run-session` is not listed separately, so CI installs `dbus-daemon` with apt to be safe. Source: [Ubuntu 24.04 runner readme](https://raw.githubusercontent.com/actions/runner-images/main/images/ubuntu/Ubuntu2404-Readme.md).

### Go library

`github.com/godbus/dbus/v5` is pure Go (no CGO), has a typed `Variant` API, and was last pushed on October 13, 2026 (v5.2.4, about 1,200 stars). See [language.md](./language.md).

## Reported, not confirmed

- **Spotify desktop `Position`.** Years of reports say it is always 0 ([community thread](https://community.spotify.com/t5/Desktop-Linux/Position-volume-through-dbus/td-p/4534107)). One thread says support was merged ([community thread](https://community.spotify.com/t5/Desktop-Linux/Populate-the-position-value-in-Dbus-with-the-position-of-the/td-p/5617426)); whether it shipped is unconfirmed. This stays open and is covered by the manual test matrix.
- **Spotify desktop `mpris:trackid` as a string** `spotify:track:<id>` instead of an object path: a KDE bug report, unconfirmed.

## Inferred

- godbus calls used by the design (`Connect`, `CallWithContext`, `Store` into `map[string]dbus.Variant`) are taken from general knowledge of the v5 API and must be checked against the package documentation during implementation.
- godbus's default session-bus discovery can fall back to `dbus-launch` autolaunch when `DBUS_SESSION_BUS_ADDRESS` is unset. `spmeta` must resolve the address itself so it never spawns a bus daemon.
- Flatpak and Snap builds of Spotify use the same bus name; not checked.

## Implications for the design

1. Match players with the dot-boundary rule, never a plain prefix.
2. Prefer, in order: `spotify`, `spotifyd`, `ncspot`, `spotify_player`. Within one preference entry, prefer a player whose `PlaybackStatus` is `Playing`; break ties by sorted suffix. Skip players whose `GetAll` fails or times out.
3. Normalize `mpris:trackid` with the regular expression `(track|episode|ad)[/:]([A-Za-z0-9]+)$` to `spotify:<type>:<id>`. This covers `/com/spotify/track/<id>`, `/spotify/track/<id>`, `/org/ncspot/spotify/track/<id>`, and the bare URI.
4. Treat `/org/mpris/MediaPlayer2/TrackList/NoTrack` as nothing loaded.
5. Prefer `xesam:url` for `url` when present.
6. Treat Spotify desktop `Position` of 0 while `Playing` as unknown (`null`) unless it changes between polls. Trust spotifyd and ncspot positions.
7. Convert microseconds to milliseconds.
8. Give every D-Bus call a timeout; a missing session bus maps to `NotAvailable`.
9. Browser players carry `.instance<PID>` suffixes and are ignored unless named with `--player`.
10. Test with a `Bus` interface and fakes; add one integration job that exports a mock MPRIS player from Go under `dbus-run-session`.

## Alternatives considered

| Option | Verdict |
| --- | --- |
| `godbus/dbus/v5` | Chosen: pure Go, typed, maintained, can export objects for the integration test |
| Shelling out to `playerctl` | Rejected: external binary, loses typed values; acceptable only as a debugging aid |
| `dbus-fast`, `dasbus`, `jeepney` | Python libraries; moot after the Go decision ([ADR 0002](../decisions/0002-language-go.md)) |

## Risks

- Spotify desktop `Position` and `trackid` shapes are unverified on current builds.
- spotify_player and Chromium bus names are unverified.
- spotifyd registers its name only after `SessionConnected`, so it is invisible before the first connection.
