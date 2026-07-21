#!/usr/bin/env bash
# Moves the coordination stack (coordinator + relay) forward to whatever git
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

# The Caddyfile is bind-mounted, so a routing/TLS change in it does NOT recreate
# the image-based caddy service on `up` above -- Caddy would keep serving the old
# config until restarted. Reload it explicitly so a Caddyfile change shipped in a
# release actually takes effect. `reload` is graceful and validates the new config
# first, so a bad Caddyfile leaves the running server untouched rather than down.
docker compose -f deploy.yml exec -T caddy caddy reload --config /etc/caddy/Caddyfile

echo "coordination-autoupdate: now running $latest"
