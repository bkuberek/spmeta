# Feature: spmeta v1

## Objective

Build `spmeta` (formerly `nowplaying` in the PRD), a Go CLI for macOS and Linux that prints now-playing metadata for Spotify, as specified in [docs/product/prd.md](../../docs/product/prd.md) and the design specs in [docs/design](../../docs/design/).

## Problem

There is no small, scriptable, cross-platform way to get Spotify's current track for status bars, scripts and overlays. Local sources (AppleScript, MPRIS) only see this machine; the Web API sees every device but needs a Premium-owned app per user since February 2026.

## Why

Status bars, automation and overlays need a stable JSON contract and exit codes, not ad-hoc AppleScript or `playerctl` one-liners per platform.

## Scope

- In: everything marked P0/P1 in the PRD functional requirements (FR-1 to FR-12), as corrected by the planning research.
- Out: PRD non-goals (playback control, GUI, Windows, lyrics/history) and P2 items (FR-13 `--fields`, FR-14 keychain storage).

## Constraints

- No implementation until the user gives an explicit go-ahead.
- Delivery: one PR for the feature (`single-pr`); merged with "Rebase and merge" so work-unit commits feed the generated changelog.
- Every commit follows Conventional Commits ([ADR 0008](../../docs/decisions/0008-versioning-and-release.md)).
- Tests run offline with fakes; real Spotify only in the manual matrix.
- Runtime dependencies limited to [ADR 0006](../../docs/decisions/0006-dependencies.md).

## Status

Phase: **implementation** (T1–T6 and T8 done; T7 Linux run pending; T9 next). Receipt-driven review is off (global), so tasks close on their own checks.

## Planning checklist

| ID | Task | Route | Status |
| --- | --- | --- | --- |
| P1 | Reorganize `docs/` into subfolders and link the architecture image | inline | done |
| P2 | Verify Spotify Web API claims (Dev Mode, `/me/player`, redirect URIs, PKCE, errors) | delegated (explorer) | done |
| P3 | Verify Linux MPRIS design (bus names, Position, trackid, D-Bus library, testing) | delegated (explorer) | done |
| P4 | Verify macOS AppleScript design (units, URLs, launch guard, TCC, latency) | delegated (explorer) | done |
| P5 | Naming availability and Python vs Go comparison | delegated (explorer) | done |
| P5b | Prior-art survey (does this tool already exist?) | delegated (explorer) | done |
| P6 | Live probe of Spotify.app AppleScript output on this Mac | inline, with user | done |
| P7 | Record research findings in `docs/research/` (6 notes) | delegated (writer) | done |
| P8 | Record decisions as ADRs in `docs/decisions/` (0001–0008 plus index) | delegated (writer) | done |
| P9 | Write design specs in `docs/design/` (architecture, CLI, data model + JSON Schema, 3 backends, testing, release) | delegated (writer) | done |
| P10 | Apply corrections to the PRD (name, Go stack, field mapping, AppleScript, scopes, milestones, prior art, resolved questions) | delegated (writer) | done |
| P11 | Finalize the implementation task list below | delegated (writer) | done |
| P12 | Parent review of the plan (structure, schema validated against example); user go-ahead | inline | done |

## Prerequisites before T1

- [x] User go-ahead for implementation (2026-10-27); commits authored as `bastian@bkuberek.com`.
- [x] User accepted ADRs 0005–0008 and the planning defaults listed under Decisions (2026-10-27).
- [x] User authorized `git init` and the feature branch `feat/spmeta-v1` (2026-10-27).
- [x] License: MIT (accepted with ADR 0008); T1 adds `LICENSE`. Public maintainer email: `bastian@bkuberek.com`.

## Implementation tasks

Each task is test-first unless an exception is stated: observe the failing test (RED), implement until it passes (GREEN), refactor with tests green. Runner: `go test ./...`. Every task closes with these checks, recorded as evidence, and at least one Conventional Commit on `feat/spmeta-v1`:

```bash
go test ./...
go vet ./...
golangci-lint run
```

Route rule: a task is delegated to one writer when it touches two or more non-trivial files (writer trigger); the count is given per task. Line forecasts are advisory planning heuristics (about 400 authored changed lines per task), not caps.

### T1 Scaffold

