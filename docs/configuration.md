# Configuration reference

Northrou reads a single TOML file. Its location depends on your OS:

| OS | Path |
|---|---|
| Linux | `$XDG_CONFIG_HOME/northrou/config.toml` (or `~/.config/northrou/config.toml`) |
| macOS | `~/Library/Application Support/northrou/config.toml` |
| Windows | `%ProgramData%\northrou\config.toml` |

Override the location with `--config /path/to/config.toml` or the
`NORTHROU_CONFIG_DIR` environment variable. The setup wizard writes this file
for you; edit it directly for advanced tweaks and restart the service. It's
for the person running the server — client apps need nothing from it.

## Example

```toml
[server]
name = "Living Room NAS"         # shown to every device that pairs
bind_addr = ""          # "" = all interfaces
port = 8674
data_dir = "/var/lib/northrou"   # database, images, ffmpeg, subtitles, HLS scratch

[media]
movie_dirs = ["/media/Movies"]
show_dirs  = ["/media/TV"]

[remote]
enabled = true
server_id = "…"         # generated at setup
connection_code = "NR-XXXXX-XXXXX"  # the credential; share with your devices

[transcode]
hw_accel = ""           # "" = auto-detect; or nvenc|qsv|videotoolbox|amf|vaapi|none
allow_software_4k = false
max_bitrate_kbps = 0    # 0 = uncapped; caps the top HLS rung for remote streams
tonemap = true          # HDR -> SDR tone mapping when transcoding for SDR clients
prefer_system_ffmpeg = false
max_transcodes = 0      # 0 = auto (derived from detected hardware)

[tmdb]
api_key = "…"           # required for posters and metadata
language = "en-US"

[update]
auto_update_disabled = false   # true turns off background self-update
```

## Fields

### `[server]`
- **name** — human-facing name for this server, chosen during `northrou setup`
  and shown to every client that pairs. Empty falls back to the hostname.
- **bind_addr** — interface to listen on; empty binds all interfaces. Admin
  actions require a local connection (loopback or private/LAN, not the
  tunnel), so a public-IP request never gets admin and must present the
  connection code to pair. Still, don't expose this port to the internet —
  remote clients are meant to reach the box over the peer-to-peer tunnel. On a
  box with a public interface (VPS/seedbox), set this to a LAN/private address
  or loopback. **Docker caveat:** the default userland proxy rewrites the
  source IP of published-port traffic to the bridge gateway (a private
  address), making internet traffic *look* local — only publish `8674` on a
  trusted network, never straight to the internet.
- **port** — HTTP port (default `8674`).
- **data_dir** — where Northrou stores everything mutable: the SQLite
  database, cached images, managed FFmpeg binaries, generated subtitles, and
  HLS transcode scratch space.

### `[media]`
- **movie_dirs** / **show_dirs** — folders the daemon scans automatically and
  that `northrou scan` uses when given no path.
- **preferred_audio_langs** / **preferred_subtitle_langs** — ordered ISO-639
  code lists (e.g. `["en"]`) deciding default audio/subtitle track when a file
  has several. This is the **household default**; each viewer can override it
  per profile in the settings page's **Language** section (their choice
  wins). Both default to English and are independent of `tmdb.language`
  (which only affects fetched metadata).

#### Recommended library layout

Northrou finds files at any nesting depth, but reads titles, years, and
episode numbers most reliably with:

```
Movies/
  Movie Title (2020)/
    Movie Title (2020).mkv
TV/
  Show Name/
    Season 01/
      Show Name - S01E01 - Episode Title.mkv
```

What the scanner recovers automatically:

- **Episodes** from `S01E01`, `s01e01`, `S01E01E02` (multi-episode), and
  `1x05` in the filename. Without a marker, it falls back to a season folder
  (`Season 01`, `S01`, `Series 1`, `Specials`), takes the show name from the
  folder above it (skipping container folders like `MKV/`), and reads a loose
  `E07`/`Episode 7` from the name.
- **Movies** from `Title (Year)` or `Title.Year` in the filename, or a
  year-bearing parent folder if that's where it lives.
- **Duplicates** (same title as `.mkv` and `.mp4`, or in two folders) collapse
  to the single best copy, ranked by resolution, then bitrate, then container
  (mkv > mp4).
- **Subtitles** next to the video: a matching `.srt`/`.ass`, a `Subs/` folder
  (including per-episode subfolders), and language/`SDH`/`forced` tags in the
  filename.

