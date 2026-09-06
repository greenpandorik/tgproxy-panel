# Phase 1 / Part D — Frontend SPA and deployment Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking. Frontend tasks (15–18) MUST also load the `frontend-design:frontend-design` skill before writing UI code.

**Goal:** Ship the admin SPA (login, dashboard, nodes, keys, site templates, settings) embedded in the panel binary, plus Docker Compose deployment with Caddy, Postgres and a `fakenode` running the real relay for end-to-end apply testing.

**Architecture:** Vite + React 18 + TypeScript strict, Tailwind v4 + shadcn/ui, react-query for data, react-router, i18next (ru default, en), theme + branding injected as CSS variables from `GET /api/v1/branding`. Built output lands in `web/dist` and is embedded by `web/embed.go`. Compose: `caddy` (TLS) → `panel`; `postgres`; `fakenode` (tproxy-server at pinned commit + agent + TCP stub for MTProxy + systemctl shim).

**Tech Stack:** Node 20+, Vite 6, React 18, TS 5, Tailwind 4, shadcn/ui, @tanstack/react-query, @tanstack/react-table, react-hook-form + zod, lucide-react, recharts, i18next + react-i18next, vitest, eslint + prettier. Docker multi-stage, Caddy 2.

**Spec:** `docs/superpowers/specs/2026-09-03-webproxy-panel-design.md` (§3.3, §3.11, §3.12, §6)

## Global Constraints

Same as Parts A–C. Additionally:
- All user-visible strings live in `web/src/i18n/{ru,en}.json`; no hard-coded copy in components.
- Copy tone: short, factual. Buttons are verbs ("Создать ключ", "Применить", "Отозвать"). No marketing adjectives.
- Dark theme is the default; light theme via toggle persisted in `localStorage`.
- Brand colors come from CSS variables `--brand-primary`, `--brand-accent` set at runtime; Tailwind tokens reference them.
- Breakpoints to verify by hand: 360px, 768px, 1280px. Sidebar collapses to a drawer below 1024px; tables switch to card lists below 768px.
- API calls go through one client (`web/src/lib/api.ts`) that sends `X-CSRF-Token` from the `tgwp_csrf` cookie and redirects to `/login` on 401.
- `npm run build` must produce `web/dist`; `npm run typecheck` (`tsc --noEmit`) and `npm run lint` must pass.

---

## File structure (Part D)

```
web/package.json, vite.config.ts, tsconfig.json, tailwind (via @tailwindcss/vite), components.json, eslint.config.js, .prettierrc, index.html
web/src/main.tsx, App.tsx, routes.tsx
web/src/lib/api.ts              # fetch wrapper, csrf, error type
web/src/lib/query.ts            # QueryClient
web/src/lib/format.ts           # bytes, dates, relative time
web/src/lib/utils.ts            # cn()
web/src/i18n/index.ts, ru.json, en.json
web/src/theme/ThemeProvider.tsx # dark/light + branding vars
web/src/auth/AuthProvider.tsx   # me(), login, logout, role helpers
web/src/components/ui/*         # shadcn generated
web/src/components/shell/AppShell.tsx, Sidebar.tsx, Topbar.tsx
web/src/components/common/{StatCard,StatusBadge,CopyButton,EmptyState,ConfirmDialog,DataTable,PageHeader,ClientSupportNotice}.tsx
web/src/pages/LoginPage.tsx
web/src/pages/DashboardPage.tsx
web/src/pages/nodes/{NodesPage,NodeDetailPage,CreateNodeDialog,InstallCommandDialog,NodeLogs,NodeProfilesTab,NodeSiteTab}.tsx
web/src/pages/keys/{KeysPage,CreateKeyDialog,KeyLinkDialog,KeyDetailDrawer}.tsx
web/src/pages/sites/{SiteTemplatesPage,TemplateEditorPage}.tsx
web/src/pages/settings/{SettingsPage,BrandingForm,SecurityForm,AdminsForm}.tsx
web/src/api/{auth,nodes,keys,sites,branding,dashboard,settings}.ts  # typed endpoints + react-query hooks
web/src/api/types.ts
deploy/Dockerfile.panel
deploy/Dockerfile.fakenode
deploy/fakenode/{entrypoint.sh,systemctl,stubproxy.go,config.json,Caddyfile.unused}
deploy/docker-compose.yml, docker-compose.override.example.yml
deploy/Caddyfile
deploy/e2e-smoke.sh
README.md, docs/runbook.md
```

