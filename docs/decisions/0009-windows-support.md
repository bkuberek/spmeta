# 0009. Support Windows: Web mode first, then a media-session backend

- Status: Proposed (2026-10-29). Not scheduled. This record captures research and a plan for later review; no implementation follows it until the user accepts it.
- Date: October 29, 2026
- Deciders: Bastian Kuberek

## Context

`spmeta` v0.2.0 reads Spotify on macOS (AppleScript), on Linux (MPRIS over D-Bus), and on any platform through the Spotify Web API. The [PRD](../product/prd.md) lists Windows support as a v1 non-goal. This record describes what Windows support requires, so the decision can be made later with the evidence in one place.

### Current state on Windows

The code compiles for Windows: `GOOS=windows go vet ./...` and the amd64 and arm64 builds pass when run by hand. Nothing builds, tests, or ships it: the CI test matrix covers `ubuntu-latest` and `macos-latest` only (`.github/workflows/ci.yml`), and goreleaser builds `darwin` and `linux` only (`.goreleaser.yaml`).

These parts already work on Windows:

- Web mode, and `--source auto` falling back to Web, because the local backend reports an unsupported platform (`internal/backend/local/local_other.go`).
- Busy-port detection during login, which recognizes `WSAEADDRINUSE` (10048) (`internal/auth/login.go`).
- Token-file mode checks, which are skipped on Windows (`internal/cli/auth.go`).
- Signal handling: Go delivers Ctrl+C and Ctrl+Break as `os.Interrupt`, and console close, logoff, and shutdown as `SIGTERM` ([os/signal](https://pkg.go.dev/os/signal)).

These parts are missing or degraded:

- No local source. `spmeta doctor` reports that the platform is not supported.
- The login browser does not open, because `internal/auth/browser.go` has no Windows case. Login still prints the URL.
- Token refresh is locked only within one process (`internal/auth/lock_other.go`), so two processes can refresh at once and one can later fail with `invalid_grant`.
- The configuration directory resolves to `%USERPROFILE%\.config\spmeta` (`internal/config/paths.go`). The Windows convention is `%AppData%`, which `os.UserConfigDir` returns.
- File modes `0700` and `0600` (`internal/auth/token.go`) have no effect; the token file inherits the permissions of the user profile directory.
- Not verified: `os.Rename` over `token.json` can fail with a sharing violation while another process holds the file open.

### Local playback on Windows

Spotify on Windows has no AppleScript or MPRIS equivalent. The only system interface is the media-session API, `GlobalSystemMediaTransportControlsSessionManager` (SMTC), available since Windows 10 version 1809 ([manager](https://learn.microsoft.com/en-us/uwp/api/windows.media.control.globalsystemmediatransportcontrolssessionmanager)). Its fields map to the [data model](../design/data-model.md) as follows:

| SMTC field | `NowPlaying` field |
| --- | --- |
| `Title` | `title` |
| `Artist` | `artist`, `artists` |
| `AlbumTitle` | `album` |
| `AlbumArtist` | `album_artist` |
| `TrackNumber` | `track_number` |
| Playback status | `state` |
| Timeline `Position` | `position_ms` |
| Timeline `EndTime` minus `StartTime` | `duration_ms` |
| None | `uri`, `url`, `artwork_url`, `disc_number`, `isrc`, `explicit`, `device` |

SMTC has no track URI or ID field ([media properties](https://learn.microsoft.com/en-us/uwp/api/windows.media.control.globalsystemmediatransportcontrolssessionmediaproperties)), so `uri`, `url`, and `artwork_url` are always null, and `type` cannot be derived from a URI. The thumbnail is an image stream, not a URL. A console program can call the API without package identity, as Raymond Chen's C++ console sample shows ([The Old New Thing](https://devblogs.microsoft.com/oldnewthing/?p=108980)). Forum sources report the Spotify session's app ID as `Spotify.exe`; the Microsoft Store version's ID is not confirmed.

## Decision

Support Windows in two stages:

1. **Web mode on Windows.** Fix the browser opener, the token lock, and the configuration directory; add Windows to CI and to releases; publish a Scoop manifest.
2. **Local backend.** Add `internal/backend/smtc`, which reads SMTC through `github.com/saltosystems/winrt-go` and `github.com/go-ole/go-ole` without cgo, behind a `windows` build tag. Document it as a local source with fewer fields: users who need `uri`, `url`, or `artwork_url` use `--source web`.

Stage 2 adds two runtime dependencies and therefore amends [ADR 0006](./0006-dependencies.md). `saltosystems/winrt-go` has no tagged releases, so the module is pinned to a commit. Its `windows/media/control` package, added on January 6, 2026, provides `RequestAsync`, session listing, media properties, timeline, and playback information.

## Consequences

Stage 1:

- Windows users get Web mode, `--watch`, and `doctor`, with the same login flow as macOS and Linux.
- `internal/auth/browser.go` opens the URL with `rundll32 url.dll,FileProtocolHandler <url>`; `cmd /c start` would split the OAuth URL at each `&`.
- A new `internal/auth/lock_windows.go` locks across processes with `LockFileEx`, called through `syscall.NewLazyDLL("kernel32.dll")`, which needs no new dependency.
- The configuration directory on Windows becomes `%AppData%\spmeta` (`os.UserConfigDir`). `cli.md` and `paths.go` change together.
- Releases gain `windows` amd64 and arm64 zip archives (`format_overrides` with `formats: [zip]`) and a Scoop manifest in a new `bkuberek/scoop-bucket` repository, pushed with a token like `HOMEBREW_TAP_GITHUB_TOKEN`.
- Binaries are unsigned. SmartScreen and Smart App Control can warn about or block an executable started from Explorer; [ADR 0008](./0008-versioning-and-release.md) and `release.md` record this limitation, as they do for macOS notarization.

Stage 2:

- The schema's `source` enum gains `smtc`, which is a compatible change under the schema versioning rules in `data-model.md`.
- New documents: `docs/design/backend-windows.md` and a Windows mapping table in `data-model.md`.
- `doctor` gains a Windows checker and local line; wording that names only macOS and Linux changes (`internal/cli/read.go`, `internal/doctor/doctor.go`, `README.md`, the PRD non-goal).
- Live verification requires a Windows desktop with Spotify installed. CI tests the mapping with fakes only.

## Alternatives considered

| Option | Result | Reason not chosen |
| --- | --- | --- |
| Call SMTC from Windows PowerShell 5.1 | Same fields as the chosen option, no new dependency | Each call starts PowerShell, and the asynchronous WinRT calls need workarounds. PowerShell 7 cannot call WinRT, because .NET 5 removed built-in WinRT support ([.NET breaking change](https://learn.microsoft.com/en-us/dotnet/core/compatibility/interop/5.0/built-in-support-for-winrt-removed)). |
| Hand-written COM and WinRT calls with the standard library | Same fields, no new dependency | Reimplements a large part of `go-ole`; high maintenance cost for one API. |
| Parse the Spotify window title ("Artist - Title") | Title and artist only | No album, position, or paused state; titles that contain " - " split incorrectly. |
| Web mode only, no local backend | No new dependency | Windows users without a Spotify developer app get no output. |

## Plan

The stages break down into these tasks. Sizes are authored-line forecasts in the style of `odd/tasks/spmeta-v1.md`, not time estimates.

| Task | Content | Forecast |
| --- | --- | --- |
| W1 | Add `windows-latest` to the CI test matrix and `GOOS=windows go vet ./...` to the lint job; fix what fails | 50–150 |
| W2 | Browser opener, `LockFileEx` lock, `%AppData%` configuration directory, docs | 150–250 |
| W3 | goreleaser Windows zips, Scoop bucket, `release.md` and README install text | 100–150 |
| W4 | Amend ADR 0006 for `winrt-go` and `go-ole`; add `smtc` to the schema; update the PRD scope | 50–100 |
| W5 | `internal/backend/smtc` with a fake-session interface, mapping, fixture tests, and a manual live test | 400–600 |
| W6 | `local_windows.go`, the doctor checker and local line, wording updates | 100–200 |
| W7 | Optional: winget manifests through a fork of `microsoft/winget-pkgs` | 50 |

W1 to W3 form a release with Web mode on Windows. W4 to W6 add the local backend and depend on access to a Windows machine with Spotify.

## Open questions

- Which SMTC fields Spotify fills in (`TrackNumber`, `AlbumArtist`, `EndTime`), and how current the timeline position is. User reports describe a position stuck at 0:00 ([Spotify Community](https://community.spotify.com/t5/Desktop-Windows/Time-Bar-Stuck-at-0-00/m-p/5438221/highlight/true)), which may need a rule like the MPRIS Position-0 rule.
- The app ID of the Microsoft Store version of Spotify.
- How ads and podcast episodes appear in SMTC.
- Whether `winrt-go` leaks resources across many watch-mode polls, and how much it adds to the binary size.
- Whether `go test -race` works on the `windows-latest` runner, which requires cgo and a C compiler.
- Whether the Windows firewall prompts for the `127.0.0.1` login listener.

## Next steps

To act on this proposal:

1. Review the stages and decide whether Windows enters scope.
2. Decide the `source` value (`smtc` or `windows`) and the configuration directory.
3. If accepted, set the status to Accepted, add the W tasks to a feature document in `odd/tasks/`, and start with W1.
