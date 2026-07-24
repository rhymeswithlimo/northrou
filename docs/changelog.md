# Changelog

Notable changes to Northrou, one section per release. Every entry is built
from exactly these three headings, always in this order, and never any
other heading — a release with nothing to fix just omits that one:

1. **Added** - new features
2. **Fixed** - bug fixes
3. **Improved** - changes to existing behavior that aren't new features or fixes

`scripts/release.sh --publish` pulls the section matching the tag it's cutting
and uses it as the GitHub release body, so an entry needs to exist here
*before* publishing a version — the release fails otherwise. Write it as you
land the change, not after the fact.

## v0.1.10 - Unreleased

### Fixed

- **The home screen could get stuck loading forever in the desktop/mobile apps.**
  The content rows were built and shown only after the featured hero finished
  loading its backdrop image, so a hero image that stalled (observed over the
  peer-to-peer tunnel) left the whole page on its skeleton animation
  indefinitely, even though the actual library data had already arrived. Rows
  now render first and the hero loads separately afterward. The hero image
  itself is also more likely to actually load: it's attached to the document
  (hidden) before the backdrop starts loading rather than after, since a
  detached `<img>` fed a blob: URL never reliably fired load/error in the
  app's WebView - plus a timeout so even a stuck decode can't block the hero
  layer indefinitely.

## v0.1.9 - 2026-07-24

### Added

- **The Play button plays.** The web player is here: press Play (or an episode,
  or a Continue Watching title) and it opens full screen and starts. It picks the
  cheapest path your browser can handle - playing the file directly when it can,
  transcoding on the fly (adaptive HLS) when it can't - reads your capabilities
  from the browser so it won't hand you something that decodes to a black screen,
  resumes where you left off, shows subtitles, and remembers your position as you
  watch. Works in any current browser (Chrome, Firefox, Edge, Safari) served from
  the box or on your home network. (Playback in the desktop/mobile apps, which
  reach the server over the peer-to-peer tunnel, is a separate piece still to
  come.)
- **The featured hero on the home screen now rotates.** It fades to a different,
  randomly chosen movie or show from your library every 24 seconds instead of
  sitting on one fixed pick.
- **Title logos on the detail screen.** When TMDB has a logo (the stylized title
  treatment) for a movie or show, the detail view shows it in place of the plain
  text title, and falls back to the text when there isn't one. Existing titles
  pick up their logos on the next scan.

### Fixed

- **Remote connection codes work again.** Pairing a device from outside your home
  network failed with "No server registered for that code": the coordinator
  matched the code character for character, so the dashes in `NR-XXXXX-XXXXX` had
  to line up exactly against the dash-stripped code the app sends. It now ignores
  dashes, spaces, and case on both ends.
- **`northrou cc` prints the code the running server is actually using.** On a box
  where Northrou runs as a system service, `cc` could read a different config file
  and show a stale code while `northrou admin` and `cc rotate` showed the real
  one. It now asks the running server first.
- **`northrou admin` rotates the code on `r`.** The Remote tab's rotate action
  only fired on a capital `R`; a lowercase `r` just refreshed the view.
- **The web client's code box groups the full code.** It was grouping in blocks of
  four, which no longer matches the `NR-XXXXX-XXXXX` format.
- **Hardware acceleration is detected correctly instead of guessed.** The server
  reported whichever encoders were *compiled into* ffmpeg, so a box with an Intel
  iGPU (or no GPU at all) falsely claimed NVENC. That wasn't just a wrong label on
  the dashboard: the transcoder then tried to run every transcode on a GPU that
  wasn't there (`-hwaccel cuda`) and ffmpeg failed outright, so transcoded titles
  wouldn't play. Each backend is now confirmed with a real test-encode against the
  actual device before it's offered, and the estimated capacity follows from what
  truly works. When a usable-looking backend is rejected (for example a service
  account without access to `/dev/dri`), the reason is written to the log.

## v0.1.8 - 2026-07-24

### Added

