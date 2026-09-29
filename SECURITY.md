# Security

**English** · [Русский](SECURITY.ru.md)

## Reporting a vulnerability

Please don't open a public issue. Report privately through
[GitHub security advisories](https://github.com/greenpandorik/tgproxy-panel/security/advisories/new).
If that form is not available, open an issue that only asks for a private contact, with no
details.

In the report, say what you did, what happened and what you expected, and which version you run
(the panel shows it in the top right corner). A proof of concept helps but is not required.

People rely on these proxies to reach Telegram. Please don't publish details or a working
exploit until a release with the fix is out.

## Supported versions

Fixes go into the latest release only. The project has one maintainer, and older versions are
not patched. To update, run `sudo /opt/tgproxy-panel/install.sh --update` on the panel's server
and `sudo tgwp-agent upgrade` on each proxy server.

## What the panel does to protect an install

If a term is unfamiliar, see the [glossary](docs/start.en.md#glossary).

### Sign-in

- Passwords are hashed with argon2id and a random salt, and compared in constant time. The
  hash parameters read from the database are bounded, so a tampered hash cannot make one sign-in
  use up the memory.
- More than 10 sign-in attempts from one IP address within 10 minutes block that address for
  15 minutes.
- After 20 failed attempts with no successful sign-in in between, the account is locked for
  15 minutes. Wrong 2FA codes count too.
- Every failed sign-in goes to the log and to the "Activity log" with the reason.
- Two-factor sign-in (TOTP) is off until you set `FEATURE_TOTP=true`. When it is on, the second
  step must be finished within 5 minutes, wrong codes count toward the same limits, and the
  8 recovery codes are single-use and stored only as hashes. How to reset 2FA for a locked-out
  user is in the [runbook](docs/runbook.md).

### Sessions and roles

- A session is a random ID stored in the database. The cookie holding it is signed with
  `SESSION_SECRET`, marked `HttpOnly` and `SameSite=Lax`, and `Secure` when `PANEL_PUBLIC_URL`
  starts with `https://`.
- A session lasts 24 hours and is extended while you use the panel. Logging out deletes it.
  Changing your password or turning on 2FA ends all your other sessions.
- Every request that changes something must carry an `X-CSRF-Token` header equal to the
  `tgwp_csrf` cookie. The values are compared in constant time.
- There are three roles. An owner can do everything. An admin can do everything except manage
  accounts, panel settings and backups. A viewer can only look and change their own password
  and 2FA.
- A test walks every API route. Each route must refuse a visitor without a session, and each
  change must be refused without the CSRF token and for a viewer. Routes that are public on
  purpose, and the few changes a viewer may make, are listed in the test by name.

### Secrets in the database

- Proxy secrets of keys and servers, TOTP secrets and the Telegram bot token are encrypted with
  AES-256-GCM under `MASTER_KEY`. Each encrypted value records its key version, so the key can
  be replaced with `panel keys rotate`. The panel has to be stopped while the command runs; the
  steps are in the [runbook](docs/runbook.md).
- Agent tokens, install tokens, subscription link tokens and 2FA recovery codes are stored only
  as SHA-256 hashes. A copy of the database does not give usable tokens.
- An install command works once and expires after 24 hours.
- Without `MASTER_KEY` the encrypted secrets cannot be recovered, even from a backup. Keep a
  copy of `.env` somewhere other than the panel's server.
- Backups can be encrypted with [age](https://age-encryption.org) to a public key
  (`BACKUP_AGE_RECIPIENT`). The private key stays off the panel's server.

### Servers

- The agent on each server connects out to the panel and presents its own token. When the
  panel's address is `https://`, the connection uses TLS 1.2 or newer.
- On telemt servers, the control API (port 9091), the metrics (9090) and the WEB listener
  (18080) listen only on the server's loopback interface. The install script adds an nftables
  rule that drops outside traffic to these ports, and the control API requires a token on top
  of that.
- Downloads are checked against pinned checksums. The server installer checks the telemt archive
  against the SHA-256 set in the panel (`TELEMT_SHA256_X86_64`), and tproxy-server is built from a
  pinned commit (`TPROXY_COMMIT`). `tgwp-agent upgrade` and telemt updates refuse a download
  without a pinned SHA-256 or with a wrong one, and leave the installed version in place.

### Web pages and uploads

- The panel is served with a Content Security Policy that allows only its own scripts. It
  cannot be put in a frame (`X-Frame-Options: DENY`, `frame-ancestors 'none'`), and every
  response has `X-Content-Type-Options: nosniff`.
- The public subscription page (`/s/<token>`) shows server names, addresses and links. The
  key's "Key name", "Issued to" and "Note" fields do not appear on it. It allows 60 requests per
  minute from one IP address, is sent with `Cache-Control: no-store` and `X-Robots-Tag: noindex`,
  and its policy forbids any request to other sites.
- Cover site archives (ZIP) are read in memory and never unpacked to disk. The panel limits
  their size and number of files and rejects unsafe paths. It also rejects external scripts,
  styles and media, frames, forms, `<base>`, manifests, service workers, inline `on…` handlers
  and `javascript:` links. Inline styles and scripts are moved into separate files.
- SVG files for branding are parsed as XML and rejected if they contain `<script>`,
  `<foreignObject>`, `<iframe>`, event handlers or external links. They are served in a sandbox
  with their own policy.

### Logs and outbound connections

- The request log records only the route pattern, such as `/s/{token}`, so links with tokens
  in the path never reach the log. Requests to `/healthz` and `/metrics` are not logged.
- `/metrics` is closed with `METRICS_TOKEN`. With real servers the panel does not start without
  it. See [monitoring](docs/monitoring.md).
- Apart from your servers and the integrations you set up yourself (Telegram alerts, a webhook,
  backup upload), the panel makes one outbound request on its own: the update check at
  `api.github.com`. `UPDATE_CHECK=false` turns it off.
- Webhook and backup upload addresses must be `https://`, with no login, query or fragment in
  the URL. Webhook messages carry a timestamp and an HMAC-SHA256 signature made with
  `ALERT_WEBHOOK_SECRET`.

## What you should do

- Keep `.env` secret: it holds `MASTER_KEY`, `SESSION_SECRET` and `METRICS_TOKEN`. The
  installer writes it with mode `0600`. Don't put it in git, and keep a copy off the server.
- Serve the panel over HTTPS. `install.sh` does this with Caddy. Without HTTPS the session cookie
  is sent without the `Secure` flag.
- Keep the panel behind the bundled Caddy, or another proxy that overwrites
  `X-Forwarded-For`. The panel takes the client's address from the last value of that header,
  and the per-IP limits and the "Activity log" depend on it. If port 8080 is reachable from the
  internet, anyone can forge the header.
- Don't publish the Postgres or panel ports. The shipped Compose file publishes only Caddy's 80
  and 443.
- Turn on 2FA with `FEATURE_TOTP=true` and give the viewer role to people who only need to
  look.

Installation is described in the [setup guide](docs/setup.en.md), how the panel is built in
the [reference](docs/reference.md).
