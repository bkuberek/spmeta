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

`Endpoints` is defined in `internal/auth`, and `internal/backend/web` reuses it for the `API` field. `Endpoints.WithDefaults()` fills each empty field with its production URL.

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

Details the steps leave open:

- `LoginOptions` has `ClientID` and `Path` (the token file), both required. `Port`, `Endpoints`, `HTTPClient`, `Open`, `Now`, `Sleep`, `Stderr`, and `Log` default to the production values when zero, except that a nil `Open` means "only print the URL" (`--no-browser`). `Login` returns the token as stored, read back from the file.
- The listener is IPv4 only (`tcp4` on `127.0.0.1`). A busy port is recognized by `EADDRINUSE`, or `WSAEADDRINUSE` on Windows; any other listen failure is `KindUnexpected`: "could not listen on the loopback interface."
- The callback server answers in plain text. A request to `/callback` with the right `state` but neither `code` nor `error` gets 400, "Missing authorization code. Return to the terminal.", and is ignored. Methods other than `GET` get 404. Only the first usable callback counts.
- The `error` value is server-controlled text, so the message shows it only when it is a plain OAuth code (lowercase letters, digits, and underscores); otherwise the message is "Spotify authorization failed."
- For a 400 from the code exchange, the message is "Spotify refused the login: <error_description>". The description is server text: it is shown only when it is one printable line of at most 200 bytes that does not contain the authorization code; otherwise the `error` code is shown, or nothing. The hint is "Check the Client ID, and that http://127.0.0.1/callback is a redirect URI of your Spotify app."
- A 200 without a `refresh_token` is `KindUnexpected` ("the Spotify accounts service returned no refresh token."), because a token file without one is not valid.
- Cancelling the context (Ctrl-C) while waiting is `KindAuthRequired`: "login was cancelled."
- `auth.OpenBrowser(url)` is the default `Open`: it starts `open` or `xdg-open` without waiting, and returns an error on other platforms, which `Login` logs and ignores.

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
- A readable file is accepted only when `version` is 1 and `client_id`, `access_token`, and `refresh_token` are present; anything else counts as unparsable (`KindAuthRequired`). Times are stored in UTC to the second.
- `token.json.lock` (mode 0600), the lock file used by refresh, is created on first use and never deleted, because removing a lock file lets two processes lock different files. `auth logout` removes only `token.json`.
- `client_id` is the ID used at login; refresh always uses it, even if the configuration now names another one (logged under `--debug`).
- `authorized_at` is not changed by refresh, because refresh does not extend the refresh token's 6-month lifetime.
- A missing file means "not logged in" (`KindNotAvailable`, exit 4, hint "Run `spmeta auth login`."). A file that exists but cannot be read or parsed is `KindAuthRequired` (exit 5).
- A file whose mode allows group or other access is still used; `auth status` and `doctor` warn about it.

## Refresh

`auth.TokenSource.Token(ctx, force bool) (string, error)` returns a valid access token:

1. Read the token file. If `force` is false and `expires_at` is more than 60 s away, return the access token.
2. Acquire an exclusive `flock` on `token.json.lock` in the same directory, polling with `LOCK_NB` every 100 ms for up to 10 s (`KindUnexpected` on timeout). This keeps a status bar and a watch process from refreshing at the same moment.
3. Re-read the token file. If another process refreshed it (the access token changed and `expires_at` is more than 60 s away), release the lock and return it.
4. `POST https://accounts.spotify.com/api/token` with `grant_type=refresh_token`, `refresh_token`, and `client_id`.
5. On 200, update `access_token`, `expires_at`, and `scope`; replace `refresh_token` only when the response includes one. Write atomically, release the lock, and return.
6. On 400 with `error: invalid_grant`, return `KindAuthRequired`: "Spotify rejected the saved login (refresh tokens expire 6 months after login, or access was revoked)." Hint: "Run `spmeta auth login`." Other 400 errors are also `KindAuthRequired`. Network errors and 5xx are `KindUnexpected`.

Details the steps leave open:

- `TokenSource` is a struct. `Path` is required; `Endpoints`, `HTTPClient`, `Now`, `Sleep` (the wait between lock attempts), and `Log` default to the production values when zero.
- For a 400 other than `invalid_grant`, the message is "Spotify refused to refresh the saved login: <error>." and shows only the `error` code (lowercase letters, digits, and underscores). `error_description` is server text and is never echoed.
- Any status other than 200 and 400 is `KindUnexpected`, and so is a 200 without an `access_token` or a positive `expires_in`. A response without `scope` keeps the saved scope.
- On platforms without `flock` (anything but macOS, Linux, and the BSDs, such as Windows), step 2 uses an in-process mutex. Goroutines of one process still refresh once, but two processes can refresh at the same moment and the last write wins; if Spotify rotated the refresh token in between, the loser may later get `invalid_grant` and must log in again. Native locking elsewhere would need a new dependency ([ADR 0006](../decisions/0006-dependencies.md)).

## Read request