---

### Task 15: Frontend scaffold, shell, theme, i18n, auth, login

**Files:** everything under `web/` listed above except pages other than `LoginPage` and `DashboardPage` placeholder.

**Interfaces:**
- `api.request<T>(method, path, body?, opts?)` throws `ApiError {status, code, message, fields}`; `api.get/post/patch/put/del` helpers.
- `useAuth()` → `{user, loading, login(username,password), logout(), isWriter, isOwner}`.
- `useBranding()` → branding object; `ThemeProvider` applies `data-theme` on `<html>` and sets `--brand-primary/--brand-accent`.
- Route table: `/login`, `/` (dashboard), `/nodes`, `/nodes/:id`, `/keys`, `/sites`, `/sites/:id`, `/settings`. Protected routes redirect to `/login`.

- [ ] **Step 1: Scaffold**

```bash
cd web
npm create vite@latest . -- --template react-ts
npm i react-router-dom @tanstack/react-query @tanstack/react-table react-hook-form @hookform/resolvers zod lucide-react recharts i18next react-i18next clsx tailwind-merge
npm i -D tailwindcss @tailwindcss/vite vitest @testing-library/react @testing-library/jest-dom jsdom prettier eslint-config-prettier
npx shadcn@latest init -d
npx shadcn@latest add button input label card dialog dropdown-menu table badge tabs select switch textarea tooltip sheet separator skeleton toast alert scroll-area checkbox popover command
```
Add scripts to `package.json`: `"typecheck": "tsc --noEmit"`, `"lint": "eslint ."`, `"test": "vitest run"`, `"format": "prettier --write src"`. Set `vite.config.ts` `server.proxy['/api'] = 'http://localhost:8080'` and `build.outDir = 'dist'`.

- [ ] **Step 2: API client**

`web/src/lib/api.ts`:
```ts
export class ApiError extends Error {
  constructor(public status: number, public code: string, message: string, public fields: Record<string, string> = {}) {
    super(message);
  }
}

function csrf(): string {
  const m = document.cookie.match(/(?:^|; )tgwp_csrf=([^;]*)/);
  return m ? decodeURIComponent(m[1]) : '';
}

export async function request<T>(method: string, path: string, body?: unknown, init: RequestInit = {}): Promise<T> {
  const headers: Record<string, string> = { ...(init.headers as Record<string, string>) };
  if (body !== undefined && !(body instanceof FormData)) headers['Content-Type'] = 'application/json';
  if (method !== 'GET') headers['X-CSRF-Token'] = csrf();
  const res = await fetch(path, { method, headers, body: body instanceof FormData ? body : body === undefined ? undefined : JSON.stringify(body), credentials: 'same-origin', ...init });
  if (res.status === 401 && !path.endsWith('/auth/login')) {
    window.dispatchEvent(new Event('tgwp:unauthorized'));
  }
  if (res.status === 204) return undefined as T;
  const ct = res.headers.get('content-type') ?? '';
  const data = ct.includes('application/json') ? await res.json() : await res.text();
  if (!res.ok) {
    const e = typeof data === 'object' && data && 'error' in data ? (data as { error: { code: string; message: string; fields?: Record<string, string> } }).error : { code: 'http_' + res.status, message: String(data) };
    throw new ApiError(res.status, e.code, e.message, e.fields ?? {});
  }
  return data as T;
}

export const api = {
  get: <T>(p: string) => request<T>('GET', p),
  post: <T>(p: string, b?: unknown) => request<T>('POST', p, b),
  put: <T>(p: string, b?: unknown) => request<T>('PUT', p, b),
  patch: <T>(p: string, b?: unknown) => request<T>('PATCH', p, b),
  del: <T>(p: string) => request<T>('DELETE', p),
};
```

