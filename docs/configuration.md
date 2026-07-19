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
coordination_url = "https://app.northrou.sh"
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

[auth]
# Optional. Social sign-in is off unless oauth_issuer is set; the emailed pin
# needs no setup and works with no internet.
oauth_issuer    = ""            # coordination broker base URL
oauth_providers = ["google"]    # what to offer on the login screen

[tmdb]
api_key = "…"           # required for posters and metadata
language = "en-US"

[email]
# Sign-in pins are delivered through the coordination relay, so you do not have
# to configure anything here or run a mail server.
relay_url = "https://app.northrou.sh"     # hosted pin delivery (default)
# relay_token = "…"              # optional bearer token, if your relay requires one
# relay_disabled = true          # turn the relay off; pins are logged instead
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

These are the server's own filesystem paths, so they are set on the server, not
from a client: edit this file directly, or use the Library tab in
`northrou admin`, which checks each folder exists as you add it. The client apps
and settings page can trigger a scan but never choose the folders.

You do not have to configure these to scan on demand: point `northrou scan` at
any folder or drive directly, e.g. `northrou scan /media/movies` or
`northrou scan D:\media`. You can pass more than one path. Movies and TV
episodes are told apart by filename; add `--tv` to force everything under the
given paths to be treated as TV episodes, which helps for shows with messy
names that don't parse as episodes on their own.

### `[remote]`
- **enabled** - turn peer-to-peer remote access on/off. This governs the tunnel
  the client apps use; a browser opened against the box's own address on the same
  network reaches it directly regardless, since that is just the server serving
  its own pages.
- **coordination_url** - the signaling broker that lets remote devices find your
  server. Defaults to the hosted `app.northrou.sh`, so remote access works out
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

### `[auth]`
- **oauth_issuer** - the coordination broker that runs Google/Apple sign-in and
  signs the assertions this server verifies. Empty (the default) disables social
  sign-in entirely. The server never holds an OAuth client secret: it only ever
  verifies the broker's signature against its published JWKS.
- **oauth_providers** - which buttons the login screen offers, e.g.
  `["google", "apple"]`. Listing one the broker does not run just means that
  button fails, so keep it in step with the broker's own configuration.

Social sign-in is a shortcut, not a second way in. It proves control of an email
address, exactly as the pin does, and an identity that is not this server's
account address is refused.

### `[tmdb]`
- **api_key** - a free [TMDB](https://www.themoviedb.org/settings/api) key.
  Without it, files are scanned and probed but flagged as unmatched (no posters
  or rich metadata).
- **language** - metadata language, e.g. `en-US`, `de-DE`.

### `[email]`
Login is passwordless: users receive a one-time pin by email. Delivery has two
backends:

1. **The coordination relay** at `relay_url` (the default). It owns the mail
   infrastructure and the template, so you never run a mail server.
2. **The server log** (WARN level), if the relay is disabled. Local, single-box
   use only. Never rely on this on a box others can reach.

There is deliberately no SMTP option. Running mail is the one part of
self-hosting that reliably fails (SPF/DKIM/DMARC, IP reputation, port 25 blocked
by most ISPs), and a sign-in code that lands in spam locks you out of your own
server. Delivery is the relay's job; if you want mail fully in-house, run your
own relay and point `relay_url` at it. Out of the box you configure nothing.

- **relay_url** - pin-delivery service. Defaults to `https://app.northrou.sh`.
  The relay delivers the email; it never sees or stores your account, library, or
  media. Email is readable in transit by any mail hop, so the relay operator can
  technically see codes and recipient addresses. If that matters to you, run your
  own relay and point this at it.
- **relay_token** - optional bearer token, if your relay requires one.
- **relay_disabled** - set `true` to never use the relay (pins are logged
  instead).

## Environment overrides

- `NORTHROU_CONFIG_DIR` - directory holding `config.toml`.
- `NORTHROU_DATA_DIR` - overrides `server.data_dir`.
