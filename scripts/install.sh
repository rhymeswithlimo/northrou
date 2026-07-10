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
curl -sSL "$url" -o "$tmp/northrou.tar.gz" || err "download failed: $url"

# Verify checksum if the checksums file is available.
if curl -sSL "https://github.com/${REPO}/releases/download/${tag}/checksums.txt" -o "$tmp/checksums.txt" 2>/dev/null; then
  if command -v sha256sum >/dev/null 2>&1; then
    want=$(grep "  ${asset}\$" "$tmp/checksums.txt" | awk '{print $1}')
    got=$(sha256sum "$tmp/northrou.tar.gz" | awk '{print $1}')
    [ "$want" = "$got" ] || err "checksum verification failed"
    info "Checksum verified."
  fi
fi

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
if [ "${NORTHROU_NO_SERVICE:-}" != "1" ]; then
  info "Registering the system service…"
  if [ "$(id -u)" = "0" ] || [ "$os" = "darwin" ]; then
    "$bindir/northrou" install || info "Service registration skipped (run 'northrou install' manually if desired)."
  else
    info "Run 'sudo ${bindir}/northrou install' to register the background service."
  fi
fi

info "Done! Run 'northrou setup' to create your account and point at your media."
