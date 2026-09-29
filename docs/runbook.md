# Operations runbook

## WEB diagnostics and Telemt update manager

Open the server's Statistics tab and run diagnostics before changing policy or restarting a
service. A report combines public external checks with agent and runtime readings, keeps
timestamps and history, and exports to JSON. `not_available` means the check did not
run, and it is left out of the executed-check total. A green report does not replace
testing carriers and reconnects with a real Telegram client.

The Telemt update card starts a per-node update to the release the panel pins. The UI
requires drain support and asks for a 120-second drain timeout. The agent checks
capabilities, verifies the download's SHA256, pauses and drains, keeps the previous
binary and configuration, swaps, restarts and verifies Telemt, then reopens admission.
When drain is required and support cannot be established, the update is refused. Read
every step and the final outcome, because a process that restarted has not yet shown
that WEB admission works.

Only `ok` means the update completed. `refused` means it was declined, `failed` means
you need to read the step that failed, and `rolled_back` records a recovery to the
previous build. `rollback_failed` and `needs_attention` need an operator. Keep the job
details, read the service and agent logs, check admission, then run diagnostics again.

A website change also needs an apply followed by public checks of the root and the
assets. The catalogue holds 15 built-in sites plus your own custom templates. Review
any legacy bundles still assigned when you upgrade an existing panel. Use an HTTP
upstream only on a server that reports support for it, with a permitted local or private
IP origin behind Telemt's front.

## Upgrading the panel

The panel container keeps no state of its own. Everything lives in Postgres and in the `paneldata` volume, which only holds the agent binary staged for server installs and upgrades. To upgrade:

```bash
cd deploy
docker compose pull        # or: docker compose build panel, if you build locally
docker compose up -d panel
```

On start, `panel serve` runs pending migrations and re-seeds the preset site templates before it starts listening, so a version bump is a plain restart. If a migration ever has to run on its own (for example before a blue/green cutover), use `docker compose run --rm panel migrate`.

A panel upgrade does not touch the servers: they keep the engine and agent they have until you upgrade them. When a panel release moves the pinned telemt, apply it per server with `tgwp-agent upgrade` on the host (see "Upgrading the pinned version" under "telemt servers"). A `tproxy` server moved to a new `TPROXY_COMMIT` still needs a re-install; see "Reinstalling the agent" below.

## Master key rotation

`MASTER_KEY` encrypts profile secrets, access-key secrets, TOTP secrets and the stored Telegram bot token at rest (AES-GCM via `internal/crypto.Box`; every ciphertext blob starts with a 2-byte key version). The panel decrypts each row with the version it was written under (`MASTER_KEY_V<n>` for every version below the current one) and encrypts new and updated rows with the current `MASTER_KEY_VERSION`. `panel keys rotate` re-encrypts every existing row under a new key version, so an old key can eventually be retired.

Procedure:

1. **Stop the panel.** `panel keys rotate` refuses to run while the panel holds the database lock, because a running panel's connections would race the transaction that rewrites every encrypted row.

   ```bash
   docker compose stop panel
   ```

2. **Generate the new key and set `MASTER_KEY_NEW`** (32 random bytes in base64, the same format as `MASTER_KEY`):

   ```bash
   openssl rand -base64 32
   # put the result in .env as MASTER_KEY_NEW=<value>
   ```

3. **Run the rotation.** For a preview, `--dry-run` counts rows per column first and writes nothing:

   ```bash
   docker compose run --rm panel keys rotate --dry-run
   docker compose run --rm panel keys rotate
   ```

   This re-encrypts `profiles.secret_enc`, `access_keys.secret_enc`, `admin_users.totp_secret_enc`, `admin_users.totp_pending_enc` and `settings.telegram_alerts.bot_token_enc` in one transaction. Every row not already at the new version is decrypted with the current key or keys and encrypted again with the new one, and every new blob is decrypted back and compared with the original plaintext before the commit. Any failure, whether a row that will not decrypt or a verification mismatch, rolls the whole transaction back, so a failed rotation changes nothing. The command prints how many rows it touched per column and the environment lines to set next. Those lines carry placeholders instead of key material: the command never prints a key.

