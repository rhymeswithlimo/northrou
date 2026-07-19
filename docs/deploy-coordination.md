# Deploying the coordination stack

This is the runbook for standing up Northrou's public coordination stack: the
coordinator (WebRTC signaling + OAuth broker), the relay (pin-delivery email),
and Caddy in front of both. This is maintainer infrastructure, not something a
self-hoster needs to run — see [configuration.md](configuration.md) for
pointing a box at a coordinator (the hosted default or your own).

Everything below assumes one small box and the hostname `app.northrou.sh`. The
coordinator and relay share that single hostname because their paths never
collide (`/ws`, `/oauth/*` vs `/v1/pin/send`), so there's one DNS record and
one TLS cert instead of two.

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
`app.northrou.sh/oauth/*`.

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

One record, at wherever `northrou.sh` is managed:

| Type | Name | Value |
|---|---|---|
| A | app | box's public IPv4 |

(AAAA too if the box has IPv6.) If DNS is on Cloudflare, set it to DNS-only
(grey cloud) — Caddy's automatic Let's Encrypt needs to answer ACME on port 80
directly. Verify before continuing:

```sh
dig +short app.northrou.sh    # must return the box's IP
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
4. Authorized redirect URI (exact): `https://app.northrou.sh/oauth/google/callback`
5. Save the Client ID and Client secret.

## 6. Sign in with Apple

1. [Apple Developer](https://developer.apple.com/account) → Certificates,
   Identifiers & Profiles.
2. Identifiers → + → Services IDs. Create one (e.g. `sh.northrou.signin`) —
   this is `OAUTH_APPLE_SERVICE_ID`.
3. Edit it → enable Sign in with Apple → Configure:
   - Domains and Subdomains: `app.northrou.sh`
   - Return URLs: `https://app.northrou.sh/oauth/apple/callback`
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
in the repo root needs no edits — it already routes `app.northrou.sh` by path.

`OAUTH_ISSUER` is not optional — `coordination/cmd/coordinator/main.go` won't
build the OAuth broker without it, and the coordinator falls back to being a
plain signaling relay with no log complaint beyond a `slog.Warn`. If sign-in
buttons don't appear, check this first.

`OAUTH_REDIRECTS` is the allow-list of client redirect targets the broker will
honor (a bare entry is an exact match, one ending in `/` is a prefix match).
`northrou://` covers the native apps; add any fixed web-client origins you
control. An empty list refuses every redirect — the safe failure, since an
open redirector would launder a real Google login into any site.

```sh
docker compose -f deploy.yml up -d --build
docker compose -f deploy.yml logs -f   # watch for "oauth broker enabled" and cert issuance
```

## 9. Verify

```sh
curl https://app.northrou.sh/healthz         # -> ok
curl https://app.northrou.sh/oauth/jwks      # -> JWKS JSON with your ES256 public key
curl -i -N -H "Connection: Upgrade" -H "Upgrade: websocket" \
  -H "Sec-WebSocket-Version: 13" -H "Sec-WebSocket-Key: x3JJHMbDL1EzLkh9GBhXDw==" \
  https://app.northrou.sh/ws                 # -> 101

# relay's own /healthz is under the catch-all route, so check it inside the box:
docker compose -f deploy.yml exec relay wget -qO- localhost:9100/healthz   # -> ok
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

## Things to know

- **No TURN server.** Both sides use only public STUN
  (`stun.l.google.com:19302`, hardcoded in `backend/internal/remote/peer.go`
  and `frontend/js/api/tunnel.js`). That gets most home NATs through, but
  symmetric NAT (some carrier-grade NAT, mobile networks, locked-down
  corporate) fails to hole-punch, with no media-relay fallback. Fixing this
  means running [coturn](https://github.com/coturn/coturn) and making the ICE
  server list configurable — currently hardcoded on both ends.
- **OAuth on web clients is a known rough edge.** The native apps return via
  the `northrou://` scheme, which allow-lists cleanly. A web client served by
  a self-hosted box lives at that box's own address, which can't be
  pre-allow-listed for arbitrary boxes — inherent to the self-hosted model.
  The emailed pin always works everywhere; social sign-in is a shortcut where
  redirects are known in advance.
- **Secrets live in `deploy.yml`.** Keep it `chmod 600`, never commit it, and
  back up `oauth-signing.pem` and the Apple `.p8` somewhere durable — losing
  the signing key invalidates every box's cached JWKS verification.
