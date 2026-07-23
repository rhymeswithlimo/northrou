# HTTP API reference

All endpoints are under `/api`. Responses are JSON. Authenticated endpoints
require an `Authorization: Bearer <access_token>` header. This is the contract
the frontend consumes.

## Auth

There are **no accounts, emails, or passwords.** A server has one credential —
its **connection code** — and any number of **profiles** (Netflix-style
viewers with a name and optional avatar); each profile has its own watch
history and recommendations. A device pairs by presenting the code (remote
clients) or by connecting directly to the box (local requests, which need no
code), and receives tokens scoped to a profile: a short-lived JWT access token
and a long-lived, rotating, revocable refresh token.

**Admin is not a profile and not a token.** It's a property of *how* the
request arrived: a **local** request — one that did not go through the remote
tunnel and whose peer address is loopback or private/LAN — may perform admin
mutations; a tunneled request, or a direct hit from a public IP, may not.
Admin **reads** (status, config, hardware, scan progress) are open to any
signed-in session regardless. See [Admin mutations](#admin-mutations) for the
full rule and its one operational caveat (Docker's userland proxy).

| Method | Path | Body | Notes |
|---|---|---|---|
| POST | `/api/auth/pair` | `{code, device_name?, ephemeral?}` | Exchanges the connection code for `{profile, profiles[], access_token, refresh_token, expires_at, server_name}`. Tokens default to the first profile; `profiles[]` is the full list for the picker. `device_name` labels this device in the paired-devices list (User-Agent when omitted). `ephemeral: true` (local requests only) returns an access token with no stored session and no `refresh_token` — how the operator's own tooling (`northrou status`, TUI, CLI) signs in without appearing in the devices list. **Remote** requests must supply the correct `code` (`401` otherwise) and always get a tracked session; **local** requests are trusted and may omit it. Wrong-code attempts are globally rate-limited (`429`). |
| POST | `/api/auth/select-profile` | `{refresh_token, profile_id}` | Switches the active profile, rotating the refresh token. `404` if the profile doesn't exist, `401` on a bad/rotated refresh token. |
| POST | `/api/auth/refresh` | `{refresh_token}` | Rotates and returns a new token pair for the same profile |
| POST | `/api/auth/logout` | `{refresh_token}` | Revokes the refresh token |
| GET | `/api/me` | - | `{profile, profiles[], admin, server_name}` for the current session. `admin` is `true` only for a local (non-tunnel) request, recomputed on every call. |
| POST | `/api/me/language` | `{audio, subtitle}` | Set the profile's preferred audio/subtitle language (ISO-639; empty clears it) |

The connection code is drawn from a 10-character ambiguity-free alphabet
(`NR-XXXXX-XXXXX`, ~50 bits), shown during setup, in Server admin, and by
`northrou cc`. Rotating it (`POST /api/admin/connection-code/rotate`, or
`northrou cc rotate`) revokes every paired device's session, so the old code
and old devices go together. A `profile` object is `{id, name, avatar?}`.

### Profiles

Any signed-in profile may list, add, or rename profiles. **Deleting** one is
destructive (it removes that viewer's watch history and recommendations), so
it's an admin action.

| Method | Path | Body | Elevation | Notes |
|---|---|---|---|---|
| GET | `/api/profiles` | - | no | `{profiles[]}` |
| POST | `/api/profiles` | `{name, avatar?}` | no | Create → `201` with the profile |
| PATCH | `/api/profiles/{id}` | `{name, avatar?}` | no | Rename / re-avatar → the updated profile |
| DELETE | `/api/profiles/{id}` | - | **local** | `204`. `403` over the tunnel; `409` if it's the last profile (never leave zero). |

### Admin mutations

Admin mutations (change config, start a scan, manual-match, apply an update,
delete a profile) require a local request as defined above — no elevation
token, no code to enter. A tunneled or public-IP request gets `403`.

> **Exposing the box's HTTP port is unsafe.** "Local" is judged from the real
> TCP peer, never a client header, so a public IP is correctly untrusted. But a
> NAT/proxy that rewrites the source to a private address — notably Docker's
> default userland proxy publishing a port to the internet — can make remote
> traffic *look* private and regain admin. Bind to the LAN/loopback (or keep
> the port off the public internet); remote clients are meant to arrive over
> the tunnel, not the port.

## First-run setup

Only usable while no account exists, and only from a local request (`403` over
the tunnel). Driven by the terminal setup wizard (`northrou setup`) — there is
no browser setup page.

| Method | Path | Body |
|---|---|---|
| GET | `/api/setup/status` | → `{needs_setup, server_name}` |
| POST | `/api/setup/complete` | `{server_name?, profile_name?, tmdb_api_key, enable_remote, movie_dirs?, show_dirs?}` → `{profile, connection_code, access_token, refresh_token}` |

Setup names the server, creates the first profile (`profile_name`, or `Me` if
omitted), issues the connection code, and signs the operator in with an
ephemeral session (empty `refresh_token` — the wizard is the operator's own
terminal, not a paired device).

`movie_dirs` / `show_dirs` are the one exception to "media folders are never
settable over the API": setup is local-only and once-ever, and sending them
through the daemon writes them into the daemon's own `config.toml` (which may
not be the file the wizard process reads, e.g. a service running as root).
Each path must be absolute and exist on the server (`400` otherwise); they
merge with any folders already on disk. After setup, folders are managed on
the server itself (`northrou admin` → Library).

## Library

| Method | Path | Notes |
|---|---|---|
| GET | `/api/movies` | List movies. Optional `?limit=&offset=` pagination; missing/non-positive `limit` returns the whole library. Newest-added first. |
| GET | `/api/movies/{id}` | Detail incl. media info, cast/crew, tagline, certification |
| GET | `/api/movies/{id}/similar` | Related titles from your own library |
| GET | `/api/shows` | List shows. Same pagination as movies. |
| GET | `/api/shows/{id}` | Detail incl. seasons, episodes, cast/crew |
| GET | `/api/shows/{id}/similar` | Related shows from your own library |
| GET | `/api/search?q=&limit=` | Case-insensitive title search, prefix matches first. Empty `q` → `[]`. |
| GET | `/api/unmatched` | Files needing manual correction |
| GET | `/api/admin/tmdb-search?q=&kind=movie\|episode` | Server-side TMDB search for the Fix-match UI (the key never leaves the box) |
| GET | `/api/images/{path}` | Cached poster/backdrop/still/headshot images (served `immutable`) |

Detail responses carry `rating` (TMDB vote average), `tagline`,
`certification` (preferring US/GB/CA/AU), `cast[]`/`crew[]`
(`{id, name, role, profile_url}`), and for movies `collection_id`. Episodes
carry `still_url` and `air_date`. Every image is an `/api/images/...` URL —
the client never talks to TMDB directly.

`/similar` is computed from the local library only (same TMDB collection
first, then shared genres, tie-broken by rating), so it only ever surfaces
titles the household actually owns.

JSON responses are gzip-compressed when the client sends
`Accept-Encoding: gzip`. Media, HLS segments, images, and WebVTT are not
(already compressed or binary).

## Streaming

| Method | Path | Notes |
|---|---|---|
| GET | `/api/media/{id}/stream` | Serve the file. Direct/remux/audio paths stream directly; full-transcode returns an HLS playlist URL |
| GET | `/api/media/{id}/plan` | Preflight: returns the transcode decision without streaming |
| GET | `/api/media/{id}/hls/{session}/{file}` | HLS playlist and segments |

**Client capabilities** are passed as query params on `/stream` and `/plan`:

```
?video=h264,hevc,av1&audio=aac,eac3&containers=mp4&max_resolution=2160&hdr=1&dolby_vision=1&atmos=1&remote=0
```

Absent parameters fall back to a conservative default (H.264 + AAC in MP4,
1080p).

When the server is already at its max concurrent transcodes (derived from
detected hardware: encoder count for GPUs, CPU cores for software), a
transcode request gets `503` with `Retry-After` rather than queueing.
Direct-play and remux requests are stream copies and are never rejected.

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
`{kind, id, title, year, poster_path}` (`kind` is `"movie"` or `"show"`).

`POST /api/watch` takes `media_kind` of `"movie"` or `"episode"` (defaults to
`"movie"`; `movie_id` is still accepted as an alias for `media_id`). Recording
episodes is what makes a partway-through show resumable; only movies feed the
taste profile, since it's built from movie features (genre, director, decade)
with no episode equivalent.

`GET /api/continue-watching` returns items most-recently-watched first,
excluding anything completed or watched under 30 seconds. Each is
`{kind, id, show_id?, title, season?, number?, position_sec, duration_sec,
backdrop_url, stream_url}`. For an episode, the display identity is the show
(`title`/`backdrop_url`, opened via `show_id`) while `id` and `season`/`number`
identify what actually resumes.

With watch history, rows are personalized (Recommended for You, director rows,
decade×genre, collection completion, etc.). With none, `/api/home` returns
library-composition **category rows** instead — "Critically Acclaimed Films",
"2000s Blockbusters", "American TV Shows", and similar — so a fresh install is
immediately browsable. There is no onboarding quiz.

## Admin

Reads are open to any signed-in session (they expose status, not controls).
**Mutations are local-only** — `403` over the remote tunnel; see
[Admin mutations](#admin-mutations).

| Method | Path | Local only | Notes |
|---|---|---|---|
| GET | `/api/admin/config` | no | Editable configuration (no secrets) |
| GET | `/api/admin/scan` | no | Scan progress |
| GET | `/api/admin/streams` | no | Active streams (mode, codecs, backend, client) |
| GET | `/api/admin/hardware` | no | Detected acceleration + estimated capacity |
| GET | `/api/admin/update` | no | Check for a newer release |
| GET | `/api/admin/logs` | no | Tail of the server log, plain text; `?n=` lines (default 200, max 5000) |
| GET | `/api/admin/sessions` | no | Paired devices: `[{id, device_name, profile_name, paired_at, last_seen_at}]` |
| PATCH | `/api/admin/config` | **yes** | Partial configuration update |
| POST | `/api/admin/scan` | **yes** | Start a library scan of the server-configured folders |
| POST | `/api/admin/match` | **yes** | Force a file to a specific TMDB title (manual correction) |
| POST | `/api/admin/update` | **yes** | Download and install the latest release |
| POST | `/api/admin/connection-code/rotate` | **yes** | Mint a fresh connection code → `{connection_code}` |
| DELETE | `/api/admin/sessions/{id}` | **yes** | Revoke one paired device (`404` if unknown) |

### Devices & code rotation

`GET /api/admin/sessions` lists devices currently paired, one entry per device
across token rotations, named from `device_name` (falling back to
User-Agent). `POST /api/admin/connection-code/rotate` replaces the code **and
revokes every paired device's session**; the running server re-registers with
the coordinator under the new code immediately. Devices on the server's own
network re-pair automatically (local pairing needs no code); every remote
device must re-enter the new code. CLI equivalents: `northrou devices`,
`northrou devices revoke <id>`, `northrou cc rotate`.

### Configuration

`/api/admin/config` exposes the subset of `config.toml` the settings screen
edits: `server_name`, `prefer_system_ffmpeg`, `max_transcodes` (0 = auto),
`allow_software_4k`, `tonemap`, `remote_enabled`, `connection_code`, and
`preferred_audio_lang`/`preferred_subtitle_lang` (ISO-639, default `en`),
which drive track selection independently of the TMDB metadata language.

`PATCH` is a true partial update — only fields you send change, and
`max_transcodes` applies immediately. Invalid values return `400`.

**Bind address, port, and `data_dir` are deliberately not editable here**:
changing them through the connection you're using is how you lock yourself
out of your own server. **The TMDB key is never returned**, only a
`has_tmdb_key` boolean; it's write-only through the same `PATCH` (send
`tmdb_api_key` to set/replace, empty string to remove), taking effect on the
next scan with no restart.

### Manual match

`POST /api/admin/match` is the escape hatch for files the scanner can't place
(they appear under `GET /unmatched`) or placed wrong. Body:
`{path, kind, tmdb_id, season?, episode?}` (`kind` is `"movie"` or
`"episode"`; `season`/`episode` required for episodes). Links the file to the
given TMDB id, fetches full metadata, extracts subtitles, and clears the
unmatched flag. `northrou match` does the same from the CLI.

**Media folders are not here, in either direction.** A folder is a path on
the server's own filesystem, so it's set on the server (`northrou admin` →
Library), never over the API — a client that could rewrite it could point the
scanner at any directory the daemon can read. `POST /api/admin/scan` triggers
a scan of whatever folders are already configured; it doesn't choose them.

## Health

| Method | Path |
|---|---|
| GET | `/api/health` |
