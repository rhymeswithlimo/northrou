# AGENTS.md

Guidance for AI coding agents (Claude, ChatGPT, Gemini, and similar)
working in the Northrou repository. For end-user install and usage docs, see
[README.md](README.md) and [docs/](docs/).

## What this is

Northrou is an open-source, self-hosted media server for a household's own
movie and TV library (4K HEVC / TrueHD Atmos handled natively). It streams
that library across the internet from the owner's own hardware,
peer-to-peer, so media never passes through external servers.

The server runs on the user's hardware, the client apps (iOS, Android,
desktop, web) connect to it, one person sets the server up once and shares a
connection code, and people enter that code to reach it — there are no
accounts, emails, or passwords. It is fully open-source and
forkable/self-buildable.

Three components, all in this repo: the backend, the coordination server,
and the Tauri client. The client is built by Vite, embedded into the server
binary by `go:embed`, and shipped as Tauri apps from the same tree, so a
change to the API contract and its consumer lands in one commit.

## Repo layout - TWO Go modules

```
backend/       server binary   → module github.com/rhymeswithlimo/northrou/backend
coordination/  signaling broker (cmd/coordinator) — the only official broker
               → module github.com/rhymeswithlimo/northrou/coordination
frontend/      Tauri client: Vite (vanilla ES modules, no framework)
               src-tauri/ the shell; plugins/ native chrome per platform
               swift/ SwiftUI design reference for the iOS chrome (iOS 18+)
scripts/       install.sh
docs/          configuration.md, api.md, architecture.md
```

> **Gotcha:** the repo root is NOT a Go module. `go build ./...` from the root
> fails. Always work from inside `backend/` or `coordination/`, or use the
> root `Makefile`.

## Build / test / run

```sh
# From the repo root (Makefile handles both modules + the client):
make frontend     # Vite build -> staged into backend/internal/web/assets
make build        # frontend + bin/northrou and bin/coordinator (version ldflags)
make frontend-dev # client on :5173 with /api proxied to a local server
make test         # go test ./... in both modules
make vet
make run          # build + ./bin/northrou serve -v

# Or per-module:
cd backend && go build ./... && go test ./...
cd coordination && go build ./... && go test ./...

# Cross-compile check (all 6 targets; pure-Go so no CGo toolchain needed):
cd backend && CGO_ENABLED=0 GOOS=linux GOARCH=arm64 go build ./cmd/northrou

# Release archives (needs a git tag; goreleaser installed):
goreleaser release --snapshot --clean
```

Go 1.26+.

## Releases

Releases are cut by the project maintainer only, using tooling that isn't
part of this repo. If your change is worth a release, mention it in the PR
and let the maintainer handle cutting it — don't try to build, tag, or
publish one yourself.

**Update [docs/changelog.md](docs/changelog.md) whenever you land a change
big enough to matter to someone running the server** (a new feature, a real
bug fix, a behavior change) — not for internal refactors, docs, or test-only
commits. Add the bullet under the in-progress version's `### Added` /
`### Fixed` / `### Improved` heading (create the heading if it's the first
of its kind for that version) as part of the same piece of work, not
retroactively before a release. **These three headings are strict**: never
invent a different one (no `### Changed`, `### Breaking`, `### Security`,
etc.), and when a version uses more than one, they always appear in that
exact order — Added, then Fixed, then Improved.

## Architecture (summary, with full detail in docs/architecture.md)

The server binary is assembled in `internal/app`. Cobra CLI in `internal/cli`.

