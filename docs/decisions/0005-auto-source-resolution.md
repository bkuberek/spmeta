# 0005. Resolve the auto source local-first, preferring active playback

- Status: Accepted (2026-10-27)
- Date: October 27, 2026
- Deciders: Bastian Kuberek

## Context

The PRD conflicts with itself. FR-6 says `--source auto` "tries local, then Web API if logged in," while the edge-case table says `auto` "prefers local only if local `state` is `playing`; otherwise Web." The PRD lists this as an open question: should `auto` prefer Web when a different device is playing and local is only paused?

Constraints:

- Local reads are free and fast; Web reads cost quota and about 1 s of latency.
- A user who paused the desktop app and started music on a phone wants to see the phone's track.
- A user without Web configured must get the same behavior as a local-only build.

## Decision

`auto` resolves in this order:

1. Read local. If local reports `state: playing`, return it. Web is not called.
2. If Web is configured (a token file exists), read Web. If Web reports `is_playing: true` on any device, return it.
3. If local returned an item (paused, or stopped with a track loaded), return it.
4. If Web returned an item (paused on another device), return it.
5. Otherwise, exit with the highest-precedence error among the sources tried.

Step 4 is a refinement beyond the PRD: when the desktop app is closed and the phone is paused, the user sees the paused phone track instead of exit 3.

Error precedence, highest first: `PermissionDenied` (6), `AuthRequired` (5), `RateLimited` (7), unexpected (1), `NothingPlaying` (3), `NotAvailable` (4). Errors that need the user to act come first; "a source is reachable but idle" beats "nothing is reachable."

Any local error, including a timeout or permission denial, lets `auto` try Web. Until the Web backend exists, steps 2 and 4 are skipped.

`--source local` and `--source web` read exactly one source and map its error directly to an exit code.

## Consequences

- Local playback never costs a Web request, which protects the shared per-developer quota.
- A paused local app no longer hides active playback elsewhere.
- In watch mode, Web is polled only while local is not playing, and no more often than `web_interval`.
- The algorithm is specified in [architecture.md](../design/architecture.md) and tested as a table in the resolver package.

## Alternatives considered

| Alternative | Why not |
| --- | --- |
| Local whenever available, Web only when local is unavailable (FR-6 literal) | Hides phone playback whenever the desktop app is open and paused |
| Web first when configured | Costs quota and latency on every read; contradicts local-first |
| Read both concurrently and pick | Doubles quota use for no benefit when local is playing |
