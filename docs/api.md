# HTTP API reference

All endpoints are under `/api`. Responses are JSON. Authenticated endpoints
require an `Authorization: Bearer <access_token>` header. This is the contract
the frontend consumes.

## Accounts, profiles, and admin

There is **one account** per server: a single email address that is the
authentication root. Under it live any number of **profiles** (Netflix-style
viewers with a name and optional avatar); each profile has its own watch history
and recommendations. **Admin is not a profile.** It is a short-lived capability
proven by a one-time code emailed to the account address; anyone who can read
that email can elevate.

## Auth

Authentication is passwordless. A device submits the account email, receives a
one-time pin by email, and exchanges it for tokens scoped to a profile. Access
tokens are short-lived JWTs; refresh tokens are long-lived, rotating, revocable,
and remember which profile the device is using.

| Method | Path | Body | Notes |
|---|---|---|---|
| GET | `/api/auth/oauth/config` | - | `{providers[], start_url}`. Empty `providers` means social sign-in is off. |
| POST | `/api/auth/oauth/signin` | `{assertion, nonce}` | Exchanges a broker assertion for the same `{profile, profiles[], access_token, refresh_token, expires_at}` verify-pin returns. `403` if the identity is not this server's account; `401` if the assertion doesn't verify. |
| POST | `/api/auth/request-pin` | `{email}` | Emails a one-time sign-in pin if `email` is the account address. Always returns `200` with a generic message (no enumeration). |
| POST | `/api/auth/verify-pin` | `{email, pin}` | Exchanges a valid pin for `{profile, profiles[], access_token, refresh_token, expires_at}`. Tokens default to the first profile; `profiles[]` is the full list for the picker. `401` on wrong/expired/exhausted pin. |
| POST | `/api/auth/select-profile` | `{refresh_token, profile_id}` | Switches the active profile. Rotates the refresh token and returns `{profile, access_token, refresh_token, expires_at}` scoped to `profile_id`. No pin required. `404` if the profile does not exist, `401` on a bad/rotated refresh token. |
| POST | `/api/auth/refresh` | `{refresh_token}` | Rotates and returns a new token pair for the same profile |
| POST | `/api/auth/logout` | `{refresh_token}` | Revokes the refresh token |
| GET | `/api/me` | - | `{account:{email}, profile, profiles[], admin}` for the current session |

> **`admin` is not "may this profile administer".** Every profile may
> administer (admin is gated on an emailed OTP, not identity), so the client
> should show the Server Admin section to **all** profiles. `admin` is `true`
> only while the current token is already OTP-elevated; use it to decide whether
> to skip the OTP prompt, not whether to reveal the admin section at all.

### Social sign-in (optional)

Off unless `[auth] oauth_issuer` is set. It is a shortcut, not a second way in:
Google/Apple prove control of an email address, which is exactly what the pin
proves, so an identity that is not the account address is refused with `403`.
The pin always works and needs no setup or internet.

**The server holds no OAuth secrets.** Google and Apple both require a registered
client with fixed redirect URIs, which a self-hosted box at an arbitrary address
cannot have, and a secret shipped in an open-source binary is not a secret. So
the credentials live on the coordination broker, which:

1. runs the provider flow at its own stable redirect URI,
2. mints a short-lived (2 min) **assertion**: an ES256 JWT carrying the verified
   email, the client's nonce, `aud: northrou-server`, and a `jti`,
3. hands it back to the client in the URL **fragment**, which never reaches a
   server, an access log, or a `Referer` header.