A file that still won't match can be corrected with
`northrou match <file> --tmdb-id <id>` (add `--tv --season N --episode N` for
an episode) or the admin `POST /api/admin/match` endpoint, rather than
renaming by trial and error. Add `--tv` to `northrou scan` to force everything
under a path to be treated as episodes.

These are the server's own filesystem paths, so they're set on the server, not
from a client: edit this file directly, or use the Library tab in
`northrou admin`, which checks each folder exists as you add it. Client apps
can trigger a scan but never choose the folders.

You don't have to configure these to scan on demand — point `northrou scan` at
any folder or drive directly (`northrou scan /media/movies`,
`northrou scan D:\media`, multiple paths accepted). Add `--tv` to force
everything under the given paths to TV, which helps for shows with messy names
that don't parse as episodes on their own.

### `[remote]`
- **enabled** — turn peer-to-peer remote access on/off. This governs the
  tunnel the client apps use; a browser on the box's own network reaches it
  directly regardless, since that's the server serving its own pages.
- **server_id** / **connection_code** — generated during setup. The
  **connection code is the credential** — sharing it lets a device pair and
  sign in, so treat it like a password. Rotating it (edit and restart) stops
  new devices from pairing but leaves already-paired devices signed in;
  retrieve it any time with `northrou cc`. Remote access always uses
  Northrou's official coordinator — there's no coordinator URL to configure.

### `[transcode]`
- **hw_accel** — force a specific acceleration backend, `none` for software,
  or empty to auto-detect.
- **allow_software_4k** — permit software 4K transcoding (not real-time on
  most hardware; off by default).
- **max_bitrate_kbps** — cap the highest adaptive-streaming rung for remote
  playback. `0` is uncapped.
- **tonemap** — apply HDR→SDR tone mapping when transcoding for SDR clients.
- **probe_dolby_vision** — run a second, frame-level ffprobe during scanning
  to recover the Dolby Vision profile when it's missing from stream metadata.
  Off by default (adds a per-file frame read, slowing scans slightly); turn it
  on for DV-heavy libraries so dual-layer profile 7 transcodes correctly
  instead of being mistaken for plain HDR. When a client supports AV1 and the
  box has a hardware AV1 encoder, transcodes target AV1 automatically.
- **prefer_system_ffmpeg** — use a system-installed FFmpeg instead of the
  managed download (recommended in Docker and on Apple Silicon with a native
  Homebrew FFmpeg).
- **max_transcodes** — cap concurrent expensive transcodes. `0` (default)
  derives the cap from detected hardware, which is almost always right; set it
  only to protect a box sharing its CPU with other work. Direct play and remux
  are stream copies and never count against it. Requests over the cap get
  `503` with `Retry-After` rather than queueing. Editable from Server admin.

### `[tmdb]`
- **api_key** — a free [TMDB](https://www.themoviedb.org/settings/api) key.
  Without it, files are scanned and probed but flagged as unmatched (no
  posters or rich metadata).
- **language** — metadata language, e.g. `en-US`, `de-DE`.

### `[update]`
- **auto_update_disabled** — turn off the background self-update check. Off
  (feature enabled) by default: the server checks GitHub for a newer release
  every 6 hours and, once nothing is streaming, downloads, verifies, and
  applies it, then exits so the system service restarts into the new version.
  Never runs for a dev build or inside a container regardless of this
  setting — Docker/Podman deployments update by pulling a new image tag
  instead, since a self-replaced binary would be lost on the next
  `docker compose up`. `northrou update` still works manually any time.

## Environment overrides

- `NORTHROU_CONFIG_DIR` — directory holding `config.toml`.
- `NORTHROU_DATA_DIR` — overrides `server.data_dir`.

## Deploying the coordination server (maintainer only)

This section is the runbook for standing up Northrou's *public* coordination
server — maintainer infrastructure, not something a self-hoster runs. Servers
use the official coordinator automatically; there is no self-hosting path for
it. Skip this unless you're standing up `coord.northrou.sh` itself.

The coordinator relays only WebRTC signaling (SDP + ICE) so clients and home
servers can hole-punch a direct peer-to-peer connection; it never sees media
and holds no accounts or secrets (authentication is the connection code,
verified at the box, not here). The setup below assumes one small box at
`coord.northrou.sh`, with the web client hosted separately on Cloudflare Pages
at `app.northrou.sh` so the two never fight over a hostname.

```
  clients / home servers            Vultr box (Ubuntu LTS)
        │  wss/https        ┌──────────────────────────────────────┐
        └───────────────────►  Caddy :80/:443  (auto Let's Encrypt) │
                            │     └─► coordinator :9000  (signaling) │
                            └──────────────────────────────────────┘
```

Only Caddy is exposed on 80/443; the coordinator listens on plain HTTP inside
the Docker network and serves `/ws`, `/healthz`, `/stats`.

**1. The box.** Ubuntu LTS, smallest plan (the coordinator is stateless and
tiny).

```sh
apt update && apt upgrade -y
curl -fsSL https://get.docker.com | sh
apt install -y ufw git
ufw allow OpenSSH && ufw allow 80 && ufw allow 443 && ufw --force enable
```

The compose file only publishes Caddy, so port 9000 stays internal by simply
not mapping it (Docker manipulates iptables directly).

**2. DNS.** One `A` record, `coord` → the box's public IPv4 (`AAAA` too for
IPv6). `app` is not a box record — Cloudflare Pages owns it (see below). If
DNS is on Cloudflare, set `coord` to DNS-only (grey cloud) so Caddy's
automatic Let's Encrypt can answer ACME on port 80 directly. Verify:
`dig +short coord.northrou.sh` returns the box's IP.