4. **Update the environment** with the printed lines and fill in the placeholders yourself. The new key goes to `MASTER_KEY`, the version goes up by one, and the *old* `MASTER_KEY` value becomes a new `MASTER_KEY_V<n>` entry. That entry exists so a row the rotation missed, or a backup from before the rotation, can still be decrypted (see below).

   ```
   MASTER_KEY=<new key, base64>
   MASTER_KEY_VERSION=<n+1>
   MASTER_KEY_V<n>=<old key, base64>   # the key MASTER_KEY held before this rotation
   ```

   Remove `MASTER_KEY_NEW` from the environment. Only `keys rotate` reads it.

5. **Start the panel.**

   ```bash
   docker compose up -d panel
   ```

Notes:

- Running `keys rotate` again after a successful rotation does nothing: every row is already at the target version, so every count comes back zero. It is safe to run more than once.
- Keep an old `MASTER_KEY_V<n>` for as long as any row, or any backup you might restore, still uses it. A backup taken before a rotation holds ciphertext at the old version, and restoring it needs that key in the environment even after the live database has moved on.
- `keys rotate` needs a database connection and talks to Postgres directly, the same way `db backup` and `db restore` do. It does not start the HTTP server.

## Update check

The topbar's version chip and star count come from `GET /api/v1/status/update`, which
the panel answers from GitHub repository metadata: `GET https://api.github.com/repos/<GITHUB_REPO>/releases/latest`
(latest tag, release page, publish date) and `GET https://api.github.com/repos/<GITHUB_REPO>`
(stars). The panel fetches only public repository metadata and sends nothing about the
installation: no version, hostname, server count or key count. The comparison with the
running version happens locally, and a `dev` build is never reported as outdated.

The answer is cached for an hour and refreshed on the first request after that, one
refresh at a time. When GitHub is unreachable or rate-limits the panel (anonymous calls get
60 requests an hour per source IP), the response keeps the last good numbers with
`"stale": true`, and the panel waits at least 5 minutes before trying again. Setting
`GITHUB_TOKEN` to a token with no scopes raises the limit to 5000 an hour. The token is sent as
`Authorization: Bearer` and never logged.

To turn the check off, set `UPDATE_CHECK=false` and restart the panel. The endpoint
then answers `"enabled": false` with only `current` and `repo_url` filled in, and the panel
makes no request to GitHub at all.

## Two-factor authentication lockout

Two-factor login is off unless `FEATURE_TOTP=true`. When it is on, an admin who enrolled and then lost both the authenticator app and all eight recovery codes cannot finish a login. The password step returns only a five-minute challenge, and the one route that turns the second factor off (`POST /api/v1/auth/totp/disable`) needs the session that login would have created. For the last owner, that locks the whole installation out.

The way back in runs on the host, where shell access is already a higher bar than the panel login:

```bash
docker compose exec panel /app/panel admin totp-reset <username>
```

It clears `totp_enabled` and the stored secret and deletes that user's recovery codes. The account returns to password-only login and can enrol again from Settings → Security. The command affects the one user it names and nobody else.

Notes:

- The reset writes an audit entry in the same transaction: action `auth.totp_reset`, the named admin as target, `ip` recorded as `cli`, meta `{"username": ...}`. Its `admin_user_id` is NULL because the actor is a shell on the host, not a panel account. Filter the activity log by `auth.` to see it next to the logins it explains.
- Recovery codes are shown exactly once, when enrolment is confirmed, and stored only as sha256 hashes. They cannot be shown again; a user who wants a fresh set disables and re-enrols.
- Setting `FEATURE_TOTP` back to `false` also lets a locked-out user in, but it does so for every account at once and leaves the enrolment in the database. For a single person, use `totp-reset`.

