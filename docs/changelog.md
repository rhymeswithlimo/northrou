# Changelog

Notable changes to Northrou, one section per release. Each release entry uses
whichever of these apply — a release with nothing to fix just omits that
heading:

- **Added** - new features
- **Fixed** - bug fixes
- **Improved** - changes to existing behavior that aren't new features or fixes

`scripts/release.sh --publish` pulls the section matching the tag it's cutting
and uses it as the GitHub release body, so an entry needs to exist here
*before* publishing a version — the release fails otherwise. Write it as you
land the change, not after the fact.

## v0.1.6 - Unreleased

### Added

- **App clients** now build and release. APKs and desktop. IOS still needs to be implemented.
- `northrou cc` prints this server's connection code (the code apps and the web
  client use to pair), so you don't have to dig it out of config.toml. Aliases:
  `code`, `connection-code`. On a box running the system service (as root), run
  it with `sudo`.

### Fixed
- A box set up before the hosting split kept dialing the old coordinator host,
  so remote pairing failed with "no server registered for that code" and pins
  stopped delivering. A stale `coordination_url`/`relay_url` pointing at the old
  single host is now migrated to the current coordinator automatically on load
  (a custom self-hosted coordinator is left untouched).
- Displayed error messages now read consistently - capitalized and ending in a
  full stop - whatever their source, including raw coordinator/backend strings
  that are lowercase by Go convention.
- Remote signaling now sends WebSocket keepalive pings (both the coordinator and
  the box), so an idle-timeout proxy in front of the coordinator (e.g. Cloudflare
  closes idle WebSockets after ~100s) can't silently drop a box's registration
  while it sits idle between pairings.

## v0.1.5 - 2026-07-21

### Added
- **External subtitles.** Northrou now discovers subtitle files that sit next to
  a video, not just tracks embedded in the container: a matching `.srt`/`.ass`, a
  `Subs/` folder (including one per-episode subfolder each), loosely named files
  in a single-video folder (`English.srt`, `Latin American.spa.srt`), and VobSub
  `.sub`. Language, `SDH`, and `forced` are read from the filename, and non-UTF-8
  subtitles (the common cp1252/Latin-1 scene releases) are transcoded so accents
  no longer mojibake.
- **Language settings.** A new Language section on the settings page picks the
  preferred audio and subtitle language (default English). The server plays the
  preferred-language audio track when a file has several, skipping commentary
  tracks, and preselects the matching subtitle.
- **Manual match.** `northrou match <file> --tmdb-id <id>` and the admin
  `POST /api/admin/match` endpoint force a file to a specific TMDB title when it
  will not auto-match or matched wrong, so a stubborn filename is never a dead
  end.
- **Per-profile language.** Each viewer sets their own preferred audio and
  subtitle language in Settings (Netflix-style); it drives which audio track
  plays and which subtitle turns on by default, overriding the server default.
- **Dolby Vision profiles.** Northrou now reads the DV profile: cross-compatible
  profile 8.1/8.4 plays as HDR10/HLG on HDR clients, DV-native clients get any
  profile direct, and dual-layer profile 7 is transcoded instead of shipped as
  unplayable "HDR". Optional `probe_dolby_vision` recovers the profile from
  frame data for libraries that need it.
- **AV1 transcoding.** When a client supports AV1 and the box has a hardware AV1
  encoder, transcodes target AV1 for far better quality per bitrate (notably on
  remote streams); everything else still gets H.264.
- **DVD/VobSub subtitle OCR.** Embedded `dvd_subtitle` tracks and external
  `.idx`/`.sub` pairs are now OCR'd to WebVTT via Tesseract, like PGS.
- **Fix match UI.** The settings page lists titles the scanner couldn't identify
  and lets you search TMDB by name and link the right one, right from the app.

### Fixed
- Files with embedded cover art (a poster image muxed as a video stream) could
  have the thumbnail chosen as the main video and trigger a pointless transcode.
  The real video stream is now always selected.
- Duplicate copies of one title (the same movie as `.mkv` and `.mp4`, or in two
  folders) collapsed to a nondeterministic winner and left orphaned rows behind.
  Northrou now keeps the best copy deterministically (resolution, then bitrate,
  then container) and prunes the rest.
- Deleting a media file now removes its title on the next scan, and deleting the
  better of two duplicate copies promotes the remaining one instead of leaving a
  dead entry. (Also fixed a bug where orphaned media-file rows were never pruned
  because the cleanup ran with an already-cancelled context.)

