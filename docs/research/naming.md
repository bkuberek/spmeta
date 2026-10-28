# Research: tool name

Date: October 27, 2026

This note records the availability checks behind the name `spmeta`. The decision is [ADR 0003](../decisions/0003-name-spmeta.md).

## Question

Is the PRD's working name `nowplaying` available on the package registries `spmeta` will publish to, and which alternative is free and compatible with Spotify's naming rules?

## Verified findings

Availability was checked against the PyPI JSON API and the Homebrew formula API on October 27, 2026.

| Name | PyPI | Homebrew | Other |
| --- | --- | --- | --- |
| `nowplaying` | Taken: [nowplaying 0.2.62](https://pypi.org/project/nowplaying/), a D-Bus now-playing lookup from 2009 | No formula; near collision with [`nowplaying-cli`](https://formulae.brew.sh/api/formula/nowplaying-cli.json) (macOS) | Many GitHub repositories |
| `spmeta` | Free | Free | GitHub `SubPointSolutions/spmeta2` is an unrelated SharePoint project |
| `spotnow`, `nowspot`, `spotnp` | Free | Free | Minor repositories with no stars |
| `trackcat`, `tunecat`, `np-cli` | Free | Free | Minor repositories with no stars |

`spotify-cli` is taken (see [prior-art.md](./prior-art.md)).

## Naming rules (user input)

The user decision states that Spotify's naming rules forbid names that start with "Spot" or that are similar to "Spotify" in sound or spelling. That rules out `spotnow`, `spotnp`, `nowspot`, and `sptify`. The research notes do not record the URL of the Spotify policy page; this rule is taken from the user decision and was not re-verified here.

Following the same rule, documentation describes the tool as "now-playing metadata for Spotify" and never as a Spotify product.

## Not checked

- Distribution packages (Repology results were inconclusive).
- Trademark registrations.
- The Go module path `github.com/bkuberek/spmeta` and the tap `bkuberek/homebrew-tap`; both are under the user's own account.

## Implications for the design

- The binary, Go module, Homebrew formula, `.deb`/`.rpm` package, configuration directory, and environment-variable prefix all use `spmeta` (`SPMETA_*`).
- The PRD keeps a one-line note that the tool was formerly named `nowplaying`.
