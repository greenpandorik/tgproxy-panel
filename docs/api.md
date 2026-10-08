# TGProxy Panel API: tokens, scopes and examples

Use the management API to automate your own panel from scripts, bots and integrations. Users are called keys in the API: the Users screen corresponds to `/api/v1/keys`.

## Create a personal API token

Sign in to your panel and open Settings → API tokens. Choose a name, an expiry and the permissions your integration needs. The default duration is 30 days; you can choose an integer from 1 to 365 days. Each account can have up to 50 active tokens. Viewers can create read-only tokens.

Copy the `tgwp_…` secret before closing the creation dialog: it appears only once. The panel stores its SHA-256 digest and a public prefix, so the token list cannot recover it. Store the secret in your integration’s secret manager or environment; avoid source code, logs and shared screenshots.

Send requests over HTTPS to **your installed panel**, for example `https://panel.example.com/api/v1`. `tgproxypanel.com` hosts this documentation and is not your management API endpoint.

Use one `Authorization: Bearer <token>` header. A validated personal Bearer token does not need cookies or `X-CSRF-Token`. Invalid or duplicate Authorization headers do not fall back to a signed-in browser session. Token support does not change the panel’s CORS policy; use these examples from a server or command line.

Revoke a token in Settings → API tokens when it is no longer needed. Expired and revoked tokens stop working. Changing your password revokes all your personal tokens; deleting the account invalidates its tokens. The token list shows expiry and last use; last-use timestamps update at most once per minute.

## Permissions and account roles

Every request must be allowed by both the token’s scopes and the account’s current role. Write scopes do not automatically include read scopes. Role changes take effect on subsequent requests. Read-only and full-rights presets select scopes; full rights still follows the account’s role and the endpoint catalog below.

| Resource | Scopes | What it covers |
|---|---|---|
| `nodes` | `nodes:read`, `nodes:write` | Servers, configuration, installation and applies |
| `users` | `users:read`, `users:write` | Users, bindings and subscription links; routes use /keys |
| `monitoring` | `monitoring:read`, `monitoring:write` | Dashboard, monitoring and alerts |
| `sites` | `sites:read`, `sites:write` | Cover websites |
| `branding` | `branding:read`, `branding:write` | Panel and subscription page appearance |
| `settings` | `settings:read`, `settings:write` | Automation settings allowed by the account role |
| `audit` | `audit:read` | Activity log |

Reading sensitive values requires write permission even when the HTTP method is GET: this includes user secrets, proxy links and QR codes, node installation commands and registration secrets. A read scope does not grant these values. Use the catalog to check the scope for each route.

Token creation, listing and revocation, account administration, password and 2FA changes, backups and subscription-service credentials require a browser session. A personal token cannot create another token or manage these operations. Agent, probe, metrics and subscription-service tokens have separate purposes and cannot be replaced by a personal token.

Mutation audit entries include the personal token’s ID in `api_token_id`, so you can identify the integration that made a change without storing its secret in the audit log.

### Browser-session token management

These routes require the signed-in account’s session cookie; POST and DELETE also require `X-CSRF-Token` matching `tgwp_csrf`. They are unavailable to personal Bearer tokens. Listings and revocation concern only the current account’s tokens.

| Method | Path | Response |
|---|---|---|
| GET | `/api/v1/api-tokens` | `{items: [...], total}` |
| GET | `/api/v1/api-tokens/scopes` | `{scopes: [{id, resource, action}], max_expires_in_days: 365, max_tokens: 50}` |
| POST | `/api/v1/api-tokens` | HTTP 201: `{token, api_token}` |
| DELETE | `/api/v1/api-tokens/{id}` | HTTP 204 |

Creation body:

```json
{"name": "Service Bot", "expires_in_days": 30, "scopes": ["nodes:read"]}
```

Names are trimmed and must contain 1–80 Unicode characters. At least one recognized scope is required; duplicate scopes normalize. Unavailable scopes for the current account are omitted from `/api-tokens/scopes`. Token metadata contains `id`, `name`, `prefix`, `scopes`, `created_at`, `expires_at`, nullable `last_used_at` and nullable `revoked_at`. The creation response is `Cache-Control: no-store`; the secret is absent from subsequent listings. Revocation is idempotent for an owned token; another account’s ID appears absent.