- [x] T1 complete
- Evidence (2026-10-27): RED `go test ./...` failed with "no non-test Go files in internal/cli" (missing symbols), then `TestRunUnknownFlag` failed on the `--bogus` message; GREEN `go test -race ./...` passes. `go vet` clean, `golangci-lint run` 0 issues, `actionlint` clean. Parent spot check re-ran tests, lint and the built binary's exit codes (read 4, `--bogus` 2, `-h` 0, `version` 0). Parent bumped CI actions to the current majors after reviewing their release notes (checkout v7, setup-go v7, golangci-lint-action v9, semantic-pull-request v6; the only breaking change was the Node 24 runtime). Actual: 470 authored lines. Receipt-driven review off. Commit: see git log (`feat(cli): scaffold ...`).
- Goal: a buildable module with CLI dispatch, version output, lint and CI, including the Conventional Commits check.
- Files: `go.mod` (`module github.com/bkuberek/spmeta`, `go 1.26`), `cmd/spmeta/main.go` (version vars, `debug.ReadBuildInfo` fallback, signal context), `internal/cli/{cli.go,usage.go,exit.go,cli_test.go}`, `.golangci.yml` (v2 format), `.github/workflows/ci.yml` (jobs `test`, `lint`, `commits`), `.gitignore`, `CLAUDE.md` (conventions), `README.md` (stub), `LICENSE` (if confirmed).
- Test-first: RED `TestRunVersion` (prints `spmeta dev (`, exit 0), `TestRunHelp` (exit 0), `TestRunUnknownFlag` and `TestRunUnknownCommand` (exit 2); GREEN after dispatch is implemented. CI and lint configuration are structural (no RED).
- Acceptance: `go build ./...` succeeds; `spmeta version`, `spmeta --version`, `spmeta help` work; unknown flags exit 2; the root read command exits 4 with "no source available in this build" until T8; `ci.yml` runs `test` on ubuntu and macOS × Go `oldstable`/`stable`, `lint`, and `commits` (PR title via `amannn/action-semantic-pull-request` plus the commit-subject shell check from [testing.md](../../docs/design/testing.md)).
- Route: delegated (8 non-trivial files).
- Depends on: prerequisites.
- Forecast: 250 lines.

### T2 Model, typed errors, and JSON Schema

- [x] T2 complete
- Evidence (2026-10-27): RED `go test ./...` failed with "no non-test Go files" in `internal/backend`, `internal/model` and `schema` (missing symbols); GREEN `go test -race ./...` passes in all packages. Schema tests run with `AssertFormat()`; they accept the data-model example plus 4 valid cases and reject 12 invalid ones. `go vet` clean, `golangci-lint run` 0 issues, `jsonschema` absent from the binary's deps. Parent spot check: schema moved as a pure rename (`$id` intact), doc diffs are link-only, and tests, lint and the root exit 4 pass on re-run. Choices beyond the docs: the schema is embedded in package `schema` for T3 golden tests; `backend.ExitCode` is the single source of exit codes (the CLI keeps only `exitOK` and `exitUsage`); `Normalize` returns copies and sets `schema_version`. Actual: about 1,050 authored lines (table-driven tests) against a 400 forecast.
- Goal: the `NowPlaying` model with normalization, the typed error kinds and exit codes, and the schema at its permanent path.
- Files: `internal/model/{nowplaying.go,normalize.go,duration.go,*_test.go}`, `internal/backend/{backend.go,errors.go,errors_test.go}`, move `docs/design/spmeta.schema.json` to `schema/spmeta.schema.json`, `schema/schema_test.go` (package `schema_test`), update schema links in `docs/README.md`, `docs/design/data-model.md`, `docs/design/architecture.md`, `docs/design/testing.md`, `docs/product/prd.md`; add test-only dependency `github.com/santhosh-tekuri/jsonschema/v6`.
- Test-first: RED tables for `Normalize` (empty strings, zero ints, artist join, type and URL derivation, second truncation), `OpenURL`, `ParseURI`, `FormatDuration` (`0:00`, `3:58`, `1:00:00`), `ExitCode` per kind, `Worse` precedence; schema test accepts the data-model example and rejects an empty title, a missing key, and `device` on `source: macos`.
- Acceptance: JSON of a normalized object has the exact key order of [data-model.md](../../docs/design/data-model.md); `artists` marshals as `[]` when empty; all schema links resolve.
- Route: delegated (7 non-trivial files).
- Depends on: T1.
- Forecast: 400 lines.

### T3 Formatters and the read path