- **New browse rows on the home screen**, driven by richer TMDB metadata:
  **"Directed by …"** rows (auteur-ranked, so they favor filmmakers over
  franchise assemblers), **theme rows** from keywords that span both movies and
  TV, **studio rows** for recognizable labels (Marvel Studios, DreamWorks, A24…),
  and **"Created by …"** rows for TV showrunners.
- **`northrou backfill-metadata`** fetches keywords, production companies, and TV
  creators for titles matched before those were ingested (newly scanned titles
  get them automatically). Renamed from `backfill-keywords`, which still works.
  Run it once with the service stopped, then restart it.

### Fixed

- **The hosted web client at app.northrou.sh now actually updates on release.**
  Its deploy was landing as a Cloudflare Pages preview instead of production, so
  new releases never reached the live domain.

### Improved

- **Title details show a tighter, ranked Cast & Crew list.** Instead of the full
  billed cast, it now leads with the director (or a show's creators), then
  top-billed actors, capped at 12. A writer is shown only occasionally, when a
  short cast leaves room. Producers are never listed.
- **Server settings are managed on the server, not in the app.** The client's
  Settings screen no longer duplicates server administration (scanning, streaming
  and ffmpeg options, the TMDB key, remote access, the connection code, and
  paired devices); those live on the server via `northrou admin`. Profiles,
  playback, language, and update-checking stay in the app.
- **The cold-start home screen (before any watch history) is far more diverse.**
  Genre rows now surface distinctive genres (Animation, Horror, Romance) instead
  of burying them under Action and Drama; the old per-decade "20X0s Blockbusters"
  rows are collapsed into a single demoted "Blockbusters" row; franchise rows
  appear in a stable order; and movies and shows are interleaved rather than
  segregated, so no one category crowds out the rest. Rows are capped tighter -
  browse rows show up to 8 titles, and "Recommended for You" is quality-gated
  (up to 12, never padded with weak picks).

## v0.1.7 - 2026-07-24

### Added

- **Recommendations now understand theme and tone, not just genre.** Northrou
  ingests each title's TMDB keyword tags and builds a local content vector per
  title (TF-IDF over keywords with a genre backbone, pure Go, no external
  services), so it can tell "small-town dread" from "small-town comedy" where
  before both were just "Drama". This powers new personalized home rows -
  **Because You Watched …** (the unwatched titles thematically closest to
  something you just finished) and **Movies About …** (rows built from the
  keywords that dominate your history) - each with a one-line subtitle
  explaining why it's there.
- **`northrou backfill-keywords`** fetches keyword tags for titles that were
  matched before this release (new scans get them automatically). Run it once,
  ideally with the service stopped, then restart the service so it reloads.

### Fixed

- **`northrou scan` now warns when a running service is detected on the
  configured port.** `scan` opens its own database connection directly rather
  than talking to a running server, so running it as a different user (e.g.
  the invoking user's own `$HOME` instead of the root the installed service
  runs as) silently scanned an unrelated, empty database while the real
  service's library never got touched. It now prints the resolved config path
  and data_dir and tells you how to point `--config` at the running service
  if they differ. `northrou doctor`'s "server: running and healthy" check is
  reworded to make clear it only confirms something answers on that port, not
  that it's the same config/data_dir this invocation resolved.
- **`northrou doctor` (and `northrou install`) now warn if the machine is
  configured to suspend on lid close.** Northrou is often installed on a
  laptop repurposed as a home server; closing the lid either suspends it
  mid-stream/scan, or, if `suspend.target` happens to be masked, sends
  `systemd-logind` into a CPU-pegging suspend-retry loop instead. The warning
  includes the one-line fix (`HandleLidSwitch`/`HandleLidSwitchExternalPower`
  in `/etc/systemd/logind.conf`).
- **Poster, backdrop, still, and cast images never rendered anywhere in the
  web client.** Two independent bugs: (1) `/api/images/*` requires the same
  Bearer auth as the rest of the API, but every image was a plain
  `<img src="/api/images/...">` - a browser can't attach an Authorization
  header to that, so every image request 401'd silently. Images are now
  fetched through the authenticated client and handed to `<img>` as a `blob:`
  object URL. (2) Separately, home-row items ship a bare cache-relative
  `poster_path` (no `/api/images/` prefix) while every other endpoint sends a
  ready-to-use `poster_url`; the frontend treated them as interchangeable, so
  even authenticated home-row image requests pointed at the wrong path.

### Improved

- **"More Like This" is now ranked by thematic similarity, not just shared
  genre.** Related titles are ordered by how close their keyword content vectors
  sit, blended with same-collection and same-director signals, and each result
  shows a short reason ("From the same collection", "Directed by …", "Shares the
  theme …"). Titles without keywords yet fall back to the old shared-genre
  ranking. Recommendations also learn from what you skip: a title shown
  repeatedly on the home screen but never played drifts down so fresher picks
  surface, and home rows the household consistently ignores are quietly rested.
- **`northrou scan` now shows live progress and reports why files didn't
  match.** A real library scan (ffprobe + TMDB per file) can run tens of
  minutes with no output at all until now. It redraws a `processed/total`
  line in place on a terminal (plain periodic lines when piped/redirected),
  and once done, lists each currently-unmatched file with its actual reason
  (previously only visible at `-v`/Debug log level, or by querying
  `GET /api/unmatched` directly).

## v0.1.6 - 2026-07-23

### Added

- **App clients** now build and release. APKs and desktop. IOS still needs to be implemented.
- **Sign-in is gone. The server connection code is now the only credential.**
  There are no more accounts, emails, one-time pins, or Google/Apple sign-in.
  A remote client (the apps and the web client) enters the connection code to
  pair; each device then keeps its own revocable session. This is a breaking
  change: on upgrade your household re-pairs with the connection code (find it
  in setup, in Server admin, or with `northrou cc`), and setup no longer asks
  for an email.
- **Admin is now local-only.** Changing server settings, scanning, deleting a
  profile, and installing updates are allowed only from a local connection to the
  box — one from your own network (a loopback or private/LAN address) that is not
  the remote tunnel. The apps, which always reach the box over the tunnel, become
  pure players; open the server's LAN address in a browser (or use the CLI) to
  administer it. A request from a public IP is treated like a remote client (no
  admin, code required), so do not expose the HTTP port to the internet — see the
  `bind_addr` note in configuration.md. The old emailed admin code is gone.
- **Self-hosting a coordination server has been removed.** Northrou uses the
  official coordinator exclusively; the `coordination_url` and
  `self_hosted_coordinator` settings and the pin-delivery relay are gone.
- `northrou cc` prints this server's connection code (the code apps and the web
  client use to pair), so you don't have to dig it out of config.toml. Aliases:
  `code`, `connection-code`. On a box running the system service (as root), run
  it with `sudo`.
- **Setup now happens in your terminal.** `northrou setup` walks through
  everything on the server itself - name your server, add media folders, an
  optional TMDB key, and remote access - and finishes by showing the connection
  code and kicking off the first scan. No browser needed, which is exactly what
  a headless NAS over SSH wants; the browser setup page is gone. Re-running it
  on a configured server shows a recap and opens the dashboard.
- **Servers have names.** Setup asks what to call the server ("Living Room
  NAS"); every device that pairs sees the name instead of a bare address or
  code. Stored as `name` under `[server]`; editable via the settings API.
- `northrou status` shows what the server is doing in one shot - service state,
  addresses, setup progress, remote access, library counts, ffmpeg readiness -
  and, when something is missing, the exact next command to run.
- `northrou doctor` checks the setup end to end (config, data dir, media
  folders, port, ffmpeg, Tesseract, TMDB key, coordinator reachability) with
  pass/warn/fail lines, and exits non-zero when something is actually broken.
- `northrou start` / `stop` / `restart` control the installed service; no more
  `uninstall && install` just to restart.
- `northrou logs [-f] [-n N]` shows (or follows) the server's log. The daemon
  now writes a size-rotated log file under `data_dir/logs` regardless of how it
  was started, and the settings page's View logs button shows the same tail.
- **See and revoke paired devices.** `northrou devices` lists every device
  paired with the server (also in the TUI's new Remote tab and in local web
  settings); revoke one with `northrou devices revoke <id>`. The list means
  streaming clients: the operator's own tooling (status, the TUI, the CLI)
  signs in ephemerally and never appears in it.
- **Rotate the connection code.** `northrou cc rotate` (or the button in the
  TUI/settings) mints a fresh code and signs every device out; devices at home
  re-pair automatically, remote ones need the new code.
- **Add, change, or remove the TMDB API key after setup.** If you skipped the
  key during setup (or want to change or clear it), there's now a field for it
  in local Server admin settings and a `northrou tmdb-key set <key>` /
  `northrou tmdb-key clear` command. The key stays write-only (the server never
  echoes it) and the change takes effect on the running server immediately -
  no restart, the next scan uses it.

### Fixed
- Turning remote access on (in setup or settings) now starts the tunnel
  immediately; it used to silently wait for the next server restart, so the
  connection code you had just been shown didn't work yet. Rotating the code
  re-registers with the coordinator on the spot for the same reason.
- `northrou update` now restarts the running service itself after installing,
  instead of telling you to `uninstall && install`.
- Displayed error messages now read consistently - capitalized and ending in a
  full stop - whatever their source, including raw coordinator/backend strings
  that are lowercase by Go convention.
- Remote signaling now sends WebSocket keepalive pings (both the coordinator and
  the box), so an idle-timeout proxy in front of the coordinator (e.g. Cloudflare
  closes idle WebSockets after ~100s) can't silently drop a box's registration
  while it sits idle between pairings.
- **Security: the admin gate can no longer be bypassed with a spoofed IP header.**
  A request arriving directly on the HTTP port could previously set
  `X-Forwarded-For`/`X-Real-IP` to a loopback address and be treated as a trusted
  local request, granting code-free pairing and every admin action. The server no
  longer trusts those headers; local trust is decided only from the real
  connection, and a foreign `Host` (DNS-rebinding) is rejected too.
- **Security: the connection code no longer leaks to remote or read-only
  sessions.** It is returned only to trusted local requests, is kept out of the
  server and coordinator logs, and the unmatched-files list and `GET /api/admin/logs`
  no longer expose host filesystem paths or logs to a remote session.
- **Security: a malformed remote request or subtitle can no longer crash the
  server.** A bad tunnel request, a truncated VobSub `.sub`, or an oversized PGS
  subtitle used to be able to panic the process; these are now contained.
- **Security: `northrou update` and the install script now fail closed.** Updates
  refuse to apply without a valid `checksums.txt` (and a valid signature when the
  build embeds a release key); the install script verifies the checksum on macOS
  too (it silently skipped it before) and aborts if it can't.
- The managed ffmpeg download is verified before it is put in place, so a failed
  or tampered download can no longer leave an unverified binary behind.
- Refresh-token rotation is now atomic, so a token can't be used twice, and a
  replayed (already-rotated) token now signs the whole device out.

### Improved
- The client's connect page is now a welcome page: enter your server's
  connection code and start watching, with the celebration moved to the moment
  a device successfully pairs.
- Settings shows the server's name everywhere it used to show a bare address
  or connection code.
- Cross-origin requests to the server are now restricted to the Northrou apps
  and same-machine browsers, so a random website you visit can't script the
  server behind your back.
- The coordinator and the box now cap connections, pairings, and message sizes.
  The coordinator also refuses to let a different server displace a connection
  code that another server has already registered, and a box whose registration
  is refused now keeps retrying (loudly) instead of sitting silently offline.
- Request and download sizes are capped across the server (API bodies, ffmpeg,
  images, TMDB) so an oversized or hostile response can't exhaust memory or disk,
  and abandoned transcodes can no longer fill the disk.

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
