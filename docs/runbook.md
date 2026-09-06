# Operations runbook

## Upgrading the panel

The panel container has no state of its own — everything lives in Postgres and the `paneldata` volume (which only holds the agent binary staged for node installs/upgrades). To upgrade:

```bash
cd deploy
docker compose pull        # or: docker compose build panel, if you build locally
docker compose up -d panel
```

On start, `panel serve` runs pending migrations and re-seeds preset site templates before it starts listening, so a version bump is a plain restart. If a migration ever needs to run standalone (e.g. before a blue/green cutover), use `docker compose run --rm panel migrate`.

Nodes are not affected by a panel upgrade unless you also bump `TPROXY_COMMIT` and reinstall — see "Reinstalling the agent" below.

## Master key rotation

`MASTER_KEY` encrypts profile secrets, access-key secrets, TOTP secrets and the stored Telegram bot token at rest (AES-GCM via `internal/crypto.Box`; every ciphertext blob is prefixed with a 2-byte key version). The panel decrypts with whichever version a row was written under (`MASTER_KEY_V<n>` for every version below the current one) and encrypts new/updated rows with the current `MASTER_KEY_VERSION`. `panel keys rotate` re-encrypts every existing row under a new key version, so an old key can eventually be retired.

Procedure:

1. **Stop the panel.** `panel keys rotate` refuses while the panel holds the database lock — rotating under a running panel would race its own connections against the transaction rewriting every encrypted row.

   ```bash
   docker compose stop panel
   ```

2. **Generate the new key and set `MASTER_KEY_NEW`** (32 random bytes, base64 — same format as `MASTER_KEY`):

   ```bash
   openssl rand -base64 32
   # put the result in .env as MASTER_KEY_NEW=<value>
   ```

3. **Run the rotation.** `--dry-run` first counts rows per column without writing anything, if you want a preview:

   ```bash
   docker compose run --rm panel keys rotate --dry-run
   docker compose run --rm panel keys rotate
   ```

   This re-encrypts `profiles.secret_enc`, `access_keys.secret_enc`, `admin_users.totp_secret_enc`, `admin_users.totp_pending_enc` and `settings.telegram_alerts.bot_token_enc` in one transaction: every row not already at the new version is decrypted with the current key(s) and re-encrypted with the new one, and every new blob is decrypted back and compared against the original plaintext before commit. Any failure — a row that won't decrypt, a verification mismatch — rolls the whole transaction back, so a failed rotation changes nothing. The command prints a per-column count of rows it touched and the env lines to set next, with placeholders instead of the actual key material — it never prints a key.

4. **Update the environment** with the printed lines, filling in the placeholders yourself: the new key goes to `MASTER_KEY`, the version increments, and the *old* `MASTER_KEY` value becomes a new `MASTER_KEY_V<n>` entry (kept only so any row a rotation missed, or a backup restored from before the rotation, can still be decrypted — see below).

   ```
   MASTER_KEY=<new key, base64>
   MASTER_KEY_VERSION=<n+1>
   MASTER_KEY_V<n>=<old key, base64>   # the key MASTER_KEY held before this rotation
   ```

   Remove `MASTER_KEY_NEW` from the environment — it is read only by `keys rotate`.

5. **Start the panel.**

   ```bash
   docker compose up -d panel
   ```

Notes:

- Running `keys rotate` again after a successful rotation is a no-op: every row is already at the target version, so it reports all zero counts. It is safe to run more than once.
- Do not remove an old `MASTER_KEY_V<n>` while any row — or any backup you might restore — still references it; a restored backup taken before a rotation still holds ciphertext at the old version, and restoring it needs that key to be present in the environment even after the live database has moved on.
- `keys rotate` needs a database connection but talks to Postgres directly, the same way `db backup`/`db restore` do — it does not start the HTTP server.

## Update check

The topbar's version chip and the star count come from `GET /api/v1/status/update`, which
the panel answers from GitHub repository metadata: `GET https://api.github.com/repos/<GITHUB_REPO>/releases/latest`
(latest tag, release page, publish date) and `GET https://api.github.com/repos/<GITHUB_REPO>`
(stars). Only public repository metadata is fetched; nothing about the installation — its
version, hostname, node or key counts — is sent. The comparison against the running version
happens locally, and a `dev` build is never reported as outdated.

