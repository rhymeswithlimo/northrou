# HTTP API reference

All endpoints are under `/api`. Responses are JSON. Authenticated endpoints
require an `Authorization: Bearer <access_token>` header. This is the contract
the frontend consumes.

## Accounts, profiles, and admin

There are **no accounts, emails, or passwords.** A server has one credential —
its **connection code** — and any number of **profiles** (Netflix-style viewers
with a name and optional avatar); each profile has its own watch history and
recommendations. Presenting the code (or connecting locally) signs a device in
and lets it pick a profile. **Admin is not a profile and not a token**: admin
actions are allowed only from a local connection to the box (its own network, or
the CLI), never over the remote tunnel.

## Auth

The **server connection code is the sole credential**. There are no accounts,
emails, pins, or OAuth. A device pairs by presenting the code (remote clients) or
by connecting directly to the box (local requests, which need no code), and
receives tokens scoped to a profile. Access tokens are short-lived JWTs; refresh
tokens are long-lived, rotating, revocable, and remember which profile the device
is using.

| Method | Path | Body | Notes |
|---|---|---|---|
| POST | `/api/auth/pair` | `{code}` | Exchanges the connection code for `{profile, profiles[], access_token, refresh_token, expires_at}`. Tokens default to the first profile; `profiles[]` is the full list for the picker. **Remote (tunneled)** requests must supply the correct `code` (`401` otherwise); **local** requests are trusted and may omit it. Wrong-code attempts are globally rate-limited (`429`). |
| POST | `/api/auth/select-profile` | `{refresh_token, profile_id}` | Switches the active profile. Rotates the refresh token and returns `{profile, access_token, refresh_token, expires_at}` scoped to `profile_id`. `404` if the profile does not exist, `401` on a bad/rotated refresh token. |
| POST | `/api/auth/refresh` | `{refresh_token}` | Rotates and returns a new token pair for the same profile |
| POST | `/api/auth/logout` | `{refresh_token}` | Revokes the refresh token |
| GET | `/api/me` | - | `{profile, profiles[], admin}` for the current session. `admin` is `true` only for a local (non-tunnel) request. |
| POST | `/api/me/language` | `{audio, subtitle}` | Set the current profile's preferred audio/subtitle language (ISO-639; empty clears it) |

> **`admin` means "this request is local", not "this profile may administer".**
> It is `true` only for a trusted local request: one that did **not** arrive over
> the tunnel **and** whose peer address is loopback or a private/LAN range. A
> request from a public IP on the direct path (an exposed port) is treated like a
> remote client — no admin, and pairing still needs the code. The same device is
> `admin` on the LAN and read-only remotely. Show the Server Admin **controls**
> only when `admin` is true; admin **reads** are open to any session.

The connection code is drawn from a 10-character ambiguity-free alphabet
(`NR-XXXXX-XXXXX`, ~50 bits). It is displayed during setup, shown in Server
admin, and printed by `northrou cc`. Rotating it stops new devices from pairing
but does not sign out already-paired devices (they hold their own refresh
tokens). A `profile` object is `{id, name, avatar?}`.

### Profiles

Any signed-in profile may list, add, or rename profiles. **Deleting** a profile
is destructive (it removes all of that viewer's watch history and
recommendations), so it is an admin action: allowed only from a local
connection, like other server mutations.

| Method | Path | Body | Elevation | Notes |
|---|---|---|---|---|
| GET | `/api/profiles` | - | no | `{profiles[]}` |
| POST | `/api/profiles` | `{name, avatar?}` | no | Create a profile → `201` with the profile |
| PATCH | `/api/profiles/{id}` | `{name, avatar?}` | no | Rename / re-avatar → the updated profile |
| DELETE | `/api/profiles/{id}` | - | **local** | `204`. `403` over the tunnel (local only); `409` if it is the last profile (never leave zero). |

### Admin mutations

Admin mutations (change config, start a scan, manual-match, apply an update,
delete a profile) are allowed only from a **local** request: one that did not go
through the remote tunnel **and** whose peer address is loopback or a private/LAN
range. That is a browser on the server's own network, or the `northrou` CLI. A
tunneled (remote) request, or a direct request from a public IP, is refused with
`403`. There is no elevation token and no code to enter: admin is a property of
*how* the box was reached. Admin **reads** (status, config, hardware, scan
progress) stay open to any signed-in session.