## Backups and restore

The panel takes its own dumps with `pg_dump --format=custom` (the postgres client tools ship in the panel image) and writes them to `DATA_DIR/backups`, which is `/data/backups` on the `paneldata` volume under compose. A dump holds the whole database. Encrypted secrets stay encrypted in it, because `MASTER_KEY` lives in `.env` and never in the database. **Back up `MASTER_KEY`, `MASTER_KEY_V*` and `SESSION_SECRET` separately.** A database restored without the matching master key cannot decrypt a single profile secret or agent token.

**Process visibility.** `pg_dump` and `pg_restore` receive the database URL, password included, as a command-line argument. For the few seconds a dump or restore runs, any other process on the same host or in the same container namespace can read it in `ps aux` (and `/proc/<pid>/cmdline`). This is how libpq tools take a DSN, and the panel could only avoid it by rewriting the call around a `~/.pgpass` or service file. It is why the panel host is expected to be **single-tenant**: nothing untrusted should run next to the panel container, and `docker compose exec` or `run` access to the panel image deserves the same care as a shell on the host.

### Taking a backup

- **From the UI:** Settings → Backups → "Create backup" (owner only). The table lists every dump with its size, source and time, and each row can be downloaded or deleted. One dump runs at a time; a second request while one is in flight is refused with `backup_running`.
- **From the host:**

```bash
docker compose exec panel /app/panel db backup
```

It writes the same file and the same row, so a dump taken by a cron job on the host shows up in the UI.

### Nightly backups

Settings → Backups → "Nightly backup": pick a UTC hour and how many dumps to keep (1–60). The worker checks every 10 minutes and dumps once a day at that hour. Dumps beyond the limit are deleted oldest first, both the files and their rows. Retention only touches scheduled dumps, so a backup you took by hand stays until you delete it.

A panel that was down for the whole window skips that night; it does not catch up with a dump at boot.

### Restoring

A restore destroys what is there (`pg_restore --clean --if-exists` drops every existing object first) and cannot run under a live panel. It is therefore a CLI command, and it refuses twice: without `--yes`, and while the panel holds the database lock. Stop the panel first.

```bash
cd deploy
docker compose exec panel /app/panel db backup      # 1. a fresh dump of what you are about to replace
docker compose stop panel                           # 2. the restore refuses while the panel is running
docker compose run --rm panel db restore /data/backups/tgwp-20260101T030000Z-manual.dump --yes
docker compose start panel                          # 3. the panel reloads everything from the restored database
```

Notes:

- `docker compose exec panel /app/panel db restore ... --yes` fails on purpose with "the panel holds the database lock, so it is still running". `exec` needs the panel process alive, which is exactly the situation the guard is there for. Use `stop` and `run --rm` as above.
- The file argument may be a bare name or the full `/data/backups/...` path, but it must resolve inside the backup directory. To restore a file from elsewhere on the host, copy it into `/data/backups` first.
- A dump never contains its own `backups` row, because the row is written after `pg_dump` finishes. Right after a restore, the Backups tab therefore lists what existed when the dump was taken, minus that dump. The files on disk are untouched.
- Restore with the same `MASTER_KEY` and `MASTER_KEY_V*` the dump was taken under, or every encrypted secret becomes unreadable.
- There is no lock file. `panel serve` takes a Postgres [advisory lock](https://www.postgresql.org/docs/current/functions-admin.html#FUNCTIONS-ADVISORY-LOCKS) on the shared database and holds it on a dedicated connection for as long as it runs; `db restore` and `keys rotate` try to take the same lock and refuse when they cannot. This works across containers and hosts, which a pid check could not: under Compose, a running `panel serve` and a `docker compose run --rm panel` recovery container are both pid 1. Postgres releases the lock by itself when the connection ends, so a panel that was killed leaves nothing to clean up. It also means a second `panel serve` against the same database exits at once with "another panel instance holds the lock".

### Restoring somewhere else

`pg_restore` is a stock postgres tool, so a dump can also be loaded outside the panel, for instance into an empty database on another host:

```bash
pg_restore --clean --if-exists --no-owner --dbname "postgres://tgwp:...@host:5432/tgwp" tgwp-20260101T030000Z-manual.dump
```

## A server shows `rolled_back`

The agent's apply is transactional per server: it backs up the current `profiles.json`, `mtproxy.env` and site before writing anything, and if the relay does not come back healthy after the restart, it restores that backup and reports `rolled_back` instead of leaving the server half-applied.

1. Open the server's job history (`GET /api/v1/nodes/{id}/jobs`, or Apply history on the server's Maintenance tab) and read the failed job's log. It records which step failed: `-check` rejected the profiles, the restart failed, or health did not come back.
2. If the log says `relay -check rejected profiles: ...`, the desired state is invalid for that server's relay config (for example a limit above the server's global limits). Fix the key, profile or limit that caused it and apply again.
3. To reproduce it yourself, SSH to the server and run the same check the agent ran:
   ```bash
   tproxy-server -config /etc/tproxy-server/config.json -profiles-file /etc/tproxy-server/profiles.json -check
   ```