### Improved
- Episode detection is more forgiving of real-world layouts: `S01`/`Season 1`/
  `Series 1`/`Specials` folders, an intermediate `MKV/`-style folder between the
  season and the files, the show name taken from a parent folder, a loose
  `E07`/`Episode 7`, single-digit `1x5`, and a movie year found only in a parent
  folder. 10-bit vs 8-bit video is now captured from ffprobe.

## v0.1.4 - 2026-07-21

### Added
- Hosted web client at `app.northrou.sh`. People can sign in and stream from any
  browser, not just the desktop app - it pairs to a box over the tunnel the same
  way. It's a static build hosted on Cloudflare Pages and deployed from GitHub on
  each release (`.github/workflows/deploy-web.yml`). A browser loading the client
  now correctly reaches the box over the tunnel instead of assuming it was served
  by the box; setup and install mention the from-anywhere URL.

### Improved
- The default coordination host moved from `app.northrou.sh` to
  `coord.northrou.sh`, freeing `app.northrou.sh` for the hosted web client above.
  Boxes and clients now default to `wss://coord.northrou.sh/ws` for signaling and
  `https://coord.northrou.sh` for pin delivery. Self-hosters pointing at their own
  coordinator are unaffected.

### Fixed
- Login pins and admin codes now self-heal against a stale relay token. When a
  box is pointed at the hosted relay it always uses the shared client token, so
  a box left with a wrong `relay_token` from earlier manual config no longer
  gets a silent 401 forever with no code ever delivered. Previously the shared
  token was only filled in when `relay_token` was empty.
- The client no longer flashes the wrong page before a boot-time redirect
  (e.g. opening the app URL while signed out briefly showed the home screen
  before bouncing to sign-in). Every page now starts hidden and reveals itself
  only once its boot code decides to stay; a redirect happens on a blank screen.
  No-JS visitors and a missed reveal both degrade gracefully (shown, not stuck
  blank).
- `northrou serve` no longer dies with a raw "address already in use" when the
  system service is already running. Like `northrou setup`, it now detects the
  running Northrou and prints the URL(s) to open instead. If the port is held
  by a *different* program, it says so and tells you how to move Northrou off
  it (set `port` under `[server]` in config.toml, with a free port suggested),
  rather than surfacing the bind error. `setup` gets the same foreign-port
  guidance.
- OTP code entry: pasting or autofilling a 6-digit code (the natural way to
  enter one) left the Unlock / Verify button disabled, because filling the
  boxes in code doesn't emit an input event and the button's enable check never
  re-ran. It now does. Affected admin unlock and could affect sign-in.
- Settings: the current profile is now marked `(you)` instead of "This profile".

## v0.1.3 - 2026-07-20

### Fixed
- Opening the server's address in a browser on a not-yet-configured box now
  sends you to the setup wizard instead of loading the app, which failed every
  request and showed a misleading "Couldn't reach your server". The home page
  now checks first-run status on boot: a box with no account yet (same-origin)
  goes to setup, and a configured box this device isn't signed into goes to
  sign-in. This also fixes the same wrong error when opening the LAN address
  from a second device (e.g. a phone) that has no session yet.
- Setup wizard polish: the final "Open my library" button now fills the width
  of its section instead of shrinking to its label, and the metadata (TMDB API
  key) step now shows the Northrou logo like the other steps.
- Desktop app: remote sign-in with a connection code failed with "Could not
  reach the coordination server: The operation is insecure". The app's Content
  Security Policy `connect-src` did not allow the coordinator, so the signaling
  WebSocket (`wss://app.northrou.sh`) was blocked by the webview before it
  opened. Allowed the coordinator origin. (A self-hoster pointing at a custom
  coordinator still needs to add its origin to the CSP and rebuild.)
- Login pins now deliver out of the box. A fresh install talks to the hosted
  relay with the shared client token it now ships by default, instead of
  presenting no token and getting a silent HTTP 401 (the relay treats the token
  as a scan-deterrent, with per-recipient rate limiting as the real anti-abuse
  control). A self-hoster on their own relay still sets a private `relay_token`.
- A failed login-pin delivery is no longer silent either. When the relay
  rejects a send it is now logged at ERROR with the reason and remedy, instead
  of a single easily-missed WARN.

