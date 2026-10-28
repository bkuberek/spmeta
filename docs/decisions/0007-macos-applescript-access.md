# 0007. Read Spotify.app on macOS through AppleScript with a process guard

- Status: Accepted (2026-10-27)
- Date: October 27, 2026
- Deciders: Bastian Kuberek

## Context

Three ways exist to read the current track from Spotify.app on macOS ([macos-applescript.md](../research/macos-applescript.md)):

- AppleScript through `osascript`, using Spotify's scripting dictionary.
- JXA (JavaScript for Automation) through `osascript -l JavaScript`, same dictionary and TCC path, native JSON.
- The private MediaRemote framework, which since macOS 15.4 only serves `com.apple.*` bundle IDs and needs a `/usr/bin/perl` workaround.

The live probe confirmed the AppleScript path on Spotify.app 1.3.4.258: field units, URI formats, bundle-ID targeting, and a warm latency of 150 to 170 ms. It also showed that the first call can block for about 12.9 s on the Automation consent prompt.

## Decision

Use AppleScript through `/usr/bin/osascript` with these rules:

- Check `/usr/bin/pgrep -x Spotify` from Go before invoking `osascript`. If Spotify is not running, return `NotAvailable` without sending any Apple event.
- Check `running of application id "com.spotify.client"` again inside the script.
- Target `application id "com.spotify.client"`, never the application name.
- Never name a variable `st`.
- Separate fields with ASCII unit separator (character id 31); wrap every field read in `try`.
- Compute `(player position * 1000) as integer` in the script; read `duration` as milliseconds.
- Derive `url` in Go from the URI; never pass `spotify url` through as a web URL.
- Use a 2 s timeout for normal reads. `spmeta doctor` triggers and waits for Automation consent with a 120 s timeout. A read timeout returns an error whose hint says to run `spmeta doctor`.

The exact script and error mapping are in [backend-macos.md](../design/backend-macos.md).

## Consequences

- A closed Spotify app is never launched by `spmeta`; a small race remains if Spotify quits between the process check and the Apple event, mitigated by the in-script `running` check. The manual matrix verifies "quit, no launch."
- macOS attributes the Automation prompt to the responsible process (Terminal, iTerm2, Ghostty, SketchyBar), so users grant permission once per host application.
- AppleScript output parsing is custom, but the format is fixed and covered by golden tests.

## Alternatives considered

| Alternative | Why not |
| --- | --- |
| JXA with JSON output | Same TCC path and latency; saves the parser but loses the probe-verified script and adds JavaScript coercion quirks for missing values. Revisit if parsing proves brittle |
| MediaRemote | Locked to Apple bundle IDs since macOS 15.4; the workaround depends on `/usr/bin/perl` internals |
| A name-based `tell application "Spotify"` guarded by `is running` (the PRD's script) | Name resolution can launch the app at compile time on some systems |
| A cgo or `NSAppleScript` binding | Requires CGO, breaks cross-compilation, same TCC behavior |