4. If the log says `rollback FAILED; manual intervention needed`, the automatic restore did not fully succeed. SSH in, compare `/etc/tproxy-server/profiles.json` and `/etc/mtproxy/mtproxy.env` with the backups under `/var/lib/tgwp-agent/backup/<timestamp>/`, and restore them by hand before you retry `apply`.

## A server shows offline

`offline` means the panel has not had a heartbeat from that server's agent for `OFFLINE_AFTER` seconds (default 90). On the server:

```bash
systemctl status tgwp-agent
journalctl -u tgwp-agent -n 200 --no-pager
```

The usual causes: the agent process crashed or was never started (`systemctl status` shows which), the server cannot reach `PANEL_URL` (check DNS, the firewall and outbound HTTPS), or the server's agent token was revoked (issue a new one as in "Reinstalling the agent" below). Check `systemctl status tproxy-server` and `mtproxy` as well. The agent runs independently of the relay, so a server can be "online" in the panel while the relay itself is `degraded`.

## Reinstalling the agent

If a server's agent token is lost or revoked, or the host was rebuilt, generate a new install command from the server page (or `GET /api/v1/nodes/{id}/install-command`) and run it on the host:

```bash
curl -fsSL https://panel.example.com/api/v1/install/<new-token>.sh | sudo bash
```

It is safe to run on a host that already has `tproxy-server`: if `/usr/local/bin/tproxy-server` exists, the installer skips the official `install.sh` step, registers the server again, and refreshes `/etc/tgwp-agent/agent.env` and the `tgwp-agent` systemd unit with the new token.

To move a server to a **newer agent or engine**, use `tgwp-agent upgrade` on the host instead. The server still holds a valid token, so the upgrade happens in place, with no new install token and no second run of the installer. See "Upgrading the pinned version" under "telemt servers". A re-install is for when the token itself is gone.

## Relay restart caveat (tproxy engine)

This applies to the `tproxy` engine only; on a telemt server an apply never restarts anything (see "telemt servers" below). Every apply that changes profiles, MTProxy secrets or the site ends with `systemctl restart tproxy-server` (and `mtproxy` if the secrets changed). `tproxy-server` cannot reload live. Existing sessions on the server drop when it restarts, and it loads the site into memory once at startup, so a change to the site alone also needs a restart and gets one. On busy servers, apply during quiet hours; the panel does not stagger or schedule applies for you.

## telemt servers

Everything below applies to servers created with the `telemt` engine (`GET /api/v1/nodes/{id}` shows `engine`). It is based on this research: [`docs/research/2026-09-06-telemt-and-meko.md`](research/2026-09-06-telemt-and-meko.md).

