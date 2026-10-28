# Web backend and authentication

The Web backend reads `GET /v1/me/player` from the Spotify Web API after a one-time Authorization Code with PKCE login. It is the last feature built ([ADR 0004](../decisions/0004-web-api-built-last.md)). This document is the implementation spec: user setup, the login flow, the token file, refresh, the read request, response handling, mapping, rate limits, and timeouts. Evidence is in [web-api.md](../research/web-api.md); items marked "observed, undocumented" there are handled defensively and covered by synthetic fixtures.

## Packages

- `internal/auth`: PKCE, the loopback login server, the token file, and refresh with a file lock.
- `internal/backend/web`: the API client, response handling, and mapping.

Both take base URLs and an `*http.Client` as parameters so tests use `httptest.Server`:

```go
type Endpoints struct {
	Accounts string // default "https://accounts.spotify.com"
	API      string // default "https://api.spotify.com"
}
```

Only `api.spotify.com` and `accounts.spotify.com` are contacted, and only in Web mode or by `auth` and `doctor`.

## One-time user setup

`auth login` prints these steps when no Client ID is configured, and the README repeats them:

1. Sign in to the [Spotify Developer Dashboard](https://developer.spotify.com/dashboard) with an account that has Premium, and create an app with the Web API enabled.
2. Add the redirect URI `http://127.0.0.1/callback`, with no port. Spotify then accepts any port at request time.
3. In the app's user management, add the Spotify account you listen with (Development Mode allows 5 users).
4. Copy the Client ID and run `spmeta auth login --client-id <id>`, or set `client_id` in `config.toml`.

Each user brings their own Client ID; a shared one would hit the 5-user cap.

## Login flow

`auth.Login(ctx, opts LoginOptions) (Token, error)` implements these steps:

1. Generate the code verifier: 48 bytes from `crypto/rand`, encoded with `base64.RawURLEncoding`, which yields exactly 64 characters from `[A-Za-z0-9_-]` (a subset of the allowed set).
2. Compute the code challenge: `base64.RawURLEncoding(SHA-256(verifier))`.
3. Generate `state`: 16 bytes from `crypto/rand`, `base64.RawURLEncoding`.
4. Listen on `127.0.0.1:<port>` (port 0 by default, or `--port`). If the port is in use, return `KindUnexpected`: "port N is in use."
5. Build the redirect URI `http://127.0.0.1:<actual port>/callback`.
6. Build the authorization URL `https://accounts.spotify.com/authorize` with query parameters `client_id`, `response_type=code`, `redirect_uri`, `code_challenge_method=S256`, `code_challenge`, `state`, and `scope=user-read-playback-state`.
7. Print `Open this URL to log in:` and the URL to stderr. Unless `--no-browser`, run `open <url>` on macOS or `xdg-open <url>` on Linux; a failure to open is logged and ignored.
8. Serve HTTP on the listener until one of these happens, or 180 s pass:

   | Request | Response | Effect |
   | --- | --- | --- |
   | `GET /callback` with `state` not equal to the generated value | 400, "State mismatch. Return to the terminal." | Ignored; keep waiting |
   | `GET /callback?error=<code>` with matching `state` | 200, "Authorization was not granted. You can close this tab." | Return `KindAuthRequired`: "Spotify authorization failed: <code>" |
   | `GET /callback?code=<code>` with matching `state` | 200, "spmeta is logged in. You can close this tab." | Continue to step 9 |
   | Any other path | 404 | Ignored |

   On timeout, return `KindAuthRequired`: "no response from the browser within 180 s." Shut the server down with a 1 s grace period in every case.
9. Exchange the code: `POST https://accounts.spotify.com/api/token`, `Content-Type: application/x-www-form-urlencoded`, body `grant_type=authorization_code`, `code`, `redirect_uri` (byte-identical to step 5), `client_id`, `code_verifier`.
10. On 200, write the token file with `authorized_at = now` and `expires_at = now + expires_in`. On a 400 JSON error (`{"error": "...", "error_description": "..."}`), return `KindAuthRequired` with the description. Network errors and 5xx return `KindUnexpected`.

## Token file

Path and permissions are defined in [cli.md](./cli.md): `$XDG_CONFIG_HOME/spmeta/token.json`, directory 0700, file 0600.

```json
{
  "version": 1,
  "client_id": "0123456789abcdef0123456789abcdef",
  "access_token": "<secret>",
  "token_type": "Bearer",
  "refresh_token": "<secret>",
  "scope": "user-read-playback-state",
  "expires_at": "2026-10-27T18:40:00Z",
  "authorized_at": "2026-10-27T17:40:00Z"
}
```

- Writes are atomic: write a temporary file in the same directory with mode 0600, `fsync`, then `rename` over `token.json`.
- `client_id` is the ID used at login; refresh always uses it, even if the configuration now names another one (logged under `--debug`).
- `authorized_at` is not changed by refresh, because refresh does not extend the refresh token's 6-month lifetime.
- A missing file means "not logged in" (`KindNotAvailable`, exit 4, hint "run `spmeta auth login`"). A file that exists but cannot be read or parsed is `KindAuthRequired` (exit 5).
- A file whose mode allows group or other access is still used; `auth status` and `doctor` warn about it.

## Refresh

`auth.TokenSource.Token(ctx, force bool) (string, error)` returns a valid access token:

1. Read the token file. If `force` is false and `expires_at` is more than 60 s away, return the access token.
2. Acquire an exclusive `flock` on `token.json.lock` in the same directory, polling with `LOCK_NB` every 100 ms for up to 10 s (`KindUnexpected` on timeout). This keeps a status bar and a watch process from refreshing at the same moment.
3. Re-read the token file. If another process refreshed it (the access token changed and `expires_at` is more than 60 s away), release the lock and return it.
4. `POST https://accounts.spotify.com/api/token` with `grant_type=refresh_token`, `refresh_token`, and `client_id`.
5. On 200, update `access_token`, `expires_at`, and `scope`; replace `refresh_token` only when the response includes one. Write atomically, release the lock, and return.
6. On 400 with `error: invalid_grant`, return `KindAuthRequired`: "Spotify rejected the saved login (refresh tokens expire 6 months after login, or access was revoked)." Hint: "run `spmeta auth login`." Other 400 errors are also `KindAuthRequired`. Network errors and 5xx are `KindUnexpected`.

## Read request

```text
GET https://api.spotify.com/v1/me/player?additional_types=episode
Authorization: Bearer <access token>
Accept: application/json
User-Agent: spmeta/<version> (+https://github.com/bkuberek/spmeta)
```

Read at most 1 MiB of the response body. The request timeout is 5 s.

## Response handling

| Response | Handling | Evidence |
| --- | --- | --- |
| 200 | Decode and map (see Mapping). A decode error is `KindUnexpected` | Documented |
| 200, `currently_playing_type: "ad"` | `type: ad`; map whatever `item` holds (often null); state from `is_playing` | Observed, undocumented |
| 200, `item: null`, any other type | `KindNothingPlaying` | Documented that `item` is nullable |
| 204 | `KindNothingPlaying` | Observed, undocumented |
| 401 | Call `Token(ctx, force=true)` and retry once. A second 401 is `KindAuthRequired` | Documented |
| 403 | `KindPermissionDenied`: "Spotify refused access (403)" plus the body's `error.message`. Hint: "Your account may not be on the app's user allowlist in the Spotify Developer Dashboard, or the app owner's Premium subscription may have lapsed." | Allowlist documented; Premium lapse reported |
| 429 | See Rate limits | Documented |
| 5xx, or a network error | Wait 1 s and retry once; then `KindUnexpected` | Defensive |
| Any other status | `KindUnexpected`: "unexpected HTTP status N from the Spotify Web API" | Defensive |

## Rate limits

Spotify computes limits over a rolling 30-second window per developer account and returns 429 with `Retry-After` in seconds; the body may include `"reason": "QUOTA_EXCEEDED"`.

- Parse `Retry-After` as integer seconds; if it is missing or invalid, use 5 s.
- The client option `MaxRetryWait` decides whether to wait inside the read:
  - One-shot reads use `MaxRetryWait = 10 s`: when `Retry-After` is at most 10 s, sleep and retry, up to 3 attempts in total. Otherwise, or after the third 429, return `KindRateLimited` (exit 7).
  - Watch mode uses `MaxRetryWait = 0`: return `KindRateLimited` immediately with `RetryAfter`; the watch loop sleeps.
- The `KindRateLimited` message includes the wait, for example "rate limited by Spotify; retry after 30 s", plus " (QUOTA_EXCEEDED)" when the body says so.
- Never retry in a tight loop.

## Mapping

| Field | Track (`item.type == "track"`) | Episode (`item.type == "episode"`) |
| --- | --- | --- |
| `state` | `is_playing` true: `playing`; false: `paused` | same |
| `type` | `track` (or `ad` per the response table) | `episode` |
| `title` | `item.name` | `item.name` |
| `artists` | `item.artists[].name` | `[item.show.publisher]` |
| `album` | `item.album.name` | `item.show.name` |
| `album_artist` | `item.album.artists[].name` joined with `", "` | null |
| `track_number`, `disc_number` | `item.track_number`, `item.disc_number` | null |
| `duration_ms` | `item.duration_ms` | `item.duration_ms` |
| `position_ms` | `progress_ms` | `progress_ms` |
| `uri` | `item.uri` | `item.uri` |
| `url` | `item.external_urls.spotify` | `item.external_urls.spotify` |
| `artwork_url` | Largest of `item.album.images[]` | Largest of `item.images[]`, else `item.show.images[]` |
| `isrc` | `item.external_ids.isrc` | null |
| `explicit` | `item.explicit` | `item.explicit` |
| `device` | `device.name`, `device.type`, `device.volume_percent` | same |

"Largest image" means the greatest `width * height`; when dimensions are null, take the first entry (Spotify lists the largest first). An `item.type` other than `track` or `episode` maps the common fields (`name`, `uri`, `duration_ms`) with `type: unknown`. Unknown JSON fields are ignored. Local files (`item.is_local: true`) keep `uri`; `url` and `artwork_url` are null when absent.

## Timeouts

| Request | Timeout | Retry |
| --- | --- | --- |
| `GET /v1/me/player` | 5 s | Once after 1 s on 5xx or network error |
| `POST /api/token` | 10 s | None |
| Login callback | 180 s | None |
| Token lock | 10 s | Polls every 100 ms |

## Secrets

- Access tokens, refresh tokens, authorization codes, verifiers, and `Authorization` headers never reach the logger, error messages, or stdout.
- Debug logging for HTTP records method, host, path, status, and duration only. The token endpoint's request and response bodies are never logged.
- A test performs a fake login, refresh, and read with `--debug` and asserts that none of the secret values appear in stderr.

## Testing hooks

- `httptest.Server` stands in for both endpoints through `Endpoints`.
- The browser opener is a `func(url string) error` field, replaced in tests by a function that calls the callback URL directly.
- `now func() time.Time` and `sleep func(ctx, time.Duration) error` are injected so expiry and `Retry-After` tests run instantly.