> **Exposing the box's HTTP port is unsafe.** "Local" is judged from the real TCP
> peer (`RemoteAddr`), never a client header, so a public IP is correctly
> untrusted. But if the port sits behind a NAT/proxy that rewrites the source to
> a private address — notably Docker's default userland proxy publishing the port
> to the internet — remote traffic can *look* private and regain admin. Bind to
> the LAN/loopback (or keep the port off the public internet) on any box where
> that applies; remote clients are meant to arrive over the tunnel, not the port.

## First-run setup

Only usable while no account exists, and only from a local request (`403` over
the tunnel).

| Method | Path | Body |
|---|---|---|
| GET | `/api/setup/status` | → `{needs_setup}` |
| POST | `/api/setup/complete` | `{profile_name?, tmdb_api_key, enable_remote}` → `{profile, connection_code, access_token, refresh_token}` |

Setup creates the first profile (named `profile_name`, or `Me` if omitted),
issues the server **connection code** (returned as `connection_code`), and signs
the operator straight in. Since setup runs locally, that session is
admin-capable. Media folders are not part of setup: they are configured on the
server itself (`northrou admin` → Library), since the paths describe the
server's own filesystem.

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
| GET | `/api/admin/tmdb-search?q=&kind=movie\|episode` | Search TMDB by title for the Fix-match UI (server-side; the key never leaves the box) |
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
?video=h264,hevc,av1&audio=aac,eac3&containers=mp4&max_resolution=2160&hdr=1&dolby_vision=1&atmos=1&remote=0
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

Reads are available to any signed-in session (they expose status, not controls),
so a dashboard needs nothing special. **Mutations are local-only**: they are
refused with `403` when the request arrives over the remote tunnel, and allowed
from a browser on the server's own network or the CLI. See
[Admin mutations](#admin-mutations).

| Method | Path | Local only | Notes |
|---|---|---|---|
| GET | `/api/admin/config` | no | Editable configuration (no secrets) |
| GET | `/api/admin/scan` | no | Scan progress |
| GET | `/api/admin/streams` | no | Active streams (mode, codecs, backend, client) |
| GET | `/api/admin/hardware` | no | Detected acceleration + estimated capacity |
| GET | `/api/admin/update` | no | Check for a newer release |
| PATCH | `/api/admin/config` | **yes** | Partial configuration update |
| POST | `/api/admin/scan` | **yes** | Start a library scan of the server-configured folders |
| POST | `/api/admin/match` | **yes** | Force a file to a specific TMDB title (manual correction) |
| POST | `/api/admin/update` | **yes** | Download and install the latest release |

### Configuration

`/api/admin/config` exposes the subset of `config.toml` the settings screen edits:
`prefer_system_ffmpeg`, `max_transcodes` (0 = auto), `allow_software_4k`,
`tonemap`, `remote_enabled`, `connection_code`, and the language preferences
`preferred_audio_lang` / `preferred_subtitle_lang` (ISO-639 codes, default
`en`), which drive audio/subtitle track selection independently of the TMDB
metadata language.

### Manual match

`POST /api/admin/match` is the escape hatch for files that the scanner cannot
place (they appear under `GET /unmatched`) or placed wrong. Body:
`{path, kind, tmdb_id, season?, episode?}` where `kind` is `"movie"` or
`"episode"` (`season`/`episode` required for episodes). It links the file to the
given TMDB id, fetches full metadata, extracts subtitles, and clears the
unmatched flag. The `northrou match` CLI command does the same on the server.

**Media folders are not here, in either direction.** A folder is a path on the
server's own filesystem, so it is set on the server (`northrou admin` → Library),
never over the API: a client that could rewrite it could point the scanner at any
directory the daemon can read. `POST /api/admin/scan` still triggers a scan of
whatever folders are configured there; it just does not choose them.

**The TMDB key is never returned**, only a `has_tmdb_key` boolean, so reading the
config never leaks it. It can be written.

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
