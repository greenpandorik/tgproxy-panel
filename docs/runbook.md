# Operations runbook

**English** · [Русский](runbook.ru.md)

This page covers what to do with the panel once it is installed: how to upgrade the panel and
the servers, how to take backups, and what to check when something stops working. The sections
about failures share one shape: how you notice the problem, what to check, which commands to
run, and how to tell that the fix worked.

Unfamiliar words are explained in the [glossary](start.en.md#glossary). Installation is covered
in detail in the [setup guide](setup.en.md), how the panel works in the
[reference](reference.md), and Prometheus and Grafana metrics in [monitoring](monitoring.md).

Commands run on one of two machines: the panel server or a proxy server. Each command says which
one. The [start guide](start.en.md) shows how to log in to a server over SSH. The commands are
written for the root user. If you logged in as another user, run `sudo -i` first to become root.
Replace `panel.example.com` with your panel's domain.

## Contents

Planned work:

- [Where things are](#where-things-are)
- [Upgrading the panel](#upgrading-the-panel)
- [Upgrading servers](#upgrading-servers)
- [Backups and restore](#backups-and-restore)
- [Master key rotation](#master-key-rotation)
- [Automatic restarts and the route to Telegram](#automatic-restarts-and-the-route-to-telegram)
- [Checks from other networks](#checks-from-other-networks)
- [Alerts to your own endpoint](#alerts-to-your-own-endpoint)

When something is wrong:

- [The panel does not open](#the-panel-does-not-open)
- [Two-factor authentication lockout](#two-factor-authentication-lockout) and a forgotten password
- [A server is offline](#a-server-is-offline)
- [A server is degraded](#a-server-is-degraded)
- [Changes do not reach a server](#changes-do-not-reach-a-server)
- [A telemt update failed](#a-telemt-update-failed)
- [Some people cannot connect](#some-people-cannot-connect)
- [A check found a problem](#a-check-found-a-problem)
- [Telegram alerts do not arrive](#telegram-alerts-do-not-arrive)
- [Reinstalling the agent](#reinstalling-the-agent)
- [The cover site did not change](#the-cover-site-did-not-change)

## Where things are

### In the panel

The first page is Overview. The line at the top says whether everything works, and the "Needs
attention" block below lists problems across all servers. Some problems come with a button that
fixes them or opens the server in question.

Servers lists every server with its state: Healthy, Degraded or Offline. The "⋯" menu of each
server has "Apply now" and "Install command". The same page has a collapsed "Update telemt on
servers" block.

A server's page is split into tabs, arranged in three groups:

- Observe: Health, Stats, Checks, Logs;
- Configure: Settings (telemt servers only), Keys, Cover site;
- Maintain: the Maintenance tab with versions, the telemt update, "Apply history", restarting the
  proxy, the install command and deleting the server.

The server page header has an "Apply now" button. It sends the pending changes to the server
without waiting for the next scheduled apply, and it is active only when there are such changes.

The panel's Settings are split into tabs as well. For the whole panel: Notifications, Branding,
Accounts, Backups. Just for you: My interface, Password and 2FA. Only the owner sees Accounts and
Backups, and only the owner can change the panel settings.

The open tab is stored in the page address, so reloading the page, links to a tab and the
browser's Back button all work. Activity log shows who changed what in the panel, and when. It
keeps 180 days of entries, while server statistics and "Apply history" keep 30 days.

### On the panel server

A panel installed with the one-line installer lives in `/opt/tgproxy-panel`. The main file there
is `.env`: it holds the settings and the secrets, including the master key `MASTER_KEY` that
encrypts key secrets in the database. Keep a copy of this file off the server. Next to it are
`docker-compose.yml`, `Caddyfile` and a copy of `install.sh`.

The panel runs in three Docker containers: `panel` (the panel itself), `postgres` (the database)
and `caddy` (the certificate and HTTPS). Run `docker compose` commands from the panel directory:

```bash
cd /opt/tgproxy-panel
docker compose ps                     # container state
docker compose logs --tail 100 panel  # the last lines of the panel log
```

Data lives in Docker volumes: `pgdata` (the database), `paneldata` (backups, uploaded branding
images and the agent files used to install servers) and `caddydata` (certificates).
**`docker compose down -v` deletes the volumes together with the data. Never run it on a live
panel.**

If you installed the panel by hand from source, run the same `docker compose` commands from the
`deploy` directory of your clone.

### On a proxy server

The panel manages a proxy server through the agent. The agent is the `tgwp-agent` program: it
keeps the connection to the panel, applies changes, and reports the server's state every 30
seconds.

| What | Where |
|---|---|
| Panel address and agent token | `/etc/tgwp-agent/agent.env` |
| Agent working files | `/var/lib/tgwp-agent` |
| telemt settings | `/etc/telemt/telemt.toml` |
| telemt control-API token | `/etc/telemt/api.token` |
| Cover site on a telemt server | `/var/lib/telemt/public` |
| Network tuning | `/etc/sysctl.d/90-tgwp.conf` |

The services on a server are `tgwp-agent` (the agent), either `telemt` or `tproxy-server` and
`mtproxy` (the proxy itself, depending on the engine), and `caddy` (the certificate and HTTPS).
This is how you check the state and the log of any of them:

```bash
systemctl status tgwp-agent
journalctl -u tgwp-agent -n 100 --no-pager
```

While the server is online, the same logs are also available in the panel, on the Logs tab.

telemt rewrites `telemt.toml` itself through its API, so do not edit it by hand while telemt is
running. The token in `api.token` is the API's only protection: treat it as a password and never
paste it into a message. The installer writes the network tuning in `90-tgwp.conf` for both
engines: BBR and fq, larger backlogs, TCP Fast Open and short keepalives, taken from
MTPROTO_FIX_By_MEKO. If a server needs its own values, delete the file. More about telemt is in
the [research notes](research/2026-09-06-telemt-and-meko.md).

### Asking telemt directly

On a telemt server, as root. The API listens on the server's loopback address only and cannot be
reached from outside.

```bash
TOKEN="$(tr -d '\r\n' < /etc/telemt/api.token)"
tapi() { curl -s -H @- "http://127.0.0.1:9091$1" <<<"Authorization: $TOKEN"; }
tapi /v1/health/ready   # is telemt ready to accept connections
tapi /v1/system/info    # the running version
tapi /v1/users          # users, quotas and counters
tapi /v1/config         # the settings that can be changed through the API
```

`-H @-` reads the header from standard input. That keeps the token out of the process list, which
every user on the server can read. Do not pass it as `-H "Authorization: $TOKEN"`.

## Upgrading the panel

When a new version is out, the version chip at the top of the panel is highlighted and links to
the release page. For this the panel reads the latest release and the star count from GitHub
once an hour. It sends nothing about your installation. If GitHub is unreachable or rate-limits
the panel (60 requests an hour per IP without a token), the panel shows the last known data and
tries again no sooner than 5 minutes later. A GitHub token with no scopes in `GITHUB_TOKEN` raises
the limit to 5000 requests an hour, and `UPDATE_CHECK=false` in `.env` turns the GitHub calls off
entirely.

1. Take a backup and download it (see [Making a backup](#making-a-backup)).
2. On the panel server, run:

   ```bash
   sudo /opt/tgproxy-panel/install.sh --update
   ```

   The script downloads the new version, restarts the panel and waits until it answers. The data
   and `.env` stay as they are. Only the three lines that pin the telemt version change:
   `TELEMT_VERSION`, `TELEMT_SHA256_X86_64` and `TELEMT_SHA256_MUSL_X86_64`. The script takes them
   from the new release and prints each change as `old → new`. To install a specific release, add
   `--version 2.10.0`.

The panel container keeps no state of its own: everything lives in the database and in the
`paneldata` volume. On start the panel updates the database schema itself, there is no separate
step for it. To update the schema without starting the panel, run
`docker compose run --rm panel migrate`.

A panel built from source is upgraded like this:

```bash
git pull
cd deploy && docker compose up -d --build panel
```

A panel upgrade does not touch the servers: they keep their telemt and agent. If the script
reported a new telemt version, upgrade the servers as the [next section](#upgrading-servers)
describes. Older agents keep working with a newer panel, but new readings appear only once the
agent is upgraded.

It worked if the script ended with a summary showing the panel address and version, the panel
opens, and the chip shows the new version. If the script printed
`The panel did not become healthy within 120s`, go to
[The panel does not open](#the-panel-does-not-open).

## Upgrading servers

Upgrade the servers when the panel upgrade reported a new telemt version, or when the "Telemt
updates" card on the Maintenance tab shows an Installed version different from the Recommended
version.

There are three ways:

- "Update telemt on servers" on the Servers page upgrades several telemt servers one after
  another;
- "Update Telemt" on the Maintenance tab upgrades one server;
- `tgwp-agent upgrade` on the server itself upgrades telemt and the agent.

The first two drain WEB sessions first: the server stops accepting new WEB connections (this is
called closing admission), waits up to 120 seconds for the current ones to end on their own, and
closes the rest. Admission opens again after the update. `tgwp-agent upgrade` does not drain:
restarting telemt drops the current connections, and Telegram reconnects by itself.

### Several telemt servers

1. Open Servers and expand "Update telemt on servers". It lists only telemt servers that are
   online right now.
2. Tick the servers in the order they should be upgraded: the number next to a server shows its
   place in the queue. Put first the server whose failure you can live with most easily. The new
   version is tried on it before the others.
3. Click "Start sequential update" and confirm.

The panel takes the pinned version and its SHA256 checksum and upgrades the servers one at a
time. Before each server it checks that the server is healthy. The agent verifies the download's
checksum, drains WEB sessions, replaces telemt while keeping the previous version, restarts it,
checks the version and readiness, and reopens admission. Then the panel checks the server from
its own side and watches it for a minute. Only after that does the queue move on to the next
server. A server that already runs the pinned version is skipped.

Any error stops the queue, and the remaining servers are left alone. "Stop subsequent updates"
does not interrupt the server being upgraded right now: that one finishes and the next ones do
not start. A server in maintenance mode (see
[Automatic restarts and the route to Telegram](#automatic-restarts-and-the-route-to-telegram))
also stops the queue.

The queue is stored in the database and survives a panel restart. If the connection dropped while
the command was being sent and it is unknown whether it arrived, the panel does not send it a
second time. It stops the queue with
`dispatch was interrupted; inspect the node before starting a new rollout`. Open that server,
look at the "Telemt updates" card on its Maintenance tab, and start a new queue.

It worked if the queue shows Complete and every server's "Telemt updates" card says "Updated and
checked". If the queue shows "Needs attention", the reason is written under it, and
[A telemt update failed](#a-telemt-update-failed) explains what to do next.

### One server from the panel

Open the server, the Maintenance tab, the "Telemt updates" card, and click "Update Telemt". The
button appears when the installed version differs from the recommended one, and works while the
server is online. The steps show up in the card, and each one has "Technical details".

The panel requires telemt on the server to support draining. If it does not, or the panel could
not find out, the update is refused with "Update refused; binary unchanged" and nothing on the
server changes. Upgrade such a server over SSH.

### One server over SSH

On the proxy server:

```bash
tgwp-agent upgrade --check   # show what would change, change nothing
tgwp-agent upgrade           # show the plan and ask before doing it
tgwp-agent upgrade --yes     # no questions; required when there is no terminal
```

The command reads the panel address and the token from `/etc/tgwp-agent/agent.env` and asks the
panel what this server should be running. It compares the answer with what is installed and
replaces only what differs: telemt, the agent or both. `--telemt` and `--agent` limit the upgrade
to one program.

Every download is checked against the panel's checksum before anything is replaced. On a
mismatch the command stops and changes nothing. The previous version is kept on disk, the service
is restarted, and the command waits for proof that the new version works. For telemt that is
readiness within 60 seconds, for the agent it is a running service that has reconnected to the
panel. Without that proof the command puts the previous version back and restarts the service on
it. This rollback does not need the network.

Restarting telemt drops the server's current connections, so upgrade servers one at a time. Run
the command from an ordinary SSH session. Inside the `tgwp-agent` service it refuses to run,
because restarting the service would kill it halfway through.

It worked if the command ended with `this node is at the panel's pinned versions` and the
Versions block on the Maintenance tab shows the new version. The agent reads the telemt version
from telemt itself, so that block shows what is actually running.

If the agent is too old and answers with a configuration error instead of a plan, upgrade the
server with the install command (see [Reinstalling the agent](#reinstalling-the-agent)).

### Changing the telemt version by hand

The pinned telemt version normally changes with a panel upgrade. To install another version
yourself:

1. Take the SHA256 checksum of `telemt-x86_64-linux-gnu.tar.gz` from the telemt release page.
2. In `/opt/tgproxy-panel/.env`, change two lines together: `TELEMT_VERSION` and
   `TELEMT_SHA256_X86_64`. The panel validates them on start: the version must look like `3.5.9`
   and the checksum must be 64 hexadecimal characters. With an error in either line the panel
   does not start, so an unverified download never reaches the servers.
3. Restart the panel: `cd /opt/tgproxy-panel && docker compose up -d panel`.
4. Upgrade the servers in any of the ways in this section.

Going back to an older version works the same way, with its number and checksum. The
`TELEMT_SHA256_MUSL_X86_64` line is used only by the `make e2e-telemt` demo. If you use it,
change that line as well.

### Servers with tproxy

On servers with the older tproxy engine, `tgwp-agent upgrade` upgrades only the agent.
`tproxy-server` is pinned to the `TPROXY_COMMIT` commit in the panel's `.env`. To move to another
commit, reinstall the server with a new install command (see
[Reinstalling the agent](#reinstalling-the-agent)).

## Backups and restore

A backup holds the whole database: servers, keys, settings and the activity log. Key secrets in
it stay encrypted with the master key, and the master key itself lives in `.env` and is not part
of the backup. **A database restored without the same `MASTER_KEY` and every earlier
`MASTER_KEY_V<n>` cannot decrypt a single secret.** So keep both the backups and the `.env` file
off the server. The [start guide](start.en.md#what-next) shows how to download `.env` to your
computer.

Uploaded logos, favicons and login backgrounds are stored in the `paneldata` volume, under
`/data/branding`. They are not in the database, so they are not in the backup either.

While a backup or a restore runs, the database password is visible for a few seconds in the
process list (`ps aux`) to everyone on the server: that is how the PostgreSQL tools take the
database address. So nothing untrusted should run on the panel server, and access to
`docker compose exec` and `docker compose run` deserves the same care as root access.

### Making a backup

In the panel: Settings → Backups → "Create backup". Only the owner sees this tab. The table lists
every backup with its size, source and time, and each one can be downloaded or deleted. One
backup runs at a time.

On the panel server:

```bash
cd /opt/tgproxy-panel
docker compose exec panel /app/panel db backup
```

The command prints the file path, for example
`backup written: /data/backups/tgwp-20260101T030000Z-manual.dump`. This backup shows up in the
table too.

Backups live on the same server as the panel and are lost with it. Download a fresh backup
regularly with Download, or set up uploading as described below.

### Nightly backups

Open Settings → Backups → "Nightly backup". Turn on "Run every night", pick a UTC hour and how
many backups to keep, from 1 to 60.

The panel checks the schedule every 10 minutes and takes one backup a day at the chosen hour.
Backups beyond the limit are deleted oldest first. The limit applies only to scheduled backups:
manual ones stay until you delete them. If the panel was down for the whole chosen hour, that
night gets no backup, and the panel does not catch up after it starts.

The schedule works if the next morning the table has a backup with the Scheduled source.

### Checking a backup

```bash
cd /opt/tgproxy-panel
docker compose exec panel /app/panel db verify tgwp-20260101T030000Z-manual.dump
```

The command restores the backup into a temporary database `tgwp_restore_<…>`, checks the servers
table and that every stored secret decrypts with the current master keys, and then deletes the
temporary database. It does not touch the live database. It needs disk space for roughly one more
database, and the database user needs the right to create databases (CREATEDB). The standard
installation's user has it. If the command finished without output, the backup is fine.

`BACKUP_VERIFY=true` in `.env` runs the same check after every backup. It loads the server, keep
that in mind on a small VPS.

### Encrypting and uploading backups

The panel can encrypt every backup with an [age](https://github.com/FiloSottile/age) key and
send the encrypted file to your HTTPS endpoint.

1. On your computer, create a key pair: `age-keygen -o tgproxy-backup.key`. The command prints
   the public key, which starts with `age1`. The `tgproxy-backup.key` file holds the private key:
   keep it with you and never put it on the panel server.
2. Add to `/opt/tgproxy-panel/.env`:

   ```
   BACKUP_AGE_RECIPIENT=age1...
   BACKUP_UPLOAD_URL=https://backup.example.com/tgproxy
   BACKUP_UPLOAD_TOKEN=...
   ```

   Put the public key from step 1 in `BACKUP_AGE_RECIPIENT`. `BACKUP_UPLOAD_URL` is optional.
   When it is set, the panel sends `PUT <url>/<name>.dump.age` there, and the optional
   `BACKUP_UPLOAD_TOKEN` goes in an `Authorization: Bearer` header. There is no upload without
   encryption: with `BACKUP_UPLOAD_URL` but no `BACKUP_AGE_RECIPIENT` the panel does not start.
3. Restart the panel: `cd /opt/tgproxy-panel && docker compose up -d panel`.

Only the encrypted file leaves the server. The ordinary backup stays on the server and can be
downloaded and restored as usual. The result for the latest backup is shown on the Backups tab in
the "Latest backup protection" block: Encrypted, "Uploaded off-host", "Restore verified". If
encryption or upload failed, the error is shown there too, and the backup itself still stays in
the list. For scheduled backups the panel also shows "A backup could not be encrypted or
uploaded" on the Overview page.

To restore such a backup, decrypt it with the private key first:

```bash
age --decrypt -i tgproxy-backup.key -o tgwp-20260101T030000Z-manual.dump tgwp-20260101T030000Z-manual.dump.age
```

### Restoring on the same server

Use this when you deleted keys or servers by mistake, or broke the settings, and want the
database back as it was when the backup was taken. Everything changed after the backup is lost.

**Restore a backup with the same panel version that took it.** A restore does not remove tables
that the backup does not contain. If the backup predates a panel upgrade, the newer version may
fail to start on such a database when it updates the schema.

On the panel server:

```bash
cd /opt/tgproxy-panel
docker compose exec panel /app/panel db backup   # 1. a copy of what is about to be replaced
docker compose stop panel                        # 2. the restore refuses while the panel runs
docker compose run --rm panel db restore tgwp-20260101T030000Z-manual.dump --yes
docker compose start panel                       # 3. the panel reloads everything from the restored database
```

A restore first drops everything in the database. That is why the command requires `--yes` and
refuses to run while the panel is up, with
`the panel holds the database lock, so it is still running`. For the same reason a restore does
not work through `docker compose exec`: `exec` runs inside the live panel. Use `stop` and
`run --rm` as above.

The file name can be given bare or in full, `/data/backups/…`, but the file must be in the backup
directory. Put a file from elsewhere there first, as in the [next section](#restoring-on-a-new-server).

After the restore:

- The backups list in the panel shows what existed when the backup was taken, minus that backup
  itself: its row is written to the database only after the file is ready. The files on disk are
  not touched.
- The servers keep running whatever they were sent last. To send them the keys from the restored
  database, open Servers and pick "Apply now" in the "⋯" menu of each server. "Compare with the
  server" on a server's Keys tab shows whether the keys match.

It worked if the command printed
`restore complete; restart the panel so it picks up the restored state`, the panel opens, and
the keys and servers are as they were when the backup was taken.

How the panel knows it is running: a running panel holds a PostgreSQL lock on a dedicated
connection. Restore and master key rotation try to take the same lock and refuse when they
cannot. This works across containers and hosts. If the panel crashed, PostgreSQL releases the
lock by itself, and there is nothing to clean up. A second panel on the same database exits at
once with `another panel instance holds the lock on this database; stop it before starting a second one`.

### Restoring on a new server

Use this when the panel server died or you are moving to another one. You need a downloaded
backup and the `.env` file saved from the old server.

1. Find out which panel version took the backup. For a recent backup, it is the `PANEL_VERSION`
   line in the saved `.env`.
2. Point the panel domain's A record at the new server and wait until it works. The
   [start guide](start.en.md) shows how to check it with `nslookup`.
3. Install the same panel version on the new server, for example 2.10.0:

   ```bash
   curl -fsSL https://raw.githubusercontent.com/greenpandorik/tgproxy-panel/main/install.sh | sudo bash -s -- --version 2.10.0
   ```

   Any admin login and password will do here: after the restore, the accounts from the backup
   apply.
4. Carry over the master keys. Open `.env` on the new server, for example with
   `nano /opt/tgproxy-panel/.env`. Replace the values of `MASTER_KEY` and `MASTER_KEY_VERSION`
   with those from the saved file, and add every `MASTER_KEY_V<n>` line from it, if there are
   any. Leave the other lines alone, especially `POSTGRES_PASSWORD`: the database on the new
   server was created with the new password. If Prometheus scrapes the panel, carry over
   `METRICS_TOKEN` as well.
5. Upload the backup to the server. On your computer, in the folder with the backup, run:

   ```
   scp tgwp-20260101T030000Z-manual.dump root@203.0.113.20:/root/
   ```

   Then, on the new server, put the file into the backup directory:

   ```bash
   cd /opt/tgproxy-panel
   docker compose exec panel mkdir -p /data/backups
   docker compose cp /root/tgwp-20260101T030000Z-manual.dump panel:/data/backups/
   docker compose exec -u root panel chown panel:panel /data/backups/tgwp-20260101T030000Z-manual.dump
   ```

6. Restore the database and start the panel:

   ```bash
   docker compose stop panel
   docker compose run --rm panel db restore tgwp-20260101T030000Z-manual.dump --yes
   docker compose up -d panel
   ```

   The last command recreates the container, so the panel reads the edited `.env`. `start` would
   run it with the old keys.
7. If the installed version is older than the latest one, upgrade the panel:
   `sudo /opt/tgproxy-panel/install.sh --update`.
8. Upload the logo and other images again: Settings → Branding.

It worked if you log in with the old login and password, the keys and servers are in place, and
key links open, which means the master key matched. The proxy servers do not need reinstalling.
Their agents connect to the panel by domain and find the new server by themselves once the DNS
record updates.

### Restoring into another PostgreSQL

`pg_restore` is a stock PostgreSQL tool, so a backup can also be loaded without the panel, for
instance into an empty database on another host:

```bash
pg_restore --clean --if-exists --no-owner --dbname "postgres://tgwp:...@host:5432/tgwp" tgwp-20260101T030000Z-manual.dump
```

## Master key rotation

`MASTER_KEY` encrypts, in the database, the secrets of access keys and profiles, the two-factor
secrets and the stored Telegram bot token. Every encrypted value remembers the version number of
its key. The panel decrypts each value with the key of its version (earlier keys live in the
`MASTER_KEY_V<n>` lines) and encrypts new values with the current `MASTER_KEY`, numbered
`MASTER_KEY_VERSION`. `panel keys rotate` re-encrypts every value with a new key, after which the
old key can eventually be retired.

Rotate the key if the `.env` file, or a copy of it, may have reached someone else. Every step runs
on the panel server.

1. Take a backup and download it.
2. Stop the panel. While it runs, the rotation refuses to start: the panel's connections would
   interfere with the transaction that rewrites every encrypted value.

   ```bash
   cd /opt/tgproxy-panel
   docker compose stop panel
   ```

3. Generate the new key:

   ```bash
   openssl rand -base64 32
   ```

   Open `.env` (for example, `nano .env`) and put the result in the `MASTER_KEY_NEW=` line. If
   there is no such line, add it.
4. Run the dry run first: it counts the values per column and writes nothing. Then run the
   rotation itself:

   ```bash
   docker compose run --rm panel keys rotate --dry-run
   docker compose run --rm panel keys rotate
   ```

   The command re-encrypts `profiles.secret_enc`, `access_keys.secret_enc`,
   `admin_users.totp_secret_enc`, `admin_users.totp_pending_enc` and the bot token in
   `settings.telegram_alerts` in one transaction. It decrypts every new value back and compares
   it with the original before committing. Any error rolls the whole transaction back, so a
   failed rotation changes nothing. At the end the command prints how many values it re-encrypted
   and the lines to put into `.env`. Those lines carry placeholders instead of keys: the command
   never prints a key.
5. Edit `.env` following the printed lines. The new key moves into `MASTER_KEY`, the version goes
   up by one, and the previous `MASTER_KEY` value moves into a new `MASTER_KEY_V<n>` line. For the
   first rotation it looks like this:

   ```
   MASTER_KEY=<new key, the same as in MASTER_KEY_NEW>
   MASTER_KEY_VERSION=2
   MASTER_KEY_V1=<old key, the one MASTER_KEY held before>
   ```

   Empty the `MASTER_KEY_NEW` line: only the rotation reads it.
6. Start the panel:

   ```bash
   docker compose up -d panel
   ```

   This recreates the container with the new values from `.env`. `start` would run the panel with
   the old ones, and it could not decrypt the re-encrypted values.
7. Save a new copy of `.env` off the server.

It worked if the panel opens and shows key links. The most thorough check is a fresh backup, since
checking it decrypts every secret:

```bash
docker compose exec panel /app/panel db backup
docker compose exec panel /app/panel db verify <file name from the previous command>
```

Also keep in mind:

- If the rotation was interrupted, or you are not sure it went through, you can run it again as
  long as `.env` is not edited yet (before step 5). Values already encrypted with the new key are
  skipped.
- Keep an old `MASTER_KEY_V<n>` in `.env` for as long as any value, or any backup you might
  restore, still uses it. A backup taken before the rotation is encrypted with the old key, and
  restoring it needs that key even after the live database has moved on.
- The rotation talks to PostgreSQL directly, like `db backup` and `db restore`, and does not start
  the panel's web server.

## Automatic restarts and the route to Telegram

The "Recovery and Telegram egress" card is on a server's Settings tab and exists only on telemt
servers. It sets what the agent does when telemt stops responding, and which way telemt reaches
Telegram.

### Restarts

The "When a running engine stops responding" field:

- "Observe only" is the default: the agent restarts nothing;
- "Restart after repeated failures": the agent checks telemt every 30 seconds and restarts it
  only after several failures in a row. By default that is 3 failures, then a 300-second pause
  after a restart, and at most 2 restarts an hour.

The numbers are set in "Failure threshold and recovery budget": "Consecutive failures" from 2 to
20, "Cooldown, seconds" from 60 to 86400, "Attempts per hour" from 1 to 6.

The agent restarts only a running service that stopped responding. It never starts a stopped
service, including one stopped by hand. It never reopens WEB admission that was closed on
purpose either. "Recovery history" lists what the agent did.

"Maintenance mode" suspends automatic restarts and route switching. While it is on, the panel
does not run the scheduled full check on this server, does not open problems from its readings
or external checks, and a telemt update queue stops on it. Turn it on while you work on the
server.

### Route to Telegram

"Route to Telegram through SOCKS5 or WARP" is for a server whose hosting provider or country gets
in the way of reaching Telegram. It usually shows on the Health tab: the "Telegram datacenters"
block has red latencies or failing connections.

The "Outbound route" field:

- "Existing engine configuration": the panel does not manage the route;
- "Direct to Telegram";
- "Through local SOCKS5": telemt reaches Telegram through a SOCKS5 proxy on the same server, for
  example a WARP endpoint or a tunnel to a reserve VPS.

The route applies only to telemt's outbound connections. The server's own routes, SSH and the
agent's connection to the panel stay as they are. SOCKS5 must listen on this server's loopback
address, for example `127.0.0.1:1080`. WARP or the tunnel itself is installed separately. This
mode cannot be combined with a sponsor channel tag (Middle Proxy).

"Switch to reserve on failure" adds a "Reserve SOCKS5, address:port" field. The primary and
reserve routes are checked with a TCP connection to two Telegram datacenters. Such a check shows
that the route is open. It does not prove that a Telegram client works through it. After a
failover the reserve stays active, and returning to the primary is manual so the route does not
flip back and forth. "Check and restore primary route" checks the primary again before switching
back.

To go back to "Existing engine configuration", first select and save "Direct to Telegram". telemt
routes defined for separate scopes (`scopes`) are kept through every switch.

Saving checks the new route and applies it on the server. If telemt needs a restart for it, the
current connections drop.

The route works if the latencies in "Telegram datacenters" on the Health tab are back to normal
and "Resource headroom and routes" shows the route as Healthy.

## Checks from other networks

The panel checks servers from its own server. To see whether the proxy opens from particular
providers' networks, run the external check `tgwp-probe` in those networks. Its results appear
on the server's Checks tab, in the "External network checks" block.

1. On the panel server, add to `/opt/tgproxy-panel/.env`:

   ```
   PROBE_TOKEN=<at least 32 random characters, for example from openssl rand -hex 32>
   PROBE_LOCATIONS=isp-a,isp-b
   ```

   List in `PROBE_LOCATIONS`, comma-separated, the names of the networks where the checks will
   run: Latin letters, digits, `_` and `-`. Restart the panel: `docker compose up -d panel`.
2. Build the check from source (Go is required): `go build -o tgwp-probe ./cmd/probe`. Put the
   `tgwp-probe` file into `/usr/local/bin` on a machine in each network.
3. Copy `tgwp-probe@.service` and `tgwp-probe@.timer` from `deploy/probe` into
   `/etc/systemd/system`. Copy `example.env` to `/etc/tgwp-probe/<name>.env`, give it mode 0600
   and fill it in:
   - `PANEL_URL`: the panel address;
   - `NODE_HOST`: the proxy server's domain;
   - `NODE_ID`: the server's identifier (a UUID), visible in the address bar on the server's
     page;
   - `PROBE_LOCATION`: one of the names in `PROBE_LOCATIONS`;
   - `TGWP_PROBE_TOKEN`: the same value as `PROBE_TOKEN` on the panel.
4. Enable the timer:

   ```bash
   systemctl daemon-reload
   systemctl enable --now tgwp-probe@<name>.timer
   ```

One instance watches one server and runs once a minute. It performs a real TLS handshake with
certificate verification and requests the cover site. A report older than three minutes counts as
stale, and when there is no report, Overview shows "External check … has not reported for a
while". An old report cannot replace a newer one. Do not list networks in `PROBE_LOCATIONS` where
the check is not running yet, or you get problems about missing reports that you already expect.

A check with a real Telegram client is added with `--client-check /absolute/path` on the
`ExecStart` line of the service file. It is an adapter to a Telegram client that you installed in
that network yourself: the check has no client of its own. The program receives the server's
domain and a newline on standard input, has 45 seconds, and must return at most 4096 bytes of
JSON:

```json
{"faketls":{"status":"ok","latency_ms":120},"web":{"status":"not_run","latency_ms":0}}
```

The allowed statuses are `ok`, `failed` and `not_run`. `ok` must mean a successful exchange
through the corresponding proxy. An open port is not enough for it. Keep the program's secrets in
a separate mode-0600 file that the service can read, and never pass them as arguments or print
them. If the program reads a personal Telegram session database, create a dedicated unprivileged
user with access to that directory for it instead of `DynamicUser`. Without such a program both
client checks stay "Not checked". Real-client checks need your working keys and independent
networks; they were not run during the panel's development.

## Alerts to your own endpoint

Besides the Telegram bot, the panel can send problems to your HTTPS endpoint (a webhook). This
works whether or not the bot is enabled. Set in `/opt/tgproxy-panel/.env`:

```
ALERT_WEBHOOK_URL=https://alerts.example.com/tgproxy
ALERT_WEBHOOK_SECRET=<at least 32 characters>
```

and restart the panel: `docker compose up -d panel`.

The panel sends a `POST` with a JSON body of `event_id`, `node_id`, `kind`, `message` and `at`.
On the receiving side:

- verify the signature: the `X-TGWP-Signature` header holds a hex HMAC-SHA256, computed with the
  secret over `X-TGWP-Timestamp + "." + the raw request body`;
- reject timestamps older than five minutes;
- drop repeats by `event_id`.

The panel makes up to three attempts and does not follow redirects. Problems are stored in the
database, but webhook delivery has no queue of its own beyond these retries. If the panel server
itself fails, it cannot report that. So watch `https://panel.example.com/healthz` with external
monitoring: a running panel answers `ok`.

## The panel does not open

What you see: the browser shows an error instead of the panel, every server turned Offline at
once, or alerts stopped arriving.

1. Open `https://panel.example.com/healthz` in a browser. A running panel answers `ok`.
2. On the panel server, look at the containers:

   ```bash
   cd /opt/tgproxy-panel
   docker compose ps
   ```

   Three containers must be running: `panel`, `postgres` and `caddy`. The first two show
   `healthy` in their status.
3. If `panel` keeps restarting or is stopped, read the log:

   ```bash
   docker compose logs --tail 100 panel postgres
   ```

   A panel that could not start ends its log with a line that starts with `error:` and names the
   cause. Most often it is a broken `.env`, for example `error: config: METRICS_TOKEN is required …`
   or `error: config: MASTER_KEY: …`. Compare `.env` with your saved copy and bring back the lost
   lines. `another panel instance holds the lock on this database` means another copy of the
   panel is already using this database: stop it.

   If the database is still starting on a slow disk, wait a couple of minutes. Check free space
   with `df -h`: if the disk is full (100% in the `Use%` column), the database and the panel do
   not start until you free some space. Another possible cause is a panel image that does not
   match the server's architecture. The image is built for `x86_64` and `aarch64`, and `uname -m`
   shows the server's architecture.
4. If the browser complains about the certificate, read Caddy's log:
   `docker compose logs --tail 50 caddy`. Caddy gets the certificate by itself, but for that the
   domain's A record must point at this server and ports 80 and 443 must be open.
5. Once the cause is fixed, start everything again:

   ```bash
   docker compose --profile caddy up -d
   ```

   `--profile caddy` is needed because Caddy is defined in a separate profile. If the panel was
   installed without a domain (`--local`), start it like this:
   `docker compose -f docker-compose.yml -f docker-compose.local.yml up -d`.

It worked if `https://panel.example.com/healthz` answers `ok`, the panel log has a
`panel listening` line, and the servers are Healthy again within a minute or two.

## Two-factor authentication lockout

This applies while two-factor login is available, which is the default (`FEATURE_TOTP=false`
in `.env` hides it). An admin who lost both the authenticator app and all eight recovery
codes cannot log in. After the password the panel waits five minutes for a code, and the second
factor can be turned off only from inside the panel. If this is the last owner, nobody is left to
manage the panel.

The way back in runs on the panel server, where access is already better protected than the panel
login.

```bash
cd /opt/tgproxy-panel
docker compose exec panel /app/panel admin totp-reset <username>
```

The command turns off the second factor for the one named user and deletes that user's recovery
codes. Other accounts are not affected. The user then logs in with the password alone and can
turn the second factor on again: the user menu in the top right → "Password and 2FA".

It worked if the command printed
`two-factor authentication disabled for <username>; recovery codes deleted` and logging in with
the password no longer asks for a code.

Also keep in mind:

- The reset is written to the activity log in the same transaction: action `auth.totp_reset`, the
  account as the target, IP `cli`, and no user, because the reset came from the server. Filter
  Activity log by Login and the entry appears next to the login attempts it explains.
- Recovery codes are shown once, when the second factor is turned on, and stored only as hashes.
  They cannot be shown again. For a new set, turn the second factor off and on again.
- `FEATURE_TOTP=false` also lets a locked-out user in, but it does so for everyone at once and
  leaves the second-factor settings in the database. For one person, use `totp-reset`.

### Forgotten password

There is no password reset command. Create a new owner on the panel server:

```bash
cd /opt/tgproxy-panel
docker compose exec panel /app/panel admin create <new-username> '<new-password>' owner
```

Log in with it, open Settings → Accounts and delete the old account. If you want the old username
back, create it again there. The password from the command stays in the server's command history,
so change it after logging in: the user menu → "Password and 2FA".

## A server is offline

What you see: the server list shows Offline, Overview has "Server is not responding", and Telegram
gets `🔴 … is not connected` (or its Russian version, depending on the notification language). The server's Health tab says "Server is not connected" and shows
commands to check.

It means the panel has had no report from the agent for more than 90 seconds. The threshold is
set in Settings → Notifications → "Treat a server as down after, sec". The agent runs separately
from the proxy: a server can be Offline while the proxy keeps letting people in, and the other
way round.

If every server went Offline at once, the panel is the likely cause: start with
[The panel does not open](#the-panel-does-not-open).

1. Log in to the server over SSH. If you cannot, open the hosting provider's control panel: the
   server may be powered off or blocked. Reboot it from there.
2. Check the agent:

   ```bash
   systemctl status tgwp-agent
   journalctl -u tgwp-agent -n 100 --no-pager
   ```

   If the service is not `active (running)`, start it: `systemctl restart tgwp-agent`.
3. Read the last lines of the agent log:
   - `connected to panel`: the agent reached the panel. Wait a minute.
   - `session ended` together with `invalid node token`: the panel no longer knows this server,
     for example because it was deleted from the panel or registered again with another install
     command. Reinstall the agent (see [Reinstalling the agent](#reinstalling-the-agent)).
   - `session ended` with a connection error: the server cannot reach the panel. Find the address
     the agent uses and check whether the panel answers from this server:

     ```bash
     grep TGWP_PANEL_URL /etc/tgwp-agent/agent.env
     curl -fsS https://panel.example.com/healthz
     ```

     If `curl` did not print `ok`, check the panel's DNS record, outbound connections to port 443
     in the server's and the hosting provider's firewall, and whether the panel itself is up.
   - `update recovery requires attention`: the agent could not finish an interrupted telemt
     update, see [A telemt update failed](#a-telemt-update-failed).
4. Check the proxy itself: `systemctl status telemt caddy` on a telemt server, or
   `systemctl status tproxy-server mtproxy caddy` on a tproxy server.

It worked if the server is Healthy again within a minute and Telegram gets
`🟢 … is connected again`.

## A server is degraded

What you see: the server list shows Degraded, the Health tab says "Running with limitations", and
Overview has one of these problems: "Some services are unavailable", "Telemt is not ready to
accept connections", "WEB sessions are not being admitted", "Disk is running out of space",
"Memory is running low".

The agent is online, but the proxy is stopped or reports that it is not fully ready.

1. Open the server, the Health tab, the "Service health" block. It shows which service is down.
2. If telemt is down (the relay on a tproxy server), read its log on the server:

   ```bash
   systemctl status telemt
   journalctl -u telemt -n 50 --no-pager
   ```

   telemt does not start with settings it considers invalid, and the log names the reason. Once
   it is fixed, restart it: Maintenance → "Restart telemt", or `systemctl restart telemt`. A
   restart drops the current connections.
3. If telemt runs but "Accepts connections" shows down, ask telemt directly (see
   [Asking telemt directly](#asking-telemt-directly)): `tapi /v1/health/ready`. An error answer
   carries a `reason` field:
   - `admission_closed`: new WEB sessions are refused on purpose, by the pause or drain buttons,
     or admission stayed closed after an update. Open the Stats tab, the "WEB transport" block,
     and click Resume. If the button answers with `interrupted update requires recovery…`, go to
     [A telemt update failed](#a-telemt-update-failed).
   - `no_healthy_upstreams`: telemt cannot reach any Telegram datacenter. The "Telegram
     datacenters" block on the Health tab shows which ones. The cause is the server's outbound
     connectivity: check the firewall and routing. If the hosting provider blocks Telegram, send
     telemt through SOCKS5 or WARP (see [Route to Telegram](#route-to-telegram)).
4. If Caddy is down, read `systemctl status caddy` and `journalctl -u caddy -n 50 --no-pager`. It
   is usually the certificate or closed ports 80 and 443.
5. The disk problem appears when the disk is 90% full, the memory one at 95%. Look at `df -h` and
   `free -h`.

It worked if the server is Healthy again. Problems on Overview close by themselves once the cause
is gone.

## Changes do not reach a server

What you see: a new key stays Pending for a long time, Overview has "Could not apply settings",
Telegram gets `❌ Apply failed on …`, and the server list marks the server as pending.

The panel sends changes to the servers every 45 seconds (Settings → Notifications → "How often to
apply changes, sec"). Each such push is called an apply. Applies are recorded in "Apply history"
on the server's Maintenance tab with the result Succeeded, Failed or "Rolled back". Rolled back
means the server returned to its previous state because the new one did not work. Clicking a row
opens "Operation details" with the agent's log.

1. If the server is Offline, the changes wait for it. Bring the server back online first.
2. Open the latest failed apply and read the last lines of the log. Common messages are listed
   below.
3. Fix the cause and click "Apply now" in the server page header, or "Apply again" on Overview.

It worked if the history has a new Succeeded row and the keys are no longer Pending.

### On a telemt server

An apply on a telemt server is a series of calls to its API: users, WEB profiles and, if the cover
site changed, a settings reload. On a reload the new settings take effect at once, and old
connections are allowed to finish for up to 30 seconds. Current connections are not dropped, and
the log says `no restart`.

A telemt restart, which drops connections, is needed for changes in the "Addresses and Fake-TLS"
card on the Settings tab ("Fake-TLS domain", "Fake-TLS port", "Backup masking domains", "Public
IP") and for WEB transport settings that telemt cannot change on the fly. The log then has
`restarting telemt: the Fake-TLS listener and the vhost address are process-owned`. If the restart
fails, the apply puts back the previous domain, port and address and restarts telemt on them.
**After the Fake-TLS domain or port changes, every Fake-TLS link already issued for the server
stops working**: the domain and port are baked into the link's secret. Send the links out again.
WEB links are unaffected.

Common messages:

- `reload N still draining after 1m0s, new generation active`: not an error. The new settings
  already work and old connections are still finishing. The apply counts as successful.
- `reload N did not finish in time (state …)`: telemt is stuck or not answering. Read
  `systemctl status telemt` and `journalctl -u telemt -n 50 --no-pager`.
- `TLS-front profiles are not ready for domains: <domain>` in a rolled-back apply. telemt learns
  the TLS fingerprint of its Fake-TLS domain by connecting to it on port 443, and it does not
  switch to new settings until it has one. So from the server itself, nothing answers TLS at
  `https://<domain>/`. Usually Caddy is down or has no certificate, or the A record points
  elsewhere. Check `systemctl status caddy`, `journalctl -u caddy -n 50 --no-pager` and, on the
  server, `curl -sSI --resolve "<domain>:443:127.0.0.1" https://<domain>/`. Once the site
  answers again, it clears up by itself: telemt takes the fingerprint again (every 600 seconds by
  default) and the next apply succeeds. Backup masking domains must also answer HTTPS on port 443
  from this server, or the apply rolls back.
- `warning: telemt deferred … until a process restart`, with "Settings are saved but will not
  take effect until a restart" on Overview: telemt stored the change but enables it only after a
  restart. Click "Restart the proxy" on Overview or "Restart telemt" on the Maintenance tab. A
  restart drops the current connections.

### On a tproxy server

Every apply that changes keys, MTProxy secrets or the site ends with a restart of `tproxy-server`
(and of `mtproxy` if the secrets changed). `tproxy-server` cannot reload its settings live and
reads the site into memory only at startup. So every such apply drops the current connections on
the server. On busy servers, apply changes when fewer people are connected: the panel does not
schedule applies for you.

Before writing anything, the agent saves the current `profiles.json`, `mtproxy.env` and site. If
the relay does not come back healthy after the restart, the agent restores what it saved and
reports "Rolled back", so the server is never left half-changed.

- `relay -check rejected profiles: ...`: the new settings do not fit the relay configuration on
  this server, for example a key limit above the server's global limit. Fix the key or the limit
  and apply again. The same check can be run on the server:

  ```bash
  tproxy-server -config /etc/tproxy-server/config.json -profiles-file /etc/tproxy-server/profiles.json -check
  ```

- `rollback FAILED; manual intervention needed`: the automatic rollback did not complete. Compare
  `/etc/tproxy-server/profiles.json` and `/etc/mtproxy/mtproxy.env` with the saved copies in the
  newest directory under `/var/lib/tgwp-agent/backup/`, restore them by hand and apply again.

## A telemt update failed

What you see: the "Telemt updates" card on the Maintenance tab shows a status other than "Updated
and checked", the queue on the Servers page stopped with "Needs attention", and Overview has a
new problem for this server.

| Status | What happened | The server now |
|---|---|---|
| "Updated and checked" | Everything went through | Runs the new version |
| "Update refused; binary unchanged" | The pre-update check stopped it: the version is already installed, telemt is not answering, it cannot drain, the download failed or the checksum did not match | Nothing changed |
| "Update failed" | The update stopped without replacing telemt | Runs the previous version |
| "Previous version restored" | The new version failed its checks and the agent put back the previous one with its settings | Runs the previous version |
| "Rollback failed — attention required" | The new version did not work, and putting back the previous one failed too | Most likely not serving |
| "Verification incomplete — attention required" | The new version runs, but WEB admission stayed closed or the panel's checks after the update failed | Needs checking |

Each step in the card has "Technical details" with the agent's message, and the error text is
shown under the steps.

1. "Update refused", "Update failed" or "Previous version restored": the server works as before.
   Read at which step and why it stopped, fix the cause and try again. If telemt on the server
   cannot drain, upgrade it over SSH with `tgwp-agent upgrade`.
2. "Verification incomplete": open Checks → "Full server check" → "Run diagnostics" and see what
   failed.
3. Applies, route changes and the WEB transport buttons answer with
   `interrupted update requires recovery; inspect agent logs and restart the agent after resolving the recovery error`.
   This means the agent holds a record of an unfinished update. It happens after "Rollback
   failed", after "Update failed" at the pause, drain or replace steps, after "Verification
   incomplete" with admission closed, and when an update was interrupted, for example by a server
   reboot.

   Before replacing telemt, the agent writes to disk that an update is starting and keeps
   verified copies of the working version and its settings: `/var/lib/tgwp-agent/telemt-known-good`
   and `telemt-known-good.toml`. On start, the agent looks at this record. If the update did not
   finish, it puts the saved copies back, restarts telemt and restores the previous WEB admission
   state. Until the record is closed, the agent refuses new applies, route changes and updates.

   On the proxy server, read the agent log and check the disk:

   ```bash
   journalctl -u tgwp-agent -n 200 --no-pager
   df -h
   ```

   Fix the cause the log names, for example a full disk. Then restart the agent:

   ```bash
   systemctl restart tgwp-agent
   ```

   After that the server runs the previous version, and the update can be tried again. If putting
   the previous version back failed, the agent log has `update recovery requires attention` with
   the reason. Do not delete the record `/var/lib/tgwp-agent/telemt-update-journal.json` blindly:
   it is what lets the agent put the working version back. The `telemt-known-good*` files are kept
   until the next update.

Why the update queue stopped is written under it:

- `pre-update health check failed`: the server was unhealthy before the update, start with
  [A server is degraded](#a-server-is-degraded);
- `node is in maintenance mode`: the server has maintenance mode on;
- `node update ended <status>; remaining nodes were not started`: the update of this server did
  not succeed, the table above explains the status;
- `post-update observation failed; rollout stopped`: within a minute after the update the server
  stopped reporting healthy; the text of a problem found on the server can appear here instead;
- `dispatch was interrupted; inspect the node before starting a new rollout`: see
  [Several telemt servers](#several-telemt-servers).

It worked if the card shows "Updated and checked" (or "Previous version restored" after the agent
restart), the server is Healthy, and applies go through again.

## Some people cannot connect

What you see: someone says Telegram hangs while connecting through the proxy, and it works for
everyone else. The server is Healthy.

Work from the person towards the server.

1. Find the person's key under Access keys and look at its status:
   - Revoked: the key was revoked or it expired. The panel revokes expired keys by itself, and
     such a key cannot be extended. Issue a new one.
   - Pending: the key has not reached the server yet, see
     [Changes do not reach a server](#changes-do-not-reach-a-server).
   - Active: go to the next step.
2. Look at the key's limits (they work on telemt servers): "Traffic quota, GB", "Max unique IPs",
   "Max connections". If the traffic hit the quota or more devices use the key than it allows,
   telemt refuses new connections. Raise the limit or give people separate keys.
3. Find out which link the person uses:
   - The WEB link works only in Telegram Desktop and recent Telegram for Android. On an iPhone and
     in other apps, the Fake-TLS link is needed.
   - A Fake-TLS link issued before the server's Fake-TLS domain or port changed no longer works.
   - After Rotate on the key, its previous links stop working.
   - After "Rotate link" or "Revoke link" on the subscription, the previous subscription link stops
     working.

   In the last three cases, send a fresh link from "Connection links" on the key.
4. If people on one provider or in one region cannot connect while others can, the provider is
   probably blocking the Fake-TLS domain or port. What you can do:
   - add "Backup masking domains" on the server's Settings tab, in the "Addresses and Fake-TLS"
     card. Every key gets extra Fake-TLS links with other domains, and links already issued keep
     working. The next apply restarts telemt;
   - give the WEB link to those who use Telegram Desktop or Android;
   - add a server in another network.

   [Checks from other networks](#checks-from-other-networks) let you see the server from a
   particular provider's network.
5. Check the server even though it is Healthy:
   - Overview has no "WEB sessions are not being admitted" problem. If it does, open the Stats
     tab, the "WEB transport" block, and click Resume;
   - on the Stats tab, the "WEB capacity" block shows None for "Saturated resources". If
     resources are saturated, new WEB connections wait or get refused. What happens then is set in
     "Overload protection" on the Settings tab;
   - "Check from the panel" on the Checks tab passes "Fake-TLS mask". If it does not, open the
     Fake-TLS port (8443 by default) in the hosting provider's firewall;
   - Checks → "Full server check" finds nothing wrong.

It worked if the person is connected: mobile Telegram shows the proxy as connected under "Data and
Storage" → "Proxy", and the key's traffic grows.

## A check found a problem

A server's Checks tab has three kinds of checks: "Full server check" (telemt servers only),
"Check from the panel" and "External network checks"
([how to set them up](#checks-from-other-networks)).

### Check from the panel

The panel checks the server from outside, from its own server: DNS, ports 80 and 443, the
certificate, the site and, on telemt servers, the Fake-TLS mask. It runs with "Run check", and
the result is saved. Failed checks are shown on top with details, passed ones are collapsed.

- "DNS record": the domain does not resolve (fix the DNS record and wait), or it points at
  another IP, and then the details read `<resolved addresses> (expected <address>)`. Fix the DNS
  record or, if the server's address changed, change "Public IP" in the "Addresses and Fake-TLS"
  card.
- "Port 80 (HTTP)" or "Port 443 (HTTPS)": the port is unreachable from the panel. Check that the
  server's and the hosting provider's firewall allow inbound 80 and 443, and that the service
  listening on them runs: `systemctl status caddy` on a telemt server,
  `systemctl status caddy tproxy-server` on a tproxy server. When port 443 fails, the
  certificate, post-quantum and site checks are skipped with `skipped: tcp_443 failed`. Fix 443
  first and run the check again.
- "TLS certificate" marked `(expiring soon)`: the certificate expires in less than 7 days. Caddy
  renews it by itself, so read `journalctl -u caddy -n 50 --no-pager`. A common cause is port 80
  being closed at renewal time. Let's Encrypt rate limits happen too.
- "TLS certificate" failed outright: the certificate is for another domain, is not trusted, or
  the handshake failed. The exact error is in the details. The certificate must be issued for
  exactly the server's domain.
- "Post-quantum key exchange": informational only, it never affects the overall result. The
  server did not choose `X25519MLKEM768`, and the details name what it chose instead. Caddy does
  this out of the box, so usually something else answers on port 443 (a CDN, an old reverse
  proxy) or Caddy is too old. Clients are not affected.
- "Site responds" with `redirect to ... not followed`: the site at `https://<domain>/` answered
  with a redirect instead of a page. The check does not follow redirects on purpose, otherwise a
  server could send the panel's request anywhere. Whatever sits in front of the site must answer
  `/` with a 200 page directly. Look for an unwanted redirect rule in the site settings.
- "Fake-TLS mask": the Fake-TLS port does not answer with the masking domain's certificate. Check
  that the Fake-TLS port (8443 by default) is open in the hosting provider's firewall and that
  telemt runs. The server must also be able to reach its own public address on port 443; the
  [setup guide](setup.en.md) has more on this.

The DNS, port 80 and port 443 checks always run, independently of each other. Only the
certificate, post-quantum and site checks can be skipped, and only when port 443 fails.

### Full server check

On a telemt server, open Checks → "Full server check" and click "Run diagnostics". It walks the
whole chain: DNS, ports and TLS, Caddy, the WEB transport, telemt, the connection to Telegram,
and each published IP address of the domain separately (up to eight). Some checks the panel runs
from outside, others come from the agent's readings.

Each check ends in one of four results: passed, warning, failed and "could not run". "Could not
run" means the panel was unable to perform it, for example because the server is offline. Such
checks are counted separately and are neither passes nor failures. A failed check comes with a
hint on what to do.

"Diagnostics history" keeps earlier runs, and "Export report" saves the result as JSON: attach it
when you ask for help. A passing check does not guarantee that every Telegram client connects, so
after changes, connect yourself from a real Telegram app.

The panel also runs this check by itself: at most one server per pass, and each server no more
often than every 15 minutes. It skips offline servers and those in maintenance mode. A failed
check opens a "Diagnostics: …" problem on Overview, and the problem closes when the check passes
again. For counters that only grow, such as TLS handshake failures, only growth since the
previous check counts as a problem.

## Telegram alerts do not arrive

The panel writes to Telegram when a server goes offline and comes back, when an apply fails, and
when checks open or close a problem.

1. Open Settings → Notifications, the "Telegram alerts" block. It must say "Token set", and "Chat
   ID" must be filled in. If either is missing, the panel silently sends nothing.
2. Click "Send test message" (owner only). The test uses what is in the form right now and falls
   back to the saved values for empty fields, so you do not have to save first.
   - An error about a missing token or chat ID: that value is empty both in the form and in the
     saved settings.
   - An error from Telegram. `Unauthorized` means the bot token is wrong or revoked.
     `chat not found` or `Forbidden: bot was blocked by the user` means the chat ID is wrong or the
     bot was never added to that chat. Open a direct chat with the bot, or add it to the group or
     channel, and send `/start` once.
   - "Test message sent": delivery works, go to the next step.
3. Check that "Enable alerts" is on. The test ignores this switch, but real alerts are not sent
   without it.
4. Mind the limit: each pair of server and alert kind sends at most one message per 5 minutes. The
   window starts only after a successful send, so a failed attempt does not hold back the next
   one. A server that went offline and came back twice within 5 minutes produces one message.
5. If the test works but real alerts never come, look for `telegram config` or
   `telegram send failed` warnings in the panel log:

   ```bash
   cd /opt/tgproxy-panel
   docker compose logs panel | grep -i telegram
   ```

It worked if the test message reached the right chat and the next event on a server arrived too.

## Reinstalling the agent

Reinstall when the server's agent token is lost or revoked (`invalid node token` in the agent
log), the server was rebuilt from scratch, the agent is too old for `tgwp-agent upgrade`, the
agent itself is broken, or a tproxy server needs another `tproxy-server` commit. A regular
upgrade does not need a reinstall, `tgwp-agent upgrade` is enough.

1. Get a new install command: the server page → Maintenance → "Show install command", or Servers
   → the server's "⋯" menu → "Install command". The previous command stops working. The new one
   is valid for 24 hours.
2. Run it on the proxy server as root. It looks like this:

   ```bash
   curl -fsSL https://panel.example.com/api/v1/install/<token>.sh | sudo bash
   ```

The command is safe to run on a server that already has the proxy. On a tproxy server the
installer skips installing `tproxy-server` if `/usr/local/bin/tproxy-server` already exists. On a
telemt server it downloads telemt again, verifies the checksum, runs `init-node` again (the
telemt API token is kept) and restarts the service. The rest of the installation runs again too,
Caddy and the firewall rules included. At the end the server registers with the panel again and
the new agent token is written to `/etc/tgwp-agent/agent.env`.

If the installation failed, the installer says whether the same command can be run again: until
the server is registered, the install token stays valid.

It worked if the installer printed `Node <domain> is registered with the panel.` and the server
is Healthy in the panel again.

## The cover site did not change

What you see: you changed the site on the Cover site tab, but the server's domain still shows the
old one.

1. Look at the status on the Cover site tab. "Pending apply" means the site waits for the next
   apply; click "Apply now" if you do not want to wait. Deployed means the site is already on the
   server. If it says the last apply failed, open "Apply history" on the Maintenance tab: when the
   rollback succeeds, the server keeps the previous site.
2. Assigning the same template again changes nothing. Every server gets its own variant of a
   template, with a different block order and different class and file names, so servers cannot
   be matched by identical pages. The variant depends only on the template and the server, so the
   same template on the same server produces the same site. The panel does not send it again and
   restarts nothing. A new site goes to the server only when the template content changed, another
   template was chosen, or you clicked "Apply now" yourself. You can compare `bundle_hash` and
   `deployed_hash` in the response of `GET /api/v1/nodes/{id}/site`.
3. "Previously deployed website" means the server still runs a site installed before the template
   catalogue. Pick one of the 15 built-in sites or your own template under Cover websites and
   assign it to the server.
4. Your own site ("Existing HTTP website") works only on telemt servers that report support for
   it. The address must start with `http://` and point at a loopback or private IP address with no
   path, for example `http://127.0.0.1:8080`. "Test and apply" first checks the address on the
   server itself. If it does not answer, the previous site stays.

On a tproxy server a site change restarts the relay, see [On a tproxy server](#on-a-tproxy-server).

It worked if the server's domain shows the new site in a private browser window and "Site
responds" passes in "Check from the panel".
