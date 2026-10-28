# Research: Spotify Web API for now-playing reads

Date: October 27, 2026

This note records what the Spotify Web API documentation says about reading the current playback state, what it does not say, and what that means for the `spmeta` Web backend. The design that follows from it is in [backend-web.md](../design/backend-web.md).

## Question

Can `spmeta` read playback on any device through the Web API, and which constraints (Development Mode, OAuth, scopes, error shapes, rate limits) shape the design?

## Evidence levels

Each finding carries one of three labels:

- **Verified**: stated on a Spotify documentation page listed next to it.
- **Observed, undocumented**: widely reported by clients and community threads, not stated in the documentation.
- **Inferred**: a conclusion drawn from the verified items, not stated anywhere.

## Verified findings

### Development Mode and quotas

- New apps need a Premium owner. If the owner's Premium lapses, the app stops working and resumes when the owner resubscribes. Source: [February 2026 migration guide](https://developer.spotify.com/documentation/web-api/tutorials/february-2026-migration-guide).
- New apps in Development Mode are limited to 5 users, and each user must be added to the app's allowlist. Existing apps keep their prior limits. Source: [quota modes](https://developer.spotify.com/documentation/web-api/concepts/quota-modes).
- The number of Client IDs per developer rose from 1 to 25 in July 2026. Quotas are counted per developer account. Source: [July 2026 changelog](https://developer.spotify.com/documentation/web-api/references/changes/july-2026).

### Removed and restored fields

- Removed from Development Mode responses: track `popularity`, `available_markets`, and `linked_from`; album `label`, `popularity`, `album_group`, `available_markets`, and `external_ids`. Source: [February 2026 changelog](https://developer.spotify.com/documentation/web-api/references/changes/february-2026).
- `external_ids` was restored on tracks and albums in March 2026. The changelog does not name ISRC explicitly. Source: [March 2026 changelog](https://developer.spotify.com/documentation/web-api/references/changes/march-2026).

### Player endpoints

- `GET /v1/me/player` and `GET /v1/me/player/currently-playing` remain available. Source: [Get playback state reference](https://developer.spotify.com/documentation/web-api/reference/get-information-about-the-users-current-playback).
- `currently_playing_type` is one of `track`, `episode`, `ad`, or `unknown`, and `item` is nullable. `device.is_private_session` exists. Same source.
- The `additional_types` query parameter "might be deprecated in the future"; clients should key off `item.type`. Same source.

### OAuth

- Redirect URIs must be HTTPS or a loopback literal (`http://127.0.0.1` or `http://[::1]`); `localhost` is rejected. A loopback URI registered without a port accepts any port at request time. This applies to loopback literals only. Source: [redirect URIs](https://developer.spotify.com/documentation/web-api/concepts/redirect_uri).
- `GET /v1/me/player` needs only the `user-read-playback-state` scope. `user-read-currently-playing` is for `/me/player/currently-playing`. Source: [scopes](https://developer.spotify.com/documentation/web-api/concepts/scopes).
- PKCE: the code verifier is 43 to 128 characters from `[A-Za-z0-9_.-~]`. The token request takes `grant_type`, `code`, `redirect_uri`, `client_id`, and `code_verifier`. Source: [Authorization Code with PKCE](https://developer.spotify.com/documentation/web-api/tutorials/code-pkce-flow).
- Refresh takes `grant_type=refresh_token`, `refresh_token`, and `client_id`. A new refresh token is "not always included" in the response. An expired or revoked refresh token returns `invalid_grant`, which requires re-authorization. Refresh tokens last 6 months from authorization, and refreshing does not extend that lifetime. Source: [refreshing tokens](https://developer.spotify.com/documentation/web-api/tutorials/refreshing-tokens).

### Rate limits

- A 429 response "normally" includes `Retry-After` in seconds. Limits are computed over a rolling 30-second window. Spotify publishes no numbers, and Development Mode limits are lower than extended quota. Source: [rate limits](https://developer.spotify.com/documentation/web-api/concepts/rate-limits).
- A 429 body may include `"reason": "QUOTA_EXCEEDED"`. Source: [July 2026 changelog](https://developer.spotify.com/documentation/web-api/references/changes/july-2026).

## Observed, undocumented

- **204 when nothing is active.** Widely observed for `/me/player`; not stated in the reference.
- **`item: null` with `currently_playing_type: "ad"`.** Observed pairing during ads; not documented.
- **403 when the owner's Premium lapses.** Only community reports, for example [spotDL issue 2634](https://github.com/spotDL/spotify-downloader/issues/2634). A 403 for a user who is not on the allowlist is documented.

## Unverified

- The PRD's date "February 11, 2026" for the Development Mode change does not appear on the fetched pages.
- Response shapes for ads, private sessions, and episodes were not confirmed against the OpenAPI specification.
- Quotes from documentation pages were captured as paraphrases, not verbatim text.

## Implications for the design

1. Request only `user-read-playback-state`; drop `user-read-currently-playing`.
2. Expect `auth login` every 6 months. A refresh that fails with `invalid_grant` exits 5 with a re-login hint, and `auth status` shows the estimated refresh-token expiry.
3. On 429, honor `Retry-After` and surface `reason: QUOTA_EXCEEDED` when present. Never retry in a tight loop.
4. Report `type: ad` only when `currently_playing_type == "ad"`. Any other `item: null` is "nothing playing".
5. Key the item mapping off `item.type`. Keep `additional_types=episode` in the request while it works.
6. Treat 204 and the ad shape as observed behavior: handle them defensively and cover them with fixtures marked as synthetic.
7. The 403 hint names both causes: the user is not on the app's allowlist, or the app owner's Premium lapsed.
8. Each user brings their own Client ID. A shared Client ID would hit the 5-user cap.

## Risks

- Undocumented ad, private-session, and 204 shapes can change without notice.
- The cause of a 403 is ambiguous from the response alone.
- Setup friction (Premium-owned app per user) limits the Web mode audience.
- Spotify changed the platform terms twice in 2026 and can change them again.