**3. Deploy.**

```sh
git clone https://github.com/rhymeswithlimo/northrou.git && cd northrou
docker compose -f deploy.yml up -d --build
docker compose -f deploy.yml logs -f   # watch for "coordination server listening" and cert issuance
```

No secrets to fill in — the coordinator takes only `COORD_ADDR` (defaulted),
and `Caddyfile` already proxies `coord.northrou.sh`.

**4. Verify.**

```sh
curl https://coord.northrou.sh/healthz   # -> ok
curl https://coord.northrou.sh/stats     # -> {"servers":N,"sessions":N}

# --http1.1: curl negotiates HTTP/2 by default, which doesn't do the old
# Connection:Upgrade handshake (you'd see 426 instead of 101).
# --max-time: a successful upgrade leaves the connection open, so without
# it curl just hangs after printing the response.
curl -i -N --http1.1 --max-time 5 -H "Connection: Upgrade" -H "Upgrade: websocket" \
  -H "Sec-WebSocket-Version: 13" -H "Sec-WebSocket-Key: x3JJHMbDL1EzLkh9GBhXDw==" \
  https://coord.northrou.sh/ws           # -> 101
```

**5. Auto-update on release (optional).** By default the box sits on whatever
commit was cloned. `scripts/coordination-autoupdate.sh` + its systemd timer
track **published GitHub releases only** (never every push to `main`): it
polls the GitHub releases API, and only when the tag differs from what's
checked out does it `git checkout` and rebuild.

```sh
apt install -y jq
cp scripts/coordination-autoupdate.service scripts/coordination-autoupdate.timer /etc/systemd/system/
systemctl daemon-reload
systemctl enable --now coordination-autoupdate.timer
```

Until the first release is cut, every run is a no-op. Trigger a check on
demand with `systemctl start coordination-autoupdate.service`.

**The web client on Cloudflare Pages.** The Vite app in `frontend/` is hosted
at `app.northrou.sh`, separate from this box — a static build that reaches
servers over the tunnel, needing no server of its own. GitHub Actions builds
and deploys it only when a release is published
(`.github/workflows/deploy-web.yml`). One-time setup: create a Cloudflare
Pages "Direct Upload" project named `northrou-web`, add `app.northrou.sh` as
its custom domain, create a Cloudflare Pages API token, and add
`CLOUDFLARE_API_TOKEN` / `CLOUDFLARE_ACCOUNT_ID` as GitHub repo secrets. The
client's built-in coordinator URL is `wss://coord.northrou.sh/ws`, so bring
coordination up before shipping a client that points at it.

**Things to know:**

- **No TURN server.** Both sides use only public STUN
  (`stun.l.google.com:19302`, hardcoded in `backend/internal/remote/peer.go`
  and `frontend/js/api/tunnel.js`). That clears most home NATs, but symmetric
  NAT (some carrier-grade/mobile/corporate networks) fails to hole-punch, with
  no relay fallback. Fixing this means running
  [coturn](https://github.com/coturn/coturn) and making the ICE server list
  configurable — currently hardcoded on both ends.
- **The coordinator is a code-validity oracle**, so its `connect` handler is
  rate-limited per client IP and globally
  (`coordination/internal/broker/limiter.go`). It sees real client IPs via
  `X-Forwarded-For` from Caddy — keep that header trustworthy (don't expose
  9000 directly).