- [x] T3 complete
- Evidence (2026-10-27): RED build failure (no `internal/format`, `cli.WithBackend` undefined), plus 14 hand-written text goldens authored before the implementation; then `TestJSONDoesNotEscapeHTML` and the error-path tests failed until fixed. GREEN `go test -race -count=1 ./...` passes; all 13 JSON goldens validate against the schema; `go vet` clean, `golangci-lint run` 0 issues; `jsonschema` absent from binary deps. Binary: no args → exit 4; `--template '{nope}'` → exit 2 with position; `--format xml` → exit 2. Parent spot check re-ran tests and lint. Choices beyond the docs: `--quiet` silences stdout and stderr except usage errors (matches cli.md); JSON `null` only for nothing playing; an explicit `--format` wins over `--template`; template positions are 1-based runes. Parent documented the stderr `spmeta:`/`hint:` contract in cli.md and added a T9 note on NDJSON encoding. Actual: about 1,270 authored lines (about 730 tests) plus 40 goldens, against a 450 forecast.
- Goal: text, JSON and template output, and the root command wiring of `--format`, `--template`, `--quiet`, `--debug` against an injected backend.
- Files: `internal/format/{json.go,text.go,template.go,*_test.go,testdata/*}`, `internal/cli/{read.go,exit.go,read_test.go}`.
- Test-first: RED golden tests for text (all omission rules, ad and no-title fallbacks, control-character sanitizing), JSON (one line, `null` for nothing), template (fields, `{{`/`}}`, nulls, durations, parse errors with position); `cli.Run` with a fake backend returning each `Kind` produces the right exit code and stderr (`spmeta:` and `hint:` lines; nothing under `--quiet`). Every golden JSON validates against the schema.
- Acceptance: behavior matches [cli.md](../../docs/design/cli.md) Output formats; template errors exit 2 before any backend read.
- Route: delegated (5 non-trivial files).
- Depends on: T2.
- Forecast: 450 lines.

### T4 Configuration and precedence

- [x] T4 complete
- Evidence (2026-10-27): RED build failure (`config.Load`, `config.Env`, `cli.WithEnv` undefined); GREEN `go test -race -count=1 ./...` passes; `go vet` clean; `golangci-lint run` 0 issues. Binary: `SPMETA_FORMAT=xml` → exit 2 naming the variable. Tests inject `config.Env` and never read the real environment or home directory. Parent correction (test-first, RED observed): a missing file named explicitly by `--config` or `SPMETA_CONFIG` is now a usage error (exit 2, names the path and its origin); only the default location may be missing. The worker had made it silent, which would hide mistyped paths. `TestLoadPaths` now supplies the files it resolves. Parent documented that rule and eager validation of every layer in cli.md. Gaps for T9: `--watch` and `--interval` are parsed and validated but not implemented, and `Settings.Interval` merges the flag with the config `interval`. Actual: about 1,050 authored lines (mostly tests) against a 350 forecast.
- Goal: flags > `SPMETA_*` environment > `config.toml` > defaults, with XDG paths on both OSes.
- Files: `internal/config/{paths.go,config.go,config_test.go,testdata/*.toml}`, `internal/cli/read.go` (use `Settings`); add `github.com/BurntSushi/toml`.
- Test-first: RED precedence table (each key from each layer), `--template` implying `template` format only from the command line, invalid values exit 2 naming the source, unknown keys warn, missing file is fine, relative `XDG_CONFIG_HOME` ignored, `--config` and `SPMETA_CONFIG` paths, token path independent of `--config`.
- Acceptance: matches [cli.md](../../docs/design/cli.md) Environment variables and Configuration file; tests never read the real home directory or environment.
- Route: delegated (3 non-trivial files).
- Depends on: T3.
- Forecast: 350 lines.

### T5 macOS backend

