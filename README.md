<picture>
  <source media="(prefers-color-scheme: dark)" srcset="docs/brand/logo-dark.png">
  <img src="docs/brand/logo-light.png" alt="TGProxy Panel" height="32">
</picture>

# TGProxy panel

A control panel for Telegram proxy nodes. It creates and revokes keys, pushes configuration to nodes over gRPC, serves a decoy site on each node and watches node health. A node runs telemt (the default engine) or tproxy-server plus MTProxy; a small agent on the node takes instructions from the panel. One panel manages many nodes.

Setup guides with screenshots: [English](docs/setup.en.md), [Русский](docs/setup.ru.md).

<p align="center">
  <img src="docs/screenshots/dashboard.png" width="49%" alt="Dashboard">
  <img src="docs/screenshots/keys.png" width="49%" alt="Keys">
</p>
<p align="center">
  <img src="docs/screenshots/login.png" width="49%" alt="Login">
  <img src="docs/screenshots/node-detail.png" width="49%" alt="Node page">
</p>

## What it does

- Keys: shared (one secret for a group) or personal (one per person, revoked individually), batch creation from a name template, bound to one or more nodes.
- Links: on a telemt node every key gets a WEB link (`https://t.me/webproxy?...`) and a Fake-TLS link (`https://t.me/proxy?...`), with QR codes; a tproxy node gives the WEB link only.
- Subscription pages: a public per-key URL that shows every node the key is bound to as links and QR codes, without exposing the panel.
- Per-key limits on telemt nodes: traffic quota, up/down rate, max unique IPs, max connections, enforced by telemt itself, plus per-key traffic stats.
- Nodes: installed with one command pasted into a root shell; the engine (`telemt` or `tproxy`) is chosen per node at creation.
- Decoy sites: five built-in presets and an editor; every node gets a uniquified copy so nodes do not share a fingerprint.
- Monitoring: live sessions, streams and traffic per node over 1h/6h/24h/7d; a Prometheus `/metrics` endpoint and a Grafana dashboard.
- Alerts to Telegram when a node goes offline, comes back, or an apply fails.
- Audit log of every mutating action with filters by action, user and date.
- Access control: `owner`, `admin` and `viewer` roles, TOTP second factor with recovery codes.
- Backups: on-demand and nightly `pg_dump`, restore from the CLI; master-key rotation that re-encrypts every secret.
- Topbar chips: the panel version with an update highlight read from GitHub releases, the repository star count, and nodes online.
- Forms keep a draft for 24 hours after an accidental close and offer to continue it; every page and dialog has a `?` that opens a help panel describing each field with examples (`?` key opens it too).
- Node load: CPU and RAM in the nodes list and on the dashboard, with a CPU/RAM/disk history chart on the node page and in Monitoring.

## Quick start

The installer sets up the panel host in one command. It needs a fresh Ubuntu 22.04+ / Debian 12+ host with root, ports 80 and 443 free, and a DNS A record for the panel's domain already pointing at it. The script installs Docker when it is missing, downloads the compose files of the latest release into `/opt/tgproxy-panel`, writes `.env` with freshly generated secrets, starts the stack, waits for it to become healthy and creates the first admin (role `owner`).

```bash
# interactive: asks for the domain, the ACME e-mail and the admin credentials
curl -fsSL https://raw.githubusercontent.com/greenpandorik/tgproxy-panel/main/install.sh | sudo bash

# non-interactive
curl -fsSL https://raw.githubusercontent.com/greenpandorik/tgproxy-panel/main/install.sh | sudo bash -s -- \
  --domain panel.example.com --email me@example.com --admin-user root --admin-password 'a-strong-password' --yes

# no domain: no Caddy, no TLS, panel on http://<ip>:8080 (real nodes cannot join such a panel)
curl -fsSL https://raw.githubusercontent.com/greenpandorik/tgproxy-panel/main/install.sh | sudo bash -s -- --local --yes
```

At the end it prints the URL and, when you did not pass one, the generated admin password (once). The install directory holds `docker-compose.yml`, `Caddyfile`, `.env` (mode 0600; keep a copy off the host, `MASTER_KEY` encrypts every secret in the database) and a copy of the script for later:

```bash
sudo /opt/tgproxy-panel/install.sh --update              # move to the latest release (or --version 1.2.0)
sudo /opt/tgproxy-panel/install.sh --uninstall           # stop the stack; asks before removing the data volumes
sudo /opt/tgproxy-panel/install.sh --uninstall --purge   # remove containers, volumes and the directory
cd /opt/tgproxy-panel && docker compose logs -f panel    # logs
```