**Where things live.** Config: `/etc/telemt/telemt.toml` (0600, owned by the `telemt` account). telemt rewrites it itself through its control API, so never edit it by hand while telemt is running. Control-API token: `/etc/telemt/api.token`, 0600, generated by `init-node` and read by the agent. The token is the API's entire authentication; treat it as a password and never paste it into a ticket. Cover site: `/var/lib/telemt/public`. Unit: `/etc/systemd/system/telemt.service`. Network tuning: `/etc/sysctl.d/90-tgwp.conf` (BBR and fq, larger backlogs, TCP Fast Open, short keepalives, taken from MTPROTO_FIX_By_MEKO). The installer writes it for both engines and applies it with `sysctl --system`; delete it if a host needs its own values. Logs: `journalctl -u telemt`, and `journalctl -u tgwp-agent` for the agent's side of the conversation.

**Talking to the API by hand** (on the server, as root; it listens on loopback only and rejects everything else):

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

**An apply is a set of API calls.** The agent reconciles users (`/v1/users`), pushes the WEB profile array (`PATCH /v1/config`) and, when the cover site changed, asks for a draining runtime reload (`POST /v1/system/reload`). Live sessions survive all of it: the job log says `no restart`, and `ApplyResult.RestartedRelay` stays false. A job log that mentions a restart on a telemt server is a bug worth reporting.

**What does need a restart** (`systemctl restart telemt`, which drops live sessions): changes to listeners, meaning `classic_port`, the Fake-TLS and WEB listener definitions and `synlimit*`, and to `web.limits`. telemt reports anything it had to postpone as `deferred_process_fields` on a reload, and the agent copies that into the job log as `warning: telemt deferred … until a process restart`.

**The one thing in the panel that causes a restart** is the server's Fake-TLS card. Saving a new `tls_domain` or `classic_port` marks the server dirty, and the next apply patches `[censorship] tls_domain`, the Fake-TLS entry of `[[server.listeners]]` and `[general.links] public_port` on the server, then restarts telemt and waits for `/v1/health/ready`. The job log names each field it moved and says `restarting telemt: the Fake-TLS listener is process-owned`, and `ApplyResult.RestartedRelay` comes back true; this is the only telemt apply for which it does. If the restart fails, the apply puts back the previous domain, listener and link port and restarts telemt onto them. **Every Fake-TLS link already issued for that server stops working**, because the domain and port are baked into the link's `ee…` secret, so reissue the links from the panel afterwards. WEB links are unaffected.

**Draining reloads take time.** A profile change asks telemt for a draining runtime reload, which activates the new generation at once and lets the old generation's sessions finish for up to 30 s. The agent waits `30 s + 30 s` for it. If the operation is still `draining` when that budget runs out, the job log says `reload N still draining after 1m0s, new generation active` and the apply **succeeds**: the change is already live and only old sessions are still winding down. Only a reload that ends in `failed` or `rolled_back` rolls the apply back.

**API change (telemt phase).** `GET /api/v1/keys/{id}/links` returns one entry per server: `[{node_id, node_name, hostname, engine, links:[{kind, tme, tg}]}]`, where `kind` is `web` or `faketls`. It used to return a flat array of link objects with no grouping by server. Anything that reads that endpoint directly (scripts, integrations) needs updating; the panel UI and the subscription page changed with it.

**telemt troubleshooting: the three messages you will actually see.**