## curl: read a server and manage users

Replace the example address and UUIDs with values from your panel. Load your token from your secret store; the value below is a placeholder. Reading a node and listing users requires `nodes:read` and `users:read`. Creating, disabling and enabling users requires `users:write` and an owner or admin account.

### Set the connection values

```bash
export TGWP_PANEL_URL='https://panel.example.com'
export TGWP_API_TOKEN='tgwp_replace_with_your_token'
export TGWP_NODE_ID='replace-with-node-uuid'
export TGWP_KEY_ID='replace-with-user-uuid'
```

### Read a node and list active users

```bash
curl --fail-with-body --silent --show-error \
  -H "Authorization: Bearer $TGWP_API_TOKEN" \
  "$TGWP_PANEL_URL/api/v1/nodes/$TGWP_NODE_ID"

curl --fail-with-body --silent --show-error \
  -H "Authorization: Bearer $TGWP_API_TOKEN" \
  "$TGWP_PANEL_URL/api/v1/keys?state=active&page=1&per_page=50"
```

### Create personal access

```bash
curl --fail-with-body --silent --show-error \
  -X POST "$TGWP_PANEL_URL/api/v1/keys" \
  -H "Authorization: Bearer $TGWP_API_TOKEN" \
  -H 'Content-Type: application/json' \
  --data "{\"label\":\"Automation example\",\"type\":\"PERSONAL\",\"node_ids\":[\"$TGWP_NODE_ID\"],\"expires_at\":null}"
```

`label`, `type` and at least one `node_ids` UUID are required. `type` is uppercase `PERSONAL` or `SHARED`. `expires_at: null` means no user access expiry; supply a future RFC 3339 timestamp for a time limit. This is separate from the API token’s expiry. The response is HTTP 201 with a user object; copy its `id` into `TGWP_KEY_ID`. Creation responses can contain user secrets and links: handle them privately.

### Pause and resume access

```bash
curl --fail-with-body --silent --show-error \
  -X POST -H "Authorization: Bearer $TGWP_API_TOKEN" \
  "$TGWP_PANEL_URL/api/v1/keys/$TGWP_KEY_ID/disable"

curl --fail-with-body --silent --show-error \
  -X POST -H "Authorization: Bearer $TGWP_API_TOKEN" \
  "$TGWP_PANEL_URL/api/v1/keys/$TGWP_KEY_ID/enable"
```

Both calls return HTTP 200 with the updated user. These examples leave the user enabled. Resuming access does not remove an expired access date or undo revocation.

## Python without external packages

This script uses the same environment variables and permissions as the curl examples. It reads a node, paginates through active users, then creates a user with seven days of access and disables and enables that new user. Save it as `example.py` and run `python3 example.py`. It prints identifiers and state rather than the complete creation response.

```python
import json
import os
from datetime import datetime, timedelta, timezone
from urllib.error import HTTPError
from urllib.parse import urlencode
from urllib.request import Request, urlopen

base = os.environ["TGWP_PANEL_URL"].rstrip("/") + "/api/v1"
token = os.environ["TGWP_API_TOKEN"]
node_id = os.environ["TGWP_NODE_ID"]
if not base.startswith("https://"):
    raise ValueError("Use the HTTPS address of your own panel")

def api(method, path, body=None):
    headers = {"Authorization": "Bearer " + token}
    data = None
    if body is not None:
        headers["Content-Type"] = "application/json"
        data = json.dumps(body).encode("utf-8")
    req = Request(base + path, data=data, headers=headers, method=method)
    try:
        with urlopen(req, timeout=30) as response:
            raw = response.read()
            return json.loads(raw) if raw else None
    except HTTPError as exc:
        raw = exc.read()
        try:
            error = json.loads(raw).get("error", {})
        except (ValueError, AttributeError):
            error = {"code": "http_error", "message": "Non-JSON response"}
        raise RuntimeError(
            f"HTTP {exc.code}: {error.get('code')}: "
            f"{error.get('message')}; fields={error.get('fields', {})}"
        ) from None

node = api("GET", "/nodes/" + node_id)
print(node["name"], node["status"])
page = 1
while True:
    result = api("GET", "/keys?" + urlencode({
        "state": "active", "page": page, "per_page": 50
    }))
    for user in result["items"]:
        print(user["id"], user["label"], user["state"])
    if not result["items"] or page * result["per_page"] >= result["total"]:
        break
    page += 1

expires = datetime.now(timezone.utc) + timedelta(days=7)
user = api("POST", "/keys", {
    "label": "Automation example", "type": "PERSONAL",
    "node_ids": [node_id], "expires_at": expires.isoformat()
})
print("Created user:", user["id"])
api("POST", "/keys/" + user["id"] + "/disable")
api("POST", "/keys/" + user["id"] + "/enable")
```