Before changing anything the script runs a pre-flight: the domain must resolve to this host's public IP, ports 80 and 443 (8080 in local mode) must be free, and the install directory must be usable; a failed check offers re-run / continue / quit on a terminal and stops the script otherwise (`--skip-preflight` skips it). `install.sh --help` lists every option; each one can also be given as an environment variable `TGWP_<NAME>` (`TGWP_DOMAIN`, `TGWP_YES`, ...). `--dir` changes the install directory, `--image <ref>` replaces the `ghcr.io/greenpandorik/tgproxy-panel:<version>` reference (for mirrors, and for tests: `make test-install` runs the script against a locally built image). The script does not touch the firewall; open 80/443 (or 8080 in local mode) yourself. The published image is built for `linux/amd64` and `linux/arm64`.

### Manual setup

You need Docker with Compose v2 (`docker compose ...`) for the panel host, and one Linux x86_64 host per node.

```bash
cd deploy
cp ../.env.example .env
# fill in MASTER_KEY and SESSION_SECRET (32 random bytes, base64 each):
sed -i.bak "s|^MASTER_KEY=.*|MASTER_KEY=$(openssl rand -base64 32)|" .env
sed -i.bak "s|^SESSION_SECRET=.*|SESSION_SECRET=$(openssl rand -base64 32)|" .env
sed -i.bak "s|^METRICS_TOKEN=.*|METRICS_TOKEN=$(openssl rand -hex 32)|" .env && rm .env.bak

# behind Caddy, on a real domain, with TLS:
docker compose up -d --build postgres panel caddy

# or, for local use without Caddy/TLS, expose the panel directly on :8080:
docker compose -f docker-compose.yml -f docker-compose.override.example.yml up -d --build postgres panel
```

`METRICS_TOKEN` is required too: with the default `NODE_DRIVER=gateway` the panel refuses to start without it (the commands above fill it in). `TELEMT_VERSION` and `TELEMT_SHA256_X86_64` come pre-filled in `.env.example`.

`PANEL_DOMAIN` (defaults to `localhost`) controls what Caddy requests a certificate for; for a real deployment point a DNS record at the host and set `PANEL_DOMAIN=panel.example.com` and `PANEL_PUBLIC_URL=https://panel.example.com` in `.env`. On `localhost`, Caddy issues an internal (self-signed) certificate: either trust its local CA (`docker compose exec caddy caddy trust`, or copy `/data/caddy/pki/authorities/local/root.crt` out of the `caddydata` volume into your OS/browser trust store) or curl it with `--insecure` / `-k`.

To run a published image instead of building from the checkout, use `deploy/docker-compose.release.yml` (the file the installer deploys: `panel` comes from `ghcr.io/greenpandorik/tgproxy-panel:${PANEL_VERSION:-latest}`, Caddy sits under the `caddy` profile, `deploy/docker-compose.local.yml` publishes :8080 for local mode), or replace the `build:` block of the `panel` service in `deploy/docker-compose.yml` with `image: ghcr.io/greenpandorik/tgproxy-panel:1.2.0` (the compose file has a comment at that spot). Each release also ships `panel-linux-{amd64,arm64}` and `tgwp-agent-linux-{amd64,arm64}` binaries with a `SHA256SUMS` file.

### First admin

The installer creates the first admin for you. On a manual setup the image ships with no admin users; create the first one directly in the running panel container:

```bash
docker compose exec panel /app/panel admin create <username> <password>
```

Add `owner`, `admin`, or `viewer` as a fourth argument to set the role (default `owner`). Log in at the panel's URL with that username and password.

### Adding a node

In the panel UI (or via `POST /api/v1/nodes`), create a node with a hostname and an ACME email. The response includes an `install_command` like:

```
curl -fsSL https://panel.example.com/api/v1/install/<token>.sh | sudo bash
```

Paste that into a root shell on a fresh Ubuntu/Debian x86_64 host with a public IPv4 address. The node's A record must already resolve to the host: before installing anything the script runs pre-flight checks (x86_64, systemd, the panel reachable, the public IP, the A record, ports 80/443 free) and, when DNS or a port is wrong, offers `[r] re-run the checks  [c] continue anyway  [q] quit` with nothing installed yet (`curl … | sudo TGWP_SKIP_PREFLIGHT=1 bash` goes ahead regardless). The node is registered with the panel only after Caddy holds a certificate and, on a telemt node, telemt reports ready, so an install that fails before the "Registration" step can simply be run again with the same command. What it installs depends on the node's engine (see below). The install token expires after 24 hours; regenerate one from the node page if it lapses before you run it.

## Node engines

A node is created with one of two engines, fixed at creation time, and the install script has a branch for each. Both branches write `/etc/sysctl.d/90-tgwp.conf` (BBR with `fq`, larger accept and SYN backlogs, TCP Fast Open, short keepalives) and apply it with `sysctl --system`; keys the kernel rejects are reported and skipped, and the file can be deleted.

**`telemt`** (the default) runs a single pinned telemt process:

