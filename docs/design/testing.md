# Testing

All automated tests run offline with fakes; real Spotify is touched only in the manual matrix. This document defines the test strategy, the fixture catalogue with the provenance of each fixture, CI, the manual matrix, and acceptance criteria mapped to the PRD's functional requirements.

## Strategy

Every implementation task is test-first: write the failing test (RED), implement until it passes (GREEN), then refactor with the tests green. The runner is `go test ./...`.

| Layer | What is tested | Technique |
| --- | --- | --- |
| Model | `Normalize`, `ParseURI`, `OpenURL`, `FormatDuration` | Table-driven unit tests |
| Formatters | Text, JSON, template rendering; template parse errors; control-character sanitizing | Golden files; every golden JSON validated against the schema |
| Configuration | Precedence of flags, environment, file, defaults; invalid values; unknown keys | Table-driven, with injected `env` and `readFile` |
| macOS backend | Parser and error mapping | Fake `Runner` returning fixture stdout, stderr, and errors |
| MPRIS backend | Selection, mapping, track ID normalization, position rule | Fake `Bus` returning fixture `Properties` |
| MPRIS adapter | godbus calls and variant conversion | Integration test under `dbus-run-session` (Linux CI only) |
| Web auth | PKCE vectors, login server, token file, refresh, lock | `httptest.Server`, injected clock and opener, temporary directories |
| Web backend | Response table, retries, rate limits, mapping | `httptest.Server` serving fixtures |
| Resolver | ADR 0005 algorithm and error precedence | Fake backends, table-driven |
| Watch | Change detection, emission, error handling, signals | Fake reader sequences, injected `Sleep` |
| CLI | Flag parsing, usage errors, exit codes, `--quiet`, `--debug` secrecy | `cli.Run` with injected stdout, stderr, environment, and backends |

Conventions:

- Table-driven tests with named cases (`t.Run(tc.name, ...)`).
- Golden files live in each package's `testdata/` directory. `go test ./... -update` rewrites them; a reviewer checks the diff.
- Tests never read the real home directory, environment, session bus, or network. Packages take these as parameters.
- `t.Parallel()` where tests share no state.
- Schema validation uses `github.com/santhosh-tekuri/jsonschema/v6` against `schema/spmeta.schema.json` (test-only dependency, [ADR 0006](../decisions/0006-dependencies.md)).

## Fixture catalogue

Provenance values: **live probe** (recorded from Spotify.app on this Mac), **research** (shape documented in the client's source or Spotify's documentation), **synthetic** (constructed; shape unverified).

### macOS (`internal/backend/macos/testdata/`)

Stdout fixtures use the read script's output format with real U+001F separators.

| Fixture | Represents | Provenance |
| --- | --- | --- |
| `track-playing.txt` | `track-paused.txt` with the state changed to `playing`, all fields | Synthetic, derived from the live paused capture; replace with a live capture while Spotify plays (pending) |
| `track-playing-2.txt` | Second probe record, Lofuu | Live probe |
| `track-paused.txt` | "So Cold - Remix" by Breaking Benjamin, `paused`, all fields | Live probe (raw `osascript` output, Spotify 1.3.4.258, October 27, 2026) |
| `stopped.txt` | `STOPPED` | Synthetic |
| `no-track.txt` | `NO_TRACK` | Synthetic |
| `not-running.txt` | `NOT_RUNNING` | Synthetic |
| `episode.txt` | `spotify:episode:<id>` | Synthetic; shape unverified |
| `ad.txt` | `spotify:ad:<id>`, empty artist and album | Synthetic; prefix unverified |
| `local-file.txt` | `spotify:local:Artist:Album:Title:215`, empty artwork | Synthetic; shape unverified |
| `separators-in-title.txt` | Title with a tab, a pipe, and a newline | Synthetic |
| `missing-fields.txt` | Empty album, album artist, artwork, track and disc numbers | Synthetic |
| `bad-integer.txt` | Position `120,5` and duration `2.38341E+5` | Synthetic (locale and integer-overflow guard) |
| `wrong-field-count.txt` | `OK` record with 12 fields | Synthetic |
| `stderr-1743.txt` | `execution error: Not authorized to send Apple events to Spotify. (-1743)` | Synthetic; typical text, not captured live |
| `stderr-600.txt` | `execution error: Spotify got an error: Application isn't running. (-600)` | Synthetic |
| `stderr-1712.txt` | `execution error: Spotify got an error: AppleEvent timed out. (-1712)` | Synthetic |

