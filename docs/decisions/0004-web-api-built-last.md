# 0004. Keep Web API mode in v1 and build it last

- Status: Accepted
- Date: October 27, 2026
- Deciders: Bastian Kuberek

## Context

The Web API is the only source that sees playback on other devices, but since 2026 each user needs their own Premium-owned app in Development Mode, capped at 5 users, with refresh tokens that expire 6 months after authorization ([web-api.md](../research/web-api.md)). That makes Web mode a power-user feature. The local backends need no setup and serve every user.

## Decision

Web API mode stays in the v1 scope and is the last feature built. The build order is:

1. Scaffold.
2. Model and formatters.
3. macOS backend.
4. Linux backend.
5. Resolver and watch mode.
6. Doctor and README.
7. Web authentication.
8. Web backend.
9. Release.

The plan supports a local-only release cut after step 6: the release task depends only on the local milestones, so it can run before the Web milestones.

## Consequences

- Until the Web backend exists, `--source auto` means local only, and `--source web` exits 4 with "Web mode is not available in this version."
- If the local-only cut is released, it ships as v0.1.0 and Web mode ships in the next minor version.
- The resolver is designed with the Web step from the start ([ADR 0005](./0005-auto-source-resolution.md)), so adding Web does not change local behavior.

## Alternatives considered

| Alternative | Why not |
| --- | --- |
| Drop Web mode from v1 | Loses the phone and speaker use case the PRD names as a goal |
| Build Web mode in the PRD's original position (before the resolver) | Delays the local release behind the feature with the most setup friction |
