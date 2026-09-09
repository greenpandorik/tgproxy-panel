# Security

## Reporting a vulnerability

Report privately through [GitHub security advisories](https://github.com/greenpandorik/tgproxy-panel/security/advisories/new) rather than opening an issue. Please include what you did, what happened, and what you expected; a proof of concept helps but is not required to report something.

Anyone running this software is running proxy infrastructure that people may depend on to reach Telegram, so please do not publish a working exploit before there is a fix to upgrade to.

## What the panel protects

- **Secrets at rest.** Access-key secrets, TOTP secrets and the Telegram bot token are encrypted with AES-256-GCM under `MASTER_KEY`, with versioned keys so the master key can be rotated without downtime (`panel keys rotate`).
- **Passwords.** argon2id, salted, verified in constant time, with the cost parameters bounded on read so a tampered hash cannot turn one login into a memory exhaustion.
- **Sessions.** Signed, `HttpOnly`, `Secure` when the public URL is HTTPS, `SameSite=Lax`. CSRF is a double-submit token compared in constant time.
- **Sign-in.** Rate limited per IP, with an account lockout after repeated failures. Failed attempts are logged and recorded in the audit trail. TOTP verification shares the same limiter, so the second factor cannot be brute-forced separately.
- **Authorisation.** Every route is covered by a test that walks the live router and asserts that anything mutating requires a session and a role; the exceptions are listed explicitly with their reasons.
- **The node link.** The panel never exposes telemt's control API; it lives on the node's loopback and only the node's agent talks to it. Agents authenticate with a per-node token stored as a hash.
- **Downloads to nodes.** The agent refuses to install any binary whose SHA-256 the panel cannot vouch for.
- **Uploads.** Site bundles are parsed and rejected on path traversal, external references and script vectors; branding SVGs are XML-parsed and rejected on script vectors, then served sandboxed with their own CSP.
- **Public surfaces.** Subscription pages carry no key label, owner or note, are rate limited per IP, and are served `no-store` with a policy that forbids every external request. The panel itself is served with a policy that permits no script it did not ship.
- **Logs.** Secrets are wrapped in a type that redacts them. The request log records chi's route pattern, not the URL, because `/s/{token}` and the install routes carry a secret in the path.

## Deploying safely

- Keep `.env`, `MASTER_KEY` and `SESSION_SECRET` out of version control and off shared machines.
- Put the panel behind HTTPS. Several protections, including `Secure` cookies, are only meaningful there.
- The panel's own Postgres and telemt's control API should not be reachable from the internet; the shipped Compose file keeps them off published ports.
- `UPDATE_CHECK=false` stops the only outbound call the panel makes that you did not configure.

## Supported versions

Fixes go to the latest release. This project has one maintainer; older versions are not backported.