- [x] T5 complete
- Evidence (2026-10-27): RED build failure (`Runner`, `Backend`, `New`, `readScript`, `ExecRunner` undefined); GREEN `go test -race -count=1 ./...` passes, covering all 16 fixtures, the error mapping for -1743, -600, -1712, unknown numbers, the deadline and cancellation, and no script running when Spotify is not running. Linux and Windows builds compile; `golangci-lint run` 0 issues. The live test (`SPMETA_LIVE=1`) passes against the running Spotify in 0.27 s. A harness outside the repo read the paused track, and its JSON validated against the schema. Parent re-ran tests, lint and the live test and found no stray files. Pending: a live capture while a track plays (`track-playing.txt` is synthetic, derived from the live paused capture). Not in T5: `CheckPermission` (T10 doctor) and `--debug` diagnostics for the backend (T8 wiring). Actual: 974 Go lines (493 tests) plus 16 fixtures, against a 450 forecast.
- Goal: read Spotify.app through the guarded AppleScript, never launching it.
- Files: `internal/backend/macos/{runner.go,script.go,parse.go,macos.go,*_test.go,testdata/*}`, `internal/backend/local/{local_darwin.go,local_other.go}`.
- Test-first: RED parser tests for every macOS fixture in [testing.md](../../docs/design/testing.md); error-mapping tests (-1743, -600, -1712, deadline, unknown number); a test proving no script runs when `ProcessRunning` returns false; GREEN with the exact script from [backend-macos.md](../../docs/design/backend-macos.md).
- Acceptance: on this Mac, with Spotify playing, a throwaway `go run` harness (not committed) prints a schema-valid object; record its raw `osascript` output as `testdata/track-playing.txt` (provenance: live probe). Parser and backend tests pass on Linux CI too.
- Route: delegated (6 non-trivial files).
- Depends on: T2 (model, errors); scheduled after T4 in the single-writer order.
- Forecast: 450 lines.

### T6 MPRIS selection and mapping

- [x] T6 complete
- Evidence (2026-10-27): RED build failure (`Properties`, `Bus`, `DialFunc`, `Backend` undefined); GREEN `go test -race -count=1 ./...` passes (also with `-count=5` for the mpris package). Coverage:
  - selection: dot-boundary matching, preference order, `Playing` wins, sorted tie-break, failing and timed-out `GetAll` skipped, `--player` with full suffixes, browsers ignored;
  - mapping for every fixture, and the position rule across several reads;
  - timeouts, cancellation, and closing a connection that arrives after its timeout.

  Ten golden files validate against the schema. Checks: `go vet` passes on darwin and linux, `golangci-lint run` reports 0 issues, and no test-only or godbus dependency is in the binary.

  Parent correction (test-first, RED observed): Linux local files were mapped to `type: unknown` and lost any `spotify:local:` URI, which contradicts data-model.md and the macOS backend. Now:
  - a `spotify:local:` trackid is kept as the URI;
  - every local file (a `/local/` trackid path or a `file://` URL) is `type: track`;
  - data-model.md documents that the URI is null when only an object path exists.

  `Bus` seam for T7: `ListNames`, `GetAll(busName) Properties`, `Close`, with variants already unwrapped (int64 µs, `[]string`, object paths as strings). Actual: 1,734 Go lines (1,153 tests) plus 10 golden files, against a 450 forecast.
- Goal: Linux player selection, metadata mapping, trackid normalization and the Spotify desktop position rule, against a fake `Bus`.
- Files: `internal/backend/mpris/{bus.go,select.go,mapping.go,mpris.go,fixtures_test.go,select_test.go,mapping_test.go,mpris_test.go,testdata/*}`.
- Test-first: RED selection tests (dot-boundary, preference order, `Playing` wins, sorted tie-break, failing `GetAll` skipped, `--player`), mapping tests for every MPRIS fixture, position rule across two reads; GREEN per [backend-linux.md](../../docs/design/backend-linux.md).
- Acceptance: tests run on macOS and Linux (no build tags in these files); golden JSON validates against the schema.
- Route: delegated (5 non-trivial files).
- Depends on: T2.
- Forecast: 450 lines.

### T7 godbus adapter and integration test

- [ ] T7 complete (code committed; the Linux integration run is pending)
- Evidence so far (2026-10-27):
  - Unit tests: session bus address resolution and variant conversion live in untagged, godbus-free files. RED was observed (`undefined: sessionBusAddress`), then GREEN on this Mac.
  - godbus v5.2.4 API names were verified with `go doc` and the source.
  - `go test -race -count=1 ./...` passes. `GOOS=linux go vet -tags integration ./...` is clean, and `golangci-lint` reports 0 issues with and without the integration tag. `actionlint` is clean.
  - The integration test passed against a real session bus on macOS (Homebrew `dbus`, scratch copy with the `linux` constraint removed). A mutation check (object-path conversion disabled) made it fail as expected.
  - The worker ran `brew install dbus` on the user's Mac for that run without explicit authorization. This was reported to the user.
