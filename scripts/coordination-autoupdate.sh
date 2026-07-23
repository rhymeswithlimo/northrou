#!/usr/bin/env bash
# Moves the coordination server (coordinator + Caddy) forward to whatever git
# tag GitHub currently reports as the latest published release -- and only
# then, never on every push to main. Meant to run periodically via
# coordination-autoupdate.timer (systemd). See docs/deploy-coordination.md.
set -euo pipefail

REPO_DIR="${NORTHROU_REPO_DIR:-/root/northrou}"
GH_REPO="rhymeswithlimo/northrou"

cd "$REPO_DIR"

latest="$(curl -sSL "https://api.github.com/repos/${GH_REPO}/releases/latest" | jq -r '.tag_name // empty')"
if [ -z "$latest" ]; then
    echo "coordination-autoupdate: no published release yet, nothing to do"
    exit 0
fi

current="$(git describe --tags --exact-match 2>/dev/null || true)"
if [ "$current" = "$latest" ]; then
    echo "coordination-autoupdate: already on $latest"
    exit 0
fi

echo "coordination-autoupdate: updating ${current:-<untagged>} -> $latest"
git fetch --tags origin
git checkout "$latest"
docker compose -f deploy.yml up -d --build

# Force-recreate Caddy so it picks up Caddyfile changes shipped in a release.
# A plain `up` / `restart` / `caddy reload` all fail to here: (1) Caddy is an
# image-based service, so `up` won't recreate it when only the bind-mounted
# Caddyfile's CONTENT changed; and (2) `git checkout` REPLACES the Caddyfile
# (new inode), but a file bind-mount is pinned to the old inode, so the running
# container - and `caddy reload`, which reads the in-container path - keeps
# seeing the old file. Recreating re-binds the mount to the new file.
docker compose -f deploy.yml up -d --force-recreate caddy

echo "coordination-autoupdate: now running $latest"
