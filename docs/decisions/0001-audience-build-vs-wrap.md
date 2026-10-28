# 0001. Publish for others and build rather than wrap

- Status: Accepted
- Date: October 27, 2026
- Deciders: Bastian Kuberek

## Context

The PRD describes a CLI that prints Spotify's current track on macOS and Linux, with a Web API fallback. Several tools already cover parts of this ([prior-art.md](../research/prior-art.md)): playerctl reads MPRIS on Linux, shpotify and MediaRemote readers cover macOS, and several Web API CLIs exist. None of them unifies macOS and Linux behind one interface, documents a versioned JSON schema or exit codes, combines local-first reads with a Web fallback, or documents that it never launches a closed Spotify app.

A personal shell wrapper around those tools would work for one machine but would be fragile across platforms and would offer no contract that other scripts can rely on.

## Decision

Build `spmeta` and publish it for others. The product is the contract:

- A versioned JSON schema ([data-model.md](../design/data-model.md)).
- Documented exit codes ([cli.md](../design/cli.md)).
- One interface across the macOS, Linux, and Web sources.
- Local-first reads with a Web API fallback.
- A closed Spotify app is never launched.

## Consequences

- The JSON schema and exit codes are public API. Changing them follows Semantic Versioning ([ADR 0008](./0008-versioning-and-release.md)), and `schema_version` changes on any incompatible schema change.
- Documentation, packaging, and CI are in scope for v1, not afterthoughts.
- The tool adopts proven behavior from prior art (playerctl's player matching) instead of inventing new rules.

## Alternatives considered

| Alternative | Why not |
| --- | --- |
| Shell wrapper around playerctl, an AppleScript one-liner, and a Web CLI | Fragile, platform-specific output, no schema or exit-code contract |
| Contribute JSON output to playerctl | Linux only; does not cover macOS or the Web fallback |
| Personal script only | Rejected by the user: the tool is meant for others |