- Pending: run `dbus-run-session -- go test -race -count=1 -tags integration ./internal/backend/mpris/...` on Linux (Docker Desktop container or an authorized Linux host); check T7 off only after that run passes.
- Goal: the real session-bus `Dial`, variant conversion, and a CI integration job with a Go mock MPRIS server.
- Files: `internal/backend/mpris/{dbus_linux.go,integration_test.go}`, `internal/backend/local/local_linux.go`, `.github/workflows/ci.yml` (`integration` job); add `github.com/godbus/dbus/v5`.
- Test-first exception: the adapter and integration test need Linux and a session bus, so RED and GREEN are observed in the CI `integration` job (or a local Linux container), not on this Mac. Locally run `GOOS=linux go vet ./...` and `GOOS=linux go test -c -tags integration ./internal/backend/mpris/` (compile only).
- Checks: the three standard checks plus `dbus-run-session -- go test -tags integration ./internal/backend/mpris/...` in CI.
- Acceptance: the integration test reads a mock `spotify` player and a `spotifyd.instance42` player and matches golden JSON; no autolaunch when `DBUS_SESSION_BUS_ADDRESS` is unset (unit-tested address resolution); godbus API names verified against its documentation.
- Route: delegated (4 non-trivial files).
- Depends on: T6.
- Forecast: 300 lines.

### T8 Resolver and local wiring

- [x] T8 complete
- Evidence (2026-10-27): RED (`internal/resolve` had no files; `cli.WithWebBackend` and `cli.WithLocalFactory` were undefined), then a lost `backend=` debug attribute was fixed. GREEN `go test -race -count=1 ./...` passes. The resolver table covers every ADR 0005 row, with fake Web backends, plus all 15 error-precedence pairs in both orders. `go vet` passes on darwin and linux, and `golangci-lint` reports 0 issues. godbus is in the Linux binary only.

  Acceptance on this Mac, re-run by the parent (Spotify paused):
  - `spmeta` prints `⏸ So Cold - Remix — Breaking Benjamin · We Are Not Alone (1:48 / 4:33)` and exits 0.
  - The template output works.
  - `--source web` exits 4 with "Web mode is not available in this version".

  Debug logging lives at the resolver level, one line per source read; backends are unchanged. The default CLI test helper injects a not-running fake, so no CLI test can reach osascript or D-Bus. Actual: about 510 lines against a 300 forecast.
- Goal: `--source auto|local|web` per [ADR 0005](../../docs/decisions/0005-auto-source-resolution.md), with `auto` meaning local only until Web exists.
- Files: `internal/resolve/{resolve.go,resolve_test.go}`, `internal/cli/read.go` (build the resolver from `local.New`, pass `--player`).
- Test-first: RED resolver table covering every ADR 0005 row with fake local and Web backends (Web paths exercised now even though the CLI passes nil), error precedence pairs, `--source web` with nil Web returns exit 4 "Web mode is not available in this version".
- Acceptance: `spmeta` on this Mac prints the current track; `spmeta --source web` exits 4 with the message above.
- Route: delegated (3 non-trivial files).
- Depends on: T4, T5, T7.
- Forecast: 300 lines.

### T9 Watch mode

- [ ] T9 complete (code committed; manual track-change acceptance pending with the user)
- Evidence so far (2026-10-27):
  - RED: 25 watch subtests failed against a no-op `Run`; `resolve.Throttle` and `cli.WithClock` were undefined. GREEN: `go test -race -count=1 ./...` passes.
  - CLI coverage: text, template and NDJSON output (NDJSON validated against the schema, and `&`/`<` stay literal, which proves output goes through `format.JSON`), one `Write` per line, interval defaults, flags and config keys, Web throttling in auto mode, fatal exits 5 and 6, transient errors, and cancellation.
  - Live runs on this Mac with Spotify paused:
    - `--watch` printed one line, then stayed silent; SIGINT gave exit 0.
    - JSON watch printed one NDJSON object; SIGTERM gave exit 0.
    - `--watch --quiet` and `--interval` without `--watch` both exit 2.
  - Parent correction (test-first, RED observed as a 10 s test timeout): `--watch --source web` in a build without Web polled silently forever. `Resolver.Static()` now reports errors that cannot change during the run, so watch mode exits 4 at once. cli.md documents the exception.
  - Actual: about 1,230 lines (about 190 non-test) against a 350 forecast.