```text
GET https://api.spotify.com/v1/me/player?additional_types=episode
Authorization: Bearer <access token>
Accept: application/json
User-Agent: spmeta/<version> (+https://github.com/bkuberek/spmeta)
```

Read at most 1 MiB of the response body. The request timeout is 5 s.

Details the request leaves open:

- `web.Backend` is a struct. `Tokens` is required; `Endpoints` (only `API` is used), `HTTPClient`, `Now`, `Sleep` (the wait before a retry), `Log`, `MaxRetryWait`, and `Version` (the User-Agent version, `dev` when empty) default to the production values when zero. A zero `MaxRetryWait` never waits, which is watch mode; `web.OneShotRetryWait` is the 10 s of a one-shot read.
- `Tokens` is the one-method interface `Token(ctx, force bool) (string, error)`, which `*auth.TokenSource` implements; tests pass a fake. Its `*backend.Error` values (not logged in, login expired) are returned unchanged and any other error is `KindUnexpected`.
- The 5 s timeout applies to each attempt, including reading the body. A trailing `/` on the base URL is ignored.
- Redirects are never followed: a 3xx is an unexpected status, so the client cannot leave the configured base URL.
- A cancelled or expired context ends the read at once with `KindUnexpected` ("read cancelled"), with no retry.

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

Details the table leaves open:

- The three retries have separate budgets: one refresh after a 401, one retry after a 5xx or network error (a 5xx followed by a network error counts as the same retry), and the 429 attempts under Rate limits. Each is bounded, so a read ends after a bounded number of requests.
- A 5xx that persists reports "unexpected HTTP status N from the Spotify Web API" with the second status. A network error that persists reports "could not reach the Spotify Web API."; a timeout reports "Spotify did not answer within 5 s."
- A second 401 is `KindAuthRequired`: "Spotify rejected the access token even after a refresh." Hint: "Run `spmeta auth login`." If the forced refresh itself fails, its error is returned.
- The 403 message is "Spotify refused access (403): <error.message>", or "Spotify refused access (403)." without detail. `error.message` is server text: it is shown only when it is one printable line of at most 200 bytes that contains none of the access tokens the read sent.

## Rate limits

Spotify computes limits over a rolling 30-second window per developer account and returns 429 with `Retry-After` in seconds; the body may include `"reason": "QUOTA_EXCEEDED"`.

- Parse `Retry-After` as integer seconds; if it is missing or invalid, use 5 s. Invalid includes 0, negative values, fractions, and HTTP dates, so a zero cannot cause a tight loop. A value above 24 h is capped at 24 h.
- The client option `MaxRetryWait` decides whether to wait inside the read:
  - One-shot reads use `MaxRetryWait = 10 s`: when `Retry-After` is at most 10 s, sleep and retry, up to 3 attempts in total. Otherwise, or after the third 429, return `KindRateLimited` (exit 7).
  - Watch mode uses `MaxRetryWait = 0`: return `KindRateLimited` immediately with `RetryAfter`; the watch loop sleeps.
- The `KindRateLimited` message includes the wait, for example "rate limited by Spotify; retry after 30 s", plus " (QUOTA_EXCEEDED)" when the body says so. The documentation does not place `reason` in the body, so a top-level `reason` and `error.reason` are both read.
- A retry after a 429 reuses the same access token; the wait is at most `MaxRetryWait`, and the three attempts include the first request.
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

"Largest image" means the greatest `width * height`. An image without both dimensions counts as 0, so when no image has dimensions, or two tie, the first entry wins (Spotify lists the largest first). Entries whose URL is empty or not HTTPS are skipped, because `artwork_url` must be an HTTPS URL. An `item.type` other than `track` or `episode` maps the common fields (`name`, `uri`, `duration_ms`) with `type: unknown`. Unknown JSON fields are ignored. Local files (`item.is_local: true`) keep `uri`; `url` and `artwork_url` are null when absent.

Details the table leaves open:

- `url` is `item.external_urls.spotify` when present, else derived from `uri` by `model.Normalize`. Position, device, and state are mapped for every item, including an ad whose `item` is null.
- During an ad (`currently_playing_type: "ad"`), a non-null `item` is mapped as its own `item.type` says, then `type` is set to `ad`.
- A `device` that is null maps to a null `device`; a null `volume_percent` stays null.

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
- The browser opener is the `Open func(url string) error` field of `LoginOptions`, replaced in tests by a function that calls the callback URL directly. The `cli` package exposes it as `WithBrowserOpener`, and the service URLs as `WithAuthEndpoints`.
- `now func() time.Time` and `sleep func(ctx, time.Duration) error` are injected so expiry and `Retry-After` tests run instantly. In `internal/backend/web` they are the exported `Now` and `Sleep` fields of `Backend`, and `Tokens` takes a fake. In `internal/auth` they are the exported `Now` and `Sleep` fields of `TokenSource`; `Sleep` replaces the 100 ms wait between lock attempts, which also lets a test observe that a refresher is waiting for the lock. `LoginOptions.Sleep` replaces the 180 s wait for the callback, so a timeout test returns at once.
