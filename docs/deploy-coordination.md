# Deploying the coordination stack

This is the runbook for standing up Northrou's public coordination stack: the
coordinator (WebRTC signaling + OAuth broker), the relay (pin-delivery email),
and Caddy in front of both. This is maintainer infrastructure, not something a
self-hoster needs to run — see [configuration.md](configuration.md) for
pointing a box at a coordinator (the hosted default or your own).

Everything below assumes one small box and the hostname `coord.northrou.sh`.
The coordinator and relay share that single hostname because their paths never
collide (`/ws`, `/oauth/*` vs `/v1/pin/send`), so there's one DNS record and one
TLS cert. The web client is hosted separately on Cloudflare Pages at
`app.northrou.sh` (built from GitHub on release; see "The web client on
Cloudflare Pages" below), so client and coordination never fight over one
hostname.

```
  clients / home servers            Vultr box (Ubuntu LTS)
        │  wss/https        ┌──────────────────────────────────────┐
        └───────────────────►  Caddy :80/:443  (auto Let's Encrypt) │
                            │     ├─ /v1/*  ─► relay :9100  (mail)   │
                            │     └─ else   ─► coordinator :9000     │
                            │                  (signaling + OAuth)   │
                            └──────────────────────────────────────┘
```

Only Caddy is exposed on 80/443. The coordinator and relay listen on plain
HTTP inside the Docker network; Caddy terminates TLS. The OAuth broker isn't a
third service — it's built into the coordinator, served under
`coord.northrou.sh/oauth/*`.

## 1. Accounts to line up first (some have delays)

- A Vultr account (or any box with Docker and a public IP).
- A transactional email provider for the relay's SMTP — Amazon SES, Postmark,
  Mailgun, or Resend. You'll verify a sender domain there (SPF/DKIM DNS
  records); start this early, verification can lag.
- A Google Cloud account, for Google sign-in.
- An Apple Developer account ($99/yr), for Sign in with Apple.

Both OAuth providers are optional — the coordinator runs as a pure signaling
relay with neither configured, and the emailed pin always works regardless.

## 2. The box

Ubuntu LTS, the smallest plan (1 vCPU / 1 GB is plenty — the coordinator is
stateless and tiny, the relay tinier). Attach your SSH key at creation.

```sh
apt update && apt upgrade -y
curl -fsSL https://get.docker.com | sh

# Firewall: SSH + HTTP(S) only. Service ports 9000/9100 stay internal — the
# compose file below only publishes Caddy, so just not mapping them is the
# safeguard (Docker manipulates iptables directly).
apt install -y ufw git openssl
ufw allow OpenSSH
ufw allow 80
ufw allow 443
ufw --force enable
```

## 3. DNS

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

## 4. Get the code and generate the OAuth signing key

```sh
git clone https://github.com/rhymeswithlimo/northrou.git
cd northrou

# ES256 (P-256) signing key for the OAuth broker. It MUST persist across
# restarts — regenerating it invalidates every assertion boxes have already
# verified against the published JWKS. Generate once, keep it secret, back it
# up somewhere durable.
openssl ecparam -genkey -name prime256v1 -noout -out oauth-signing.pem
chmod 600 oauth-signing.pem
```

## 5. Google OAuth client