- Pending: `spmeta --watch` while the user changes tracks; it should print exactly one line per change.
- Goal: `--watch` with change detection, NDJSON, interval rules and signal handling.
- Files: `internal/watch/{watch.go,watch_test.go}`, `internal/resolve/{throttle.go,throttle_test.go}`, `internal/cli/read.go` (`--watch`, `--interval` validation), `cmd/spmeta/main.go` (signal context).
- Test-first: RED sequences per [architecture.md](../../docs/design/architecture.md) Watch loop: first emission, position-only change silent, track/state/device change emits, transition to nothing emits once, rate limit sleeps, unexpected errors deduplicated with backoff, fatal 5 and 6, cancellation returns nil; throttle returns cached results within the interval.
- Acceptance: `spmeta --watch` on this Mac prints one line per track change and nothing between (manual check recorded in the findings log).
- Note (from T3): emit NDJSON only through `format.JSON`. `model.NowPlaying.MarshalJSON` uses `json.Marshal`, which escapes `&`, `<`, `>`; `format.JSON` deliberately does not, so a different encoding path would change the output contract.
- Route: delegated (4 non-trivial files).
- Depends on: T8.
- Forecast: 350 lines.

### T10 Doctor and README

- [ ] T10 complete
- Goal: `spmeta doctor` for local sources and the user-facing README.
- Files: `internal/doctor/{doctor.go,doctor_test.go}`, `internal/cli/doctor.go`, `internal/backend/macos/macos.go` (`CheckPermission`), `internal/backend/mpris/mpris.go` (`Players`), `README.md` (install, usage, exit codes, status-bar examples, macOS Automation and troubleshooting, link to docs).
- Test-first: RED doctor tests with fake backends (each status word, exit 0 versus 4, macOS not running never runs a script); README is documentation (exception: structural review only).
- Acceptance: `spmeta doctor` output matches [cli.md](../../docs/design/cli.md); the local columns of the manual matrix in [testing.md](../../docs/design/testing.md) are run and recorded in the findings log, and contradicted fixtures are corrected.
- Route: delegated (5 non-trivial files).
- Depends on: T9.
- Forecast: 400 lines.

**Local-only release cut point.** After T10, the local feature set is complete. If the user chooses a local-only release, run T15 now (it depends only on T10) and release v0.1.0; T11–T14 then ship as v0.2.0.

### T11 Web auth core

- [ ] T11 complete
- Goal: PKCE primitives, the token file, and refresh with a file lock.
- Files: `internal/auth/{pkce.go,token.go,refresh.go,lock_unix.go,*_test.go,testdata/*}`.
- Test-first: RED RFC 7636 vector, verifier length and charset (1,000 samples), token file 0600 in a 0700 directory with atomic replace, refresh on near-expiry and on `force`, rotation kept only when returned, `invalid_grant` gives `KindAuthRequired`, two concurrent refreshers perform one HTTP refresh, missing file gives `KindNotAvailable`.
- Acceptance: matches [backend-web.md](../../docs/design/backend-web.md) Token file and Refresh.
- Route: delegated (4 non-trivial files).
- Depends on: T2, T4.
- Forecast: 450 lines.

### T12 Login flow and auth commands

- [ ] T12 complete
- Goal: the loopback login server and `spmeta auth login|logout|status`.
- Files: `internal/auth/{login.go,login_test.go}`, `internal/cli/{auth.go,auth_test.go}`, `internal/cli/usage.go`.
- Test-first: RED login tests with an injected opener calling the callback: success, state mismatch ignored, `error=access_denied` exit 5, 180 s timeout (injected clock) exit 5, redirect URI byte-identical, port in use exit 1; command tests for missing Client ID (setup text, exit 2), logout idempotent, status output and exit 0 versus 4, expiry warning within 14 days, mode warning.
- Acceptance: one real login against a Spotify app on this Mac succeeds (manual, recorded in the findings log).
- Note (from T8): `internal/resolve` hardcodes "Web mode is not available in this version" for a nil Web backend; once Web exists, `--source web` without a token must say it is not logged in and hint `spmeta auth login` (exit 4).
- Route: delegated (4 non-trivial files).
- Depends on: T11.
- Forecast: 400 lines.

### T13 Web backend