### Improved
- Northrou's own operator-facing CLI messages (the setup wizard URLs, service
  and update status) are now highlighted so they stand out from interleaved log
  lines instead of blending in. Colour is used only on a real terminal and is
  suppressed under `NO_COLOR` or when output is piped, so journald/systemd logs
  stay clean.

## v0.1.2 - 2026-07-20

### Fixed
- Self-update no longer randomly fails with "northrou not found in archive".
  The release ships the `coordinator_*` and `relay_*` archives alongside the
  server, and they share the same `_<os>_<arch>` suffix; the updater matched on
  that suffix alone against a map of assets (random iteration order), so it
  picked the wrong archive on roughly a third of runs and then couldn't find
  the `northrou` binary inside. It now anchors on the `northrou_` archive-name
  prefix. Affected both `northrou update` and the background auto-updater.
- Privileged commands (`update`, `install`, `uninstall`) now tell you to
  re-run with `sudo` when they fail for lack of permission, instead of dumping
  a raw `permission denied` syscall error. Running `northrou update` as a
  normal user (the binary lives in root-owned `/usr/local/bin`) previously
  failed with an opaque `apply update: open /usr/local/bin/.northrou.new:
  permission denied` and no hint. The guidance is reactive - a non-root
  install into `~/.local/bin` still self-updates without sudo and is not
  nagged. The post-update restart hint also now includes `sudo`.
- CLI errors now print as a plain `Error: <message>` on stderr instead of a
  developer-facing `level=ERROR msg="command failed" err=...` log line.

## v0.1.1 - 2026-07-20

### Added
- Automatic self-update: the server now checks GitHub releases every 6 hours
  and, once nothing is streaming, downloads, verifies, and applies a newer
  release itself, then restarts into it. Off for dev builds and inside
  containers (where updates come from a new image instead); disable
  explicitly with `update.auto_update_disabled` in config.toml. `northrou
  update` still works as a manual, on-demand check/apply.

### Fixed
- `northrou setup` no longer crashes with a raw "address already in use"
  error when the system service installed by `install.sh` (which starts
  immediately, since the install script runs it as root on Linux) is already
  running. It now detects the running instance and points you at the browser
  URL instead. `install.sh`'s final message is also now conditional on
  whether it actually started the service, instead of always telling you to
  run a command that would fail.
- `northrou setup` now prints the wizard's URL(s) plainly to stdout instead of
  only logging it at WARN level when auto-opening a browser fails (the normal
  case on a headless box). It also lists the machine's actual LAN addresses,
  not just `localhost` - which was actively wrong guidance for the headless
  case, since "localhost" means the server itself, not the device you're
  reading the message on.

## v0.1.0 - 2026-07-19

### Added
- Self-hosted media server for a personal physical-media library (ripped
  BluRay/DVD, 4K HEVC/TrueHD Atmos), with a filename-parse → TMDB → ffprobe
  scanning pipeline
- Streaming decision cascade — direct play → remux → audio-only transcode →
  full transcode — with hardware-acceleration detection and an
  admission-gated concurrent-transcode cap
- Subtitles: SRT/ASS → WebVTT conversion, plus a PGS `.sup` decoder with
  optional Tesseract OCR
- A local recommendation engine: a time-decayed, completion-weighted taste
  profile driving personalized rows, and library-composition cold-start
  category rows across movies and TV
- Passwordless sign-in: one account email with Netflix-style profiles,
  emailed one-time pins, and optional Google/Apple social sign-in brokered
  through the coordination server
- Admin as an OTP-proven elevation rather than a fixed role — every profile
  can administer once it proves it with a second emailed code
- Peer-to-peer remote access: a WebRTC signaling broker plus an
  HTTP-over-datachannel tunnel, so a household's media never passes through
  a third-party server
- `northrou admin`, a bubbletea TUI for local administration
- Self-update via GitHub releases, and a managed FFmpeg/ffprobe download so
  nothing needs installing by hand
- Client apps for desktop, iOS, and Android built with Tauri, sharing one
  Vite web client embedded directly into the server binary
- Cross-compiles to all 6 targets (linux/darwin/windows × amd64/arm64, plus
  linux armv7) on pure-Go SQLite, with no CGo dependency
- A hosted coordination stack — signaling, OAuth broker, and pin-delivery
  relay — at `app.northrou.sh`, so remote access and sign-in work with
  nothing to self-host
