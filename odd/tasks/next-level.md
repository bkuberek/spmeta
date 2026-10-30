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
- [ ] N5 — Event-driven `watch` on macOS, plus ADR 0010.
  - Approach under probe: a long-lived `osascript -l JavaScript` listener for `com.spotify.client.PlaybackStateChanged` (no cgo, no new dependency). Listener never sends Apple events to Spotify, so it never launches it.
  - Acceptance: ADR 0010 records the event design for both platforms; listener lifecycle is tied to the watch context; falls back to polling when the listener fails.
- [x] N6 — AUR and Nix distribution.
  - Acceptance: goreleaser `aurs` (package `spmeta-bin`) and `nix` (NUR repository) sections that skip upload when their secret is missing; `release-check` passes; README install section and `docs/design/release.md` updated; user setup steps listed.
- [ ] N7 — README demo GIF.
  - Acceptance: a VHS tape in `docs/assets/demo.tape` and the rendered `docs/assets/demo.gif` showing `spmeta`, `--format json`, and `--watch`; user reviews the GIF before commit.
- [ ] N8 — Live trust checks with the user.
  - T10 manual rows (podcast, ad, local file, Spotify quit, Automation denied, Linux players), exit 5 (revoked access) and exit 6 (removed from the allowlist). Results recorded in this document and in `odd/tasks/spmeta-v1.md`.

## Order and routes

N1, N2, N3, N4, N5 (delegated writers, one at a time), N6 (delegated writer), N7 and N8 (need the user). Route per task is recorded under Progress.

## Checks

- Per code task: `go test ./...`, `go vet ./...`, `golangci-lint run`; Linux paths also run in the `golang` container (`docker run --rm -v "$PWD":/src -w /src golang:1 go test ./...`).
- Release config: `goreleaser check` and `goreleaser release --snapshot --clean` (via the CI `release-check` job).
- Review: RDD is off (global). Per commit, `gentle-ai review assess` sets the verification tier.

## Delivery

Strategy: `single-pr` (standing decision). Forecast: roughly 1,500 to 2,000 authored lines. Branch: `feat/next-level`.

## Progress

- 2026-10-30: branch created; code mapped (explorer).
- N1 (delegated writer; risk high → independent verifier PASS): `config.Load` returns `TokenPath` with its error so doctor runs the real Web check. RED/GREEN `TestDoctorChecksWebWhenConfigIsInvalid`. Commit 54335f8.
- N2 (delegated writer; risk high → verifier PASS against Waybar source and wiki; two doc nits fixed by the parent): `--format waybar` with Pango escaping, `examples/` for Waybar, SketchyBar, tmux. Live output checked on macOS. Commit 76ed0fc.
- N3 (delegated writer; risk high → verifier PASS; three wording fixes by the parent): `internal/reference` drives help, completions, and `spmeta(1)`; `spmeta completion bash|zsh|fish`; `internal/docgen` runs as a goreleaser before hook into `dist-extras/`; archives, .deb, .rpm, and cask ship them (zsh path per packager). `goreleaser check` and snapshot pass in Docker; .deb and .rpm installs verified in Debian and Fedora containers. Help now lists `--watch` and `--interval`.
- N4 (delegated writer; risk high → verifier PASS, then two follow-ups by the same writer): `backend.Notifier` and `Coalesce`; watch waits can be woken (`Wake`, `MinWakeGap` 100 ms flood guard; Retry-After and backoff waits cannot be shortened; a closed channel falls back to polling); `Resolver.Changes` passes through to the local backend; MPRIS notifier on one persistent godbus connection (PropertiesChanged and NameOwnerChanged, signal-name filter for godbus unicast signals). Real-bus integration test in the golang container passes. No reconnect after a session-bus restart (documented).
- N6 (delegated writer; risk high → verifier PASS with empty secrets in a scratch non-snapshot release; three doc fixes by the parent): goreleaser `aurs` (`spmeta-bin`) and `nix` (`bkuberek/nur-packages`) gated by `skip_upload` on `AUR_KEY` and `NUR_GITHUB_TOKEN`; release.yml passes both secrets. Publish order in v2.19.0: GitHub Release, Nix, cask, AUR. User setup steps are in `docs/design/release.md#aur-and-nix-one-time-setup`.
- macOS notification probe: listener registers, but two runs (5 and 10 minutes) saw no events while Spotify was paused. Needs a live play/skip by the user before N5.

## Next step

N1.