The answer is cached for an hour and refreshed lazily on the next request after that, one
refresh at a time. When GitHub is unreachable or rate-limits the panel (anonymous calls get
60 requests/hour per source IP), the response keeps the last good numbers with
`"stale": true` and the panel waits at least 5 minutes before trying again. Setting
`GITHUB_TOKEN` to a token with no scopes raises the limit to 5000/hour; the token is sent as
`Authorization: Bearer` and never logged.

To disable the check entirely set `UPDATE_CHECK=false` and restart the panel: the endpoint
then answers `"enabled": false` with only `current` and `repo_url` filled in, and no request
to GitHub is ever made.

## Two-factor authentication lockout

Two-factor login is off unless `FEATURE_TOTP=true`. When it is on, an admin who enrolled and then lost both their authenticator app and their eight recovery codes cannot finish a login at all: the password step returns only a five-minute challenge, and the only routes that can turn the second factor off (`POST /api/v1/auth/totp/disable`) need the session that login would have created. For the last owner this locks the whole installation out.

The escape hatch runs on the host, where shell access is already a higher bar than the panel login:

```bash
docker compose exec panel /app/panel admin totp-reset <username>
```

It clears `totp_enabled` and the stored secret and deletes that user's recovery codes; the account goes back to password-only login and can enrol again from Settings -> Security. It affects one named user, never all of them.

Notes:

- The reset writes an audit entry in the same transaction: action `auth.totp_reset`, target the named admin, `ip` recorded as `cli`, meta `{"username": ...}`. Its `admin_user_id` is NULL, because the actor is a shell on the host rather than a panel account — filter the audit log by `auth.` to see it next to the logins it explains.
- Recovery codes are shown exactly once, when the enrolment is confirmed, and stored only as sha256 hashes. There is no way to re-display them; a user who wants a fresh set disables and re-enrols.
- Turning `FEATURE_TOTP` back to `false` also unblocks a locked-out user, but it does so for every account at once and leaves the enrolment in the database — prefer `totp-reset` for a single person.

## Backups and restore

The panel takes its own dumps with `pg_dump --format=custom` (the postgres client tools are in the panel image) and writes them to `DATA_DIR/backups` — `/data/backups` on the `paneldata` volume under compose. A dump is the whole database; encrypted secrets stay encrypted, because `MASTER_KEY` lives in `.env` and never in the database. **Back up `MASTER_KEY`, `MASTER_KEY_V*` and `SESSION_SECRET` separately** — a database restored without the matching master key cannot decrypt a single profile secret or agent token.

**Process visibility:** `pg_dump`/`pg_restore` receive the database URL (including its password) as a command-line argument, so it is visible in `ps aux` (and `/proc/<pid>/cmdline`) to any other process on the same host or in the same container namespace for the short time the dump/restore runs — this is how libpq tools take a DSN and is not something the panel can avoid short of a `~/.pgpass`/service-file rewrite. It is why the panel host is expected to be **single-tenant**: nothing untrusted should ever run alongside the panel container, and `docker compose exec`/`run` access to the panel image should be held to the same bar as host shell access.

### Taking a backup

- **From the UI:** Settings → Backups → "Create backup" (owner only). The table lists every dump with its size, source and time; each row downloads or deletes. One dump runs at a time — a second request while one is in flight is refused with `backup_running`.
- **From the host:**

```bash
docker compose exec panel /app/panel db backup
```

It writes the same file and the same row, so a dump taken from a cron job on the host shows up in the UI.

### Nightly backups

Settings → Backups → "Nightly backup": pick a UTC hour and how many dumps to keep (1–60). The worker checks every 10 minutes and dumps once a day at that hour; dumps beyond the limit are deleted oldest first, **files and rows**. Retention only ever touches scheduled dumps — a backup you took by hand is yours until you delete it.

There is no catch-up: a panel that was down through its whole window skips that night rather than dumping at boot.

### Restoring

