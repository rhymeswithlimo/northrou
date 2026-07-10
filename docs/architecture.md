# Architecture

Northrou is a monorepo with two Go modules plus a separately-developed frontend.

```
backend/       server binary  (github.com/rhymeswithlimo/northrou/backend)
coordination/  signaling relay (github.com/rhymeswithlimo/northrou/coordination)
frontend/      Tauri client (added separately)
```

## Backend (`backend/`)

A single binary with several subcommands, assembled in `internal/app`.

```
cmd/northrou            entrypoint (cobra CLI)
internal/
  config                TOML config, defaults, OS-appropriate paths
  db                    SQLite (pure-Go modernc), goose migrations, query layer
  model                 domain types
  auth                  JWT access + rotating refresh tokens, bcrypt, middleware
  server                chi router, middleware, graceful shutdown
  api                   HTTP handlers (auth, library, stream, subtitles, home, admin)
  ffmpeg                locate/download managed static ffmpeg + ffprobe
  mediainfo             ffprobe wrapper -> normalized codec/HDR/track data
  scanner               filename parser, TMDB match, ffprobe, unmatched flagging
  metadata              TMDB client + on-disk image cache
  subtitles             SRT/ASS -> WebVTT; PGS .sup decoder + Tesseract OCR queue
  transcode             decision cascade, HLS session mgr, session tracking
  transcode/hwaccel     NVENC/QSV/VideoToolbox/AMF/VA-API detection
  recommend             taste profile, scoring/decay, row generators, cold start
  remote                WebRTC peer + HTTP-over-datachannel tunnel
  service               systemd/launchd/Windows service install
  update                self-update from GitHub releases
  tui                   bubbletea admin dashboard
  setup                 first-run wizard (browser)
  web                   embedded setup-wizard assets
```

### The streaming decision cascade

Per request, the server picks the cheapest viable path (`transcode.Decide`):

1. **Direct play** - client handles the source video, audio, and container →
   raw bytes with HTTP range support, zero CPU.
2. **Remux** - codecs fine, container incompatible → `ffmpeg -c copy` into fMP4.
3. **Audio transcode** - video fine, audio incompatible (TrueHD/DTS-HD MA) →
   copy video, transcode audio only (cheap; runs on ARM).
4. **Full transcode** - client can't decode the source video (or needs HDR→SDR)
   → HEVC→H.264 via hardware acceleration, adaptive HLS.

Atmos is preserved as far down the ladder as the client allows (passthrough →
E-AC3 JOC → AC-3 → AAC). Active streams are tracked for the admin dashboard.

Concurrent transcodes are capped to what the hardware can sustain (encoder count
for GPUs, CPU cores for software, floor of one); beyond the cap a transcode
request gets `503` + `Retry-After`, while direct play and remux (stream copies)
are never rejected. HLS transcodes emit short keyframe-aligned segments so
playback starts without waiting a full GOP, and progressive output is relayed
through a bounded read-ahead buffer to absorb disk jitter.

### The recommendation engine

Fully local, single-household, no collaborative filtering. Each watch event
updates a time-decayed, completion-weighted taste profile across genre, decade,
director, actor, language, runtime, and time-of-day dimensions. Row generators
query the profile against the unwatched library and the home screen rotates the
highest-confidence rows over time. Computed rows are cached briefly and
invalidated on a watch or a library scan, so the full-library feature load runs
once per burst rather than on every request.

Before any history exists, a **cold-start** path organizes the library the user
already owns into browsable category rows by decade + box office
("2000s Blockbusters"), critical acclaim, genre, origin country
("American TV Shows"), language, runtime, and collections, spanning both movies
and TV shows. There is no onboarding quiz.

## Coordination server (`coordination/`)

A tiny, stateless WebSocket relay. Home servers register by connection code;
clients request a server by that code; the broker relays only WebRTC signaling
(SDP + ICE). **It never sees media.**

## Remote access data flow

```
Home server ──(register)──▶ Coordinator ◀──(connect by code)── Remote client
     ▲                                                              │
     └──────────── direct WebRTC data channel (media) ─────────────┘
                    HTTP API tunneled peer-to-peer
```

Once the WebRTC connection is established, the client speaks the ordinary HTTP
API over a data channel, one channel per request, using length-prefixed frames. On
the local network, clients skip the coordinator entirely and connect to the
server's LAN address.

## Data & dependencies

- **SQLite** via pure-Go `modernc.org/sqlite`, so no CGo is needed and all six
  release targets cross-compile from one machine.
- **FFmpeg/ffprobe** are downloaded on first run into `data_dir/bin` (or a
  system install is used when `prefer_system_ffmpeg` is set).
- **Tesseract** (optional) powers PGS subtitle OCR; absent, PGS tracks are
  skipped and text subtitles still work.