### MPRIS (`internal/backend/mpris/testdata/` and `fixtures_test.go`)

`Properties` inputs are Go literals in `fixtures_test.go`, because value types matter (`int64` versus `uint64`, string versus list). Expected outputs are golden JSON files named in snake case (`spotify_desktop_playing.json`), produced by reading each fixture through a fake `Bus` and validated against the schema.

| Fixture | Represents | Provenance |
| --- | --- | --- |
| `spotifyDesktopPlaying` | Bus `spotify`, trackid `/com/spotify/track/<id>`, `Position` 0, `artUrl` on `open.spotify.com/image/` | Research (Gentoo wiki, community reports) |
| `spotifyDesktopStringTrackID` | trackid as the string `spotify:track:<id>` | Research (KDE report, unconfirmed) |
| `spotifyDesktopEpisode` | trackid `/com/spotify/episode/<id>` | Synthetic |
| `spotifyDesktopAd` | trackid `spotify:ad:<id>`, empty artist | Synthetic |
| `spotifyDesktopLocalFile` | trackid `/com/spotify/local/<hash>`, `xesam:url` `file:///...`: `uri` and `url` null, `type: unknown` | Synthetic; shape unverified |
| `spotifydPlaying` | Bus `spotifyd.instance1234`, trackid `/spotify/track/<id>`, nonzero `Position` | Research (spotifyd source) |
| `ncspotPlaying` | Bus `ncspot.instance99`, trackid `/org/ncspot/spotify/track/<id>`, `xesam:url` set | Research (ncspot source) |
| `spotifyPlayerNoTrackID` | Bus `spotify_player`, no trackid, `xesam:url` set | Research (spotify_player source) |
| `noTrack` | trackid `/org/mpris/MediaPlayer2/TrackList/NoTrack` | Research (MPRIS specification) |
| `paused`, `stoppedWithItem`, `stoppedEmpty` | `PlaybackStatus` variants | Synthetic |
| `emptyMetadata`, `missingMetadata`, `noTrackWithTitle`, missing fields | Nothing-playing and sparse shapes | Synthetic |
| `artistAsString`, `lengthAsUint64`, `lengthAsDouble` | Off-spec value types | Synthetic |
| Selection name lists | `[firefox.instance1, spotify]`; `[spotifyd.instance10, spotifyd.instance9 (Playing)]`; `[spotify (GetAll fails), spotifyd.instance1]`; `[spotifydx]`; `--player firefox` | Synthetic |

### Web (`internal/backend/web/testdata/` and `internal/auth/testdata/`)

| Fixture | Represents | Provenance |
| --- | --- | --- |
| `player-track.json` | 200, track playing, device, images with dimensions | Research (reference shape) |
| `player-track-paused.json` | 200, `is_playing: false` | Research |
| `player-episode.json` | 200, `currently_playing_type: episode`, show and publisher | Research |
| `player-ad.json` | 200, `currently_playing_type: ad`, `item: null` | Synthetic; observed, undocumented |
| `player-unknown-null-item.json` | 200, `currently_playing_type: unknown`, `item: null` | Synthetic |
| `player-local-file.json` | 200, `is_local: true`, empty `external_urls` and images | Synthetic |
| `player-images-no-dimensions.json` | Images with null width and height | Synthetic |
| `player-no-isrc.json` | Track without `external_ids` | Research (Development Mode field removals) |
| 204 response | Empty body | Synthetic; observed, undocumented |
| `error-401.json`, `error-403.json` | Web API error bodies | Research |
| `error-429-quota.json` | 429 body with `reason: QUOTA_EXCEEDED`, plus `Retry-After` header in the test | Research (July 2026 changelog) |
| `error-500.json` | Server error | Synthetic |
| `token-exchange.json` | Token response with refresh token | Research (PKCE tutorial) |
| `token-refresh-no-rotation.json` | Refresh response without `refresh_token` | Research (refresh tutorial) |
| `token-invalid-grant.json` | `{"error": "invalid_grant", ...}` | Research |

