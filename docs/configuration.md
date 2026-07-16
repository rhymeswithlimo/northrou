# Configuration reference

Northrou reads a single TOML file. Its location depends on your OS:

| OS | Path |
|---|---|
| Linux | `$XDG_CONFIG_HOME/northrou/config.toml` (or `~/.config/northrou/config.toml`) |
| macOS | `~/Library/Application Support/northrou/config.toml` |
| Windows | `%ProgramData%\northrou\config.toml` |

Override the location with `--config /path/to/config.toml` or the
`NORTHROU_CONFIG_DIR` environment variable. The setup wizard writes this file
for you; edit it directly for advanced tweaks and restart the service.

This file is for the person running the server; the client apps need nothing
from it.

## Example

```toml
[server]
bind_addr = ""          # "" = all interfaces
port = 8674
data_dir = "/var/lib/northrou"   # database, images, ffmpeg, subtitles, HLS scratch

[media]
movie_dirs = ["/media/Movies"]
show_dirs  = ["/media/TV"]

[remote]
enabled = true
coordination_url = "https://coord.northrou.app"
self_hosted_coordinator = false
server_id = "…"         # generated at setup
connection_code = "NR-XXXX-XXXX"  # share with your devices

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

[email]
# By default, sign-in pins are delivered through the hosted relay, so you do
# not have to configure anything here. Override with your own SMTP if you want
# mail to leave your own server.
relay_url = "https://relay.northrou.app"  # hosted pin delivery (default)
# relay_disabled = true          # turn the relay off (use SMTP below, or log fallback)
# smtp_host = "smtp.example.com" # your own mail server; takes precedence over the relay
smtp_port = 587                  # 587 STARTTLS (default), 465 implicit TLS, 25 plain
smtp_username = "northrou@example.com"
smtp_password = "…"
from_address = "northrou@example.com"  # defaults to smtp_username
from_name = "Northrou"
```

## Fields

### `[server]`
- **bind_addr** - network interface to listen on. Empty binds all interfaces.
- **port** - HTTP port (default `8674`).
- **data_dir** - where Northrou stores everything mutable: the SQLite database,
  cached images, the managed FFmpeg binaries, generated subtitles, and HLS
  transcode scratch space.

### `[media]`
- **movie_dirs** / **show_dirs** - folders the daemon scans automatically and
  that `northrou scan` uses when you give it no path. TV shows should follow a
  `Show Name/Season 01/Show.S01E01…` layout.

You do not have to configure these to scan on demand: point `northrou scan` at
any folder or drive directly, e.g. `northrou scan /media/movies` or
`northrou scan D:\media`. You can pass more than one path. Movies and TV
episodes are told apart by filename; add `--tv` to force everything under the
given paths to be treated as TV episodes, which helps for shows with messy
names that don't parse as episodes on their own.

### `[remote]`
- **enabled** - turn peer-to-peer remote access on/off. Local-network access
  works regardless, by connecting to the server's LAN address directly.
- **coordination_url** - the signaling broker that lets remote devices find your
  server. Defaults to the hosted `coord.northrou.app`, so remote access works out
  of the box with nothing extra to run. Point it at your own coordinator only if
  you want to self-host the signaling too (advanced).
- **self_hosted_coordinator** - informational flag noting you run your own
  coordinator instead of the hosted default.
- **server_id** / **connection_code** - generated during setup. The connection
  code is what you share with remote devices.

### `[transcode]`
- **hw_accel** - force a specific acceleration backend, `none` for software, or
  empty to auto-detect.
- **allow_software_4k** - permit software 4K transcoding (not real-time on most
  hardware; off by default).
- **max_bitrate_kbps** - cap the highest adaptive-streaming rung for remote
  playback. `0` is uncapped.
- **tonemap** - apply HDR→SDR tone mapping when transcoding for SDR clients.
- **prefer_system_ffmpeg** - use a system-installed FFmpeg instead of the managed
  download (recommended in Docker and on Apple Silicon with a native Homebrew
  FFmpeg).
- **max_transcodes** - cap how many expensive transcodes run at once. `0` (the
  default) derives the cap from the detected hardware, which is almost always
  right; set it only to protect a box that shares its CPU with other work.
  Direct play and remux are stream copies and never count against it, so
  lowering this does not restrict cheap playback. Requests over the cap get
  `503` with `Retry-After` rather than queueing. Editable from Server admin.

### `[tmdb]`
- **api_key** - a free [TMDB](https://www.themoviedb.org/settings/api) key.
  Without it, files are scanned and probed but flagged as unmatched (no posters
  or rich metadata).
- **language** - metadata language, e.g. `en-US`, `de-DE`.

### `[email]`
Login is passwordless: users receive a one-time pin by email. Delivery picks
exactly one backend, in this order:

1. **Your own SMTP**, if `smtp_host` is set.
2. **The hosted relay** at `relay_url` (the default), otherwise.
3. **The server log** (WARN level), if the relay is disabled and no SMTP is set.
   Local, single-box use only. Never rely on this on a box others can reach.

Out of the box you configure nothing here and pins go through the hosted relay.

- **relay_url** - hosted pin-delivery service. Defaults to
  `https://relay.northrou.app`. The relay delivers the email; it never sees or
  stores your account, library, or media. Email is readable in transit by any
  mail hop, so the relay operator can technically see codes and recipient
  addresses. If that matters to you, run your own SMTP (below) or your own relay.
- **relay_token** - optional bearer token, if your relay requires one.
- **relay_disabled** - set `true` to never use the relay.
- **smtp_host** - your own mail server hostname. When set, it takes precedence
  over the relay and pins leave your server directly.
- **smtp_port** - `587` for STARTTLS submission (default), `465` for implicit
  TLS, `25` for plaintext relays.
- **smtp_username** / **smtp_password** - credentials for the mail server. Leave
  both empty for an unauthenticated relay.
- **from_address** - the `From:` address on pin emails. Defaults to
  `smtp_username`.
- **from_name** - optional display name on the `From:` header.

## Environment overrides

- `NORTHROU_CONFIG_DIR` - directory holding `config.toml`.
- `NORTHROU_DATA_DIR` - overrides `server.data_dir`.