Write a vitest for `ApiError` mapping (`web/src/lib/api.test.ts`) mocking `fetch` to return a 422 envelope and asserting `fields`.

- [ ] **Step 3: Types and endpoint hooks**

`web/src/api/types.ts`: mirror the JSON shapes from Parts B/C: `Node`, `Health`, `Profile`, `AccessKey`, `KeyNode`, `Link`, `SiteTemplate`, `NodeSite`, `Branding`, `BrandingProfile`, `DashboardSummary`, `ApplyJob`, `AuditEntry`, `Settings`, `Paginated<T> = {items: T[]; total: number}`.

`web/src/api/nodes.ts` (pattern for all): 
```ts
export const nodeKeys = { all: ['nodes'] as const, one: (id: string) => ['nodes', id] as const };
export const useNodes = () => useQuery({ queryKey: nodeKeys.all, queryFn: () => api.get<Paginated<Node>>('/api/v1/nodes'), refetchInterval: 10000 });
export const useNode = (id: string) => useQuery({ queryKey: nodeKeys.one(id), queryFn: () => api.get<Node>(`/api/v1/nodes/${id}`), refetchInterval: 10000 });
export const useCreateNode = () => { const qc = useQueryClient(); return useMutation({ mutationFn: (b: CreateNodeInput) => api.post<{node: Node; install_command: string}>('/api/v1/nodes', b), onSuccess: () => qc.invalidateQueries({ queryKey: nodeKeys.all }) }); };
// plus usePatchNode, useDeleteNode, useNodeHealth, useNodeProfiles, useNodeStats, useNodeJobs, useNodeSite, useAssignSite, useRestartNode, useApplyNode, useInstallCommand
```
Same for `keys.ts` (`useKeys(filters)`, `useKey`, `useCreateKey`, `useBatchKeys`, `useRevokeKey`, `useRotateKey`, `useDeleteKey`, `useBulkKeys`, `useBindKey`, `useUnbindKey`, `usePatchKey`), `sites.ts`, `branding.ts` (`useBranding` public, `useBrandingProfiles`, `useUpdateBranding`, `useUploadBrandingAsset`, `useActivateBranding`), `dashboard.ts`, `settings.ts`, `auth.ts`.

- [ ] **Step 4: i18n**

`web/src/i18n/index.ts` initialises i18next with `ru` as `lng` unless `localStorage.lang` says otherwise, `fallbackLng: 'en'`. Both JSON files contain the same key set organised by namespace-like prefixes: `nav.*`, `auth.*`, `dashboard.*`, `nodes.*`, `keys.*`, `sites.*`, `settings.*`, `common.*` (copy, copied, save, cancel, delete, confirm, apply_now, pending, active, revoked, online, offline, degraded, error_generic). Write the Russian copy first, then English. Include the client-support notice: ru `keys.client_support = "Работает в Telegram Desktop. Android — экспериментально. iOS — в планах."`, en `"Works in Telegram Desktop. Android is experimental. iOS is planned."`

- [ ] **Step 5: Theme + branding + auth providers**

`ThemeProvider`: reads `theme` from `localStorage` else branding `theme_default`; sets `document.documentElement.dataset.theme`; sets `--brand-primary`, `--brand-accent` from branding; sets `document.title = branding.panel_name`; sets favicon `<link rel="icon">` href when `favicon_url` present; injects `custom_css` into a `<style id="brand-css">`. Tailwind theme (`src/index.css` with `@theme`): `--color-primary: var(--brand-primary)`, `--color-accent: var(--brand-accent)`, plus a neutral dark palette (`--color-bg`, `--color-surface`, `--color-border`, `--color-fg`, `--color-muted`) with a light override under `[data-theme="light"]`.