| Area | Package | Notes |
|---|---|---|
| Config | `internal/config` | Single TOML; OS-appropriate paths |
| Database | `internal/db` | SQLite (pure-Go modernc), goose migrations, hand-written queries |
| Auth | `internal/auth` | **The server connection code is the sole credential** (no email, no pins, no OAuth). A non-local request (through the tunnel, or a direct hit from a public IP) presents the code to `POST /api/auth/pair`; a trusted local request pairs with no code. Either way it gets a JWT access token scoped to a **profile** (Netflix-style) + a rotating refresh token. **Admin is derived from `remote.IsLocal(r)`, not a token/profile flag**: admin *mutations* (`RequireLocal`) need a request that is not tunneled AND whose peer IP is loopback/private; admin *reads* are open to any session. Pair attempts are rate-limited (`internal/api/limiter.go`) |
| HTTP | `internal/server`, `internal/api` | chi router; handlers return DTOs, never DB rows |
| FFmpeg | `internal/ffmpeg` | Locate/download managed static ffmpeg+ffprobe |
| Scanning | `internal/scanner`, `internal/metadata`, `internal/mediainfo` | Filename parse → TMDB → ffprobe |
| Subtitles | `internal/subtitles` | SRT/ASS→WebVTT; PGS `.sup` decoder + Tesseract OCR |
| Streaming | `internal/transcode`, `internal/transcode/hwaccel` | Decision cascade, HLS, HW detection |
| Recommendations | `internal/recommend` | Local taste profile + row/category generators |
| Remote | `internal/remote`, `/coordination` | WebRTC peer + HTTP-over-datachannel tunnel. The coordinator (`coordination/cmd/coordinator`) is the **only** broker (hardcoded `config.DefaultCoordinationURL`); it relays signaling only and rate-limits `connect`. The **client** half of the tunnel is JS (`frontend/js/api/tunnel.js`). `ServeConn` stamps tunneled requests via `remote.WithTunnel`, which the admin gate reads |
| Service/update/TUI | `internal/service`, `internal/update`, `internal/tui` | kardianos (install/start/stop/restart/status), self-update, bubbletea. **First-run setup is the TUI wizard** (`internal/tui/setup.go`, driven by `northrou setup` against the local `/api/setup/*` endpoints); there is no browser setup page. `internal/logging` writes the size-rotated `data_dir/logs/northrou.log` that `northrou logs` and `GET /api/admin/logs` read |

### The streaming decision cascade (`internal/transcode/decision.go`)
Per request, cheapest viable path: **direct play → remux → audio-only transcode
→ full transcode**. This is the most important logic in the codebase and is
heavily table-tested in `decision_test.go`. Don't regress the ordering.
Concurrent transcodes are admission-gated by `SessionManager` (weighted cap
auto-derived from hardware; cheap stream-copy paths are exempt; over the cap
returns 503 + `Retry-After`). Don't regress the cap or start counting cheap
paths against it.

### Recommendations (`internal/recommend`)
Fully local, single-household, no collaborative filtering. Watch events update a
time-decayed, completion-weighted taste profile. With history → personalized
rows; with none → cold-start **category rows** built from library composition
(blockbusters by decade, acclaimed, genre, country, etc.) across movies AND TV.
Home-row items are `{kind:"movie"|"show", id, title, year, poster_path}`.
There is **no** taste quiz (removed intentionally). Computed home rows are cached
in the `Engine` (TTL + invalidation on watch and scan-complete); new write paths
that change the catalog should invalidate it.

## Conventions & key decisions

