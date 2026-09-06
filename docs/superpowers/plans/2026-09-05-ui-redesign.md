# UI Redesign Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Re-skin the whole SPA to the approved visual language v2 (near-black neutral surfaces, hairlines, neutral primary button, monospace technical values, grouped sidebar, ⌘K palette, split login) without changing API, routes or behaviour.

**Architecture:** Tokens live in `web/src/index.css` (Tailwind v4 `@theme` + CSS variables, dark default, light mirror); shadcn components re-styled in place; shell and pages updated page by page. One small backend addition: public status endpoint for the login panel. Fonts bundled via `@fontsource-variable`.

**Tech Stack:** unchanged + `@fontsource-variable/inter`, `@fontsource-variable/jetbrains-mono`, `cmdk` (via shadcn `command`).

**Spec:** `docs/superpowers/specs/2026-09-05-ui-redesign.md` (binding). Reference mockups: scratchpad `mock/login-a.html`, `mock/dash.html` (copied to `docs/design/mock-login-a.html`, `docs/design/mock-dash.html` in Task 34).

## Global Constraints
- No git. Each task ends with `npm run typecheck && npm run lint && npm run test -- --run && npm run i18n:check && npm run build`; Go suite when Go changes.
- No behaviour/API changes except the new `GET /api/v1/status/public`.
- i18n parity; buttons are verbs; no marketing copy.
- Load `frontend-design:frontend-design` before UI work; `dataviz` before charts.
- Every task ends with Playwright screenshots (1440×900 dark + light, 390px mobile) of the pages it touched, saved under `.superpowers/sdd/redesign/shots/<task>/`, using the scratchpad venv `pwenv` and the seed script from `.superpowers/sdd/redesign/seed.sh` (mock-mode panel on :8099).

---

### Task 34: Design system foundation, shell, ⌘K, public status endpoint
- Files: `web/src/index.css` (tokens per spec, light mirror, `.mono`, `.tabular`, base 13px), `web/src/components/ui/{button,input,card,table,badge,dialog,sheet,select,tabs,dropdown-menu,tooltip}.tsx` (restyle), `web/src/components/shell/{Sidebar,Topbar,AppShell,nav.ts}` (groups, counts, footer, breadcrumb, ⌘K trigger), `web/src/components/shell/CommandPalette.tsx` (cmdk: navigation, nodes/keys search via existing hooks, actions), `web/src/components/common/{StatCard,StatusBadge,EmptyState,PageHeader,DataTable}.tsx` (restyle), `web/src/theme/ThemeProvider.tsx` (brand vars mapped to new tokens), fonts import in `main.tsx`; backend `internal/api/status.go` `GET /api/v1/status/public` → `{version, nodes_total, nodes_online, relay_commit}` (public, cached 10s, no hostnames) + test + RBAC walk public-read set; `docs/design/` mock copies.
- Tests: vitest for CommandPalette (renders sections; navigation item triggers navigate), Go test for status endpoint (anonymous 200, shape, no hostnames).
- Screenshots: dashboard (old content in new shell) dark/light/mobile.

### Task 35: Login (variant A) and 2FA step
- Files: `web/src/pages/LoginPage.tsx` (+ `LoginStatusPanel.tsx`), i18n keys `login.*`.
- Left panel: grid pattern with radial mask, wordmark from branding (logo if uploaded else diamond mark + name), status card from public status (version, nodes online/total, relay commit, api ok), footer. Right: form per spec; 2FA step in the same column; error states inline.
- Below 900px: single column (right form only, status line under it).
- Tests: existing login tests keep passing; vitest snapshot-free assertions for status panel rendering with/without data.
- Screenshots: login dark/light/mobile, 2FA step.

### Task 36: Dashboard and Monitoring
- Restyle tiles (no colored borders), chart palette (brand primary + accent + dim for offline), nodes table on dashboard (name, host mono, relay, profiles, sessions, heartbeat, open), alerts panel, recent applies; Monitoring page cards and Prometheus block in the new language.
- Screenshots.

### Task 37: Nodes and Keys
- Nodes list/detail (tabs, health grid mono, logs viewer dark hairline, site tab, check card), Create node + install command dialogs; Keys table (filters row, bulk bar, mono secrets/hosts, status dots), Create key dialog, link/QR dialog (QR on neutral surface, links in mono inputs), detail drawer, subscription section.
- Screenshots.

### Task 38: Sites, Audit, Settings, polish
- Site templates library cards, editor (line numbers mono, validation panel), assign dialog; Audit table; Settings tabs (branding list/form with live preview mapped to new tokens, security/2FA dialogs, admins, panel, backups); toasts; confirm dialogs; light theme pass; mobile pass; remove unused old styles; final full screenshot set.