`AuthProvider`: on mount `GET /api/v1/auth/me`; listens for `tgwp:unauthorized` → clears user → router navigates to `/login`. `RequireAuth` route wrapper shows a skeleton while loading.

- [ ] **Step 6: Shell and login**

`AppShell`: left sidebar (icons + labels: Dashboard `LayoutDashboard`, Nodes `Server`, Keys `KeyRound`, Site Templates `LayoutTemplate`, Settings `Settings2`), collapsible to icons-only on desktop (state in localStorage), `Sheet` drawer on `<1024px` opened from a top bar hamburger. Top bar: page title slot, language switch (ru/en), theme toggle, user menu (username, role badge, logout). Footer text from branding.

`LoginPage`: centered card, logo (or panel name), form (username, password) with zod validation, error messages from `ApiError` (`invalid_credentials`, `rate_limited`, `locked` mapped to i18n keys), optional `login_text` and `login_bg_url` from branding.

`DashboardPage` placeholder for now: four `StatCard`s from `/dashboard/summary` (nodes online/total, active keys, sessions, streams) — completed in Task 18.

- [ ] **Step 7: Verify**

Run: `cd web && npm run typecheck && npm run lint && npm run test && npm run build`
Expected: all pass; `dist/index.html` exists. Then `cd .. && go build ./cmd/panel && NODE_DRIVER=mock go run ./cmd/panel serve` and open `http://localhost:8080` → login works with the admin from Part A, the shell renders, language and theme toggles persist across reloads. Check 360/768/1280 widths in devtools.

---

### Task 16: Nodes UI

**Files:** `web/src/pages/nodes/*`

- [ ] **Step 1: NodesPage**

`DataTable` (react-table) columns: status dot + name, hostname (with copy), version (short commit), profiles `n / max` with a thin capacity bar, last seen (relative), dirty badge ("есть неприменённые изменения"), actions menu (Apply now, Install command, Delete). Empty state with a "Add node" CTA. On `<768px` render cards. Auto-refresh every 10 s.

- [ ] **Step 2: CreateNodeDialog + InstallCommandDialog**

Form fields: name, hostname (lowercase, validated by the same regex as backend), ACME email, public IP (optional). On success open `InstallCommandDialog` showing the command in a monospace block with copy button, expiry time, and three short numbered steps (run on a fresh Ubuntu 22.04+/Debian 12+ x86_64 VPS as root; DNS A record must already point to the hostname; ports 80/443 open). The dialog is reachable later from the node page ("Показать команду установки" regenerates the token, with a note that the old one stops working).

- [ ] **Step 3: NodeDetailPage**

Header: name, hostname, status badge, online/offline, buttons: Apply now, Restart relay (confirm dialog warns that active sessions drop and clients reconnect), Delete.
Tabs:
- **Overview**: health grid (relay/mtproxy/caddy active, healthz/readyz, uptime, CPU/mem/disk bars), versions (tproxy commit with link to GitHub commit, agent version), last apply time, recent apply jobs list (status badge, kind, started/finished, expandable log).
- **Profiles** (`NodeProfilesTab`): table from `/nodes/{id}/profiles` (name, linked key label or "default", carrier mode, limits summary, sync state). "Compare with node" button fetches `?live=1` and shows a diff (present in DB only / on node only).
- **Logs** (`NodeLogs`): service checkboxes (tproxy-server, mtproxy, caddy), lines selector, Follow toggle; uses `EventSource` on `/nodes/{id}/logs?...`; monospace scrolling pane with pause and clear.
- **Site** (`NodeSiteTab`): current template name, bundle/deployed hash match indicator ("deployed" / "pending apply"), file list, preview `<iframe sandbox="" src=/api/v1/nodes/{id}/site/preview>`, select another template → assign (marks pending apply).
- **Stats**: key/value table from `/nodes/{id}/stats` (MTProxy) with refresh.

- [ ] **Step 4: Verify**

`npm run typecheck && npm run lint && npm run build`; run panel with `NODE_DRIVER=mock`, create a node, open it, confirm every tab renders with offline states (503 → "node offline" inline message, not a crash).

