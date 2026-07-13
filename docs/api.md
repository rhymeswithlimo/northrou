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

Pins are 6 digits, valid for 10 minutes, single-use, and limited to 5 wrong
guesses before invalidation. Repeat requests within 60 seconds reuse the
outstanding pin instead of sending another. A `profile` object is
`{id, name, avatar?}`. Delivery goes through the hosted relay by default (no
setup required), or a household's own SMTP if configured; failing both, the pin
is logged for local single-box use. See [configuration](configuration.md).

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
| POST | `/api/setup/complete` | `{email, profile_name?, movie_dirs, show_dirs, tmdb_api_key, enable_remote, smtp_host, smtp_port, smtp_username, smtp_password, from_address, from_name}` → `{account, profile, connection_code, access_token, refresh_token}` |

Setup establishes the account email and its first profile (named `profile_name`,
or derived from the email local-part if omitted) and signs the operator straight
in with a session **elevated for the setup window**, so they can add media and
scan immediately without an email round-trip. The SMTP fields are optional;
provide them so the account can receive pins on subsequent logins.

## Library

| Method | Path | Notes |
|---|---|---|
| GET | `/api/movies` | List movies (summaries). Optional `?limit=&offset=` pagination |
| GET | `/api/movies/{id}` | Movie detail incl. media info |
| GET | `/api/shows` | List shows. Optional `?limit=&offset=` pagination |
| GET | `/api/shows/{id}` | Show detail incl. seasons & episodes |
| GET | `/api/unmatched` | Files needing manual correction |
| GET | `/api/images/{path}` | Cached poster/backdrop images (served `immutable`, long `max-age`) |

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
| POST | `/api/watch` | `{movie_id, position, duration}` | Record progress; updates the taste profile |

Each row is `{key, title, confidence, items}` where an item is
`{kind, id, title, year, poster_path}` and `kind` is `"movie"` or `"show"`.

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
| GET | `/api/admin/scan` | no | Scan progress |
| GET | `/api/admin/streams` | no | Active streams (mode, codecs, backend, client) |
| GET | `/api/admin/hardware` | no | Detected acceleration + estimated capacity |
| GET | `/api/admin/update` | no | Check for a newer release |
| POST | `/api/admin/scan` | **yes** | Start a library scan |
| POST | `/api/admin/update` | **yes** | Download and install the latest release |

## Health

| Method | Path |
|---|---|
| GET | `/api/health` |
