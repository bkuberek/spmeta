# Next level: events, status-bar output, completions, distribution, demo, trust gaps

## Objective

Take `spmeta` from "works" to "finished product": instant track changes in `watch`, first-class status-bar output, shell completions and a man page, more install channels, a README demo, and closure of the open verification gaps.

## Why

Status bars are the primary audience. They need instant updates and copy-paste configuration. Visitors decide from the README in seconds. Packagers and shell users expect completions and a man page. Untested exit paths and the doctor misreport erode trust.

## Scope (authorized 2026-10-30: "let's get 1 through 6 in whichever order you choose")

1. Event-driven `watch` (Linux MPRIS signals; macOS distributed notifications).
2. Status-bar output (`--format waybar`) and copy-paste examples.
3. README demo GIF recorded with VHS.
4. Shell completions and a man page, shipped in releases.
5. Trust gaps: doctor misreport on invalid config, T10 manual rows, live exit codes 5 and 6.
6. Distribution: AUR and Nix.

Out of scope: release-flow simplification (options A/B/C still open), Windows (ADR 0009, parked).

## Constraints

- No new runtime dependency without an ADR (ADR 0006). `CGO_ENABLED=0` stays (ADR 0008). The macOS process guard stays (ADR 0007): never launch Spotify.
- Public contract changes (flags, formats, subcommands) update `docs/design/cli.md` and `README.md` in the same commit.
- Test-first per task. Before every commit: `go test ./...`, `go vet ./...`, `golangci-lint run`.
- Never log tokens or secrets. Never interact with the user's apps (no play/pause/next, no `kill -STOP`).
- Publishing channels that need accounts or secrets (AUR SSH key, NUR repository) are configured to skip upload when the secret is missing; creating accounts, repositories, or secrets is the user's step.

## Tasks

- [x] N1 — Doctor reports the Web token state even when the config file is invalid.
  - Cause: `config.Load` returns `Settings{}` on error (`internal/config/config.go:107-121`), so `TokenPath` is empty and `newWeb` returns nil (`internal/cli/web.go:41`).
  - Acceptance: with an invalid config file and a valid token file, doctor shows the config error and a real Web check result, not `web not configured`. RED test in `internal/cli/doctor_test.go` first.
- [x] N2 — `--format waybar` and an `examples/` directory.
  - Acceptance: emits one JSON object per line with `text`, `tooltip`, `class` (and `alt`), schema documented in `docs/design/cli.md`; `class` reflects playing/paused/stopped; nothing playing emits an empty `text` and exits per the existing contract in watch mode; copy-paste configs for Waybar, SketchyBar, and tmux under `examples/`, linked from the README.
- [x] N3 — Shell completions and a man page.
  - Acceptance: `spmeta completion bash|zsh|fish` prints a script; a man page `spmeta.1` is generated from one source of truth with the usage text; goreleaser ships both in archives, `.deb`/`.rpm`, and the Homebrew cask; a test keeps completions in sync with the flag sets.
- [x] N4 — Event-driven `watch` on Linux.
  - Acceptance: the watch loop accepts an optional wake source; the MPRIS backend subscribes to `PropertiesChanged` on `/org/mpris/MediaPlayer2` over one persistent connection; a track change emits without waiting for the next poll; polling stays as the fallback and keeps `--interval` semantics; falls back to pure polling when subscription fails. Uses godbus only.
- [x] N5 — Event-driven `watch` on macOS, plus ADR 0010.
  - Approach under probe: a long-lived `osascript -l JavaScript` listener for `com.spotify.client.PlaybackStateChanged` (no cgo, no new dependency). Listener never sends Apple events to Spotify, so it never launches it.
  - Acceptance: ADR 0010 records the event design for both platforms; listener lifecycle is tied to the watch context; falls back to polling when the listener fails.
- [x] N6 — AUR and Nix distribution.
  - Acceptance: goreleaser `aurs` (package `spmeta-bin`) and `nix` (NUR repository) sections that skip upload when their secret is missing; `release-check` passes; README install section and `docs/design/release.md` updated; user setup steps listed.
- [x] N7 — README demo GIF.
  - Decision (2026-10-30, user approved the recommendation): record in Docker with VHS against the mock MPRIS player from the integration tests, with neutral sample tracks, so the demo is real `spmeta` output, reproducible, and free of the user's listening history.
  - Acceptance: a VHS tape and a script that rebuild `docs/assets/demo.gif`, showing `spmeta`, `--format json`, and `--watch` picking up a track change; linked from the README.
- [x] N8 — Live trust checks with the user (tracked in a GitHub issue; done when the user is at the Mac).
  - T10 manual rows (podcast, ad, local file, Spotify quit, Automation denied, Linux players), exit 5 (revoked access) and exit 6 (removed from the allowlist). Results recorded in this document and in `odd/tasks/spmeta-v1.md`.

- [x] N9 — Name the app macOS asks about, for any terminal or status bar.
  - Problem: `doctor` guesses the app from `TERM_PROGRAM` (`internal/doctor/doctor.go:292`), which is wrong for Warp, VS Code, Ghostty (case), tmux, kitty, Alacritty, and status bars; the exit-6 error always says "for example Terminal".
  - Acceptance: walk the parent process chain (via `ps`, no new dependency) to the first `.app` bundle and use its name; fall back to a corrected `TERM_PROGRAM` table, then a generic message; use the same name in the exit-6 hint and in `doctor`; README explains that the permission is per app (each terminal and each status bar). Added 2026-10-30 at the user's request, delivered on PR #9.

## Order and routes

N1, N2, N3, N4, N5 (delegated writers, one at a time), N6 (delegated writer), N7 and N8 (need the user). Route per task is recorded under Progress.

## Checks