---

### Task 17: Keys UI

**Files:** `web/src/pages/keys/*`, `components/common/ClientSupportNotice.tsx`

- [ ] **Step 1: KeysPage**

Filters row: search (label/owner), type (all/shared/personal), status, node. Table columns: checkbox, label (+ owner label muted), type badge, status badge (pending shows a spinner-like dot and tooltip "будет применён при следующем батче"), nodes (chips with hostnames), expires (relative, red when < 3 days), created, actions (Show link/QR, Edit, Rotate for shared, Revoke, Delete). Bulk bar appears when rows selected: Revoke, Delete, Extend (date picker) → `/keys/bulk`. Pagination (per_page 50). Cards on mobile.

- [ ] **Step 2: CreateKeyDialog**

Tabs: Shared / Personal / Batch. Fields: label (or prefix + count for batch, max 100), owner label (personal), nodes multi-select (checkbox list with capacity `n/max`, disabled when full), carrier mode select with one-line descriptions, expiry (optional date-time), advanced section for limits (numeric inputs, only shown when expanded), note. zod schema mirrors backend validation. On success: for single keys open `KeyLinkDialog` immediately; for batch show a result list with per-key "Show link" and a "Download all links (.txt)" button that builds a Blob from the returned items.

- [ ] **Step 3: KeyLinkDialog**

For each bound node: hostname header, QR image (`/keys/{id}/qr?node=...&size=256`), `t.me` link in a read-only input with Copy, secondary `tg://` link with Copy, "Download QR" (fetch PNG → Blob → anchor). Sticky `ClientSupportNotice` (info alert with the client support text). When status is `pending`, show a subtle banner: "Ключ будет активен после применения на ноде (обычно до 1 минуты)". Revoked keys show no links.

- [ ] **Step 4: Revoke / rotate flows**

Revoke personal: confirm dialog "Отозвать ключ «X»? Доступ по нему прекратится после применения." Revoke shared: confirm with stronger warning "Это общий ключ. Все, кто им пользуется, потеряют доступ." Rotate shared: warning that the old link stops working and the new link must be redistributed; on success open `KeyLinkDialog`. Edit drawer (`KeyDetailDrawer`): label, owner, note, expiry (with clear), carrier mode, limits, node bindings add/remove.

- [ ] **Step 5: Verify**

Typecheck/lint/build; manual run in mock mode: create shared and personal keys, batch of 3, open link dialog, copy works (use `navigator.clipboard` with fallback to `execCommand('copy')`), QR renders, revoke and bulk revoke update the table, filters and pagination work. Check mobile layout at 360px.

---

### Task 18: Site templates, settings, dashboard

**Files:** `web/src/pages/sites/*`, `web/src/pages/settings/*`, `web/src/pages/DashboardPage.tsx`

- [ ] **Step 1: SiteTemplatesPage + TemplateEditorPage**

List: cards with name, preset badge, updated time, actions (Edit, Duplicate as mine, Delete for non-presets). Editor: name field, HTML `<textarea>` with monospace font and line numbers via CSS counter (no code-editor dependency in phase 1), assets panel (upload files → base64 into the `assets` map, list with size and remove), "Validate" button → shows errors (red list) and warnings (amber list) from `/site-templates/validate`, live preview iframe (`sandbox`, `srcdoc` built from html + inlined css assets), Save (disabled while validation has errors). "Assign to node…" dialog with node select.

- [ ] **Step 2: SettingsPage**

Tabs:
- **Branding** (`BrandingForm`): panel name, logo/favicon/login background uploads with preview, primary/accent color pickers (`<input type=color>` + hex text), theme default, login text, support link, footer text, custom CSS textarea. Live preview: the form applies values to the current page immediately via `ThemeProvider` override until saved or reset. Shows `css_removed` warnings after save.
- **Security** (`SecurityForm`): change password (current, new, confirm; note that other sessions will be signed out).
- **Admins** (`AdminsForm`, owner only): list with role badges, create (username, password, role), delete with confirm; cannot delete self.
- **Panel**: apply interval, offline threshold (owner only) → `PUT /settings`.