- **Pure-Go SQLite** (`modernc.org/sqlite`), so all 6 targets cross-compile with
  `CGO_ENABLED=0`. **Never introduce a CGo dependency.** It breaks the release
  pipeline. (This is why we don't use `mattn/go-sqlite3` or `gosseract`.)
- **FFmpeg/ffprobe/Tesseract are external processes**, invoked by resolved
  absolute path. ffmpeg is downloaded on first run into `data_dir/bin` (or a
  system install via `prefer_system_ffmpeg`). Tesseract is optional; PGS OCR
  degrades gracefully without it.
- **ffmpeg is not ready at boot.** It downloads in the background
  (`app.ensureFFmpeg`), which then attaches the prober to the scanner
  (`Scanner.SetProber`), wires subtitles, detects hardware, and builds the
  streamer (`API.SetStreamer`). Code that needs ffmpeg must tolerate it being
  absent early (handlers return 503).
- **Paths:** use `path/filepath` for filesystem paths; use `path` (forward
  slash) ONLY for archive entries and URLs. Image cache stores forward-slash
  relative paths for serving; the DB/URLs use `/`, the filesystem uses
  `filepath`.
- **Cross-platform:** all OS-specific behavior is a `runtime.GOOS` switch in a
  single file (config paths, browser launch, binary naming), with no
  `*_windows.go`/`*_linux.go` split files. Windows executables get `.exe` via
  `ffmpeg.ExecName` / `binaryName`. `go vet` is kept clean for linux/darwin/
  windows.
- **API DTOs** are decoupled from DB rows (`internal/api/handlers_*.go`) so the
  frontend contract stays stable. Change DTOs deliberately.
- **The client is embedded, and its build output is not committed.** Only
  `backend/internal/web/assets/.gitkeep` is tracked, so `go build` works in a
  fresh clone; `/` then reports that the client isn't built. Run `make frontend`.
- **A DB read query must select what its model claims to return.** `GetMovie`
  once omitted `vote_average` and never joined credits, so `Rating`, `Cast` and
  `Crew` were silently empty everywhere despite the scanner writing them.
  `genres.go` is the writer+reader template; mirror it.
- **CSS: hover always goes inside `@media (hover: hover)`.** On touch a tap
  latches `:hover` and it sticks. See the note at the top of `css/base.css`.
- **Auth:** JWT bearer via `auth.Middleware`; admin-mutation routes wrap
  `auth.RequireLocal` (allowed only for non-tunnel requests).

## How to make common changes

- **Add a DB migration:** create `internal/db/migrations/000NN_name.sql` with
  `-- +goose Up` / `-- +goose Down`. Embedded via `//go:embed`; runs
  automatically on `db.Open`. Then add query methods to the relevant
  `internal/db/*.go` and fields to `internal/model`.
- **Add an API endpoint:** add a handler in `internal/api/handlers_*.go`,
  register the route in `internal/api/api.go` (inside the authenticated or admin
  group as appropriate), return a DTO, and document it in `docs/api.md`.
- **Add a CLI command:** define it in `internal/cli/` and attach it in
  `root.go`.

## Testing conventions

- Table-driven tests; real SQLite via `db.Open(filepath.Join(t.TempDir(), …))`.
- `httptest` servers stand in for TMDB, the admin API, and the coordination
  broker. The WebRTC tunnel is tested over **real in-process pion peers**
  (`internal/remote/tunnel_test.go`).
- Pure functions that take `goos` as a parameter (e.g. `binaryName`) let us test
  Windows behavior while running on macOS. Prefer that pattern for
  platform-specific logic.
- Run everything: `make test`. Keep `go vet ./...` clean for all three OSes.

## Known limitations / watch-outs

- **ffmpeg download URLs in `internal/ffmpeg/releases.go` are NOT SHA-256
  pinned, and can't be as-is.** The verification code (hard-fail on mismatch)
  activates the moment `asset.SHA256` is set, but the URLs are rolling
  "latest"/"getrelease" endpoints whose bytes change on every upstream rebuild
  (BtbN republishes `latest` ~daily), so a static pin would break all
  downloads within days. Fixing this means either switching to immutable
  versioned URLs (BtbN dated `autobuild-*` tags, evermeet versioned URLs; but
  johnvansickle's linux/arm build has no clean immutable URL) or verifying
  against upstream's own published checksum at download time. Do not just
  paste hashes against the current URLs.
- **Awards data (Oscar/Emmy) is unavailable** because TMDB doesn't expose it. Cold
  start uses rating/revenue/country proxies instead. Real awards would need an
  extra source (e.g. OMDb).
- The recommendation **warm path is movie-focused**; TV appears in cold-start
  categories only.
- `goreleaser` and Docker builds are configured but were validated by
  cross-compile + `goreleaser check`-equivalent, not a full release/build in
  this environment.
- **`scan`/`doctor`/`admin` resolve `config.toml` per invoking user's
  `$HOME`/XDG dirs, same as the installed service does** (see
  docs/configuration.md) — so running one as a different user than the
  service (e.g. a systemd service installed via `sudo` runs as root; running
  `northrou scan` in your own shell resolves *your* config, not root's)
  silently operates on a config/database the service never reads. This is by
  design, not a bug to fix away, but it's a real footgun: all three commands
  now warn when something is already listening on the configured port
  (`internal/cli/scan.go`, `doctor.go`, `stubs.go`'s `admin` command).

## Watch-outs added by the client work

- **The tunnel's wire format is exact.** A data channel is SCTP
  (message-oriented) but `ServeConn` reads it as a stream with a 4-byte
  `io.ReadFull`, and pion returns `ErrShortBuffer` if the message is larger than
  the read. Frames must be sent as **two messages** (header, then payload), as
  Go's `writeFrame` does. Pinned by `internal/remote/framing_test.go`.
- **`admin: true` on `/api/me` is recomputed per request** (see the Auth row
  above), not carried in the token — the same device is admin on the LAN and
  read-only through the tunnel. "Not tunneled" alone is NOT enough: the box
  binds all interfaces by default, so a public-IP hit on the direct path must
  not be trusted. Show admin *controls* only when true; admin *reads* stay open
  to everyone.
- **`frontend/swift/` needs iOS 18+** to typecheck (the `Tab(_:systemImage:value:)`
  initialiser). It is a design reference, not an app: no `@main`, no project.
- **Native code lives in `frontend/plugins/`, never in `src-tauri/gen/`**, which
  is regenerated.
- **`welcome.html` (not `connect.html`, not `setup.html`) is the client's
  code-entry page.** Same-origin browsers skip it; a same-origin box that still
  needs setup gets a "run `northrou setup`" panel on the home page instead of a
  browser wizard - setup is terminal-only by design.
- **`movie_dirs`/`show_dirs` in `POST /api/setup/complete` are the one sanctioned
  API write of media folders** (local-only, once-ever): the wizard routes them
  through the daemon so they land in the daemon's own config.toml, which may not
  be the file the wizard process reads (root service vs. user shell). Everywhere
  else folders stay TUI/config-only.
- **Rotating the connection code revokes every refresh token** and bounces the
  remote peer (`App.RestartRemote`) so the coordinator learns the new code
  immediately. Don't decouple those: a rotation that leaves old devices
  connected, or that the coordinator hasn't heard about, is broken.
- **`/api/images/*` requires the same Bearer auth as everything else, so it
  can never be a plain `<img src="...">`** — a browser can't attach an
  `Authorization` header to an image tag; it'll 401 silently with no console
  error. Images are fetched through the authenticated client and handed to
  `<img>` as a `blob:` object URL instead (`frontend/js/api/images.js`,
  `setImageSrc`). Any new image-bearing field must go through this, never a
  direct `.src =` assignment.
- **Home rows (`GET /api/home`) ship a bare cache-relative `poster_path`**
  (e.g. `w500/x.jpg`, no prefix), while every other endpoint (movies/shows/
  similar/search) sends a ready-to-use `poster_url` (`/api/images/w500/x.jpg`).
  `library.js`'s `toCard()` normalizes both into a real URL - don't
  reintroduce a plain `??` fallback between them, they are not the same shape.

## Further reading

- [docs/architecture.md](docs/architecture.md) - full subsystem + data-flow detail
- [docs/api.md](docs/api.md) - HTTP API reference (the frontend contract)
- [docs/configuration.md](docs/configuration.md) - every config.toml field
- [docs/changelog.md](docs/changelog.md) - per-release notes
- [README.md](README.md) - install & usage
- [LICENSE](LICENSE) / [NOTICE](NOTICE) - BSD 3-Clause; Northrou name/branding is
  not part of the grant
