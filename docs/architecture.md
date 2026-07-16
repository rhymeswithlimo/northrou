# Architecture

Northrou runs a server on the user's hardware; the hosted coordinator and pin
relay provide remote connectivity by default, and the client apps connect to it.
This doc covers how those pieces fit.

Northrou is a monorepo with two Go modules plus the client.

```
backend/       server binary  (github.com/rhymeswithlimo/northrou/backend)
coordination/  signaling relay + pin-delivery relay (github.com/rhymeswithlimo/northrou/coordination)
frontend/      Tauri client (Vite; built by `make frontend`)
```

The client is built by Vite and embedded into the server binary via `go:embed`
(`internal/web`), so one binary serves both the API and the UI. The same build is
what the Tauri apps bundle. Its build output is generated and not committed; see
[frontend.md](frontend.md).

### Why Tauri (one web UI, all platforms)

The frontend is **Tauri v2**, chosen because it targets desktop (Win/Mac/Linux),
iOS, and Android from a single HTML/CSS/JS codebase. Capacitor was the other
contender but covers mobile only, so it would have meant maintaining a second
shell (Electron/Tauri) plus a second plugin system for desktop. One codebase
wins for a small self-hosted project.

The catch is the video player: an `<video>` tag won't deliver 4K HEVC / TrueHD
Atmos with AirPlay/PiP/passthrough. The player is **native per platform**, wired
in through Tauri plugins: AVPlayer (iOS/Swift), ExoPlayer/Media3
(Android/Kotlin), and libmpv/libVLC on desktop (the OS WebView can't be trusted
for HEVC direct play, especially WebKitGTK on Linux; fall back to the backend
transcode cascade only when the client truly can't decode). Everything else
(browse, search, details, settings) is the shared web UI.

## Backend (`backend/`)

A single binary with several subcommands, assembled in `internal/app`.

```
cmd/northrou            entrypoint (cobra CLI)
internal/
  config                TOML config, defaults, OS-appropriate paths
  db                    SQLite (pure-Go modernc), goose migrations, query layer
  model                 domain types
  auth                  one account email + many profiles; passwordless email pins, per-profile JWT access + rotating refresh tokens, OTP-elevated admin capability, middleware; optional OAuth assertion verification (oauth.go)
  email                 delivery of one-time sign-in pins via the coordination relay (log fallback)
  server                chi router, middleware, graceful shutdown
  api                   HTTP handlers (auth, library, search, stream, subtitles, home, admin, config)
  ffmpeg                locate/download managed static ffmpeg + ffprobe
  mediainfo             ffprobe wrapper -> normalized codec/HDR/track data
  scanner               filename parser, TMDB match, ffprobe, unmatched flagging
  metadata              TMDB client + on-disk image cache
  subtitles             SRT/ASS -> WebVTT; PGS .sup decoder + Tesseract OCR queue
  transcode             decision cascade, HLS session mgr, session tracking
  transcode/hwaccel     NVENC/QSV/VideoToolbox/AMF/VA-API detection
  recommend             taste profile, scoring/decay, row generators, cold start
  remote                WebRTC peer + HTTP-over-datachannel tunnel (server half; the client half is JS)
  web                   embeds the Vite client build; serves it alongside the API
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

The client half of the tunnel is JS (`frontend/js/api/tunnel.js`), because the
WebView already has a WebRTC stack on every platform and it keeps working in a
plain browser. A browser served off the box talks to it directly; the apps,
which are never same-origin with it, always reach it through this tunnel.

The coordinator also hosts the optional **sign-in broker** (`internal/oauth`),
which is off unless provider credentials are configured. Google and Apple require
a registered OAuth client with fixed redirect URIs, which a self-hosted box at an
arbitrary address cannot have, so the credentials live here and the box never
sees them. The broker mints a 2-minute ES256 assertion carrying the verified
email; the box verifies it against `/oauth/jwks` and requires the identity to be
its own account address. That signature check is the security boundary — without
it the endpoint would accept anyone's claim. The broker learns only that an email
authenticated; it never sees media, libraries, tokens, or which box that address
belongs to.

## Pin relay (`coordination/cmd/relay`)

The only other piece of infrastructure Northrou operates centrally, and a
separate binary from the coordinator (the coordinator stays stateless; the relay
holds in-memory rate-limit counters). Home servers keep accounts and pins
entirely local and call `POST /v1/pin/send` on the relay only to deliver the pin
email, so a household never has to run a mail server. It is on by default
(`config.email.relay_url`); disabling it falls back to logging the pin. The box
speaks no SMTP itself, deliberately: self-hosted outbound mail is the classic
way to lock yourself out of your own login.

The relay has no account list and cannot distinguish a real address from a
fabricated one, so it is protected by input validation and rate limiting rather
than authentication. The **per-recipient limit is the load-bearing control**: it
stops the relay from being used to spam a third party's inbox with sign-in
codes. Per-server and global limits protect the operator's cost and sender
reputation. Mail is readable in transit like any email, so the relay is a
trusted delivery party by nature; privacy-sensitive households can run their own
relay and point `relay_url` at it. It **never sees accounts, library, or media.**

## Remote access data flow

```
Home server ──(register)──▶ Coordinator ◀──(connect by code)── Remote client
     ▲                                                              │
     └──────────── direct WebRTC data channel (media) ─────────────┘
                    HTTP API tunneled peer-to-peer
```

Once the WebRTC connection is established, the client speaks the ordinary HTTP
API over a data channel, one channel per request, using length-prefixed frames. A
browser served off the box skips all of this and talks to it same-origin.

## Data & dependencies

- **SQLite** via pure-Go `modernc.org/sqlite`, so no CGo is needed and all six
  release targets cross-compile from one machine.
- **FFmpeg/ffprobe** are downloaded on first run into `data_dir/bin` (or a
  system install is used when `prefer_system_ffmpeg` is set).
- **Tesseract** (optional) powers PGS subtitle OCR; absent, PGS tracks are
  skipped and text subtitles still work.