### Auth vectors

- RFC 7636 Appendix B: verifier `dBjftJeZ4CVP-mB92K27uhbUJU1p1r_wW1gFWFOEjXk` gives challenge `E9Melhoa2OwvFrEMTJguCHaoeK1t8URWbuGJSstw-cM`.
- Generated verifiers are 64 characters from `[A-Za-z0-9_-]`; 1,000 generated values are checked.

## Required automated cases

Beyond the fixtures above, these cases must exist:

- Text output for each fixture, including omitted parts and the ad and no-title fallbacks.
- Template: `{artist} - {title}`, `{{literal}}`, null fields, `{position}` and `{duration}` below and above one hour, unknown field, unclosed brace, stray brace.
- JSON: exact key order and key set per source; `null` output for nothing playing; every golden JSON validates against the schema.
- Token file written with mode 0600 inside a 0700 directory; atomic replace; concurrent refresh by two goroutines performs one HTTP refresh.
- Login: state mismatch ignored, `error=access_denied` returns exit 5, timeout returns exit 5, redirect URI byte-identical between authorize and exchange.
- Resolver: every row of the ADR 0005 table, including local paused with Web playing, local unavailable with Web paused, and each pair of errors for precedence.
- Watch: first emission, no emission on position change, emission on track change, state change, device change, and transition to nothing; rate-limit sleep; fatal 5 and 6; context cancellation returns nil.
- `--debug` secrecy: a full fake login, refresh, and read with `--debug`; no token, code, or verifier appears in stderr.
- Exit code for every `Kind` through `cli.Run`.

## CI

GitHub Actions workflow `.github/workflows/ci.yml`, created in task T1 and extended by later tasks:

| Job | Runs on | Steps |
| --- | --- | --- |
| `test` | Matrix: `ubuntu-latest`, `macos-latest` × Go `oldstable`, `stable` | `go vet ./...`, `go test -race ./...` |
| `lint` | `ubuntu-latest`, Go `stable` | `golangci-lint run` (golangci-lint v2, configured in `.golangci.yml`) |
| `integration` | `ubuntu-latest`, Go `stable` (added in T7) | `sudo apt-get install -y dbus-daemon`; `dbus-run-session -- go test -tags integration ./internal/backend/mpris/...` |
| `commits` | `ubuntu-latest`, pull requests only | `amannn/action-semantic-pull-request` checks the title; a shell step checks every commit subject in the pull request (see below) |
| `release-check` | `ubuntu-latest` (added in T15) | `goreleaser check`; `goreleaser release --snapshot --clean --skip=publish` |

`go.mod` declares `go 1.26` so `go install` works with the oldest supported Go release; the local toolchain is Go 1.27, and CI tests both through the `oldstable` and `stable` aliases of `actions/setup-go`.

The commit check ([ADR 0008](../decisions/0008-versioning-and-release.md)) runs with `fetch-depth: 0`:

```bash
pattern='^(feat|fix|docs|refactor|test|chore|ci|perf)(\([a-z0-9-]+\))?!?: .+'
bad=0
while IFS= read -r subject; do
  if ! printf '%s\n' "$subject" | grep -Eq "$pattern"; then
    echo "Not a Conventional Commit: $subject"
    bad=1
  fi
done < <(git log --format=%s "origin/${GITHUB_BASE_REF}..HEAD" --no-merges)
exit "$bad"
```

## Manual matrix

