# Architecture

Northrou runs a server on the user's hardware; the official coordinator
provides remote connectivity by default, and the client apps connect to it.
This doc covers how those pieces fit.

Northrou is a monorepo with two Go modules plus the client.

```
backend/       server binary  (github.com/rhymeswithlimo/northrou/backend)
coordination/  signaling broker (github.com/rhymeswithlimo/northrou/coordination)
frontend/      Tauri client (Vite; built by `make frontend`)
```

The client is built by Vite and embedded into the server binary via `go:embed`
(`internal/web`), so one binary serves both the API and the UI. The same build
is what the Tauri apps bundle. Build output is generated and not committed;
see [frontend.md](frontend.md) for the client's own framework and native-code
decisions — including why it's Tauri and how the native video player fits in.

## Backend (`backend/`)

A single binary with several subcommands, assembled in `internal/app`.

```
cmd/northrou            entrypoint (cobra CLI)
internal/
  config                TOML config, defaults, OS-appropriate paths
  db                    SQLite (pure-Go modernc), goose migrations, query layer
  model                 domain types
  auth                  connection-code pairing -> per-profile JWT access + rotating refresh tokens; middleware. Admin is transport-derived (RequireLocal / remote.IsTunnel), not a token claim
  server                chi router, middleware, graceful shutdown
  api                   HTTP handlers (auth, library, search, stream, subtitles, home, admin, config)
  ffmpeg                locate/download managed static ffmpeg + ffprobe
  mediainfo             ffprobe wrapper -> normalized codec/HDR/track data
  scanner                filename parser, TMDB match, ffprobe, unmatched flagging
  metadata               TMDB client + on-disk image cache
  subtitles              SRT/ASS -> WebVTT; PGS .sup decoder + Tesseract OCR queue
  transcode              decision cascade, HLS session mgr, session tracking
  transcode/hwaccel      NVENC/QSV/VideoToolbox/AMF/VA-API detection
  recommend              taste profile, scoring/decay, row generators, cold start
  remote                 WebRTC peer + HTTP-over-datachannel tunnel (server half; client half is JS)
  web                    embeds the Vite client build; serves it alongside the API
  service                systemd/launchd/Windows service install
  update                 self-update from GitHub releases
  tui                    bubbletea admin dashboard + first-run setup wizard
```

### The streaming decision cascade

Per request, the server picks the cheapest viable path (`transcode.Decide`):

1. **Direct play** — client handles source video, audio, and container → raw
   bytes with HTTP range support, zero CPU.
2. **Remux** — codecs fine, container incompatible → `ffmpeg -c copy` into fMP4.
3. **Audio transcode** — video fine, audio incompatible (TrueHD/DTS-HD MA) →
   copy video, transcode audio only (cheap; runs on ARM).
4. **Full transcode** — client can't decode the source video (or needs
   HDR→SDR) → HEVC→H.264 via hardware acceleration, adaptive HLS.

Atmos is preserved as far down the ladder as the client allows (passthrough →
E-AC3 JOC → AC-3 → AAC). Active streams are tracked for the admin dashboard.

Concurrent transcodes are capped to what the hardware can sustain (encoder
count for GPUs, CPU cores for software, floor of one); beyond the cap a
request gets `503` + `Retry-After`, while direct play and remux (stream
copies) are never rejected. HLS transcodes emit short keyframe-aligned
segments so playback starts without waiting a full GOP, and progressive
output is relayed through a bounded read-ahead buffer to absorb disk jitter.

### The recommendation engine

Fully local, single-household, no collaborative filtering. Each watch event
updates a time-decayed, completion-weighted taste profile across genre,
decade, director, actor, language, runtime, and time-of-day. Row generators
query the profile against the unwatched library and the home screen rotates
the highest-confidence rows over time. Computed rows are cached briefly and
invalidated on a watch or library scan, so the full-library feature load runs
once per burst rather than on every request.

Before any history exists, a **cold-start** path organizes the library into
browsable category rows by decade + box office ("2000s Blockbusters"),
critical acclaim, genre, origin country ("American TV Shows"), language,
runtime, and collections, spanning both movies and TV. There is no onboarding
quiz.

## Coordination server (`coordination/`)

A tiny, stateless WebSocket broker, and the **only** infrastructure Northrou
operates centrally. Home servers register by connection code; clients request
a server by that code; the broker relays only WebRTC signaling (SDP + ICE).
**It never sees media**, and it is the sole official coordinator — there is no
self-hosting path, no sign-in broker, and no pin relay.

The client half of the tunnel is JS (`frontend/js/api/tunnel.js`), because the
WebView already has a WebRTC stack on every platform and it keeps working in a
plain browser. A browser served off the box talks to it directly; the apps,
never same-origin with it, always reach it through this tunnel.

The broker's `connect` handler is a code-validity oracle (it answers
differently for a registered vs. unknown code), and the connection code is the
credential a client authenticates with at the box. It's rate-limited per
client IP and globally (`internal/broker/limiter.go`) so the oracle can't be
used to enumerate valid codes. The box's own `POST /api/auth/pair` is
rate-limited too; together with the ~50-bit code, that bounds brute force on
both hops.

## Remote access data flow

```
Home server ──(register)──▶ Coordinator ◀──(connect by code)── Remote client
     ▲                                                              │
     └──────────── direct WebRTC data channel (media) ─────────────┘
                    HTTP API tunneled peer-to-peer
```

Once the WebRTC connection is established, the client speaks the ordinary HTTP
API over a data channel, one channel per request, using length-prefixed
frames. A browser served off the box skips all of this and talks to it
same-origin.

## Data & dependencies

- **SQLite** via pure-Go `modernc.org/sqlite`, so no CGo is needed and all six
  release targets cross-compile from one machine.
- **FFmpeg/ffprobe** are downloaded on first run into `data_dir/bin` (or a
  system install is used when `prefer_system_ffmpeg` is set).
- **Tesseract** (optional) powers PGS subtitle OCR; absent, PGS tracks are
  skipped and text subtitles still work.
