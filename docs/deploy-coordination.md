# Deploying the coordination server

This is the runbook for standing up Northrou's public coordination server: the
coordinator (WebRTC signaling) and Caddy in front of it. This is maintainer
infrastructure, not something a self-hoster runs — boxes use the official
coordinator automatically, and there is no self-hosting path.

The coordinator relays only WebRTC signaling (SDP + ICE) so clients and home
servers can hole-punch a direct peer-to-peer connection; it never sees media,
and it holds no accounts or secrets (authentication is the server connection
code, verified at the box, not here). Everything below assumes one small box and
the hostname `coord.northrou.sh`. The web client is hosted separately on
Cloudflare Pages at `app.northrou.sh` (see "The web client on Cloudflare Pages"
below), so client and coordination never fight over one hostname.

```
  clients / home servers            Vultr box (Ubuntu LTS)
        │  wss/https        ┌──────────────────────────────────────┐
        └───────────────────►  Caddy :80/:443  (auto Let's Encrypt) │
                            │     └─► coordinator :9000  (signaling) │
                            └──────────────────────────────────────┘
```

Only Caddy is exposed on 80/443. The coordinator listens on plain HTTP inside
the Docker network; Caddy terminates TLS and proxies everything to it. The
coordinator serves `/ws`, `/healthz`, and `/stats`.

## 1. The box

Ubuntu LTS, the smallest plan (1 vCPU / 1 GB is plenty — the coordinator is
stateless and tiny). Attach your SSH key at creation.

```sh
apt update && apt upgrade -y
curl -fsSL https://get.docker.com | sh

# Firewall: SSH + HTTP(S) only. The coordinator port 9000 stays internal — the
# compose file only publishes Caddy, so just not mapping it is the safeguard
# (Docker manipulates iptables directly).
apt install -y ufw git
ufw allow OpenSSH
ufw allow 80
ufw allow 443
ufw --force enable
```

## 2. DNS

One record for coordination, at wherever `northrou.sh` is managed:

| Type | Name | Value |
|---|---|---|
| A | coord | box's public IPv4 |

(AAAA too if the box has IPv6.) `app` is not a box record — Cloudflare Pages
owns it as a custom domain (see the web-client section below). If DNS is on
Cloudflare, set the `coord` record to DNS-only (grey cloud) — Caddy's automatic
Let's Encrypt needs to answer ACME on port 80 directly. Verify before continuing:

```sh
dig +short coord.northrou.sh    # must return the box's IP
```

## 3. Deploy

```sh
git clone https://github.com/rhymeswithlimo/northrou.git
cd northrou

cp deploy.yml.example deploy.yml   # if present; otherwise adapt docker-compose
```

`Caddyfile` in the repo root needs no edits — it already proxies
`coord.northrou.sh` to the coordinator. There are no secrets to fill in: the
coordinator takes only `COORD_ADDR` (defaulted).

```sh
docker compose -f deploy.yml up -d --build
docker compose -f deploy.yml logs -f   # watch for "coordination server listening" and cert issuance
```

## 4. Verify

```sh
curl https://coord.northrou.sh/healthz   # -> ok
curl https://coord.northrou.sh/stats     # -> {"servers":N,"sessions":N}

# --http1.1 matters: curl negotiates HTTP/2 over TLS by default, and h2 doesn't
# do the old Connection:Upgrade handshake, so you'd see a 426 instead of 101.
# --max-time matters too: a successful upgrade leaves the connection open, so
# without it curl just hangs after printing the response.
curl -i -N --http1.1 --max-time 5 -H "Connection: Upgrade" -H "Upgrade: websocket" \
  -H "Sec-WebSocket-Version: 13" -H "Sec-WebSocket-Key: x3JJHMbDL1EzLkh9GBhXDw==" \
  https://coord.northrou.sh/ws           # -> 101
```

## 5. Auto-update on release (optional)

