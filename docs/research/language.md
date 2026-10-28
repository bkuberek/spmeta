# Research: implementation language

Date: October 27, 2026

This note compares Python and Go for `spmeta` against the PRD's latency budget, distribution goals, and library needs. The decision is [ADR 0002](../decisions/0002-language-go.md).

## Question

Which language best meets a 300 ms local-read budget, one-command install on macOS and Linux, and a maintained D-Bus library?

## Measured on this Mac (observed live)

| Measurement | Result |
| --- | --- |
| `python3.13 -I -c pass` | About 125 ms |
| Python startup with `argparse`, `json`, `urllib`, `http.server`, `tomllib` imported | About 160 to 170 ms |
| `osascript -e 'return 1'` (no target) | About 46 ms |
| `osascript` round trip against Spotify (warm, 5 runs) | 150 to 170 ms |

A Python local read on macOS therefore costs about 160 ms of startup plus about 160 ms for the Apple event, roughly 330 ms, which exceeds the 300 ms budget. Lazy imports reduce the startup share but leave little margin.

## Verified findings

- `jeepney` 0.9.0 was released on February 27, 2025, treats performance as a non-goal, and has limited typing. Source: [PyPI JSON](https://pypi.org/pypi/jeepney/json).
- `github.com/godbus/dbus/v5` v5.2.4 was last pushed on October 13, 2026, has about 1,200 stars, a typed `Variant` API, and no CGO.

## Inferred

- Go process startup is negligible relative to the Apple event; this was not benchmarked. A Go local read on macOS is therefore about 160 ms.
- Both languages implement the OAuth PKCE loopback flow with the standard library in roughly the same amount of code.
- Both have adequate test tooling (`pytest` fixtures, Go table-driven tests with golden files).

## Distribution comparison

| Concern | Python | Go |
| --- | --- | --- |
| Runtime on the user's machine | Required (`uv tool`, `pipx`) | None: one static binary |
| Homebrew | Formula with a Python dependency | Formula installing a prebuilt binary from a tap |
| Linux packages | Possible, uncommon | `.deb` and `.rpm` through goreleaser and nfpm |
| Cross-compilation | n/a | `darwin` and `linux` on `arm64` and `amd64` from one runner |
| Source install | `pipx install` | `go install github.com/bkuberek/spmeta/cmd/spmeta@latest` |

## Recommendation recorded by the explorer

Go, named `spmeta`. Python would win only for a personal script, for installs limited to machines that already have `uv`, or to ship the first version fastest. The user chose Go.

## Implications for the design

- The PRD rows for language, dependencies, install, CI, and repository layout change; backend behavior and exit codes carry over unchanged.
- The module path is `github.com/bkuberek/spmeta`. The local toolchain is Go 1.27.