- [ ] **Step 3: DashboardPage**

Top row `StatCard`s: nodes online/total (with offline count in red when >0), active keys (+ pending count), live sessions, live streams. Second row: traffic today (bytes up/down summed from series for all nodes; compute delta of counters between first and last point per node) and open alerts list (node name, message, time, Resolve button). Third row: recharts `AreaChart` of sessions_live over last 24h per node (one series per node, brand-derived colors from the dataviz palette guidance: load `dataviz` skill before writing chart code), and recent apply jobs across nodes (last 10, from `/nodes` + `/nodes/{id}/jobs` limited to 3 each, sorted). Empty states when there are no nodes ("Добавьте первую ноду").

- [ ] **Step 4: Verify**

`npm run typecheck && npm run lint && npm run test && npm run build`. Manual pass in mock mode across all pages at 360/768/1280; switch to English and confirm no untranslated keys appear (search the DOM for `nav.`/`keys.` raw keys). Then `make web && go build ./cmd/panel` and confirm the embedded build serves from the Go binary with deep links (`/nodes/<id>` reload returns the SPA).

---

### Task 19: Docker Compose, fakenode, e2e smoke, docs

**Files:** `deploy/*`, `README.md`, `docs/runbook.md`, `Makefile` additions

- [ ] **Step 1: Panel image**

`deploy/Dockerfile.panel`:
```dockerfile
FROM node:22-alpine AS web
WORKDIR /src/web
COPY web/package*.json ./
RUN npm ci
COPY web/ ./
RUN npm run build

FROM golang:1.26-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
COPY --from=web /src/web/dist ./web/dist
RUN CGO_ENABLED=0 go build -trimpath -ldflags='-s -w' -o /out/panel ./cmd/panel \
 && CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -trimpath -ldflags='-s -w' -o /out/tgwp-agent-linux-amd64 ./cmd/agent \
 && sha256sum /out/tgwp-agent-linux-amd64 | awk '{print $1}' > /out/tgwp-agent-linux-amd64.sha256

FROM alpine:3.20
RUN apk add --no-cache ca-certificates postgresql16-client tzdata && adduser -D -u 10001 panel
WORKDIR /app
COPY --from=build /out/panel /app/panel
COPY --from=build /out/tgwp-agent-linux-amd64* /app/agent-dist/
COPY deploy/panel-entrypoint.sh /app/entrypoint.sh
USER panel
ENV DATA_DIR=/data
VOLUME ["/data"]
EXPOSE 8080
ENTRYPOINT ["/app/entrypoint.sh"]
```
`deploy/panel-entrypoint.sh`: `mkdir -p "$DATA_DIR/agent" && cp /app/agent-dist/* "$DATA_DIR/agent/" && exec /app/panel "$@"`.

- [ ] **Step 2: fakenode image**

`deploy/fakenode/stubproxy.go` — tiny Go program: listens TCP `127.0.0.1:2398` (accepts and holds connections) and HTTP `127.0.0.1:8888` serving `/stats` with a few `key\tvalue` lines.

`deploy/fakenode/systemctl` (bash shim, installed as `/usr/local/bin/systemctl`): supports `is-active <unit>`, `restart <unit>...`, `daemon-reload`, `enable`, `start`, `stop`. Units: `tproxy-server` → `/usr/local/bin/tproxy-server -config /etc/tproxy-server/config.json -profiles-file /etc/tproxy-server/profiles.json`, `mtproxy` → `/usr/local/bin/stubproxy`, `caddy` → no-op always active. Manages pids in `/run/fake/<unit>.pid`, logs to `/var/log/fake/<unit>.log`; `journalctl` shim tails those logs (`-u`, `-n`, `-f` supported).