By default the box just sits on whatever commit you cloned — nothing pulls on
its own. `scripts/coordination-autoupdate.sh` + its systemd timer make it
track **published GitHub releases only**, never every push to `main`: it polls
`api.github.com/repos/rhymeswithlimo/northrou/releases/latest`, and only when
that tag differs from what's checked out does it `git checkout` the new tag
and `docker compose up -d --build`.

```sh
apt install -y jq   # the script parses the GitHub API response with it
cp scripts/coordination-autoupdate.service scripts/coordination-autoupdate.timer /etc/systemd/system/
systemctl daemon-reload
systemctl enable --now coordination-autoupdate.timer
systemctl list-timers coordination-autoupdate.timer   # confirm it's scheduled
journalctl -u coordination-autoupdate.service -n 20    # check a run's output
```

Until you cut the first release, the script's every run is a no-op ("no
published release yet"). To trigger an update check on demand rather than
waiting for the timer: `systemctl start coordination-autoupdate.service`.

## The web client on Cloudflare Pages

The web client (the Vite app in `frontend/`) is hosted on Cloudflare Pages at
`app.northrou.sh`, separate from this box. It's a static build that reaches
boxes over the tunnel, so it needs no server of its own. GitHub Actions builds
and deploys it **only when a release is published**
(`.github/workflows/deploy-web.yml`), so `app.northrou.sh` moves in lockstep
with releases, not on every push.

One-time setup:

1. **Create the Pages project.** Cloudflare dashboard → Workers & Pages →
   Create → Pages → **Upload assets** (a "Direct Upload" project — the GitHub
   Action does the building, so you don't connect the Git integration here).
   Name it `northrou-web` to match `--project-name` in the workflow. Drop in the
   current `frontend/dist` once to create it, or let the first release populate
   it.
2. **Add the custom domain.** Project → Custom domains → Set up a custom domain
   → `app.northrou.sh`. Cloudflare wires the DNS automatically if `northrou.sh`
   is on Cloudflare. This is what points `app.northrou.sh` at Pages, so `app`
   must NOT also be a box A record (see the DNS step — only `coord` is).
3. **Create an API token.** Dashboard → My Profile → API Tokens → Create Token →
   "Cloudflare Pages — Edit" template (or a custom token with Account →
   Cloudflare Pages → Edit). Copy it.
4. **Add repo secrets.** GitHub repo → Settings → Secrets and variables →
   Actions → New repository secret:
   - `CLOUDFLARE_API_TOKEN` — the token from step 3.
   - `CLOUDFLARE_ACCOUNT_ID` — dashboard → Workers & Pages → the account ID in
     the right sidebar.
5. **Deploy.** Publish a GitHub release, or run the "Deploy web client" workflow
   by hand from the Actions tab.

The client's built-in coordinator URL is `wss://coord.northrou.sh/ws`, so bring
coordination up there (this box) before shipping a client that points at it.

## Things to know

- **No TURN server.** Both sides use only public STUN
  (`stun.l.google.com:19302`, hardcoded in `backend/internal/remote/peer.go`
  and `frontend/js/api/tunnel.js`). That gets most home NATs through, but
  symmetric NAT (some carrier-grade NAT, mobile networks, locked-down
  corporate) fails to hole-punch, with no media-relay fallback. Fixing this
  means running [coturn](https://github.com/coturn/coturn) and making the ICE
  server list configurable — currently hardcoded on both ends.
- **The web client is not on this box.** It's hosted on Cloudflare Pages at
  `app.northrou.sh` (see the section above); Caddy here serves only the
  coordinator. A browser loading the client from `app.northrou.sh` reaches boxes
  over the tunnel, the same as the desktop app.
- **The coordinator is a code-validity oracle**, so its `connect` handler is
  rate-limited per client IP and globally
  (`coordination/internal/broker/limiter.go`). It sees real client IPs via
  `X-Forwarded-For` from Caddy; keep that header trustworthy (don't expose 9000
  directly).
