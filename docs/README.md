# spmeta documentation

This directory holds the product requirements, research, decisions, and design specifications for `spmeta`, a command-line tool that prints now-playing metadata for Spotify on macOS and Linux. The implementation task list lives outside this tree, in [odd/tasks/spmeta-v1.md](../odd/tasks/spmeta-v1.md).

## How the documents relate

Each layer builds on the one before it:

1. **Product** states what to build and why.
2. **Research** records what was verified, observed live, or inferred about Spotify's interfaces, with sources.
3. **Decisions** (ADRs) record the choices made from that research, with alternatives.
4. **Design** turns the product and decisions into implementation-ready specs.
5. **Tasks** in `odd/tasks/` order the design into test-first work units.

When documents disagree, the more specific layer wins: a design spec over the PRD's summary, an Accepted ADR over a design spec. Report the disagreement and fix the less specific document.

## Reading order

For a first read, follow this order:

1. [Product requirements](./product/prd.md): goals, functional requirements, exit codes, milestones.
2. [Decision index](./decisions/README.md): what is decided and what awaits review.
3. [Architecture](./design/architecture.md): packages, interfaces, resolver, watch loop.
4. [CLI](./design/cli.md) and [data model](./design/data-model.md): the public contract.
5. The backend spec for the area you work on.
6. [Testing](./design/testing.md) and [release](./design/release.md).

## Product

| Document | Content |
| --- | --- |
| [prd.md](./product/prd.md) | Product requirements and specification, revised after planning research |

## Research

Dated October 27, 2026. Each note separates verified findings, live observations, and inferences.

| Document | Question |
| --- | --- |
| [web-api.md](./research/web-api.md) | Spotify Web API constraints: Development Mode, OAuth, scopes, errors, rate limits |
| [linux-mpris.md](./research/linux-mpris.md) | MPRIS bus names, value shapes, player matching, Go D-Bus library |
| [macos-applescript.md](./research/macos-applescript.md) | Spotify.app scripting, including the live probe results |
| [naming.md](./research/naming.md) | Availability of the tool name |
| [language.md](./research/language.md) | Python versus Go against the latency budget and distribution goals |
| [prior-art.md](./research/prior-art.md) | Existing tools and the gap `spmeta` fills |

## Decisions

The [decision index](./decisions/README.md) lists all ADRs and their status. ADRs 0001 to 0008 are Accepted; ADR 0009 (Windows support) is Proposed and not scheduled.

## Design

| Document | Content |
| --- | --- |
| [architecture.md](./design/architecture.md) | Package layout, `Backend` interface, typed errors, resolver, watch loop, configuration precedence, dependency rules |
| [cli.md](./design/cli.md) | Commands, flags, output formats, template syntax, exit codes, environment, configuration, integration examples |
| [data-model.md](./design/data-model.md) | `NowPlaying` fields, null rules, per-source mapping |
| [spmeta.schema.json](../schema/spmeta.schema.json) | JSON Schema (draft 2020-12) for JSON output |
| [backend-macos.md](./design/backend-macos.md) | AppleScript backend: process guard, script, parser, errors |
| [backend-linux.md](./design/backend-linux.md) | MPRIS backend: bus discovery, D-Bus calls, selection, mapping |
| [backend-web.md](./design/backend-web.md) | Web backend and PKCE authentication |
| [testing.md](./design/testing.md) | Test strategy, fixture catalogue, CI, manual matrix, acceptance criteria |
| [release.md](./design/release.md) | SemVer, generated changelog, release automation, distribution channels |

## Assets

| File | Content |
| --- | --- |
| [architecture.webp](./assets/architecture.webp) | Architecture diagram: three backends behind one resolver (drawn with the original `nowplaying` name) |
