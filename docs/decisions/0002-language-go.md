# 0002. Implement in Go

- Status: Accepted
- Date: October 27, 2026
- Deciders: Bastian Kuberek

## Context

The PRD assumed Python 3.11 with `jeepney` for D-Bus. Measurements on this Mac ([language.md](../research/language.md)) show Python startup with the needed imports at about 160 to 170 ms, and the Apple event to Spotify at 150 to 170 ms, for a total of about 330 ms against a 300 ms local-read budget. Python also requires a runtime on every user's machine, which conflicts with publishing a tool for status bars and scripts ([ADR 0001](./0001-audience-build-vs-wrap.md)).

## Decision

Implement `spmeta` in Go.

- Module path: `github.com/bkuberek/spmeta`.
- Local toolchain: Go 1.27.
- D-Bus: `github.com/godbus/dbus/v5` ([ADR 0006](./0006-dependencies.md)).

## Consequences

- One static binary per OS and architecture (`darwin` and `linux`, `arm64` and `amd64`), with no runtime to install.
- Process startup is negligible relative to the Apple event, so a macOS local read fits the 300 ms budget (inferred, not benchmarked).
- Distribution through GitHub Releases, a Homebrew tap, `.deb`/`.rpm`, and `go install` ([ADR 0008](./0008-versioning-and-release.md)).
- The PRD's stack table, dependency, install, CI, and repository-layout rows are rewritten; backend behavior and exit codes carry over.

## Alternatives considered

| Alternative | Why not |
| --- | --- |
| Python 3.11+ with `jeepney` | Exceeds the latency budget on macOS; needs a runtime; `jeepney` has limited typing |
| Rust | Comparable distribution story, but slower to build and no advantage for this I/O-bound tool |