`deploy/Dockerfile.fakenode`:
```dockerfile
FROM golang:1.26-bookworm AS build
ARG TPROXY_COMMIT=52a5feb7fac38f68da5afef9cedd9b3bfc8473ca
RUN git clone https://github.com/telegramdesktop/tproxy-server.git /tproxy && cd /tproxy && git checkout -q "$TPROXY_COMMIT" \
 && CGO_ENABLED=0 go build -o /out/tproxy-server ./cmd/tproxy-server
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -o /out/tgwp-agent ./cmd/agent && CGO_ENABLED=0 go build -o /out/stubproxy ./deploy/fakenode/stubproxy.go

FROM debian:bookworm-slim
RUN apt-get update && apt-get install -y --no-install-recommends ca-certificates curl procps && rm -rf /var/lib/apt/lists/*
COPY --from=build /out/tproxy-server /out/tgwp-agent /out/stubproxy /usr/local/bin/
COPY deploy/fakenode/systemctl deploy/fakenode/journalctl /usr/local/bin/
COPY deploy/fakenode/config.json /etc/tproxy-server/config.json
COPY deploy/fakenode/entrypoint.sh /entrypoint.sh
RUN chmod +x /usr/local/bin/systemctl /usr/local/bin/journalctl /entrypoint.sh && mkdir -p /srv/tproxy-site /etc/mtproxy /var/lib/tgwp-agent /run/fake /var/log/fake
ENTRYPOINT ["/entrypoint.sh"]
```
`deploy/fakenode/config.json`: `{"public_hostname":"fakenode.local","listen":"127.0.0.1:8080","admin_listen":"127.0.0.1:8081","public_dir":"/srv/tproxy-site","profiles_file":"/etc/tproxy-server/profiles.json","limits":{"max_profiles":128}}`.

`deploy/fakenode/entrypoint.sh`: waits for the panel (`curl -sf $PANEL_URL/healthz`), writes an initial `profiles.json` (secret from `$INIT_SECRET` or a random one) with mode 0400, `mtproxy.env`, a minimal `/srv/tproxy-site/index.html`, starts `mtproxy` and `tproxy-server` via the shim, then registers: `curl -fsS $PANEL_URL/api/v1/install/$INSTALL_TOKEN/register ...` → writes `/etc/tgwp-agent/agent.env`, and finally `exec tgwp-agent` with `TGWP_*` env pointing to the paths above. If `NODE_TOKEN` env is provided, skip registration.

- [ ] **Step 3: Compose and Caddy**

`deploy/docker-compose.yml`:
```yaml
services:
  postgres:
    image: postgres:16-alpine
    environment: { POSTGRES_USER: tgwp, POSTGRES_PASSWORD: ${POSTGRES_PASSWORD:-tgwp}, POSTGRES_DB: tgwp }
    volumes: [pgdata:/var/lib/postgresql/data]
    healthcheck: { test: ["CMD-SHELL", "pg_isready -U tgwp"], interval: 5s, retries: 10 }
  panel:
    build: { context: .., dockerfile: deploy/Dockerfile.panel }
    env_file: .env
    environment:
      DATABASE_URL: postgres://tgwp:${POSTGRES_PASSWORD:-tgwp}@postgres:5432/tgwp?sslmode=disable
    depends_on: { postgres: { condition: service_healthy } }
    volumes: [paneldata:/data]
    expose: ["8080"]
  caddy:
    image: caddy:2-alpine
    ports: ["80:80", "443:443"]
    environment: { PANEL_DOMAIN: ${PANEL_DOMAIN:-localhost}, ACME_EMAIL: ${ACME_EMAIL:-} }
    volumes: [./Caddyfile:/etc/caddy/Caddyfile:ro, caddydata:/data]
    depends_on: [panel]
  fakenode:
    build: { context: .., dockerfile: deploy/Dockerfile.fakenode }
    profiles: [dev]
    environment: { PANEL_URL: http://panel:8080, INSTALL_TOKEN: ${FAKENODE_INSTALL_TOKEN:-} }
    depends_on: [panel]
volumes: { pgdata: {}, paneldata: {}, caddydata: {} }
```
`deploy/Caddyfile`:
```
{$PANEL_DOMAIN} {
	encode zstd gzip
	reverse_proxy panel:8080 {
		transport http {
			versions h2c 1.1
		}
	}
}
```
(For `localhost`, Caddy issues an internal cert; document `--insecure` / trusting the local CA in the runbook.)