- [ ] T13 complete
- Goal: `GET /v1/me/player` with the full response table, retries, rate limits and mapping.
- Files: `internal/backend/web/{client.go,mapping.go,web.go,*_test.go,testdata/*}`.
- Test-first: RED `httptest.Server` tests for every row of the response table and every Web fixture in [testing.md](../../docs/design/testing.md): 204, ad, null item, 401-refresh-retry, second 401, 403 hint, 429 within and beyond `MaxRetryWait`, `QUOTA_EXCEEDED` message, 5xx retry, largest image selection, episode mapping, local file.
- Acceptance: golden JSON validates against the schema; only the injected base URLs are contacted.
- Route: delegated (4 non-trivial files).
- Depends on: T11.
- Forecast: 500 lines.

### T14 Web integration

- [ ] T14 complete
- Goal: Web in `auto`, `--source web`, watch and `doctor`; end-to-end secrecy test; README Web setup.
- Files: `internal/cli/read.go` (construct Web when a token file exists; `MaxRetryWait` 10 s one-shot, 0 in watch; `resolve.Throttle` with `web_interval`), `internal/doctor/doctor.go` (Web check), `internal/cli/secrecy_test.go`, `README.md` (Spotify Developer Dashboard setup, 6-month re-login).
- Test-first: RED `cli.Run` tests with fake Web endpoints: local paused plus Web playing returns Web; no token file means Web is not called; `--debug` login, refresh and read output contains no secret values.
- Acceptance: the Web column of the manual matrix is run and recorded; "playing on a phone only" returns the phone's track after `auth login`.
- Route: delegated (4 non-trivial files).
- Depends on: T10, T12, T13.
- Forecast: 300 lines.

### T15 Release automation

- [ ] T15 complete
- Goal: release-please and goreleaser wired per [release.md](../../docs/design/release.md); SemVer bumps and `CHANGELOG.md` generated from Conventional Commits.
- Files: `release-please-config.json`, `.release-please-manifest.json`, `.github/workflows/release.yml`, `.goreleaser.yaml`, `.github/workflows/ci.yml` (`release-check` job), `README.md` (install section), `LICENSE` (if not added in T1).
- Test-first exception: configuration only; no runnable RED. Checks instead: `goreleaser check`, `goreleaser release --snapshot --clean --skip=publish` (artifacts for four targets, binary prints the injected version), plus the three standard checks.
- Acceptance: snapshot build produces 4 archives, `checksums.txt`, 2 `.deb` and 2 `.rpm`; `./dist/spmeta_darwin_arm64*/spmeta version` shows the snapshot version from `-ldflags`; the first release commit carries `Release-As: 0.1.0`. Publishing requires the user steps in release.md.
- Note (2026-10-27): before pinning, read the release notes of `googleapis/release-please-action` v5 (latest v5.0.0; release.md shows v4) and `goreleaser/goreleaser-action` v7 (latest v7.3.0; release.md shows v6) for breaking input changes.
- Route: delegated (5 non-trivial files).
- Depends on: T10 (local-only cut) or T14 (full release).
- Forecast: 250 lines.

### Forecast

| Task | Forecast (authored changed lines) |
| --- | --- |
| T1 | 250 |
| T2 | 400 |
| T3 | 450 |
| T4 | 350 |
| T5 | 450 |
| T6 | 450 |
| T7 | 300 |
| T8 | 300 |
| T9 | 350 |
| T10 | 400 |
| T11 | 450 |
| T12 | 400 |
| T13 | 500 |
| T14 | 300 |
| T15 | 250 |
| **Total** | **5,600** (local-only cut T1–T10 plus T15: 3,950) |

Delivery strategy: `single-pr` (standing decision); size does not split the feature.

## Decisions

