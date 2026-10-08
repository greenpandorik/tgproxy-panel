# API tokens and public API documentation

Purpose: administrators automate their own panel using scripts, bots and integrations. The supplied screenshots guide the token list and creation dialog; existing panel components, themes and RU/EN translations remain the design system.

## Authentication and permissions

Personal tokens belong to an existing administrator. A 32-byte cryptographically random secret prefixed `tgwp_` is returned exactly once; PostgreSQL stores only its SHA-256 digest and a short public prefix. All management calls use `Authorization: Bearer <token>` over HTTPS. Invalid, malformed or duplicate Authorization headers on protected routes never fall back to cookie authentication. Dedicated agent, probe, metrics and subscription-service authentication stays separate.

Permissions are the intersection of current account role, the token's scopes and an explicit route policy. New or unknown routes default to denial. Groups: `nodes`, `users` (existing `/keys` routes), `monitoring`, `sites`, `branding`, `settings`, with `read` and `write`; `audit:read` only. Secret-bearing GETs such as install commands, registration secrets, proxy links and QR codes require write permission and the existing writer role. Full rights means all automation scopes allowed by the account, not account security, administrator management, backups or service credential management.

Management and creation of personal tokens require a browser session and CSRF. Tokens cannot mint tokens, change passwords or TOTP, manipulate administrators, download backups, or issue subscription service credentials. Viewers may create read-only personal tokens. Current database role is checked on each call; deleting an administrator invalidates its tokens through cascading deletion. Password changes revoke all personal tokens. Token ID is added to mutation audit metadata; secrets never enter audit or access logs. Last-use updates are throttled to one minute.

## Contract

Session-only endpoints under `/api/v1`: GET `/api-tokens`, POST `/api-tokens`, DELETE `/api-tokens/{id}`, GET `/api-tokens/scopes`. Listings contain only the current account's records.

POST body: `{ "name": "Service Bot", "expires_in_days": 30, "scopes": ["nodes:read"] }`. Names are trimmed, 1–80 Unicode characters; duration is an integer 1–365 days; at least one recognized scope is required; duplicate scopes normalize. Maximum 50 unrevoked, unexpired tokens per account, enforced under an account row lock. Successful POST returns 201 `{ "token": "tgwp_...", "api_token": <metadata> }` and Cache-Control no-store. DELETE is idempotent for an owned token and returns 204; foreign IDs appear absent.

Metadata: `id`, `name`, `prefix`, `scopes`, `created_at`, `expires_at`, nullable `last_used_at`, nullable `revoked_at`. GET scopes returns `{ "scopes": [{ "id": "nodes:read", "resource": "nodes", "action": "read" }], "max_expires_in_days": 365, "max_tokens": 50 }`; unavailable scopes for a viewer are omitted.

## Panel

Personal Settings → API tokens. List name/prefix, status, permissions, expiration and last use; responsive layout. Create dialog has name, duration default 30, read-only/full presets and resource read/write choices. Write controls are absent for viewers. After creation, an accessible dialog shows the secret, copy action and one-time warning. No localStorage, sessionStorage, draft or durable query cache stores the secret. Clear mutation data on close; do not auto-dismiss the secret on focus changes. Revocation has confirmation. Provide localized links to `/api/` or `/en/api/` at tgproxypanel.com.

## Public docs

Indexed RU `/api/` and EN `/en/api/`, linked from home and panel, included in hreflang, sitemap and llms.txt. Explain base URL belongs to the user's installed panel, token creation, scopes, expiry/revocation, session-only operations, status/error envelope, examples using curl and Python stdlib. Cover real automation resources without advertising unrelated screenshot features. Downloadable OpenAPI 3.0.3 at `/api/openapi.json` describes Bearer security and each allowed automation endpoint. Its route/scope catalog is produced from the same explicit policy used by the backend. Examples for users and nodes must use real field names. A CI check catches stale generated contracts.

## Verification

Real PostgreSQL HTTP tests: one-time secret/digest only, scope and role enforcement, expiry/revocation/owner deletion/password rotation, wrong and mixed credentials, no CSRF bypass through invalid Bearer, session-only boundaries, cross-account revocation, validation and token limit. Walk every route with restricted tokens to catch unintended access; check policy/catalog consistency. UI tests cover create/copy/clear/revoke and viewer scope choices. Full Go race suite, frontend tests/typecheck/lint/build, site validator, and desktop/mobile browser QA. No production token is issued for testing and no live engine deployment is part of this feature.

## Integration

Worktree `feature/api-tokens` includes the already prepared telemt reliability fixes, merged locally with current website main. Notification state migration is 26; tokens use migration 27. Main remains clean until review/integration.