`deploy/docker-compose.override.example.yml`: maps `panel` port `8080:8080` for local use without Caddy.

- [ ] **Step 4: e2e smoke**

`deploy/e2e-smoke.sh` (bash, needs `curl`, `jq`):
1. Log in (`PANEL_URL`, `ADMIN_USER`, `ADMIN_PASS`), keep cookie jar and CSRF.
2. Create node `fakenode.local`; extract install token; print `FAKENODE_INSTALL_TOKEN=<token>` so the caller can `docker compose --profile dev up fakenode`.
3. Wait up to 60 s until node `online`.
4. Create a personal key bound to the node; assign preset `studio`; `POST /nodes/{id}/apply`.
5. Poll `/nodes/{id}/jobs` until the latest job is `ok` (fail after 90 s, printing the job log).
6. Assert key status `active`, `GET /keys/{id}/links` contains `https://t.me/webproxy?server=fakenode.local&secret=`, QR endpoint returns PNG, and `docker compose exec fakenode curl -s http://127.0.0.1:8081/readyz` prints `ready`.
7. Fetch `docker compose exec fakenode cat /etc/tproxy-server/profiles.json` and assert it contains the key's profile name.

Add `make e2e` that runs the script against `http://localhost:8080` (with the override file).

- [ ] **Step 5: README and runbook**

`README.md`: what it is (3 sentences), requirements, quick start (compose), first admin (`docker compose exec panel /app/panel admin create …`), adding a node (paste command), env reference (table from `.env.example`), development (Go + Node + brew Postgres, `make test`, `TEST_DATABASE_URL`), fakenode + `make e2e`, security notes (secrets encrypted, master key handling, never commit `.env`). Plain tone.

`docs/runbook.md`: upgrading the panel, rotating `MASTER_KEY` (Phase 3 command placeholder is not allowed — instead state that rotation ships in Phase 3 and describe the env layout it will use: `MASTER_KEY_VERSION=2`, `MASTER_KEY_V1=<old>`), backup/restore with `pg_dump`/`psql`, what to do when a node shows `rolled_back` (read job log, run `tproxy-server -check` on the node), when a node is offline (check `systemctl status tgwp-agent`, `journalctl -u tgwp-agent`), reinstalling the agent (regenerate install command), the relay restart caveat, and Prometheus scrape config for `/metrics`.

- [ ] **Step 6: Verify end to end**

```bash
cd deploy && cp ../.env.example .env && sed -i '' "s|^MASTER_KEY=.*|MASTER_KEY=$(openssl rand -base64 32)|; s|^SESSION_SECRET=.*|SESSION_SECRET=$(openssl rand -base64 32)|" .env
docker compose -f docker-compose.yml -f docker-compose.override.example.yml up -d --build postgres panel
docker compose exec panel /app/panel admin create root 'change-me-now-1'
PANEL_URL=http://localhost:8080 ADMIN_USER=root ADMIN_PASS=change-me-now-1 ../deploy/e2e-smoke.sh   # prints FAKENODE_INSTALL_TOKEN, then run:
FAKENODE_INSTALL_TOKEN=<token> docker compose --profile dev up -d --build fakenode
PANEL_URL=http://localhost:8080 ADMIN_USER=root ADMIN_PASS=change-me-now-1 ../deploy/e2e-smoke.sh --continue
```
Expected: the script ends with `SMOKE OK`. The real relay validated our `profiles.json` with `-check`, restarted through the shim, and serves the preset site: `docker compose exec fakenode curl -s -H 'Host: fakenode.local' http://127.0.0.1:8080/ | head` shows the preset HTML.

Also run the full verification set one last time: `go test ./... -race && golangci-lint run ./... && (cd web && npm run typecheck && npm run lint && npm run test)`.