The restore is destructive (`pg_restore --clean --if-exists` drops every existing object first) and cannot run under a live panel, so it is a CLI command and it refuses twice: without `--yes`, and while the panel holds the database lock. Stop the panel first — there is no lock file to clear, and the command refuses while the panel holds the database lock.

```bash
cd deploy
docker compose exec panel /app/panel db backup      # 1. a fresh dump of what you are about to replace
docker compose stop panel                           # 2. the restore refuses while the panel is running
docker compose run --rm panel db restore /data/backups/tgwp-20260101T030000Z-manual.dump --yes
docker compose start panel                          # 3. the panel reloads everything from the restored database
```

Notes:

- `docker compose exec panel /app/panel db restore ... --yes` deliberately fails with "the panel holds the database lock, so it is still running": `exec` needs the panel process alive, which is exactly the situation the guard exists for. Use `stop` + `run --rm` as above.
- The file argument may be a bare name or the full `/data/backups/...` path, but it must resolve inside the backup directory. Restoring an arbitrary file from the host is not supported — copy it into `/data/backups` first.
- A dump never contains its own `backups` row (the row is written after `pg_dump` finishes), so right after a restore the Backups tab lists what existed when the dump was taken, minus that dump. The files on disk are untouched.
- Restore with the same `MASTER_KEY`/`MASTER_KEY_V*` the dump was taken under, or every encrypted secret becomes unreadable.
- There is no lock file. `panel serve` takes a Postgres [advisory lock](https://www.postgresql.org/docs/current/functions-admin.html#FUNCTIONS-ADVISORY-LOCKS) on the shared database and holds it on a dedicated connection for as long as it runs; `db restore` and `keys rotate` try to take the same lock and refuse when they cannot. That works across containers and hosts — which a pid never could, since a running `panel serve` under Compose and a `docker compose run --rm panel` recovery container are both pid 1 — and Postgres releases the lock by itself when the connection ends, so a panel that was killed leaves nothing behind to clear. It also means a second `panel serve` against the same database exits immediately with "another panel instance holds the lock".

### Restoring somewhere else

`pg_restore` is a stock postgres tool, so a dump can also be loaded outside the panel — into an empty database on another host, for instance:

```bash
pg_restore --clean --if-exists --no-owner --dbname "postgres://tgwp:...@host:5432/tgwp" tgwp-20260101T030000Z-manual.dump
```

## A node shows `rolled_back`

The agent's apply is transactional per-node: it backs up the current `profiles.json`/`mtproxy.env`/site before writing anything, and if the relay fails to come back healthy after a restart, it restores that backup and reports `rolled_back` rather than leaving the node half-applied.

1. Open the node's job history (`GET /api/v1/nodes/{id}/jobs`, or the Jobs tab in the UI) and read the failed job's log — it records exactly which step failed (`-check` rejected profiles, restart failed, or health didn't come back).
2. If the log says `relay -check rejected profiles: ...`, the desired state itself is invalid for that node's relay config (e.g. a limit that exceeds the node's global limits). Fix the key/profile/limits that caused it and re-apply.
3. To reproduce independently, SSH to the node and run the same check the agent ran:
   ```bash
   tproxy-server -config /etc/tproxy-server/config.json -profiles-file /etc/tproxy-server/profiles.json -check
   ```
4. If the log says `rollback FAILED; manual intervention needed`, the automatic restore itself didn't fully succeed — SSH in, compare `/etc/tproxy-server/profiles.json` and `/etc/mtproxy/mtproxy.env` against the backups under `/var/lib/tgwp-agent/backup/<timestamp>/`, and restore by hand before retrying `apply`.

## A node shows offline

`offline` means the panel hasn't seen a heartbeat from that node's agent in `OFFLINE_AFTER` seconds (default 90). On the node:

```bash
systemctl status tgwp-agent
journalctl -u tgwp-agent -n 200 --no-pager
```

Common causes: the agent process crashed or was never started (`systemctl status` will show it), the node can't reach `PANEL_URL` (check DNS/firewall/outbound HTTPS), or the node's agent token was revoked (re-issue via "Reinstalling the agent" below). `systemctl status tproxy-server` and `mtproxy` are also worth checking — the agent stays up independently of the relay, so a node can be "online" in the panel while the relay itself is `degraded`.

## Reinstalling the agent

If a node's agent token is lost, revoked, or the host was rebuilt, regenerate an install command from the node page (or `GET /api/v1/nodes/{id}/install-command`) and re-run it on the host:

```bash
curl -fsSL https://panel.example.com/api/v1/install/<new-token>.sh | sudo bash
```

This is safe to re-run on a host that already has `tproxy-server` installed — the installer skips the official `install.sh` step if `/usr/local/bin/tproxy-server` already exists, and just re-registers the node and refreshes `/etc/tgwp-agent/agent.env` and the `tgwp-agent` systemd unit with a fresh token.

## Relay restart caveat (tproxy engine)

This is a property of the `tproxy` engine only; on a telemt node an apply never restarts anything (see "telemt nodes" below). Every apply that changes profiles, MTProxy secrets, or the site ends with `systemctl restart tproxy-server` (and `mtproxy` if secrets changed). `tproxy-server` has no live-reload: existing sessions on that node are dropped when it restarts, and the site it serves is loaded into memory once at startup, so a site-only change also requires — and gets — a restart. Prefer applying during low-traffic windows for busy nodes; the panel does not currently stagger or schedule applies for you.

## telemt nodes

Everything below applies to nodes created with the `telemt` engine (`GET /api/v1/nodes/{id}` shows `engine`). Design notes: [`docs/superpowers/specs/2026-09-06-telemt-engine.md`](superpowers/specs/2026-09-06-telemt-engine.md); the upstream research this is based on: [`docs/research/2026-09-06-telemt-and-meko.md`](research/2026-09-06-telemt-and-meko.md).

**Where things live.** Config `/etc/telemt/telemt.toml` (0600, owned by the `telemt` account, which telemt rewrites itself through its control API — never hand-edit it while telemt is running). Control-API token `/etc/telemt/api.token`, 0600, generated by `init-node` and read by the agent; it is the whole authentication for the API, so treat it like a password and never paste it into a ticket. Cover site `/var/lib/telemt/public`. Unit `/etc/systemd/system/telemt.service`. Network tuning `/etc/sysctl.d/90-tgwp.conf` (BBR + fq, larger backlogs, TCP Fast Open, short keepalives — adopted from MTPROTO_FIX_By_MEKO; written by the installer for both engines, applied with `sysctl --system`, safe to delete if a host needs its own values). Logs `journalctl -u telemt` (and `journalctl -u tgwp-agent` for the agent's side of the conversation).

**Talking to the API by hand** (on the node, as root — it listens on loopback only and rejects anything else):

```bash
TOKEN="$(tr -d '\r\n' < /etc/telemt/api.token)"
# -H @- reads the header from stdin, so the token is never in the process argument list
# (/proc/*/cmdline is world-readable). Never pass it as -H "Authorization: $TOKEN".
tapi() { curl -s -H @- "http://127.0.0.1:9091$1" <<<"Authorization: $TOKEN"; }
tapi /v1/health/ready
tapi /v1/system/info    # running version
tapi /v1/users          # users, quotas, live counters
tapi /v1/config         # editable config: censorship, server.listeners, web, general
```

**Apply is an API call, not a restart.** The agent reconciles users (`/v1/users`), pushes the WEB profile array (`PATCH /v1/config`) and, when the cover site changed, asks for a draining runtime reload (`POST /v1/system/reload`). Live sessions survive all of it; the job log says `no restart` and `ApplyResult.RestartedRelay` stays false. If a job log ever mentions a restart on a telemt node, that is a bug worth reporting, not normal behaviour.

**What does need a restart** (`systemctl restart telemt`, which does drop live sessions): changing listeners — `classic_port`, the Fake-TLS/WEB listener definitions, `synlimit*` — and `web.limits`. telemt reports anything it had to postpone as `deferred_process_fields` on a reload, and the agent copies that into the job log as `warning: telemt deferred … until a process restart`.

**The one thing in the panel that produces a restart** is the node page's Fake-TLS card: saving a new `tls_domain` or `classic_port` marks the node dirty, and the next apply patches `[censorship] tls_domain`, the Fake-TLS entry of `[[server.listeners]]` and `[general.links] public_port` on the node, then restarts telemt and waits for `/v1/health/ready`. The job log names each field it moved, says `restarting telemt: the Fake-TLS listener is process-owned`, and `ApplyResult.RestartedRelay` comes back true — the only telemt apply for which it does. If the restart fails, the apply restores the previous domain/listener/link port and restarts telemt back onto them. **Every Fake-TLS link already issued for that node stops working**: the domain and port are baked into the link's `ee…` secret, so the links must be reissued from the panel afterwards. WEB links are unaffected.

**Draining reloads take time, and that is not a failure.** A profile change asks telemt for a draining runtime reload, which activates the new generation immediately and then lets the old generation's sessions finish for up to 30 s. The agent waits `30 s + 30 s` for it. If the operation is still `draining` when that budget runs out, the job log says `reload N still draining after 1m0s, new generation active` and the apply **succeeds** — the change is already live, only the old sessions are still winding down. Only a reload that ends in `failed`/`rolled_back` rolls the apply back.

**API change (telemt phase).** `GET /api/v1/keys/{id}/links` now returns one entry per node rather than a flat list of links: `[{node_id, node_name, hostname, engine, links:[{kind, tme, tg}]}]`, where `kind` is `web` or `faketls`. The old shape was a flat array of link objects with no node grouping. Anything consuming that endpoint directly (scripts, integrations) needs updating; the panel UI and the subscription page were changed with it.

**telemt troubleshooting: the three strings you will actually meet.**

- `TLS-front profiles are not ready for domains: <domain>` in a rolled-back apply. telemt learns the TLS fingerprint of its `tls_domain` by connecting to it on **443**, and a runtime reload refuses to activate a generation whose TLS-front profile is still the built-in fallback. It means nothing is answering TLS on `https://<tls_domain>/` from the node's point of view — normally Caddy is down, has no certificate, or the A record does not point at this host. Check `systemctl status caddy` and `journalctl -u caddy -n 50`, then `curl -sSI --resolve "<hostname>:443:127.0.0.1" https://<hostname>/` on the node. The installer now waits for exactly this before it starts telemt, so a node that got past installation and then broke is almost always a lapsed certificate or a DNS change. It is self-healing once the front is back: `[censorship.tls_fetch].profile_cache_ttl_secs` defaults to 600 s, so telemt re-fetches and the dirty sweep's next apply succeeds.
- `reload N did not finish in time (state <s>)`. The reload never reached a terminal state and was not draining either — telemt is wedged or unreachable. `systemctl status telemt` and `journalctl -u telemt -n 50`. (A reload still *draining* at the deadline is **not** this error; see above.)
- `no_healthy_upstreams` as the `reason` of a `503` from `/v1/health/ready`. telemt cannot reach any Telegram DC. This is outbound connectivity from the node, not anything the panel did: check egress firewall rules and routing before looking anywhere else.

**Node stuck degraded.** `GET /v1/health/ready` returning `503` carries a `reason`: `admission_closed` (telemt is deliberately refusing new connections) or `no_healthy_upstreams` (it cannot reach any Telegram DC — check the host's outbound connectivity before anything else). The panel marks the node degraded on either. `systemctl status telemt` plus the last lines of `journalctl -u telemt` will normally name the cause; a config telemt refuses outright makes it exit at startup rather than serve a broken configuration.

**Upgrading the pinned version.** The version is pinned in the panel's environment, not on the node: `TELEMT_VERSION` plus `TELEMT_SHA256_X86_64`, the sha256 of that release's `telemt-x86_64-linux-gnu.tar.gz`. To move a fleet:

1. Take the new release's checksum from the project's release page and set **both** variables together in `.env`. They are validated on startup (a non-semver version or a checksum that is not 64 hex characters is refused), and the panel will not render an install script without them — an unverifiable download is never handed to a root shell.
2. Restart the panel so the new pin is in effect.
3. Per node, regenerate an install command from the node page and re-run it on the host. The script re-downloads telemt, verifies it against the new checksum, overwrites `/usr/local/bin/telemt`, re-runs `init-node` (which keeps the existing API token) and restarts the unit — so a node is briefly down while it restarts, and its live sessions are dropped. Roll through the fleet one node at a time.
4. Confirm with `GET /api/v1/nodes/{id}` that `telemt_version` reads the new version; the agent takes it from `/v1/system/info` on every heartbeat, so it reflects the process that is actually running rather than what was installed.

Downgrading is the same procedure with the older version and its checksum. Keep `TELEMT_SHA256_MUSL_X86_64` (used only by the `fakenode-telemt` demo image) in step with the other two if you rely on `make e2e-telemt`.

## Telegram alerts not arriving

1. Check the Settings page shows a token is set (`bot_token_set: true` in `GET /api/v1/settings`) and a chat ID is filled in. Both must be present — the panel silently sends nothing if either is empty; it only 422s on the explicit test endpoint, not on the background alert path.
2. Click "Send test message" (owner only), which calls `POST /api/v1/settings/telegram/test`:
   - `422` with `error.fields` naming `bot_token` and/or `chat_id` — that value is missing everywhere. The test button sends whatever the form currently holds for both fields and falls back to the stored values for anything left blank, so you do **not** have to save first; if it still 422s, the field really is empty.
   - `502` with a description in the body — the token/chat ID are set but Telegram's Bot API rejected the request. Read the description: `Unauthorized` means the bot token is wrong or revoked; `chat not found` / `Forbidden: bot was blocked by the user` means the chat ID is wrong or the bot was never added/started in that chat (open a DM with the bot, or add it to the group/channel, and send `/start` once).
   - `200 {"ok":true}` — delivery works. If real alerts still don't show up, check step 3.
3. Confirm "Telegram alerts" is toggled **enabled** in Settings (`PUT /api/v1/settings` with `telegram_alerts.enabled: true`) — the test button bypasses this flag, but the automatic node-offline/online and apply-failed alerts check it and send nothing when it's off.
4. Remember the 5-minute rate limit: each `(node, alert kind)` pair (`node_offline`, `node_online`, `apply_failed`) only sends once per 5 minutes. The window opens only on a *successful* delivery, so a failed send (bad token, Telegram outage) does not suppress the retry on the next tick. A node that flapped offline/online twice within 5 minutes will only produce one message. This is expected, not a bug — wait for the window to pass, or check a different node/kind, before concluding delivery is broken.
5. If the test button works but the worker-driven alerts never fire even outside the rate-limit window, check the panel logs for `"telegram config"` or `"telegram send failed"` warnings — the worker reads config through the same `TelegramConfig` path as the test endpoint, but a transient DB read failure only logs there rather than surfacing to the UI.

## Failed node checks

`POST /api/v1/nodes/{id}/check` (the node Overview tab's "Run check" button) runs six probes (seven on a telemt node, which adds `mask`) and persists the report on the node. Reading the failing check's `detail` field is the fastest path; some common cases:

- **`dns_a` fails** — either the hostname doesn't resolve at all (fix the DNS record and wait for propagation), or it resolves but not to the node's registered IP (the detail shows `<resolved ips> (expected <ip>)`) — update the DNS record, or update the node's IP in the panel if the host's address changed.
- **`tcp_80` or `tcp_443` fails** — the port isn't reachable from the panel. Check the host's firewall/security group allows inbound 80/443, and that `tproxy-server`/whatever serves HTTP(S) on the node is actually listening (`systemctl status tproxy-server` on the node). If `tcp_443` fails, `tls_cert`, `pq_kex` and `http_root` report `"skipped: tcp_443 failed"` rather than their own failure — fix `tcp_443` first and re-run before worrying about those two.
- **`tls_cert` says "expiring soon"** — the certificate's `notAfter` is under 7 days away. If ACME renewal is supposed to be automatic, check the node's certbot/ACME client logs; a common cause is port 80 having been unreachable at renewal time (see the `tcp_80` case above) or the ACME account email/rate limits. Renew manually if needed and re-run the check.
- **`tls_cert` fails outright** (handshake/verify error) — the certificate doesn't match the hostname, isn't trusted, or the handshake itself failed; the detail carries the raw TLS error. Reissue the certificate for the correct hostname.
- **`pq_kex` is not green** — informational only (`advisory: true`; it never affects `all_ok`). The panel asked the node's TLS front for a TLS 1.3 handshake with Go's default curve preferences and the server did not pick the post-quantum hybrid `X25519MLKEM768`; the detail names the curve it chose instead. Caddy negotiates X25519MLKEM768 out of the box, so on a node installed by the panel this usually means something else is terminating TLS on 443 (a CDN, an old reverse proxy) or Caddy is pinned to an old release. Nothing is broken for clients; fix it when convenient.
- **`http_root` says "redirect to ... not followed"** — the site at `https://<hostname>/` returned a 3xx instead of 200. This check deliberately does not follow redirects (a checked node redirecting the panel elsewhere would otherwise let it be used as an SSRF proxy), so a working reverse proxy/relay in front of the site must answer `/` with 200 directly, not bounce to another path or scheme. Check the node's site/vhost config for an unwanted redirect rule.

`dns_a`, `tcp_80`, and `tcp_443` always run independently of each other; only `tls_cert`/`pq_kex`/`http_root` are conditionally skipped, and only on a `tcp_443` failure.

## Re-assigning a site template

`POST /api/v1/nodes/{id}/site` re-runs the assigned template's build (normalize + per-node uniquify) and compares the resulting bundle hash against what's already deployed on that node. Uniquification is deterministic — same template, same node ID, same output — so assigning the *same* template a node is already running produces the identical bundle and does **not** mark the node dirty or trigger a redeploy/restart. This is intentional: it means re-running an assignment (e.g. from a script, or by clicking "Assign" again in the UI without changing anything) is a safe no-op rather than an unnecessary relay restart that would drop live carrier sessions.

If you expected a redeploy and didn't get one, that's usually correct behavior — check `bundle_hash`/`deployed_hash` in the response (`GET /api/v1/nodes/{id}/site`) to confirm they already match. A redeploy only happens when the template itself changed (edited content, different preset, or a different node — different node ID means a different uniquify seed, hence a different bundle) or when you explicitly trigger `POST /api/v1/nodes/{id}/apply`.

## Prometheus

The panel exposes Prometheus text-format metrics at `/metrics` (node/key counts by status, live session/stream gauges per node). `METRICS_TOKEN` is **required** when `NODE_DRIVER=gateway` — the panel refuses to start without it — and scrapes must send it as a bearer token. Example `scrape_configs` entry:

```yaml
scrape_configs:
  - job_name: tgwp-panel
    metrics_path: /metrics
    scheme: https
    static_configs:
      - targets: ["panel.example.com"]
    authorization:
      credentials: "${METRICS_TOKEN}"
```

Per-node relay metrics (`/api/v1/nodes/{id}/metrics`) proxy the node's own `tproxy-server` `/metrics` through the panel and require an authenticated admin session, so they aren't suitable for direct Prometheus scraping — the panel's own `/metrics` above is the intended scrape target.

## Stats snapshot retention

`node_stats_snapshots` gets one row per online node per minute and is pruned to 30 days by
the stats worker. The parsed columns (`sessions_live`, `streams_live`, `bytes_up`,
`bytes_down`, `sessions_created`, `limit_hits`) are what the node stats tab charts, and
`mtproxy_raw` holds the MTProxy stat map the same tab shows.

`relay_raw` is **reserved and intentionally left empty**. It used to store the node's full
Prometheus text on every snapshot — roughly 200 MB per node per 30 days of data that nothing
ever read. The column is kept so existing rows and any future opt-in raw-retention feature
have somewhere to live; do not assume it is populated. To inspect a node's live relay
metrics, use `GET /api/v1/nodes/{id}/metrics`, which proxies the node directly.

The same minute tick also deletes expired rows from `sessions`. Expired sessions were
already rejected at authentication time (`GetSession` filters on `expires_at`), so this is
housekeeping, not a security fix.
