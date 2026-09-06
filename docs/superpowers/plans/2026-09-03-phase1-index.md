# Phase 1 (MVP) — plan index

Spec: `docs/superpowers/specs/2026-09-03-webproxy-panel-design.md`

Execute the parts in order; each task inside a part depends on the previous ones.

| Part | File | Tasks | Delivers |
|---|---|---|---|
| A | `2026-09-03-phase1-a-core.md` | 1–5 | module, config, crypto, Postgres schema + sqlc, domain/links/QR, auth + sessions + CSRF + RBAC + audit |
| B | `2026-09-03-phase1-b-nodes-agent.md` | 6–9 | protobuf contract, gRPC gateway, NodeDriver (mock + gateway), node agent, node CRUD + install flow |
| C | `2026-09-03-phase1-c-keys-workers-site.md` | 10–14 | keys service/API, apply/expiry/stats workers, site validator + preset, branding, dashboard + /metrics |
| D | `2026-09-03-phase1-d-frontend-deploy.md` | 15–19 | React SPA, Docker Compose + Caddy + fakenode, e2e smoke, README/runbook |

Phase 2 (monitoring charts, RBAC polish, audit UI, more presets + uniquification, alerts via Telegram, node prerequisite checks) and Phase 3 (TOTP, subscription page, white-label profiles, Grafana, scheduled backups, master-key rotation) get their own plans after Phase 1 passes `deploy/e2e-smoke.sh`.

Verification gate for the whole phase: `go test ./... -race`, `golangci-lint run ./...`, `cd web && npm run typecheck && npm run lint && npm run test`, `make e2e` green.