- `TLS-front profiles are not ready for domains: <domain>` in a rolled-back apply. telemt learns the TLS fingerprint of its `tls_domain` by connecting to it on **443**, and a runtime reload will not activate a generation whose TLS-front profile is still the built-in fallback. From the server's point of view, nothing is answering TLS on `https://<tls_domain>/`: usually Caddy is down or has no certificate, or the A record does not point at this host. Check `systemctl status caddy` and `journalctl -u caddy -n 50`, then run `curl -sSI --resolve "<hostname>:443:127.0.0.1" https://<hostname>/` on the server. The installer waits for exactly this before it starts telemt, so a server that passed installation and then broke almost always has a lapsed certificate or a changed DNS record. It heals itself once the front is back: `[censorship.tls_fetch].profile_cache_ttl_secs` defaults to 600 s, so telemt fetches the profile again and the next apply from the dirty sweep succeeds.
- `reload N did not finish in time (state <s>)`. The reload never reached a terminal state and was not draining either, so telemt is stuck or unreachable. Check `systemctl status telemt` and `journalctl -u telemt -n 50`. A reload still *draining* at the deadline is a different case, described above.
- `no_healthy_upstreams` as the `reason` of a `503` from `/v1/health/ready`. telemt cannot reach any Telegram DC. The cause is the server's outbound connectivity, so check egress firewall rules and routing first.