- Per code task: `go test ./...`, `go vet ./...`, `golangci-lint run`; Linux paths also run in the `golang` container (`docker run --rm -v "$PWD":/src -w /src golang:1 go test ./...`).
- Release config: `goreleaser check` and `goreleaser release --snapshot --clean` (via the CI `release-check` job).
- Review: RDD is off (global). Per commit, `gentle-ai review assess` sets the verification tier.

## Delivery

Strategy: two PRs, approved by the user on 2026-10-30 as an exception to `single-pr` for readiness, not size. PR 1 (`feat/next-level`): N1–N4, N6. PR 2 (`feat/macos-events`): N5 and N7. N8 is a GitHub issue. Forecast: roughly 1,500 to 2,000 authored lines. Branch: `feat/next-level`.

## Progress

- 2026-10-30: branch created; code mapped (explorer).
- N1 (delegated writer; risk high → independent verifier PASS): `config.Load` returns `TokenPath` with its error so doctor runs the real Web check. RED/GREEN `TestDoctorChecksWebWhenConfigIsInvalid`. Commit 54335f8.
- N2 (delegated writer; risk high → verifier PASS against Waybar source and wiki; two doc nits fixed by the parent): `--format waybar` with Pango escaping, `examples/` for Waybar, SketchyBar, tmux. Live output checked on macOS. Commit 76ed0fc.
- N3 (delegated writer; risk high → verifier PASS; three wording fixes by the parent): `internal/reference` drives help, completions, and `spmeta(1)`; `spmeta completion bash|zsh|fish`; `internal/docgen` runs as a goreleaser before hook into `dist-extras/`; archives, .deb, .rpm, and cask ship them (zsh path per packager). `goreleaser check` and snapshot pass in Docker; .deb and .rpm installs verified in Debian and Fedora containers. Help now lists `--watch` and `--interval`.
- N4 (delegated writer; risk high → verifier PASS, then two follow-ups by the same writer): `backend.Notifier` and `Coalesce`; watch waits can be woken (`Wake`, `MinWakeGap` 100 ms flood guard; Retry-After and backoff waits cannot be shortened; a closed channel falls back to polling); `Resolver.Changes` passes through to the local backend; MPRIS notifier on one persistent godbus connection (PropertiesChanged and NameOwnerChanged, signal-name filter for godbus unicast signals). Real-bus integration test in the golang container passes. No reconnect after a session-bus restart (documented).
- N6 (delegated writer; risk high → verifier PASS with empty secrets in a scratch non-snapshot release; three doc fixes by the parent): goreleaser `aurs` (`spmeta-bin`) and `nix` (`bkuberek/nur-packages`) gated by `skip_upload` on `AUR_KEY` and `NUR_GITHUB_TOKEN`; release.yml passes both secrets. Publish order in v2.19.0: GitHub Release, Nix, cask, AUR. User setup steps are in `docs/design/release.md#aur-and-nix-one-time-setup`.
- macOS notification probe: listener registers, but two runs (5 and 10 minutes) saw no events while Spotify was paused. Needs a live play/skip by the user before N5.
- 2026-10-30: JXA delivery proven with a private notification name (`dev.spmeta.probe`): received within a second, about 29 MB idle, 0.0% CPU. Spotify's own notification is still unobserved live (issue #8).
- PR 1 opened: https://github.com/bkuberek/spmeta/pull/7 (CI 8/8 green). Issue #8 holds N8. Branch auto-delete enabled. Release-please token option committed on `feat/macos-events` as 9d68efc (falls back to `GITHUB_TOKEN` until `RELEASE_PLEASE_TOKEN` exists).
- N5 (delegated writer; risk high → verifier PASS, then follow-ups by the same writer): macOS backend implements `Notifier` with a long-lived `osascript -l JavaScript` listener for `com.spotify.client.PlaybackStateChanged`; sends no Apple events, so it never launches Spotify. `runWatch` waits up to 1 s for the notifier to stop; a 5 s heartbeat makes the listener exit after SIGKILL or SIGHUP of spmeta (measured 3 s). End-to-end test posts only `dev.spmeta.test.*` names and skips on CI timeout. ADR 0010 Accepted.
- N7 (delegated writer; risk high → verifier PASS; three nits fixed by the parent): `docs/assets/demo/record.sh` builds the real Linux `spmeta` and a `//go:build demo` mock MPRIS player, then records `docs/assets/demo.gif` (166 KB, 24.5 s) with VHS v0.11.0 under `dbus-run-session`. Fictional tracks; trackids produce no Spotify URIs. Frames inspected. VHS v0.12.0 writes no output, hence the pin.
- N8 (live, with the user, 2026-10-31; results in issue #8): Spotify's `PlaybackStateChanged` fires on play, skip, pause, and quit, and `--watch` printed 0.3–0.4 s later with a 30 s poll, so N5 is verified live. Podcast, local file, Spotify quit (exit 4, not relaunched), and Automation denied (exit 6, doctor names iTerm) pass. Homebrew cask on Linux passes in `homebrew/brew`. Ad and nothing-loaded are not reproducible; Web exits 5 and 6 skipped by the maintainer; Linux desktop players open.
- N9 (delegated writer; risk high → verifier PASS, resolving iTerm through zsh, script, and env -i chains; parent added the `LC_TERMINAL=iTerm2` fallback for tmux test-first, stubbed `ps` in the doctor exit-code test, and fixed the SketchyBar wording in examples): `internal/hostapp` walks one `ps` snapshot to the outermost `.app` bundle, then a launchd-rooted non-shell host (SketchyBar), then a corrected `TERM_PROGRAM` table, then generic wording. Used by `doctor` and the exit-6 hint, only on the denied path.

## Next step

N1.
