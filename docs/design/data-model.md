# Data model

Every backend maps its source into one `NowPlaying` object. JSON output is that object, serialized on one line. This document defines the fields, their null rules, and how each source maps into them. The machine-readable contract is [spmeta.schema.json](./spmeta.schema.json) (JSON Schema draft 2020-12).

## Contract

The object is public API ([ADR 0001](../decisions/0001-audience-build-vs-wrap.md)).

- `schema_version` is `1`. Any incompatible change (removing or renaming a field, changing a type, changing the meaning of a value) increments it and is a breaking change under [ADR 0008](../decisions/0008-versioning-and-release.md).
- Adding an optional field is compatible: `schema_version` stays the same and the release is a `feat`. Consumers must ignore unknown fields, so the schema does not set `additionalProperties: false`. Golden tests pin the exact key set instead.
- Every field in the schema is always present. Unknown values are `null`, never omitted.
- Key order in the output is the order in the table below.
- When nothing is playing, `--format json` prints the literal `null` and exits 3. In watch mode, `null` is one NDJSON line.

The schema file lives at `docs/design/spmeta.schema.json` during planning. Task T2 moves it to `schema/spmeta.schema.json` (the path in its `$id`) and updates every link.

## Example

```json
{"schema_version":1,"source":"macos","state":"playing","type":"track","title":"Amazone","artists":["Alvan"],"artist":"Alvan","album":"Home","album_artist":"Alvan","track_number":4,"disc_number":1,"duration_ms":238341,"position_ms":120165,"uri":"spotify:track:4brK3gpPjXArNUlU1hBXTe","url":"https://open.spotify.com/track/4brK3gpPjXArNUlU1hBXTe","artwork_url":"https://i.scdn.co/image/ab67616d0000b273a9ab54d90005cfd439f821e8","isrc":null,"explicit":null,"device":null,"fetched_at":"2026-10-27T17:40:00Z"}
```

This example is the live probe result from [macos-applescript.md](../research/macos-applescript.md) after mapping.

## Fields

| Field | JSON type | Values and null rule |
| --- | --- | --- |
| `schema_version` | integer | Always `1` |
| `source` | string | `macos`, `mpris`, or `web` |
| `state` | string | `playing`, `paused`, or `stopped`. `stopped` only when a source reports Stopped with an item still loaded (MPRIS can); the Web source never reports it |
| `type` | string | `track`, `episode`, `ad`, or `unknown` |
| `title` | string or null | Non-empty or null |
| `artists` | array of strings | Never null; empty when unknown; no empty entries |
| `artist` | string or null | `artists` joined with `", "`; null when `artists` is empty |
| `album` | string or null | Album name; for episodes, the show name |
| `album_artist` | string or null | Album artists joined with `", "` |
| `track_number` | integer or null | At least 1; a reported 0 becomes null |
| `disc_number` | integer or null | At least 1; a reported 0 becomes null |
| `duration_ms` | integer or null | At least 1; a reported 0 becomes null |
| `position_ms` | integer or null | At least 0; null when the source is unreliable (see the Linux rule) |
| `uri` | string or null | `spotify:<type>:<rest>` |
| `url` | string or null | `https://open.spotify.com/{type}/{id}`; null for ads, local files, and unknown items |
| `artwork_url` | string or null | HTTPS URL |
| `isrc` | string or null | Web only |
| `explicit` | boolean or null | Web only |
| `device` | object or null | Web only: `{"name": string, "type": string, "volume_percent": integer or null}` |
| `fetched_at` | string | UTC, RFC 3339, second precision, for example `2026-10-27T17:40:00Z` |

## Normalization rules

Every backend returns a raw `NowPlaying`; `model.Normalize` then applies these rules in order, so backends do not repeat them:

1. Trim nothing; keep strings byte-for-byte, including newlines (text and template output sanitize them, JSON keeps them).
2. Convert empty strings to null in every string field.
3. Drop empty entries from `artists`; set `artist` from `artists`.
4. Convert 0 to null in `track_number`, `disc_number`, and `duration_ms`. Negative values also become null.
5. Convert negative `position_ms` to null.
6. Derive `type` from `uri` when the backend left it empty: `spotify:track:` and `spotify:local:` give `track`, `spotify:episode:` gives `episode`, `spotify:ad:` gives `ad`, anything else gives `unknown`.
7. Derive `url` from `uri` when the backend left it null: for `spotify:track:<id>` and `spotify:episode:<id>` where `<id>` matches `^[A-Za-z0-9]+$`, produce `https://open.spotify.com/{type}/{id}`. Otherwise `url` stays null.
8. Truncate `fetched_at` to whole seconds in UTC.

## Go representation

The model lives in `internal/model`.

