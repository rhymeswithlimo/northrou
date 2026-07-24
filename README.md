<div align="center">
<img alt="Northrou" src="public/repo/Hero_Banner_JPG_v1.2__Northrou.jpg" width="100%">
</div>

<h3 align="center">Northrou</h3>

<p align="center">
  English ·
  <a href="README.zh-CN.md">简体中文</a> ·
  <a href="README.es.md">Español</a> ·
  <a href="README.fr.md">Français</a> ·
  <a href="README.de.md">Deutsch</a> ·
  <a href="README.ja.md">日本語</a>
</p>

<p align="center">Your movies and shows, streamed from your own hardware.</p>

<p align="center">
  <a href="https://northrou.sh">Website</a> ·
  <a href="https://northrou.sh/docs">Docs</a> ·
  <a href="#install">Install</a> ·
  <a href="#license">License</a>
</p>

<p align="center">
<a href="https://github.com/rhymeswithlimo/northrou/releases"><img src="https://img.shields.io/github/v/release/rhymeswithlimo/northrou" alt="Latest release"></a>
<a href="LICENSE"><img src="https://img.shields.io/badge/license-BSD--3--Clause-blue" alt="License: BSD 3-Clause"></a>
<a href="https://github.com/rhymeswithlimo/northrou/commits/main"><img src="https://img.shields.io/github/last-commit/rhymeswithlimo/northrou" alt="Last commit"></a>
<img src="https://img.shields.io/badge/Go-1.26%2B-00ADD8?logo=go&logoColor=white" alt="Go 1.26+">
</p>

---

Northrou is an open-source media server you run on your own hardware. Point
it at your movie and TV library and it streams to your phone, tablet,
desktop, or TV, at home or away, without your media ever passing through
anyone else's servers.

Playback adapts to whatever's watching. Files play back untouched wherever a
device can handle them, converting only what actually needs it, using your
GPU when one's available. Dolby Atmos and lossless audio tracks are carried
through or adapted per device instead of getting flattened to stereo.

Add a library and Northrou fills in the rest: posters, cast, and details are
matched automatically, subtitles (including image-based tracks most servers
can't touch) just work, and a recommendation engine built from your own watch
history, never shared anywhere, helps you find what to watch next.

One person sets up the server once and shares a connection code. Everyone
else enters that code in the app to connect: no accounts, no emails, no
passwords. Remote access is peer-to-peer: your server and your device talk
directly, so nothing in between ever sees what you're streaming.

## Install

```sh
curl -sSL https://raw.githubusercontent.com/rhymeswithlimo/northrou/main/scripts/install.sh | sh
northrou setup
```

The installer sets Northrou up as a background service and fetches FFmpeg
automatically, so there's nothing else to install. `setup` then walks you
through naming your server, adding your media folders, and generating your
connection code, right in the terminal, no browser needed. Install the app
on your other devices, enter the code, and you're connected.

Prefer Docker, or installing manually? The full walkthrough (including every
install path and config option) is at [northrou.sh/docs](https://northrou.sh/docs),
or see [docs/](docs/) in this repo.

## Commands

Day to day you shouldn't need most of this: `northrou admin` opens a live
terminal dashboard of streams, hardware, and capacity if you ever want to
look under the hood.

```text
northrou <command> [flags]

COMMANDS:
   setup                    set up the server in your terminal (name, media, TMDB key, code)
   status                   show what the server is doing and what to do next
   doctor                   check the setup and report anything broken
   serve                    run the server in the foreground (what the service invokes)
   install / uninstall      register / remove the system service
   start / stop / restart   control the installed service
   logs                     show or follow the server's recent log output
   admin                    open the live admin dashboard (TUI)
   scan [path...]           scan a folder or drive now (no path scans the configured dirs)
   rescan [path...]         re-scan and refetch metadata for every file, even unchanged ones
   match <file>             force a file to a specific TMDB title
   backfill-metadata        fetch keywords/studios/creators for existing titles (improves recommendations)
   cc                       print this server's connection code (for pairing apps)
   cc rotate                replace the connection code and sign every device out
   devices                  list paired devices
   devices revoke <id>      sign one paired device out
   tmdb-key                 show, set, or remove the TMDB API key
   update                   check for and install a newer release
   version                  print version info
   -h, --help               show help for a command

GLOBAL (every command):
   --config string          path to config.toml (default: OS config dir)
   -v, --verbose            enable debug logging

LOGS:
   -f, --follow             keep printing new log lines as they arrive
   -n, --lines int          trailing lines to show (default 200)

ADMIN:
   --addr string            server base URL (default from config, e.g. http://localhost:8674)

SCAN / RESCAN:
   --tv                     treat the given paths as TV episodes (default: detect by filename)

MATCH:
   --tmdb-id int            TMDB id of the movie or show to link (required)
   --tv                     treat the file as a TV episode
   --season int             season number (with --tv)
   --episode int            episode number (with --tv)

CC ROTATE:
   -y, --yes                rotate without confirmation

UPDATE:
   -y, --yes                apply the update without confirmation
   --check                  only check; do not install
```

`setup` is interactive and needs no flags. Service commands (`install`,
`start`, `stop`, `restart`, `update`) write to root-owned locations, so run
those with `sudo` on Linux. The command tells you when it needs elevation.

## Documentation

The full reference, covering every config option, the HTTP API, architecture,
and more, lives at [northrou.sh/docs](https://northrou.sh/docs). The same
pages are mirrored in this repo:

- [Configuration reference](docs/configuration.md)
- [HTTP API reference](docs/api.md)
- [Architecture](docs/architecture.md)
- [Client](docs/frontend.md)

## Development

Northrou is fully open source and self-buildable. It's a monorepo: the
server and remote-access broker are separate Go modules, and the client
(`frontend/`) is a Tauri app shared across web, desktop, iOS, and Android.

```sh
make build   # build the client, then bin/northrou and bin/coordinator
make test    # run the test suite
make run     # build and run the server locally
```

See [docs/architecture.md](docs/architecture.md) for how the pieces fit
together.

## License

BSD 3-Clause, see [LICENSE](LICENSE). You may build, run, fork, and
redistribute the software freely under those terms. The **Northrou** name,
logos, and brand assets are not part of the grant and may not be used to
endorse or promote derived products without permission (see [NOTICE](NOTICE)).