## Lists, filters and pagination

List responses use `items`; do not assume that every list accepts the same pagination parameters.

| Route / parameter | Behavior |
|---|---|
| `/keys: page` | Starts at 1; default 1. |
| `/keys: per_page` | 1–200; default 50. Values outside that range use 50. |
| `/keys: q` | Search labels, owner labels, notes and subscription slugs. |
| `/keys: type` | PERSONAL or SHARED. |
| `/keys: status` | Stored status: pending, active or revoked. |
| `/keys: state` | active, pending, expiring, expired, disabled or revoked. expiring means within seven days. |
| `/keys: node` | Filter by a node UUID; the parameter is node, not node_id. |
| `/nodes` | Returns {items, total}; no page or per_page parameters. |
| `/nodes/{id}/jobs: limit` | 1–100; default 20. Values outside that range use 20. |

`GET /keys` returns `{items, total, page, per_page}`. Fetch pages until you reach `total` or receive an empty `items` array. Lists can change while you paginate; avoid changing the filtered users in the middle of a scan.

## Applying changes and retrying requests

Creating or changing a user schedules configuration changes on its nodes; a successful HTTP response does not mean that the proxy has applied them yet. Inspect the user’s `nodes[].profile_sync`, and the node’s `dirty`, `last_apply_at` and `GET /nodes/{id}/jobs`. Job history requires `nodes:write` because it includes raw logs and errors. Jobs include `status`, `started_at`, `finished_at` and `error`; statuses are `queued`, `running`, `ok`, `failed` and `rolled_back`.

`POST /nodes/{id}/apply` requires `nodes:write` and returns HTTP 202 with `{"queued": true}`. It does not return a job ID. Poll the node and recent jobs with a reasonable delay, such as five seconds, and an overall timeout. A failed job needs inspection; repeatedly queuing applies will not explain the failure.

Use request timeouts. Retry reads after transient network or server failures with a bounded delay. Do not blindly retry user creation, secret rotation or other mutations: a lost response can follow a successful operation. Check the resulting state first to avoid duplicate users or unintended changes.

## HTTP statuses and error responses

Successful responses can be JSON or another documented format, such as a PNG or Prometheus text. HTTP 204 has no response body. Errors use this JSON envelope; `fields` is present for field validation errors:

```json
{
  "error": {
    "code": "validation",
    "message": "invalid input",
    "fields": {
      "node_ids": "bind at least one node"
    }
  }
}
```

| Status | Meaning |
|---|---|
| 200 / 201 / 202 / 204 | Read or update succeeded / resource created / queued / success without a body. |
| 400 | Malformed request or JSON; unknown JSON fields are rejected. |
| 401 | Missing, invalid, expired or revoked credentials. |
| 403 | Scope, current role or session-only route denies the operation. |
| 404 | Resource absent, or identifier not visible to the caller. |
| 409 | Conflict with existing state; reread before changing it. |
| 422 | Validation failed; inspect error.fields. |
| 500 / 502 / 503 | Internal error, node operation failure or offline node; inspect the error code and state. |

## Endpoint and scope catalog

Each path below is relative to your panel origin and includes `/api/v1`. The catalog and [downloadable OpenAPI 3.0.3 specification](https://tgproxypanel.com/api/openapi.json) come from the backend’s explicit personal-token route policy. Unlisted routes are unavailable to personal tokens. Owner-only restrictions apply in addition to scopes.

See the [generated endpoint catalog](https://tgproxypanel.com/en/api/#routes).