1. [Google Cloud Console](https://console.cloud.google.com) → create a
   project.
2. APIs & Services → OAuth consent screen → External → fill in the basics; add
   your email as a test user, or publish.
3. Credentials → Create Credentials → OAuth client ID → Web application.
4. Authorized redirect URI (exact): `https://coord.northrou.sh/oauth/google/callback`
5. Save the Client ID and Client secret.

## 6. Sign in with Apple

1. [Apple Developer](https://developer.apple.com/account) → Certificates,
   Identifiers & Profiles.
2. Identifiers → + → Services IDs. Create one (e.g. `sh.northrou.signin`) —
   this is `OAUTH_APPLE_SERVICE_ID`.
3. Edit it → enable Sign in with Apple → Configure:
   - Domains and Subdomains: `coord.northrou.sh`
   - Return URLs: `https://coord.northrou.sh/oauth/apple/callback`
4. Keys → + → enable Sign in with Apple → register → download the `.p8`
   (one-time download). Note the Key ID.
5. Your Team ID is in the top-right of the developer portal.

Collected: Service ID, Team ID, Key ID, and the `.p8` file contents.

## 7. SMTP provider for the relay

At your provider: verify your sender domain (add their SPF/DKIM records to
`northrou.sh`), then grab SMTP host / port / username / password and a
verified From address.

Vultr (and most cloud providers) block outbound SMTP: port 25 outright, and
newer accounts can have 587/465 restricted too. Use your provider's submission
port (587 STARTTLS, or 465 implicit TLS — the relay defaults to 587). If mail
silently fails, test with `nc -vz your-smtp-host 587` and open a support
ticket to lift the SMTP restriction on the instance.

## 8. Deploy

```sh
cp deploy.yml.example deploy.yml
chmod 600 deploy.yml   # holds secrets; never commit it (already gitignored)
```

Fill in `deploy.yml`: the pasted contents of `oauth-signing.pem` and the Apple
`.p8`, the Google/Apple client credentials, and the SMTP settings. `Caddyfile`
in the repo root needs no edits — it already routes `coord.northrou.sh` by path.

`OAUTH_ISSUER` is not optional — `coordination/cmd/coordinator/main.go` won't
build the OAuth broker without it, and the coordinator falls back to being a
plain signaling relay with no log complaint beyond a `slog.Warn`. If sign-in
buttons don't appear, check this first.

`OAUTH_REDIRECTS` is the allow-list of client redirect targets the broker will
honor (a bare entry is an exact match, one ending in `/` is a prefix match).
`northrou://` covers the native apps and `https://app.northrou.sh/` covers the
hosted web client; add any other fixed web-client origins you control. An empty
list refuses every redirect — the safe failure, since an open redirector would
launder a real Google login into any site.

```sh
docker compose -f deploy.yml up -d --build
docker compose -f deploy.yml logs -f   # watch for "oauth broker enabled" and cert issuance
```

## 9. Verify

```sh
curl https://coord.northrou.sh/healthz       # -> ok
curl https://coord.northrou.sh/oauth/jwks    # -> 404 until OAuth is configured (step 9b); JWKS JSON once it is

# --http1.1 matters: curl negotiates HTTP/2 over TLS by default, and h2 doesn't
# do the old Connection:Upgrade handshake, so you'd see a 426 instead of 101.
# --max-time matters too: a successful upgrade leaves the connection open, so
# without it curl just hangs after printing the response.
curl -i -N --http1.1 --max-time 5 -H "Connection: Upgrade" -H "Upgrade: websocket" \
  -H "Sec-WebSocket-Version: 13" -H "Sec-WebSocket-Key: x3JJHMbDL1EzLkh9GBhXDw==" \
  https://coord.northrou.sh/ws               # -> 101

# relay's own /healthz is under the catch-all route, so check it inside the
# box -- but not via `exec relay`: the relay image is distroless (no shell, no
# wget/curl, nothing but the binary itself), so route through caddy's
# container instead, which has both, over the internal Docker network:
docker compose -f deploy.yml exec caddy wget -qO- http://relay:9100/healthz   # -> ok
```

## 10. Auto-update on release (optional)

By default the box just sits on whatever commit you cloned — nothing pulls on
its own. `scripts/coordination-autoupdate.sh` + its systemd timer make it
track **published GitHub releases only**, never every push to `main`: it polls
`api.github.com/repos/rhymeswithlimo/northrou/releases/latest`, and only when
that tag differs from what's checked out does it `git checkout` the new tag
and `docker compose up -d --build`. Since `deploy.yml` and
`oauth-signing.pem` are gitignored (never tracked in any commit), switching
tags never touches them.

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
  `app.northrou.sh` (see the section below); Caddy here serves only the
  coordinator and relay. A browser loading the client from `app.northrou.sh`
  reaches boxes over the tunnel, the same as the desktop app.
- **OAuth redirect targets must be known in advance.** The native apps return
  via the `northrou://` scheme and the hosted web client via its fixed
  `https://app.northrou.sh/` origin — both are in `OAUTH_REDIRECTS` and
  allow-list cleanly. A client served directly by an individual box lives at
  that box's own address, which can't be pre-allow-listed for arbitrary boxes;
  there the emailed pin (which always works everywhere) is the path.
- **Secrets live in `deploy.yml`.** Keep it `chmod 600`, never commit it, and
  back up `oauth-signing.pem` and the Apple `.p8` somewhere durable — losing
  the signing key invalidates every box's cached JWKS verification.