```go
type Source string   // "macos", "mpris", "web"
type State string    // "playing", "paused", "stopped"
type ItemType string // "track", "episode", "ad", "unknown"

type Device struct {
	Name          string `json:"name"`
	Type          string `json:"type"`
	VolumePercent *int   `json:"volume_percent"`
}

type NowPlaying struct {
	SchemaVersion int       `json:"schema_version"`
	Source        Source    `json:"source"`
	State         State     `json:"state"`
	Type          ItemType  `json:"type"`
	Title         *string   `json:"title"`
	Artists       []string  `json:"artists"` // marshaled as [] when empty, never null
	Artist        *string   `json:"artist"`
	Album         *string   `json:"album"`
	AlbumArtist   *string   `json:"album_artist"`
	TrackNumber   *int      `json:"track_number"`
	DiscNumber    *int      `json:"disc_number"`
	DurationMS    *int64    `json:"duration_ms"`
	PositionMS    *int64    `json:"position_ms"`
	URI           *string   `json:"uri"`
	URL           *string   `json:"url"`
	ArtworkURL    *string   `json:"artwork_url"`
	ISRC          *string   `json:"isrc"`
	Explicit      *bool     `json:"explicit"`
	Device        *Device   `json:"device"`
	FetchedAt     time.Time `json:"fetched_at"`
}
```

Helper functions in `internal/model`:

- `Normalize(np NowPlaying) NowPlaying`: applies the rules above.
- `ParseURI(uri string) (itemType ItemType, id string, ok bool)`.
- `OpenURL(uri string) (string, bool)`: rule 7.
- `FormatDuration(ms int64) string`: `m:ss` below one hour, `h:mm:ss` at one hour or more.

## Field mapping by source

This table replaces the PRD's original table. Corrections from research and the live probe are marked in the Notes column.

| Field | macOS AppleScript | Linux MPRIS | Web API (`GET /v1/me/player`) | Notes |
| --- | --- | --- | --- | --- |
| `state` | `player state` (`playing`, `paused`; `stopped` means nothing playing) | `PlaybackStatus` (`Playing`, `Paused`, `Stopped`) | `is_playing` true gives `playing`, false gives `paused` | |
| `type` | From `uri` | From `uri` | `currently_playing_type == "ad"` gives `ad`; otherwise `item.type` | Web: key off `item.type`, not `additional_types` |
| `title` | `name` | `xesam:title` | `item.name` | |
| `artists` | `[artist]` (one string, never split) | `xesam:artist` (string list; a single string is accepted) | Track: `item.artists[].name`. Episode: `[item.show.publisher]` | Artist names can contain commas |
| `album` | `album` | `xesam:album` | Track: `item.album.name`. Episode: `item.show.name` | |
| `album_artist` | `album artist` | `xesam:albumArtist` joined | Track: `item.album.artists[].name` joined. Episode: null | |
| `track_number` | `track number` | `xesam:trackNumber` | `item.track_number` (tracks) | |
| `disc_number` | `disc number` | `xesam:discNumber` | `item.disc_number` (tracks) | |
| `duration_ms` | `duration` (milliseconds) | `mpris:length` (microseconds / 1000) | `item.duration_ms` | macOS: the dictionary says seconds; the live probe shows milliseconds |
| `position_ms` | `(player position * 1000) as integer`, computed in the script | `Position` (microseconds / 1000); see the Spotify desktop rule | `progress_ms` | |
| `uri` | `id` (fallback `spotify url`) | Normalized `mpris:trackid`; fallback parsed from `xesam:url` | `item.uri` | macOS: `id` and `spotify url` both return the URI |
| `url` | Derived from `uri` | `xesam:url` when it starts with `https://open.spotify.com/`; else derived from `uri` | `item.external_urls.spotify`; else derived from `uri` | macOS: `spotify url` is not a web URL |
| `artwork_url` | `artwork url` | `mpris:artUrl`; `https://open.spotify.com/image/<hash>` is rewritten to `https://i.scdn.co/image/<hash>` | Largest of `item.album.images[]` (tracks) or `item.images[]` (episodes) | Linux rewrite is inferred; verify in the manual matrix |
| `isrc` | null | null | `item.external_ids.isrc` | Restored in March 2026; may be absent |
| `explicit` | null | null | `item.explicit` | |
| `device` | null | null | `device.name`, `device.type`, `device.volume_percent` | |

### Spotify desktop position rule (Linux)

When the selected player's bus-name suffix is exactly `spotify` (the desktop client), the state is `Playing`, and `Position` is 0, `position_ms` is null. The MPRIS backend remembers the last `Position` per bus name. If a later poll returns a different nonzero value, it trusts positions from that player for the rest of the process. One-shot reads therefore report null when Spotify desktop reports 0. Whether current builds report `Position` stays open in the manual matrix.

### Local files

`spotify:local:...` URIs keep `uri`, set `type: track`, and leave `url` and `artwork_url` null. On the Web source, `item.is_local` is true and `external_urls` may be empty.

### Ads

`type: ad` comes from a `spotify:ad:` URI on local sources and from `currently_playing_type == "ad"` on the Web source. Title, artists, and album are whatever the source reports, often null. The `spotify:ad:` prefix and the Web ad shape are unverified; see [testing.md](./testing.md).