**Server stuck degraded.** A `503` from `GET /v1/health/ready` carries a `reason`: `admission_closed` (telemt is deliberately refusing new connections) or `no_healthy_upstreams` (it cannot reach any Telegram DC; check the host's outbound connectivity first). The panel marks the server degraded in either case. `systemctl status telemt` and the last lines of `journalctl -u telemt` normally name the cause. telemt exits at startup on a config it rejects, so it never serves a broken configuration.

**Upgrading the pinned version.** The version is pinned in the panel's environment, not on the server: `TELEMT_VERSION` plus `TELEMT_SHA256_X86_64`, the sha256 of that release's `telemt-x86_64-linux-gnu.tar.gz`. To move a fleet:

1. Take the new release's checksum from the project's release page and set **both** variables together in `.env`. The panel validates them on startup (it refuses a version that is not semver and a checksum that is not 64 hex characters) and will not render an install script without them, so an unverifiable download never reaches a root shell.

   `sudo /opt/tgproxy-panel/install.sh --update` does this step for you when the new pin ships with a panel release. It takes `TELEMT_VERSION`, `TELEMT_SHA256_X86_64` and `TELEMT_SHA256_MUSL_X86_64` from that release's `.env.example`, rewrites them in `.env` and prints each `old → new`. Nothing else in `.env` changes: secrets, the domain and admin data stay exactly as they were.
2. Restart the panel so the new pin takes effect (`install.sh --update` restarts the stack itself).
3. On each server, over ssh as root, with no panel session and no install token:

   ```bash
   tgwp-agent upgrade --check   # what would change; changes nothing
   tgwp-agent upgrade           # prints the plan and asks before doing it
   tgwp-agent upgrade --yes     # unattended; required when there is no terminal
   ```

   It reads `/etc/tgwp-agent/agent.env` and asks the panel what this server should be running (`GET /api/v1/node/upgrade`, authenticated with the server's own token, the same credential the agent uses for gRPC). It compares that with what is installed (`telemt --version`, falling back to telemt's own `/v1/system/info`, and the agent binary's `version`) and replaces only what differs. `--telemt` and `--agent` limit it to one component.

   Each replacement goes: download to a temp file, verify the panel's sha256 (**a mismatch aborts before anything is replaced**), keep the previous binary, install atomically, restart the unit, wait for it to prove itself (telemt: `/v1/health/ready` on its control API, up to 60s; the agent: the unit is active and has logged that it reconnected). On failure it puts the previous binary back from the copy on disk and restarts it. The rollback needs no network, so a bad release cannot strand a server. Restarting telemt drops the server's live sessions, so go through the fleet one server at a time.

   Run it from a login shell. Inside `tgwp-agent.service` itself the command refuses, because restarting the unit would kill the upgrade halfway through.
4. Fallback, still supported: generate an install command from the server page and run it on the host. The script downloads telemt again, verifies it against the new checksum, overwrites `/usr/local/bin/telemt`, runs `init-node` again (which keeps the existing API token) and restarts the unit. Use it when the server's agent predates `tgwp-agent upgrade` (an older binary answers the subcommand with a config error instead of a plan), when the agent binary itself is broken, or when a server needs the rest of the install applied again. It redoes everything, Caddy and the firewall rules included, and uses up a fresh install token.
5. Confirm with `GET /api/v1/nodes/{id}` that `telemt_version` shows the new version. The agent reads it from `/v1/system/info` on every heartbeat, so it reflects the process that is actually running, not what was installed.

Downgrading is the same procedure with the older version and its checksum. If you rely on `make e2e-telemt`, keep `TELEMT_SHA256_MUSL_X86_64` (used only by the `fakenode-telemt` demo image) in step with the other two.

## Telegram alerts not arriving

1. Check that the Settings page shows a token is set (`bot_token_set: true` in `GET /api/v1/settings`) and a chat ID is filled in. Both must be present. With either one empty, the background alert path sends nothing and says nothing; only the explicit test endpoint answers 422.
2. Click "Send test message" (owner only), which calls `POST /api/v1/settings/telegram/test`:
   - `422` with `error.fields` naming `bot_token` and/or `chat_id`: that value is missing everywhere. The test button sends whatever the form holds for both fields and falls back to the stored values for anything left blank, so you do **not** have to save first. If it still returns 422, the field really is empty.
   - `502` with a description in the body: the token and chat ID are set, but Telegram's Bot API rejected the request. Read the description. `Unauthorized` means the bot token is wrong or revoked. `chat not found` or `Forbidden: bot was blocked by the user` means the chat ID is wrong or the bot was never added or started in that chat; open a DM with the bot, or add it to the group or channel, and send `/start` once.
   - `200 {"ok":true}`: delivery works. If real alerts still do not arrive, go to step 3.
3. Confirm that "Telegram alerts" is **enabled** in Settings (`PUT /api/v1/settings` with `telegram_alerts.enabled: true`). The test button ignores this flag, but the automatic node-offline, node-online and apply-failed alerts check it and send nothing while it is off.
4. Mind the 5-minute rate limit. Each `(node, alert kind)` pair (`node_offline`, `node_online`, `apply_failed`) sends at most once per 5 minutes. The window opens only on a *successful* delivery, so a failed send (bad token, Telegram outage) does not hold back the retry on the next tick. A server that went offline and online twice within 5 minutes produces one message. Wait for the window to pass, or check another server or alert kind, before deciding delivery is broken.
5. If the test button works but the worker's alerts never fire, even outside the rate-limit window, look in the panel logs for `"telegram config"` or `"telegram send failed"` warnings. The worker reads the config through the same `TelegramConfig` path as the test endpoint, but a transient database read failure there is only logged and never reaches the UI.

## Failed server checks

`POST /api/v1/nodes/{id}/check` (the "Run check" button in Prerequisite check on the server's Checks tab) runs six external checks, seven on a telemt server, which adds `mask`, and stores the report on the server. The failing check's `detail` field is the quickest way to the cause. Some common cases:

- **`dns_a` fails.** Either the hostname does not resolve at all (fix the DNS record and wait for it to propagate), or it resolves to something other than the server's registered IP (the detail shows `<resolved ips> (expected <ip>)`). Update the DNS record, or update the server's IP in the panel if the host's address changed.
- **`tcp_80` or `tcp_443` fails.** The port is not reachable from the panel. Check that the host's firewall or security group allows inbound 80 and 443, and that whatever serves HTTP(S) on the server, such as `tproxy-server`, is actually listening (`systemctl status tproxy-server` on the server). When `tcp_443` fails, `tls_cert`, `pq_kex` and `http_root` report `"skipped: tcp_443 failed"` instead of a result of their own, so fix `tcp_443` and run the check again before looking at them.
- **`tls_cert` says "expiring soon".** The certificate's `notAfter` is less than 7 days away. If ACME renewal is meant to be automatic, read the server's certbot or ACME client logs. Port 80 being unreachable at renewal time (see `tcp_80` above) is a common cause, as are the ACME account email and rate limits. Renew by hand if needed and run the check again.
- **`tls_cert` fails outright** (handshake or verification error). The certificate does not match the hostname or is not trusted, or the handshake itself failed; the detail carries the raw TLS error. Issue the certificate again for the correct hostname.
- **`pq_kex` is not green.** This is informational only (`advisory: true`; it never affects `all_ok`). The panel asked the server's TLS front for a TLS 1.3 handshake with Go's default curve preferences, and the server did not pick the post-quantum hybrid `X25519MLKEM768`; the detail names the curve it chose. Caddy negotiates X25519MLKEM768 out of the box, so on a server the panel installed this usually means something else terminates TLS on 443 (a CDN, an old reverse proxy) or Caddy is pinned to an old release. Clients are not affected; fix it when convenient.
- **`http_root` says "redirect to ... not followed".** The site at `https://<hostname>/` answered with a 3xx instead of 200. The check does not follow redirects on purpose, since a server that redirects the panel elsewhere could otherwise use it as an SSRF proxy. Whatever reverse proxy or relay sits in front of the site must answer `/` with 200 directly instead of sending the request to another path or scheme. Look for an unwanted redirect rule in the server's site or vhost config.

`dns_a`, `tcp_80` and `tcp_443` always run independently of each other. Only `tls_cert`, `pq_kex` and `http_root` can be skipped, and only when `tcp_443` fails.

## Re-assigning a site template

`POST /api/v1/nodes/{id}/site` rebuilds the assigned template (normalize, then per-node uniquify) and compares the resulting bundle hash with what is already deployed on the server. Uniquification is deterministic: the same template and the same server ID give the same output. Assigning the template a server already runs therefore produces an identical bundle and does **not** mark the server dirty or trigger a redeploy or restart. Running an assignment again, from a script or by clicking "Assign" in the UI without changing anything, is safe and costs no relay restart or dropped carrier sessions.

If you expected a redeploy and did not get one, that is usually correct. Check `bundle_hash` and `deployed_hash` in the response (`GET /api/v1/nodes/{id}/site`) to confirm they already match. A redeploy happens only when the template itself changed (edited content, a different preset, or a different server, since a different server ID gives a different uniquify seed and so a different bundle) or when you trigger `POST /api/v1/nodes/{id}/apply` yourself.

## Prometheus

The panel exposes Prometheus text-format metrics at `/metrics`: server and key counts by status, and live session and stream gauges per server. `METRICS_TOKEN` is **required** when `NODE_DRIVER=gateway` (the panel will not start without it), and scrapes must send it as a bearer token. Example `scrape_configs` entry:

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

Per-node relay metrics (`/api/v1/nodes/{id}/metrics`) proxy the server's own `tproxy-server` `/metrics` through the panel and need an authenticated admin session, so Prometheus cannot scrape them directly. Scrape the panel's own `/metrics` above.

## Stats snapshot retention

`node_stats_snapshots` gets one row per online server per minute, and the stats worker prunes it to 30 days. The parsed columns (`sessions_live`, `streams_live`, `bytes_up`,
`bytes_down`, `sessions_created`, `limit_hits`) feed the charts on the server's stats tab, and
`mtproxy_raw` holds the MTProxy stat map the same tab shows.

`relay_raw` is **reserved and left empty on purpose**. Storing the server's full Prometheus text on every snapshot came to roughly 200 MB per server per 30 days that nothing read. The column stays so existing rows and any future opt-in raw retention have somewhere to live; do not assume it holds anything. To see a server's live relay metrics, use `GET /api/v1/nodes/{id}/metrics`, which proxies the server directly.

The same minute tick also deletes expired rows from `sessions`. Expired sessions were
already rejected at authentication time (`GetSession` filters on `expires_at`), so this is
housekeeping and has no security effect.
