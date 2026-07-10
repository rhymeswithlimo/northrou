# HTTP API reference

All endpoints are under `/api`. Responses are JSON. Authenticated endpoints
require an `Authorization: Bearer <access_token>` header. This is the contract
the frontend consumes.

## Auth

Access tokens are short-lived JWTs; refresh tokens are long-lived, rotating, and
revocable.

| Method | Path | Body | Notes |
|---|---|---|---|
| POST | `/api/auth/login` | `{username, password}` | Returns `{user, access_token, refresh_token, expires_at}` |
| POST | `/api/auth/refresh` | `{refresh_token}` | Rotates and returns a new token pair |
| POST | `/api/auth/logout` | `{refresh_token}` | Revokes the refresh token |
| GET | `/api/me` | - | Current user (authenticated) |

## First-run setup

Only usable while no accounts exist.

| Method | Path | Body |
|---|---|---|
| GET | `/api/setup/status` | → `{needs_setup}` |
| POST | `/api/setup/complete` | `{username, password, movie_dirs, show_dirs, tmdb_api_key, enable_remote}` → account + connection code |

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

## Admin (admin accounts only)

| Method | Path | Notes |
|---|---|---|
| POST | `/api/admin/scan` | Start a library scan |
| GET | `/api/admin/scan` | Scan progress |
| GET | `/api/admin/streams` | Active streams (mode, codecs, backend, client) |
| GET | `/api/admin/hardware` | Detected acceleration + estimated capacity |
| GET | `/api/admin/update` | Check for a newer release |
| POST | `/api/admin/update` | Download and install the latest release |

## Health

| Method | Path |
|---|---|
| GET | `/api/health` |
