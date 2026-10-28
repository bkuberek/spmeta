# 0006. Keep runtime dependencies to godbus and BurntSushi/toml

- Status: Accepted (2026-10-27)
- Date: October 27, 2026
- Deciders: Bastian Kuberek

## Context

`spmeta` is a small CLI published for others ([ADR 0001](./0001-audience-build-vs-wrap.md)). Every dependency adds supply-chain surface, binary size, and upgrade work. The Go standard library covers HTTP, the OAuth loopback server, JSON, process execution, structured logging (`log/slog`), and flag parsing. It does not cover D-Bus or TOML.

## Decision

Runtime dependencies:

| Module | Used by | Why |
| --- | --- | --- |
| `github.com/godbus/dbus/v5` | `internal/backend/mpris` (Linux build only, `//go:build linux`) | Pure Go, typed, maintained; can export objects for the integration test |
| `github.com/BurntSushi/toml` (v1) | `internal/config` | Reads the optional `config.toml`; reports undecoded keys |

Test-only dependency:

| Module | Used by | Why |
| --- | --- | --- |
| `github.com/santhosh-tekuri/jsonschema/v6` | `internal/format` tests | Validates golden JSON against the draft 2020-12 schema; pure Go |

CLI parsing uses the standard library `flag` package with hand-rolled subcommand dispatch (`auth login|logout|status`, `doctor`, `version`, `help`).

Pin exact versions in `go.mod` at scaffold time; Dependabot or Renovate is out of scope for v1.

## Consequences

- The macOS binary links no third-party code except TOML parsing, because godbus is behind a `linux` build tag.
- Flag syntax follows Go conventions: `-flag` and `--flag` are equivalent, there are no short aliases except `-h`, and flags must precede positional arguments.
- Adding any other runtime dependency requires a new ADR.

## Alternatives considered

| Alternative | Why not |
| --- | --- |
| Drop the config file for v1; use flags and `SPMETA_*` environment variables only | Removes the TOML dependency, but status-bar configurations then need environment variables for the Client ID, which many bar launchers make awkward. Viable if the user prefers zero dependencies on macOS |
| `spf13/cobra` for CLI parsing | Brings `pflag` and generated help; too heavy for four commands |
| Shell out to `playerctl` instead of godbus | External binary; untyped output; no integration-test server |
| JSON Schema validation with a Python tool in CI | Adds a second toolchain to CI; the Go library keeps tests in `go test` |