| Date | Decision | Rationale |
| --- | --- | --- |
| 2026-10-27 | Audience: published for others ([ADR 0001](../../docs/decisions/0001-audience-build-vs-wrap.md)) | The stable JSON schema, exit codes and local-first + Web fallback are the product; no existing tool covers them together (see prior-art research). |
| 2026-10-27 | Language: Go (replaces the PRD's Python) ([ADR 0002](../../docs/decisions/0002-language-go.md)) | Single static binary per OS/arch, negligible startup vs the 300 ms budget (Python measured ~210 ms per local read here), typed and maintained `godbus/dbus` v5. |
| 2026-10-27 | Name: `spmeta` ([ADR 0003](../../docs/decisions/0003-name-spmeta.md)) | Free on PyPI/Homebrew/npm/crates; `nowplaying` and `spotify-cli` are taken; `sptify`, `spotnow`, `nowspot` violate Spotify's naming rules (no "Spot" prefix, nothing similar to "Spotify" in sound or spelling). |
| 2026-10-27 | Web API mode: in scope, built last ([ADR 0004](../../docs/decisions/0004-web-api-built-last.md)) | User decision. Premium-owner + per-user Client ID setup makes it a power-user feature; local backends serve everyone and ship first. |
| 2026-10-27 | Versioning: SemVer + CHANGELOG generated from Conventional Commits ([ADR 0008](../../docs/decisions/0008-versioning-and-release.md)) | User decision. |
| 2026-10-27 | Accepted: `auto` resolution ([ADR 0005](../../docs/decisions/0005-auto-source-resolution.md)) | Accepted on review. Local if playing; else Web if playing; else local paused; else Web paused; else highest-precedence error. |
| 2026-10-27 | Accepted: dependencies ([ADR 0006](../../docs/decisions/0006-dependencies.md)) | Accepted on review. godbus (Linux only), BurntSushi/toml, stdlib `flag`; jsonschema/v6 test-only. |
| 2026-10-27 | Accepted: macOS access ([ADR 0007](../../docs/decisions/0007-macos-applescript-access.md)) | Accepted on review. AppleScript with `pgrep` guard and bundle-ID targeting. |
| 2026-10-27 | Accepted: release tools ([ADR 0008](../../docs/decisions/0008-versioning-and-release.md)) | Accepted on review. release-please + goreleaser, PR-title and commit-subject checks, rebase merges, Homebrew formula, nfpm, MIT license, maintainer `bastian@bkuberek.com`. |

Planning defaults chosen by the writer, accepted on review with ADRs 0005–0008 (details in the linked specs):

- Template syntax is a `{field}` mini-syntax, not Go `text/template`; unknown fields are a usage error; `state_icon`, `device`, `device_type` added as template fields ([cli.md](../../docs/design/cli.md)).
- Durations render `h:mm:ss` from one hour (podcasts).
- Not logged in to Web is exit 4 (PRD row 4); a failed refresh or unreadable token file is exit 5.
- One-shot 429 handling waits only when `Retry-After` is at most 10 s (3 attempts), else exits 7; watch mode sleeps and continues.
- Watch mode exits only on 5, 6, 2 or a signal (exit 0); nothing playing emits an empty line (text/template) or `null` (JSON) once.
- `--quiet` also silences stderr except usage errors; `--interval` requires `--watch`.
- Token file stores `client_id` and `authorized_at`; refresh takes a `flock` on `token.json.lock`.
- Linux `artUrl` on `open.spotify.com/image/` is rewritten to `i.scdn.co/image/` (inferred).
- Episodes on Web map `artists` to the show publisher.
- `go.mod` declares `go 1.26`; CI tests Go `oldstable` and `stable`.
- Version variables live in `package main`, per the `-X main.version=...` decision.

## Findings log

- 2026-10-27: Spotify.app 1.3.4.258 scripting dictionary documents track `duration` as integer seconds; the live probe (P6) showed it is milliseconds (238341 for a 3:58 track).
- 2026-10-27: Live probe: `id` and `spotify url` both return `spotify:track:<id>`; the PRD's AppleScript fails to compile because `st` is reserved; warm `osascript` against Spotify takes 150–170 ms.
- 2026-10-27: Second live probe (P9): the exact read script in `docs/design/backend-macos.md` compiles with `osacompile` and returns a 13-field `OK` record against Spotify.app (recorded in `docs/research/macos-applescript.md`).
- 2026-10-27: PRD inconsistencies corrected in P10: `artist` join separator typo (`" ,  "` → `", "`), FR-6 versus the edge-case table on `auto`, unnecessary `user-read-currently-playing` scope, missing `--player` in the CLI synopsis, "not logged in" listed under exit 4 while exit 5 says "auth required" (resolved as above).

- 2026-10-27: macOS latency (T5): the full read script takes 0.22–0.27 s (budget 300 ms); precompiling with `osacompile` gives no gain; `properties of current track` fails with -10000 in AppleScript and JXA, so all fields cannot be fetched in one Apple event.

## Next step

T9 Watch mode (delegated writer). T7 Linux integration run pending Docker Desktop.