The box then verifies that assertion against the broker's JWKS
(`GET {oauth_issuer}/oauth/jwks`, cached ~5 min) before trusting a word of it.
That signature check is the entire security boundary: without it the endpoint
would accept "I am you@example.com" from anyone. The box additionally requires a
matching nonce, a live expiry, the right issuer and audience, ES256 specifically
(never the token's own `alg`), and refuses a `jti` it has already seen, so a
captured assertion cannot be replayed even inside its short life.

The broker learns one thing: that an email address authenticated. It never sees
media, libraries, tokens, or which box (if any) that address belongs to.

Pins are 6 digits, valid for 10 minutes, single-use, and limited to 5 wrong
guesses before invalidation. Repeat requests within 60 seconds reuse the
outstanding pin instead of sending another. A `profile` object is
`{id, name, avatar?}`. Delivery goes through the coordination relay by default
(no setup required); if the relay is disabled the pin is logged for local
single-box use. See [configuration](configuration.md).

### Profiles

Any signed-in profile may list, add, or rename profiles. **Deleting** a profile
is destructive (it removes all of that viewer's watch history and
recommendations), so it requires an elevated token, the same admin OTP that
gates server mutations.

| Method | Path | Body | Elevation | Notes |
|---|---|---|---|---|
| GET | `/api/profiles` | - | no | `{profiles[]}` |
| POST | `/api/profiles` | `{name, avatar?}` | no | Create a profile → `201` with the profile |
| PATCH | `/api/profiles/{id}` | `{name, avatar?}` | no | Rename / re-avatar → the updated profile |
| DELETE | `/api/profiles/{id}` | - | **yes** | `204`. `403` without elevation; `409` if it is the last profile (never leave zero). |

### Admin elevation

Admin mutations require an elevated access token. A signed-in profile requests a
code (emailed to the account address), then exchanges it for a short-lived token
carrying the admin capability. Elevation lasts 10 minutes; use the returned
token as the bearer for admin mutation endpoints.

| Method | Path | Body | Notes |
|---|---|---|---|
| POST | `/api/admin/request-otp` | - | Emails an admin code to the account address. Generic `200`. Any signed-in profile may call it. |
| POST | `/api/admin/verify-otp` | `{otp}` | Exchanges a valid code for `{access_token, expires_at}` (elevated, ~10 min, scoped to the calling profile). `401` on a bad code. |

## First-run setup

Only usable while no account exists.

| Method | Path | Body |
|---|---|---|
| GET | `/api/setup/status` | → `{needs_setup}` |
| POST | `/api/setup/complete` | `{email, profile_name?, tmdb_api_key, enable_remote}` → `{account, profile, connection_code, access_token, refresh_token}` |

Setup establishes the account email and its first profile (named `profile_name`,
or derived from the email local-part if omitted) and signs the operator straight
in with a session **elevated for the setup window**, so they can administer and
scan immediately without an email round-trip. Media folders are not part of
setup: they are configured on the server itself (`northrou admin` → Library),
since the paths describe the server's own filesystem.

## Library

| Method | Path | Notes |
|---|---|---|
| GET | `/api/movies` | List movies (summaries). Optional `?limit=&offset=` pagination |
| GET | `/api/movies/{id}` | Movie detail incl. media info, cast/crew, tagline, certification |
| GET | `/api/movies/{id}/similar` | Related titles from your own library |
| GET | `/api/shows` | List shows. Optional `?limit=&offset=` pagination |
| GET | `/api/shows/{id}` | Show detail incl. seasons, episodes, cast/crew |
| GET | `/api/shows/{id}/similar` | Related shows from your own library |
| GET | `/api/search?q=&limit=` | Search movie and show titles |
| GET | `/api/unmatched` | Files needing manual correction |
| GET | `/api/images/{path}` | Cached poster/backdrop/still/headshot images (served `immutable`, long `max-age`) |

Detail responses carry `rating` (TMDB vote average), `tagline`, `certification`
(one country's rating, preferring US/GB/CA/AU), `cast[]` and `crew[]`
(`{id, name, role, profile_url}`), and for movies `collection_id`. Episodes carry
`still_url` and `air_date`. Every image is an `/api/images/...` URL: the client
never talks to TMDB.

`/api/search` matches titles case-insensitively, anywhere in the string, with
prefix matches ranked first. An empty `q` returns `[]`, not an error. Items match
the home-row shape (`{kind, id, title, year, poster_url}`).

`/similar` is computed from the local library only: same TMDB collection first
(sequels), then shared genres, tie-broken by rating. It never calls out to TMDB,
so it can only ever return titles the household actually owns.

`/api/movies` and `/api/shows` accept optional `limit` and `offset` query
parameters (both integers). A missing or non-positive `limit` returns the entire
library (the historical behavior), so existing clients are unaffected; pass a
positive `limit` to page. Results are ordered most-recently-added first.

JSON responses are gzip-compressed when the client sends `Accept-Encoding: gzip`.
Media, HLS segments, images, and WebVTT are never compressed (already-compressed
or binary).

## Streaming

| Method | Path | Notes |
|---|---|---|
| GET | `/api/media/{id}/stream` | Serve the file. Direct/remux/audio paths stream directly; the full-transcode path returns an HLS playlist URL |
| GET | `/api/media/{id}/plan` | Preflight: returns the transcode decision without streaming |
| GET | `/api/media/{id}/hls/{session}/{file}` | HLS playlist and segments |

**Client capabilities** are passed as query parameters on `/stream` and `/plan`:

```
?video=h264,hevc&audio=aac,eac3&containers=mp4&max_resolution=2160&hdr=1&atmos=1&remote=0
```

Absent parameters fall back to a conservative default (H.264 + AAC in MP4, 1080p).

When the server is already running its maximum number of concurrent transcodes
(derived from the detected hardware: encoder count for GPUs, CPU cores for
software), a transcode request is rejected with `503 Service Unavailable` and a
`Retry-After` header rather than being queued. Direct-play and remux requests are
stream copies and are never rejected. Clients should retry after the indicated
delay.

## Subtitles

| Method | Path | Notes |
|---|---|---|
| GET | `/api/media/{id}/subtitles` | List tracks (prefers SRT over PGS per language) |
| GET | `/api/media/{id}/subtitles/{track}.vtt` | WebVTT for the HTML5 `<track>` element |

## Home & recommendations

| Method | Path | Body | Notes |
|---|---|---|---|
| GET | `/api/home` | - | Ranked, rotated home rows |
| GET | `/api/continue-watching` | - | Started but unfinished items for this profile |
| POST | `/api/watch` | `{media_kind, media_id, position, duration}` | Record progress; updates the taste profile |

Each row is `{key, title, confidence, items}` where an item is
`{kind, id, title, year, poster_path}` and `kind` is `"movie"` or `"show"`.

`POST /api/watch` takes `media_kind` of `"movie"` or `"episode"`. It is omittable
and defaults to `"movie"`, and `movie_id` is still accepted as an alias for
`media_id`, so the older `{movie_id, position, duration}` body keeps working.
Recording episodes is what makes a partway-through show resumable; only movies
feed the taste profile, since that is built from movie features (genre, director,
decade) with no episode equivalent.

`GET /api/continue-watching` returns items ordered most-recently-watched first,
excluding anything completed or watched for under 30 seconds. Each is
`{kind, id, show_id?, title, season?, number?, position_sec, duration_sec,
backdrop_url, stream_url}`. For an episode the display identity is the show (its
`title` and `backdrop_url`, opened via `show_id`) while `id` and `season`/`number`
identify what actually resumes.

With watch history, rows are personalized (Recommended for You, director rows,
decade×genre, collection completion, etc.). With **no** history, `/api/home`
returns library-composition **category rows** instead, e.g. "Critically
Acclaimed Films", "2000s Blockbusters", "Action Films", "American TV Shows",
"Top-Rated TV Shows", "World Cinema", so a fresh install is immediately
browsable. There is no onboarding quiz.

## Admin

Reads are available to any signed-in profile (they expose status, not controls),
so a dashboard needs no elevation. **Mutations require an elevated token** from
`/api/auth/admin/verify-otp` (see [Admin elevation](#admin-elevation)); without
it they return `403 admin elevation required`.

| Method | Path | Elevation | Notes |
|---|---|---|---|
| GET | `/api/admin/config` | no | Editable configuration (no secrets) |
| GET | `/api/admin/scan` | no | Scan progress |
| GET | `/api/admin/streams` | no | Active streams (mode, codecs, backend, client) |
| GET | `/api/admin/hardware` | no | Detected acceleration + estimated capacity |
| GET | `/api/admin/update` | no | Check for a newer release |
| PATCH | `/api/admin/config` | **yes** | Partial configuration update |
| POST | `/api/admin/scan` | **yes** | Start a library scan of the server-configured folders |
| POST | `/api/admin/update` | **yes** | Download and install the latest release |

### Configuration

`/api/admin/config` exposes the subset of `config.toml` the settings screen edits:
`prefer_system_ffmpeg`, `max_transcodes` (0 = auto), `allow_software_4k`,
`tonemap`, `remote_enabled`, and `connection_code`.

**Media folders are not here, in either direction.** A folder is a path on the
server's own filesystem, so it is set on the server (`northrou admin` → Library),
never over the API: a client that could rewrite it could point the scanner at any
directory the daemon can read. `POST /api/admin/scan` still triggers a scan of
whatever folders are configured there; it just does not choose them.

**The TMDB key is never returned**, only a `has_tmdb_key` boolean, so a leaked
elevated token cannot also read it back. It can be written.

Bind address, port and `data_dir` are deliberately not editable here: changing
them through the very connection you are using is how you lock yourself out of
your own server.

`PATCH` is a true partial update. Every field is optional and only what you send
changes; omitting a field leaves it alone rather than zeroing it.
`max_transcodes` applies to the running server immediately. Invalid values return
`400` with the validation error. `POST /api/admin/scan` returns `400` when no
media folders are configured yet.

## Health

| Method | Path |
|---|---|
| GET | `/api/health` |
