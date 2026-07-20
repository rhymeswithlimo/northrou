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

## v0.1.1 - Unreleased

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