These scenarios need real Spotify clients. Each row is run before the release that ships the affected backend: local columns before the local-only cut (task T10), the Web column in task T14. Record outcomes, including raw output for every row marked "probe", in the feature document's findings log, and update the fixtures and [data-model.md](./data-model.md) when an observation contradicts a synthetic fixture.

| Scenario | macOS | Linux Spotify desktop | Linux spotifyd | Web |
| --- | --- | --- | --- | --- |
| Track playing | Required | Required | Required | Required |
| Paused | Required (probe) | Required | Required | Required |
| Nothing loaded after launch | Required (probe) | Required | Required | Required (204) |
| Podcast episode | Required (probe: `id` format, album, artist) | Required (probe: trackid) | Required | Required |
| Ad (free account) | Required (probe: `id` prefix) | Required (probe) | n/a | Required (probe: response shape) |
| Local file | Required (probe: `id` format) | Required | n/a | Required |
| Spotify quit: `spmeta` and `spmeta doctor` do not launch it | Required | Required | n/a | n/a |
| Automation denied: exit 6, record exact stderr for -1743 | Required (probe) | n/a | n/a | n/a |
| First run: read times out with the `doctor` hint; `doctor` waits for consent | Required | n/a | n/a | n/a |
| Spotify Connect: desktop app controlling another device | Required (probe: fields) | Required | n/a | Required |
| Hung app: `kill -STOP` the Spotify process, expect exit 1 within about 2 s, then `kill -CONT` | Required | Required | n/a | n/a |
| `Position` reported while playing (open question) | n/a | Required (probe) | Required | n/a |
| Playing on a phone only | n/a | n/a | n/a | Required |
| `--watch` prints one line per track change and nothing between | Required | Required | Required | Required |
| Access revoked in account settings: exit 5 with login hint | n/a | n/a | n/a | Required |
| Account not on the allowlist: exit 6 with both causes in the hint | n/a | n/a | n/a | Required |
| SketchyBar or launchd agent as the responsible process | Required | n/a | n/a | n/a |

## Acceptance criteria

Each functional requirement maps to automated tests and, where needed, manual rows.

| FR | Requirement | Automated evidence | Manual evidence |
| --- | --- | --- | --- |
| FR-1 | One line of text, then exit | `format` text golden tests; `cli.Run` exit 0 | Track playing |
| FR-2 | `--format json` matches the schema | Schema validation of all golden JSON for all three sources | Track playing, all columns |
| FR-3 | Template renders any field by name | Template tests including null fields and errors | none |
| FR-4 | macOS reads Spotify.app without launching it | Fake `Runner`: no script runs when `pgrep` reports not running | Spotify quit row |
| FR-5 | Linux reads MPRIS, preferring `spotify` | Selection tests; integration test | Linux columns |
| FR-6 | `--source auto|local|web` | Resolver table tests | Playing on a phone only |
| FR-7 | `auth login|logout|status` | Login server, token file, and status tests | Web login once |
| FR-8 | Web reads `/me/player` and refreshes on 401 | Response-table tests; 401-then-refresh test | Web column |
| FR-9 | Distinct exit codes | `cli.Run` test per `Kind` | Denied, quit, revoked rows |
| FR-10 | `--watch` emits on change; NDJSON in JSON mode | Watch tests | `--watch` row |
| FR-11 | `doctor` reports sources | Doctor tests with fake backends | First run row |
| FR-12 | Episodes: `type: episode`, show name as album | macOS, MPRIS, and Web episode fixtures | Podcast episode row |

FR-13 (`--fields`) and FR-14 (keychain storage) are P2 and out of scope for v1.

Release acceptance, from the PRD:

- With Spotify desktop playing, `spmeta` prints title and artist on macOS and Linux with no setup.
- `spmeta --format json` validates against the schema on all three sources.
- With music on a phone only, `spmeta` returns it after `auth login`.
- A closed Spotify app is never launched by the tool.
- `spmeta --watch` prints one line per track change and nothing between.
- Revoking the app in Spotify account settings leads to exit 5 with a clear message.
- No token or code appears in `--debug` output.
