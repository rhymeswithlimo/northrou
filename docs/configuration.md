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
- **name** - the human-facing name for this server ("Living Room NAS"), chosen
  during `northrou setup` and shown to every client that pairs with it. Empty
  falls back to the machine's hostname.
- **bind_addr** - network interface to listen on. Empty binds all interfaces.
  **Admin actions are allowed only from a local connection** — one from loopback
  or a private/LAN address that is not the remote tunnel — so a request from a
  public IP never gets admin and must present the connection code to pair. Still,
  do not expose this port to the public internet: remote clients are meant to
  reach the box over the peer-to-peer tunnel, not the HTTP port. On a box with a
  public interface (a VPS/seedbox), set `bind_addr` to a LAN/private address or
  loopback. **Docker caveat:** Docker's default userland proxy rewrites the
  source IP of published-port traffic to the bridge gateway (a private address),
  which would make internet traffic *look* local; only publish `8674` on a
  trusted network (a home LAN behind a router), never straight to the internet.
- **port** - HTTP port (default `8674`).
- **data_dir** - where Northrou stores everything mutable: the SQLite database,
  cached images, the managed FFmpeg binaries, generated subtitles, and HLS
  transcode scratch space.

### `[media]`
- **movie_dirs** / **show_dirs** - folders the daemon scans automatically and
  that `northrou scan` uses when you give it no path.
- **preferred_audio_langs** / **preferred_subtitle_langs** - ordered lists of
  ISO-639 language codes (e.g. `["en"]`) that decide which audio track is played
  and which subtitle is turned on by default when a file has several. These are
  the **household default**; each viewer can override them per profile from the
  **Language** section of the settings page (their choice wins). Both default to
  English and are independent of `tmdb.language` (which only affects fetched
  metadata).

#### Recommended library layout

Northrou finds files at any nesting depth, but it reads titles, years, and
episode numbers most reliably when you follow these conventions:

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

- **Episodes** from `S01E01`, `s01e01`, `S01E01E02` (multi-episode), and `1x05`
  in the filename. If the filename has no marker, it falls back to a season
  folder (`Season 01`, `Season 1`, `S01`, `Series 1`, `Specials`), takes the
  show name from the folder above it (skipping container folders like `MKV/` or
  `Subs/`), and reads a loose `E07`/`Episode 7` from the name.
- **Movies** from `Title (Year)` or `Title.Year` in the filename; if the year is
  only in a parent folder (`2001 - Sorcerers Stone/…`), that is used too.
- **Duplicates** (the same title as both `.mkv` and `.mp4`, or in two folders)
  collapse to the single best copy, ranked by resolution, then bitrate, then
  container (mkv > mp4).
- **Subtitles** next to the video: a matching `.srt`/`.ass`, a `Subs/` folder
  (including per-episode subfolders), and language/`SDH`/`forced` tags in the
  subtitle filename.

If a file still will not match, correct it with
`northrou match <file> --tmdb-id <id>` (add `--tv --season N --episode N` for an
episode) or the admin `POST /api/admin/match` endpoint, rather than renaming by
trial and error. Add `--tv` to `northrou scan` to force everything under a path
to be treated as episodes.

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
- **server_id** / **connection_code** - generated during setup. The
  **connection code is the credential**: sharing it lets a device pair and sign
  in, so treat it like a password. Rotating it (edit the value and restart) stops
  new devices from pairing but leaves already-paired devices signed in. Retrieve
  it any time with `northrou cc`. Remote access always uses Northrou's official
  coordinator; there is no coordinator URL to configure.

### `[transcode]`
- **hw_accel** - force a specific acceleration backend, `none` for software, or
  empty to auto-detect.
- **allow_software_4k** - permit software 4K transcoding (not real-time on most
  hardware; off by default).
- **max_bitrate_kbps** - cap the highest adaptive-streaming rung for remote
  playback. `0` is uncapped.
- **tonemap** - apply HDR→SDR tone mapping when transcoding for SDR clients.
- **probe_dolby_vision** - run a second, frame-level ffprobe during scanning to
  recover the Dolby Vision profile when it is not in the stream metadata. Off by
  default (it reads a frame per file, so scans are a little slower); turn it on
  for DV-heavy libraries so dual-layer profile 7 is transcoded rather than
  mistaken for plain HDR. When a client supports AV1 and the box has a hardware
  AV1 encoder, transcodes automatically target AV1 (no setting needed).
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

### `[update]`
- **auto_update_disabled** - turn off the background self-update check. Off
  (feature enabled) by default: the server checks GitHub for a newer release
  every 6 hours and, once nothing is streaming, downloads, verifies, and
  applies it, then exits so the system service restarts into the new version.
  Never runs for a dev build or inside a container regardless of this
  setting; Docker/Podman deployments update by pulling a new image tag
  instead, since a self-replaced binary would be lost on the next
  `docker compose up`. `northrou update` still works manually any time.

## Environment overrides

- `NORTHROU_CONFIG_DIR` - directory holding `config.toml`.
- `NORTHROU_DATA_DIR` - overrides `server.data_dir`.
