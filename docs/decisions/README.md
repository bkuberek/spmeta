# Architecture decision records

This directory holds the architecture decision records (ADRs) for `spmeta`. Each record states the context, the decision, its consequences, and the alternatives considered.

## Format

Records use a lightweight MADR format: Status, Date, Deciders, Context, Decision, Consequences, and Alternatives considered. File names follow `NNNN-kebab-title.md`.

Status values:

- **Accepted**: decided by the user; implementation follows it.
- **Proposed**: recommended by the planning phase; awaiting user review. Implementation follows the recommendation unless the user changes it before the affected task starts.
- **Superseded**: replaced by a later record, which it links to.

## Index

| ADR | Title | Status |
| --- | --- | --- |
| [0001](./0001-audience-build-vs-wrap.md) | Publish for others and build rather than wrap | Accepted |
| [0002](./0002-language-go.md) | Implement in Go | Accepted |
| [0003](./0003-name-spmeta.md) | Name the tool spmeta | Accepted |
| [0004](./0004-web-api-built-last.md) | Keep Web API mode in v1 and build it last | Accepted |
| [0005](./0005-auto-source-resolution.md) | Resolve the auto source local-first, preferring active playback | Accepted |
| [0006](./0006-dependencies.md) | Keep runtime dependencies to godbus and BurntSushi/toml | Accepted |
| [0007](./0007-macos-applescript-access.md) | Read Spotify.app on macOS through AppleScript with a process guard | Accepted |
| [0008](./0008-versioning-and-release.md) | Version with SemVer, generate the changelog, and automate releases | Accepted |
| [0009](./0009-windows-support.md) | Support Windows: Web mode first, then a media-session backend | Proposed |

## Adding a record

To add a record:

1. Copy the structure of an existing record and take the next number.
2. Set the status to Proposed.
3. Add a row to the index above.
4. When a record replaces another, set the old record's status to "Superseded by NNNN."