- Caddy from the Caddy project's signed apt repository, terminating TLS on 443 and reverse-proxying to telemt's loopback WEB listener with `X-Forwarded-For`. Caddy serves no files: the decoy site is answered by telemt itself, so a request without a valid secret is handled by the same process as a proxied one.
- telemt `TELEMT_VERSION` from `https://github.com/telemt/telemt/releases`, verified against `TELEMT_SHA256_X86_64` before it is installed to `/usr/local/bin/telemt` (0755, root).
- A `telemt` system account (`useradd --system --home /var/lib/telemt --shell /usr/sbin/nologin`); the unit runs unprivileged.
- `tgwp-agent init-node --engine telemt`, which writes `/etc/telemt/telemt.toml`, the control-API token in `/etc/telemt/api.token` (0600), `/etc/systemd/system/telemt.service` and the decoy site in `/var/lib/telemt/public`.
- An nftables table `tgwp_telemt` (`/etc/nftables.d/tgwp-telemt.nft`, included from `/etc/nftables.conf`) that drops non-loopback traffic to the internal ports.
- The node's public IPv4 address, detected on the node (`https://api.ipify.org`, falling back to the outbound route) and reported at registration; telemt needs a concrete address for its WEB vhost. The install aborts if it cannot detect one; set `public_ip` on the node in the panel and re-run.

Ports on a telemt node: **80 and 443 public** (Caddy, ACME and the WEB transport), **`classic_port` public** (Fake-TLS, default 8443), and **9090 (metrics), 9091 (control API) and 18080 (WEB listener) loopback only**. The Fake-TLS listener runs with telemt's `synlimit` nftables rules (see Credits).

**`tproxy`** runs the older stack: `tproxy-server` at the pinned `TPROXY_COMMIT`, the official MTProxy, Caddy and the agent, installed by the upstream `deploy/install.sh`. A tproxy node exposes 80 and 443 only.

Keys behave differently per engine. A telemt node gives every key two links (WEB and Fake-TLS), enforces the key's `telemt_limits` (quota, up/down rate, max unique IPs, max connections) itself, and applies profile changes over its control API without restarting anything. A tproxy node offers the WEB link only, ignores those limits, and restarts the relay on every apply. `GET /api/v1/keys/{id}/links` returns one entry per node, `[{node_id, node_name, hostname, engine, links:[{kind, tme, tg}]}]`, with `kind` being `web` or `faketls`.

Changing `tls_domain` or `classic_port` on a telemt node is the one panel action that restarts telemt, and every Fake-TLS link already issued for that node stops working, because the domain and port are part of the link's secret. WEB links are unaffected.

The node readiness check includes a `pq_kex` probe: a TLS handshake to the node on 443 that reports whether the front negotiated the post-quantum hybrid group `X25519MLKEM768`. It is advisory and never fails the check.

## Credits

