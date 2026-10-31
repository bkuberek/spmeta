# 0010. Wake watch mode with player change notifications, keeping polling as the fallback

- Status: Accepted (2026-10-30)
- Date: October 30, 2026
- Deciders: Bastian Kuberek

## Context

Status bars are the main audience of `--watch` ([prd.md](../product/prd.md)). Until now, watch mode found changes only by polling: it read the local player every `--interval` (default 1 s). That has two costs:

- Latency: a track change appears up to one interval late, so a bar lags the player by up to a second, or longer with a larger `--interval`.
- Work: on macOS every poll runs `pgrep` and an `osascript` read (150 to 170 ms each); on Linux every poll opens a D-Bus connection and calls `GetAll` on each player. Polling faster to cut latency multiplies that work while nothing changes.

Both platforms let a client hear about changes instead:

- Linux MPRIS players emit `PropertiesChanged` on `/org/mpris/MediaPlayer2`, and the bus emits `NameOwnerChanged` when a player starts or quits ([backend-linux.md](../design/backend-linux.md#change-notifications)).
- Spotify.app on macOS posts the distributed notification `com.spotify.client.PlaybackStateChanged` when the track or the playback state changes. Spotify does not document it, but other tools rely on it.

A probe on this Mac (macOS 26) confirmed that a JavaScript for Automation (JXA) program run with `osascript -l JavaScript` can observe distributed notifications: it printed a line for each notification another process posted, within a second, and used about 29 MB resident and 0.0% CPU while idle. Spotify itself posting the notification was not observed, because Spotify stayed paused during the probes.

Constraints: no cgo (`CGO_ENABLED=0`, [ADR 0008](./0008-versioning-and-release.md)), no new runtime dependency without an ADR ([ADR 0006](./0006-dependencies.md)), and `spmeta` never launches Spotify ([ADR 0007](./0007-macos-applescript-access.md)).

## Decision

A local backend may implement the optional `backend.Notifier` interface. Watch mode starts its notifications when the resolver offers them (`--source auto` or `local`), and a notification wakes the loop to read at once. Polling at `--interval` stays and remains the fallback:

- **Linux**: the MPRIS backend subscribes to `PropertiesChanged` and `NameOwnerChanged` over one persistent godbus connection, as built in task N4. godbus is already a dependency.
- **macOS**: the macOS backend starts one long-lived `/usr/bin/osascript -l JavaScript -` process running a listener script that observes `com.spotify.client.PlaybackStateChanged` and prints a line per notification ([backend-macos.md](../design/backend-macos.md#change-notifications)). The listener sends no Apple events, so it never launches Spotify and needs no Automation permission; it starts even when Spotify is not running. It also writes a `tick` heartbeat line every 5 s, which Go ignores, so that it dies of SIGPIPE, or exits on the failed write, once `spmeta` is gone.
- **Web**: the Web API has no push interface, so `--source web` polls only.

Common rules:

- A notification only wakes the loop. The read that follows takes a full snapshot through the normal read path, so watch output does not depend on what a notification carries.
- Wakes coalesce to at most one pending wake, and a wake never starts a read sooner than `MinWakeGap` (100 ms) after the previous read.
- Failing to start notifications is not an error: watch mode polls and `--debug` logs why. A notifier that stops during the run closes its channel, and watch mode polls for the rest of the run.
- The listener lives as long as the watch run: it is stopped when the watch context ends, and watch mode waits up to 1 s for it to stop before `spmeta` exits. On macOS the process is always reaped, and the heartbeat ends a listener that `spmeta` could not stop, such as after SIGKILL or SIGHUP.
- No cgo and no new dependency on either platform.

## Consequences

- On Linux and macOS, `--watch` prints a track or state change as soon as the player announces it instead of up to one interval later, and a larger `--interval` no longer delays those changes. Position progress, Web device changes, and anything else no notification announces still come from polling.
- On macOS, every `--watch` run has one extra `osascript` process, 13 to 29 MB resident, idle between notifications except for the 5 s heartbeat (0.0% CPU measured). Startup waits for the listener to print `ready` (under 200 ms measured, slowest on a cold first run; 2 s at most), which delays the first line by that much.
- If `spmeta` is killed without a chance to stop the listener, the listener outlives it by at most one heartbeat, 5 s (about 3 s observed).
- `com.spotify.client.PlaybackStateChanged` is an undocumented interface. If Spotify stops posting it or renames it, the listener stays idle and watch mode silently falls back to polling at `--interval`; nothing breaks, but changes are no longer immediate.
- Spotify posting the notification is not yet verified live on this Mac; the check is item "Instant watch" in [issue #8](https://github.com/bkuberek/spmeta/issues/8). The automated test proves the listener path end to end with a private notification name that the test posts itself.
- The Linux notifier does not reconnect after a session-bus restart, and the macOS notifier does not restart a listener that dies. Both fall back to polling for the rest of the run.

## Alternatives considered

| Alternative | Why not |
| --- | --- |
| cgo binding to `NSDistributedNotificationCenter` | Requires cgo, which [ADR 0007](./0007-macos-applescript-access.md) and [ADR 0008](./0008-versioning-and-release.md) rule out (`CGO_ENABLED=0`, cross-compiled releases) |
| A Swift or Objective-C helper binary shipped with `spmeta` | A second build toolchain, a second binary to sign and package in every channel, for what a JXA script does with the system `osascript` |
| An `osascript` stay-open script with an `on idle` handler | Still polling, inside another process; it adds a process without removing the latency |
| Spotify Web API push | The Web API has no push or webhook interface for playback; polling is the only option for `--source web` |
| Faster polling instead of notifications | Multiplies `osascript` runs or D-Bus calls while nothing changes, and still lags by up to one interval |
