#!/usr/bin/env sh
# Northrou installer: detects your OS/architecture, downloads the matching
# release binary from GitHub, installs it, and registers the system service.
#
#   curl -sSL https://raw.githubusercontent.com/rhymeswithlimo/northrou/main/scripts/install.sh | sh
#
# Environment overrides:
#   NORTHROU_VERSION   install a specific tag (default: latest)
#   NORTHROU_BIN_DIR   install location (default: /usr/local/bin, or ~/.local/bin without root)
#   NORTHROU_NO_SERVICE=1  skip `northrou install` (service registration)
set -eu

REPO="rhymeswithlimo/northrou"

info()  { printf '\033[1;34m==>\033[0m %s\n' "$1"; }
err()   { printf '\033[1;31mError:\033[0m %s\n' "$1" >&2; exit 1; }

# --- detect platform ---
os=$(uname -s)
case "$os" in
  Linux)  os=linux ;;
  Darwin) os=darwin ;;
  *) err "unsupported OS: $os (Windows users: download the .zip from the releases page)" ;;
esac

arch=$(uname -m)
case "$arch" in
  x86_64|amd64)   arch=amd64 ;;
  aarch64|arm64)  arch=arm64 ;;
  armv7l|armv7)   arch=armv7 ;;
  *) err "unsupported architecture: $arch" ;;
esac
info "Detected platform: ${os}/${arch}"

# --- resolve version ---
if [ "${NORTHROU_VERSION:-}" = "" ]; then
  info "Finding the latest release…"
  tag=$(curl -sSL "https://api.github.com/repos/${REPO}/releases/latest" \
        | grep -o '"tag_name": *"[^"]*"' | head -1 | sed 's/.*"tag_name": *"\([^"]*\)".*/\1/')
  [ -n "$tag" ] || err "could not determine latest version"
else
  tag="$NORTHROU_VERSION"
fi
ver="${tag#v}" # goreleaser archive names drop the leading 'v'
info "Installing Northrou ${tag}"

# --- download & extract ---
asset="northrou_${ver}_${os}_${arch}.tar.gz"
url="https://github.com/${REPO}/releases/download/${tag}/${asset}"
tmp=$(mktemp -d)
trap 'rm -rf "$tmp"' EXIT

info "Downloading ${asset}…"
curl -fsSL "$url" -o "$tmp/northrou.tar.gz" || err "download failed: $url"

# Verify the checksum, fail-CLOSED. goreleaser always publishes checksums.txt, so
# a missing file, a missing entry, or no available hashing tool aborts the
# install rather than running an unverified binary. macOS ships `shasum`, not
# `sha256sum`, so support both (the old script silently skipped verification on
# macOS because it only checked for sha256sum).
info "Verifying checksum…"
curl -fsSL "https://github.com/${REPO}/releases/download/${tag}/checksums.txt" -o "$tmp/checksums.txt" \
  || err "could not download checksums.txt; refusing to install an unverified binary"

if command -v sha256sum >/dev/null 2>&1; then
  sha256() { sha256sum "$1" | awk '{print $1}'; }
elif command -v shasum >/dev/null 2>&1; then
  sha256() { shasum -a 256 "$1" | awk '{print $1}'; }
else
  err "no sha256 tool (sha256sum or shasum) found; cannot verify the download"
fi

want=$(awk -v f="$asset" '$2 == f {print $1}' "$tmp/checksums.txt")
[ -n "$want" ] || err "no checksum entry for ${asset}; refusing to install"
got=$(sha256 "$tmp/northrou.tar.gz")
[ "$want" = "$got" ] || err "checksum verification failed for ${asset}"
info "Checksum verified."

tar -xzf "$tmp/northrou.tar.gz" -C "$tmp"

# --- install ---
bindir="${NORTHROU_BIN_DIR:-}"
if [ -z "$bindir" ]; then
  if [ "$(id -u)" = "0" ]; then bindir=/usr/local/bin; else bindir="$HOME/.local/bin"; fi
fi
mkdir -p "$bindir"
install -m 0755 "$tmp/northrou" "$bindir/northrou"
info "Installed to ${bindir}/northrou"

case ":$PATH:" in
  *":$bindir:"*) : ;;
  *) info "Note: add ${bindir} to your PATH." ;;
esac

# --- register the service ---
service_started=0
if [ "${NORTHROU_NO_SERVICE:-}" != "1" ]; then
  info "Registering the system service…"
  if [ "$(id -u)" = "0" ] || [ "$os" = "darwin" ]; then
    if "$bindir/northrou" install; then
      service_started=1
    else
      info "Service registration skipped (run 'northrou install' manually if desired)."
    fi
  else
    info "Run 'sudo ${bindir}/northrou install' to register the background service."
  fi
fi

# One path either way: `northrou setup` detects an already-running service and
# drives it, or starts the server itself if none is installed.
info "Done! Run 'northrou setup' to name your server, add your media folders, and get your connection code."
info "Away from home, you and anyone with your connection code can watch from any device at https://app.northrou.sh."