[telemt](https://github.com/telemt/telemt) is the proxy that runs on telemt nodes: one process serving the WEB transport, Fake-TLS, the decoy site and a control API. The panel pins a release (`TELEMT_VERSION`) and verifies its sha256 before installing it. telemt is distributed under its own license, TELEMT PL 3; the license notice stays with the binary they ship.

[MTPROTO_FIX_By_MEKO](https://github.com/Mekotofeuka/MTPROTO_FIX_By_MEKO) is the SYN rate-limit fix for the connection problems Telegram proxies started seeing in June 2026. telemt carries that fix as `synlimit`, which the panel enables on every Fake-TLS listener. The sysctl tuning the installer writes to `/etc/sysctl.d/90-tgwp.conf` is taken from that project.

[tproxy-server](https://github.com/telegramdesktop/tproxy-server) and MTProxy are what the tproxy engine runs: Telegram's WEB proxy relay and the official MTProxy behind it.

## Architecture

```
admin browser --HTTPS--> Caddy --> panel (Go binary, embedded SPA, HTTP + gRPC h2c) <--> PostgreSQL
                                        ^
                                        | gRPC, dialled out by the agent (h2c behind Caddy)
                                        |
node (telemt):  tgwp-agent --> telemt: WEB listener (loopback, behind Caddy :443),
                               Fake-TLS listener (:classic_port), decoy site, control API :9091
node (tproxy):  tgwp-agent --> tproxy-server + MTProxy, Caddy for TLS
```

The panel is one Go binary with the SPA embedded; it keeps everything in PostgreSQL. Nodes never accept inbound connections from the panel: the agent dials the panel's public URL, so a node needs only outbound reachability plus its own public ports. Changes to keys and sites mark a node dirty; a worker sweeps dirty nodes every `APPLY_INTERVAL` seconds or when you press Apply, and sends the desired state to the agent. On a telemt node the agent applies it over telemt's loopback control API without a restart; on a tproxy node it rewrites `profiles.json` and `mtproxy.env` and restarts `tproxy-server` and `mtproxy`, which drops live connections. Heartbeats from the agent carry service health and stats; a node with no heartbeat for `OFFLINE_AFTER` seconds is marked offline.

## Monitoring

The Monitoring page (`/monitoring`) shows every node's live sessions/streams and up/down traffic rate over a selectable window (1h/6h/24h/7d), backed by `GET /api/v1/monitoring/overview?from&to&step`:

```
GET /api/v1/monitoring/overview?from=2026-09-04T00:00:00Z&to=2026-09-05T00:00:00Z&step=60
```

Returns `{nodes: [{node_id, node_name, hostname, status}], series: {<node_id>: [{t, sessions_live, streams_live, bytes_up_rate, bytes_down_rate}]}}`. Rates are bytes/second computed from consecutive stats-snapshot deltas and clamped to 0 across a counter reset (e.g. a relay restart); each node's series is capped at 600 points. A `step` above 60 seconds is aggregated by the database into step-wide buckets (gauges averaged, counters carried by their closing value), so a wide window returns a bounded number of rows and each point summarises its bucket instead of being one arbitrary 60s sample. `from`/`to` default to the last 24 hours and the requested span is capped at 31 days; snapshots are retained for 30, so a wider window returns `400` rather than scanning the whole table. The same span cap applies to `GET /api/v1/monitoring/nodes/{id}/series`. Any authenticated role can read both.

For scraping instead of viewing, the panel exposes Prometheus text format at `/metrics` (outside the auth group, gated by `METRICS_TOKEN`):

```yaml
scrape_configs:
  - job_name: tgwp-panel
    metrics_path: /metrics
    scheme: https
    scrape_interval: 60s
    authorization:
      credentials_file: /etc/prometheus/tgwp-token
    static_configs:
      - targets: ["panel.example.com"]
```

`METRICS_TOKEN` is required whenever `NODE_DRIVER=gateway`; the panel refuses to start without it. Keep it out of the browser and give it only to your scraper.

See [`docs/monitoring.md`](docs/monitoring.md) for enabling `/metrics`, importing the Grafana dashboard ([`deploy/grafana/tgwp-panel.json`](deploy/grafana/tgwp-panel.json), scrape example at [`deploy/prometheus.example.yml`](deploy/prometheus.example.yml)), the full metric reference, and why there is no per-key breakdown.

## Audit log

The Audit page (`/audit`) lists every mutating action with who did it, when, and what changed, backed by `GET /api/v1/audit`. It supports server-side filtering:

| Query param | Meaning |
| --- | --- |
| `action` | Prefix match, e.g. `action=key.` matches `key.create`, `key.revoke`, etc. |
| `user` | Exact match on the acting admin's username. |
| `from`, `to` | RFC3339 timestamps, inclusive range on `created_at`. |
| `page`, `per_page` | Pagination. `per_page` caps at 200; `page` caps at 100000. |

Any authenticated role can read the audit log; only owner/admin actions ever appear as entries (viewers cannot mutate anything besides their own session/password, so nothing they do shows up here beyond that).

## Site presets

`GET /api/v1/site-templates` ships five built-in presets alongside any templates you create: `blog`, `docs`, `portfolio`, `product`, `studio`. Each is a small, self-contained static site (no forms, images, scripts, or external resources) meant to look like an ordinary small-business or personal page in front of the proxy.

Assigning a preset to a node runs it through **uniquification** first: block order, CSS class names, asset filenames, and any wording marked as having variants are all re-randomized per node, deterministically seeded from the node's ID. Class renaming skips `url(...)` bodies, quoted strings and comments in the CSS, so a stylesheet that references an asset (`background: url(/logo.png)`, `@font-face { src: ... }`) keeps working. Two nodes running the same preset therefore serve byte-different HTML/CSS, and the same node re-assigned the same preset without any change to the template gets the exact same output back (no accidental redeploy). The point is to defeat simple probing: an outside observer fingerprinting what a proxy's cover site looks like across your fleet by diffing HTML/class names/asset names will not find a repeating signature.

## Telegram alerts

Configure a bot token and chat ID under Settings, Telegram (`PUT /api/v1/settings`, owner only; the token is encrypted at rest and never sent back to the browser, only whether one is set). A "Send test message" button calls `POST /api/v1/settings/telegram/test` (owner only), which sends "Test message from `<panel name>`" using the form's in-progress token and chat ID if you typed them (neither is persisted by the test), else the stored ones, so the button works before you save. It returns `200 {"ok":true}` on success, `422` naming the missing field when neither the body nor the stored settings supply a token or a chat ID, or `502` with the Telegram API's error description on failure (e.g. wrong chat ID, blocked bot).

Once enabled, alerts fire automatically for:

- a node going offline (missed heartbeats past `OFFLINE_AFTER`) and coming back online,
- an apply job failing on a node (message includes the first line of the error).

Each `(node, alert kind)` pair is rate-limited to at most one message per 5 minutes, so a flapping node or a repeatedly failing apply does not spam the chat.

## Node prerequisite checks

The node Overview tab has a "Run check" button (writers only) that calls `POST /api/v1/nodes/{id}/check` and persists the result on the node (`last_check`, shown in `GET /api/v1/nodes/{id}`). It runs six probes in order (seven on a telemt node, which adds `mask`), sharing one 15-second budget:

| Check | Verifies |
| --- | --- |
| `dns_a` | The hostname resolves, and (if the node's IP is known) one of the resolved addresses matches it. |
| `tcp_80` | Port 80 is reachable, needed for ACME HTTP-01 and any HTTP to HTTPS redirect. |
| `tcp_443` | Port 443 is reachable. `tls_cert`, `pq_kex` and `http_root` are skipped (not failed) if this fails, since they depend on it. |
| `tls_cert` | A valid TLS handshake completes for the hostname, the certificate chain and hostname verify, and it is not within 7 days of expiring. |
| `pq_kex` | The TLS front negotiates the post-quantum hybrid key exchange `X25519MLKEM768`; the detail names the group that was negotiated. Advisory: its result is shown but never counted in the check's overall pass/fail. |
| `http_root` | `GET https://<hostname>/` returns 200 with a non-empty body. Redirects are not followed (a checked node cannot use this to make the panel issue requests elsewhere). |
| `mask` | telemt nodes only: a TLS handshake to `hostname:classic_port` with `tls_domain` as SNI and no MTProto secret is answered with a certificate valid for `tls_domain`, so the Fake-TLS listener looks like the site it masks. |

## Two-factor authentication

Set `FEATURE_TOTP=true` to turn on TOTP two-factor login; the routes 404 `feature_disabled` when it is off. Enrolment is self-service for every role, from Settings, Security:

1. **Turn on** starts a setup (`POST /api/v1/auth/totp/setup`) and shows a QR code plus the secret in a copyable field, for apps that cannot scan (Google Authenticator, Aegis, 1Password, any standard 30s/6-digit/SHA1 TOTP app all work).
2. Type the 6-digit code the app shows **and your current password** to confirm (`POST /api/v1/auth/totp/confirm {password, code}`). The password is what stops a stolen live session from enrolling *its own* authenticator and locking the real owner out; confirming also ends every other session on the account, the way a password change does. This enables TOTP and shows **eight recovery codes once** (`xxxxx-xxxxx`, high-entropy, stored only as a sha256 hash, never re-displayed) with copy-all and download-`.txt` buttons. Save them somewhere safe before dismissing the dialog.
3. From then on, `POST /api/v1/auth/login` returns `200 {totp_required:true, challenge:<token>}` with no session for that account; the login page's second step calls `POST /api/v1/auth/totp/verify {challenge, code}` (or `{challenge, recovery_code}`) to finish. A wrong code counts toward the same account lock and per-IP rate limit as a wrong password. The challenge is valid for five minutes: past that, verify answers `401 challenge_expired` (distinct from `invalid_code`) and the login page offers "Start over" instead of telling the user their code is wrong.
4. **Turn off** (`POST /api/v1/auth/totp/disable {password, code|recovery_code}`) needs the password *and* a second factor, so a stolen live session alone cannot strip 2FA.

If an admin loses both their authenticator and their recovery codes, see [Two-factor authentication lockout](docs/runbook.md#two-factor-authentication-lockout). The escape hatch is a host-side CLI command, not a panel route:

```bash
docker compose exec panel /app/panel admin totp-reset <username>
```

## Subscription links

Every access key can get a public, unauthenticated "subscription" link: one URL an end user opens on their phone to see every node the key is bound to, as a QR code and t.me/tg:// links, without ever seeing the panel. From the key's detail drawer or link dialog:

- **Create/rotate**: `POST /api/v1/keys/{id}/subscription` returns `{url, qr_data_uri}` once (the database only ever stores the token's sha256 hash). Calling it again rotates: the old link stops working immediately.
- **Revoke**: `DELETE /api/v1/keys/{id}/subscription`.
- The key JSON carries `subscription_active: bool` so the UI can show whether a link is currently live.
- The link itself, `GET /s/{token}` (HTML) or `GET /s/{token}.json`, is public, rate-limited to 60 requests/minute per IP, never cached (`Cache-Control: no-store`), and never reveals the key's label, owner label or note, only node names, hostnames, links and QR codes. An unknown or already-revoked token answers 404; a token whose key has since been revoked answers 410, both as a small branded HTML page on the human-facing route, and as JSON on the `.json` route.

## Branding profiles

Settings, Branding manages one or more named profiles (panel name, primary/accent color, default theme, support link, footer text, logo/favicon uploads); the active one styles the login page, the SPA header, and every public subscription page. Owner/admin only:

| Route | Behaviour |
| --- | --- |
| `GET /api/v1/branding/profiles` | List every profile. |
| `POST /api/v1/branding/profiles {name}` | Create a new profile (not yet active). |
| `PUT /api/v1/branding/profiles/{id}` | Edit a profile's fields. |
| `POST /api/v1/branding/profiles/{id}/activate` | Make this profile the active one. |
| `POST /api/v1/branding/profiles/{id}/upload` | Upload a logo/favicon asset (SVGs are parsed and rejected if they contain script vectors). |
| `DELETE /api/v1/branding/profiles/{id}` | Delete a profile. Refuses on the currently active one; activate another profile first. |

`GET /api/v1/branding` (public, no auth) serves the subset the login page and public pages need before anyone is signed in.

## Backups and restore

Settings, Backups (owner only) takes on-demand dumps and can schedule nightly ones, using `pg_dump`/`pg_restore` bundled in the panel image:

| Route | Behaviour |
| --- | --- |
| `POST /api/v1/backups` | Runs `pg_dump --format=custom` inline and records a row. `201` with `{id, name, size, kind, created_at}`. One dump at a time; a second request while one is running gets `409 backup_running`. |
| `GET /api/v1/backups` | List every dump (manual and scheduled) with size and timestamp. |
| `GET /api/v1/backups/{id}/download` | Streams the dump file. |
| `DELETE /api/v1/backups/{id}` | Deletes the row and the file. `204`. |

The **nightly backup** setting (part of `GET`/`PUT /api/v1/settings`, field `backup_schedule: {enabled, hour, keep}`) picks a UTC hour and how many scheduled dumps to retain (1 to 60, oldest deleted first); it never touches a dump you took by hand. A dump is the whole database. Encrypted secrets stay encrypted, since `MASTER_KEY` is never stored in the database, so **back up `MASTER_KEY`/`MASTER_KEY_V*`/`SESSION_SECRET` separately**, or a restored database cannot decrypt a single profile secret or agent token.

Restore is a destructive CLI command, not a panel route (`pg_restore --clean --if-exists` drops every existing object first): see [Backups and restore](docs/runbook.md#backups-and-restore) in the runbook for the full stop-panel/restore/start-panel procedure.

## Key rotation

`MASTER_KEY` encrypts every profile secret, access-key secret, TOTP secret and the stored Telegram bot token at rest. `panel keys rotate` re-encrypts every row under a new key version in one transaction, so an old key can eventually be retired:

```bash
docker compose stop panel                      # rotation refuses to run under a live panel
docker compose run --rm panel keys rotate --dry-run   # preview: row counts only, nothing written
docker compose run --rm panel keys rotate             # re-encrypts, prints the env lines to set next
```

It reads the new key from `MASTER_KEY_NEW` (32 random bytes, base64) and refuses to run if that equals the current `MASTER_KEY`, since rotating onto the same key material would just burn a version number. It never prints key material, only row counts and env-variable placeholders. Full procedure, including what to do with the old key afterwards, in [Master key rotation](docs/runbook.md#master-key-rotation).

## Grafana

A ready-made dashboard (node/key counts, live sessions/streams) lives at [`deploy/grafana/tgwp-panel.json`](deploy/grafana/tgwp-panel.json), built on top of the `/metrics` Prometheus endpoint described under [Monitoring](#monitoring) above. See [`docs/monitoring.md`](docs/monitoring.md) for importing it and the full metric reference.

## Roles

| Role | Can do |
| --- | --- |
| `owner` | Everything, including panel settings (`PUT /settings`), the Telegram test button, and managing admin accounts (`/admins`). |
| `admin` | Everything except panel settings and admin account management: nodes, keys, site templates, applies, node checks, resolving alerts, branding. |
| `viewer` | Read-only everywhere. The only mutations a viewer can make are logging out, changing their own password, and enrolling or removing their own second factor; every other write returns 403. |

## Environment reference

All variables live in `.env.example`; copy it to `.env` and fill in the blanks.

| Variable | Purpose |
| --- | --- |
| `DATABASE_URL` | Postgres connection string for the panel. In the compose deployment it is set by `docker-compose.yml` itself. |
| `MASTER_KEY` | 32 random bytes, base64. Encrypts profile/key secrets at rest. **Never commit this.** |
| `MASTER_KEY_VERSION` | Which master key is currently active for new writes (starts at `1`). |
| `MASTER_KEY_NEW` | Set only while running `panel keys rotate`; not read anywhere else. |
| `SESSION_SECRET` | 32 random bytes, base64. Signs admin session cookies. **Never commit this.** |
| `PANEL_HTTP_ADDR` | Listen address for the panel's HTTP+gRPC(h2c) server (default `:8080`). |
| `PANEL_PUBLIC_URL` | The externally reachable URL admins and nodes use to reach the panel, no trailing slash. |
| `DATA_DIR` | Where the panel stages the agent binary it serves to nodes for install/upgrade. |
| `NODE_DRIVER` | `gateway` (real gRPC agents) or `mock` (for tests/demos without real nodes). |
| `METRICS_TOKEN` | **Required when `NODE_DRIVER=gateway`** (the panel refuses to start without it). `/metrics` is mounted at the panel root, outside the auth group, so it requires `Authorization: Bearer <token>`. Only `NODE_DRIVER=mock` (tests/demos) may leave it empty. |
| `TPROXY_COMMIT` | The `tproxy-server` commit the install script pins nodes to. Must be 7-40 lowercase hex characters; it is interpolated into a root-run script. |
| `TELEMT_VERSION` | The telemt release telemt nodes install (default `3.5.6`). A three-part version like `3.5.6`; it is interpolated into a root-run script. |
| `TELEMT_SHA256_X86_64` | **Required when `NODE_DRIVER=gateway`.** sha256 of the release asset `telemt-x86_64-linux-gnu.tar.gz` for `TELEMT_VERSION`, 64 hex characters. The install script verifies the download against it before running anything, so a wrong or empty value is the difference between a pinned install and an unverified one. Change it together with `TELEMT_VERSION`. |
| `TELEMT_SHA256_MUSL_X86_64` | sha256 of the same release's `telemt-x86_64-linux-musl.tar.gz`. Used only by the `fakenode-telemt` demo image. |
| `GITHUB_REPO` | Repository the update chip reads releases and stars from (default `greenpandorik/tgproxy-panel`). |
| `GITHUB_TOKEN` | Optional GitHub token for the update check, to raise the unauthenticated rate limit. Never logged. |
| `UPDATE_CHECK` | `true` (default) lets the panel call the GitHub API for the update chip; `false` disables every outbound call; the version chip stays, without the update highlight, and the star count is not shown. |
| `FEATURE_TOTP` | Enables TOTP two-factor login when `true`. |
| `LOG_LEVEL` | `debug`, `info`, `warn`, or `error`. |
| `APPLY_INTERVAL` | Seconds between sweeps that re-apply state to nodes marked dirty. |
| `OFFLINE_AFTER` | Seconds without a heartbeat before a node is marked offline. |
| `PANEL_DOMAIN`, `ACME_EMAIL` | Read by the compose Caddy service only: the domain to obtain a certificate for and the ACME account email. |
| `POSTGRES_PASSWORD` | Read by the compose files only: the password of the bundled PostgreSQL (the installer generates one; `DATABASE_URL` for the panel is derived from it). |
| `PANEL_VERSION`, `PANEL_IMAGE` | Read by `deploy/docker-compose.release.yml` only: the image tag to run (`1.0.0`, `latest`), or a full image reference that replaces `ghcr.io/greenpandorik/tgproxy-panel:<PANEL_VERSION>` (mirrors, tests). Written by `install.sh`. |
| `TEST_DATABASE_URL` | Postgres connection string used by `go test` (see below). |

The update check calls `GET https://api.github.com/repos/<GITHUB_REPO>/releases/latest` and `GET https://api.github.com/repos/<GITHUB_REPO>`, caches the answer server-side for an hour, and serves the last good answer (`stale: true`) when a fetch fails. The result is exposed at `GET /api/v1/status/update` for any authenticated role. A build whose version is not a release number (for example `dev`) is never marked outdated.

## Development

You need Go 1.26+, Node 22+, and a local PostgreSQL 16 (`brew install postgresql@16 && brew services start postgresql@16` on macOS). Create `tgwp` and `tgwp_test` databases and point `DATABASE_URL` / `TEST_DATABASE_URL` at them (see `.env.example`).

```bash
make run          # go run ./cmd/panel serve, using your local .env
cd web && npm ci && npm run dev   # SPA dev server with API proxy
```

Run the test suite with:

```bash
make test         # go test -p 1 ./...
```

Tests run with `-p 1` (one package at a time) because the Go test suite shares a single Postgres database (`TEST_DATABASE_URL`) across packages; running packages in parallel would race on that shared state.

Before sending changes, also run:

```bash
gofumpt -l ./cmd ./internal ./proto   # should print nothing
golangci-lint run ./...
cd web && npm run typecheck && npm run lint && npm run test
```

`make build` produces the panel binary for this machine; `make build VERSION=v1.2.3` injects the version into `internal/version.Version` at link time, the way the release workflow does. See [CONTRIBUTING.md](CONTRIBUTING.md) for conventions and the release procedure.

## fakenode and the end-to-end smoke test

Both engines have a containerised demo node, so the whole panel to agent to relay path can be exercised without a VPS:

```bash
make e2e          # tproxy engine: deploy/Dockerfile.fakenode (real tproxy-server + a stub MTProxy)
make e2e-telemt   # telemt engine: deploy/Dockerfile.fakenode-telemt (the real telemt release binary)
```

Each target brings up postgres + the panel with the override file on `:8080`, creates an admin and a node of that engine, runs the demo container against a fresh install token, drives the flow end to end (assigns a key and the `studio` preset site, applies, asserts the relay serves that site with the key's profile active) and prints `SMOKE OK`, then tears the stack down with its volumes.

`deploy/fakenode` runs the real `tproxy-server` relay (built from the pinned commit) plus a stub MTProxy backend, wired up with shims for `systemctl`/`journalctl` so the unmodified agent can manage it exactly as it would a real Linux host. To drive the flow by hand, see `docs/runbook.md` or run `deploy/e2e-smoke.sh` directly; it needs the two-phase `--continue` flag because the fakenode container cannot start until a node (and its install token) exists.

The telemt image downloads `telemt-x86_64-linux-musl.tar.gz` for `TELEMT_VERSION` and verifies it against `TELEMT_SHA256_MUSL_X86_64` (the musl asset is statically linked, so it runs on a slim Debian image; real nodes install the gnu asset pinned by `TELEMT_SHA256_X86_64`). telemt publishes x86_64 Linux builds only, so the container runs as `linux/amd64`, emulated and therefore slower on an arm64 host. To keep the stack up and poke at it by hand, run the phases yourself: `deploy/e2e-smoke-telemt.sh` prints an install token, `docker compose --profile dev-telemt up -d --build fakenode-telemt` starts the node, and `deploy/e2e-smoke-telemt.sh --continue` runs the assertions. `docker compose logs fakenode-telemt` shows the node's own startup, and `docker compose exec fakenode-telemt journalctl -u telemt` its telemt log.

The telemt demo node differs from a real one in three ways, all forced by the container:

- **No Caddy.** The panel only ever talks to the agent, and telemt's WEB listener is loopback-only, so there is nothing for a TLS terminator to do here (and no certificate it could get for a fictional domain).
- **`init-node --no-synlimit`.** telemt installs its own nftables rules for the Fake-TLS listener at startup and aborts if it cannot; an unprivileged container has neither `CAP_NET_ADMIN` nor a writable netfilter namespace.
- **`init-node --no-tls-emulation`.** telemt learns the TLS fingerprint of the real `tls_domain` by connecting to it on 443. `fakenode-telemt.local` resolves nowhere, so the profile stays the built-in fallback, which telemt tolerates at startup but refuses on a runtime reload, failing every apply.

Both flags exist for that bench only. Real nodes run with the synlimit rules and with TLS emulation on, which is what makes the Fake-TLS listener look like the site it masks behind.

**`make e2e-telemt` needs outbound reachability to Telegram's DCs.** telemt's `/v1/health/ready` reports `no_healthy_upstreams` until it has a healthy upstream, and the smoke test waits for the node to come online, so an environment that blocks that egress fails the target with a node that never leaves `pending`, not with a defect in the panel. `docker compose exec fakenode-telemt journalctl -u telemt` names the cause.

## Installer test

`make test-install` (`deploy/test-install.sh`) checks `install.sh` end to end: it runs `bash -n` and shellcheck, builds the panel image from the checkout, starts a privileged `docker:27-dind` container with the repository mounted, loads the image into it and runs the installer there in `--local --yes --from-checkout` mode. It then asserts the generated `.env` (every key, mode 0600), the running stack, `/healthz`, an admin login via `POST /api/v1/auth/login`, an `--update` pass, and that `--uninstall --purge` leaves no containers, volumes or install directory behind. Needs Docker on the host; shellcheck is installed with brew when missing.

## Security notes

- Profile and access-key secrets (and the Telegram bot token) are encrypted at rest with `MASTER_KEY` (AES-GCM). Losing `MASTER_KEY` makes existing encrypted data unrecoverable; back it up somewhere separate from the database.
- Node agent tokens and install tokens are **not** encrypted: only their SHA-256 hash is stored, and the plaintext is shown once at registration. A database leak therefore cannot yield a usable agent token.
- `/metrics` publishes node UUIDs and node/key counts. It is protected by `METRICS_TOKEN`, which is mandatory in gateway mode; keep it out of the browser and give it only to your scraper.
- Uploaded branding SVGs are parsed with an XML decoder and rejected if they contain script vectors, and every branding asset is served with `Content-Security-Policy: default-src 'none'; style-src 'unsafe-inline'; sandbox` plus `X-Content-Type-Options: nosniff`.
- Session cookies are signed with `SESSION_SECRET` and are `HttpOnly`; the CSRF cookie is deliberately not, since it is read back and echoed in the `X-CSRF-Token` header (double-submit pattern).
- The update check is the only outbound call the panel makes that you did not configure yourself (Telegram alerts go to the bot API only once a bot token is set). It sends no data about your deployment beyond the request itself, and `UPDATE_CHECK=false` turns it off.
- Never commit `.env`, `MASTER_KEY`, or `SESSION_SECRET` to version control. `deploy/.env` is meant to stay local to the host running Compose.
- Rotate `MASTER_KEY` with `panel keys rotate` (stop the panel first); see "Master key rotation" in `docs/runbook.md`.

## Status

Version 1.2.0. Both engines pass the containerised end-to-end tests (`make e2e`, `make e2e-telemt`). The install script and real Telegram clients have not yet been exercised on a public VPS by the maintainers: do the first production install on a test VPS and verify a connection from Telegram Desktop before relying on it. Issues and pull requests are welcome.

## License

Copyright (C) 2026 greenpandorik. AGPL-3.0, see [LICENSE](LICENSE). telemt, which the panel installs on telemt nodes, is distributed under its own license (TELEMT PL 3); the panel does not bundle it.
