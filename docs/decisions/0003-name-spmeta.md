# 0003. Name the tool spmeta

- Status: Accepted
- Date: October 27, 2026
- Deciders: Bastian Kuberek

## Context

The PRD's working name `nowplaying` is taken on PyPI and is close to the `nowplaying-cli` Homebrew formula ([naming.md](../research/naming.md)). Spotify's naming rules forbid names that start with "Spot" or that are similar to "Spotify" in sound or spelling, which rules out `spotnow`, `spotnp`, `nowspot`, and `sptify`.

## Decision

Name the tool `spmeta`. Use the name for the binary, the Go module (`github.com/bkuberek/spmeta`), the Homebrew cask, the Linux packages, the configuration directory (`spmeta`), and the environment-variable prefix (`SPMETA_`). Describe the tool as "now-playing metadata for Spotify."

## Consequences

- All PRD and design references change from `nowplaying` to `spmeta`; the PRD keeps a one-line "formerly nowplaying" note.
- Documentation never presents `spmeta` as a Spotify product.

## Alternatives considered

| Alternative | Why not |
| --- | --- |
| `nowplaying` | Taken on PyPI; near collision on Homebrew |
| `spotnow`, `nowspot`, `spotnp` | Violate Spotify's naming rules |
| `trackcat`, `tunecat`, `np-cli` | Free, but do not signal the Spotify focus |
