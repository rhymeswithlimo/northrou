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

[tmdb]
api_key = "…"           # required for posters and metadata
language = "en-US"
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
- **coordination_url** - the signaling broker. Use the public one or your own.
- **self_hosted_coordinator** - informational flag for self-hosters.
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

### `[tmdb]`
- **api_key** - a free [TMDB](https://www.themoviedb.org/settings/api) key.
  Without it, files are scanned and probed but flagged as unmatched (no posters
  or rich metadata).
- **language** - metadata language, e.g. `en-US`, `de-DE`.

## Environment overrides

- `NORTHROU_CONFIG_DIR` - directory holding `config.toml`.
- `NORTHROU_DATA_DIR` - overrides `server.data_dir`.
