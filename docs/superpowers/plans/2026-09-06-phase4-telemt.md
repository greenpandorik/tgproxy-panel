# Phase 4 — telemt node engine Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add telemt as the default node engine: nodes run one telemt process (WEB mode behind Caddy + Fake-TLS listener), the agent applies changes through telemt's Control API without restarts, keys get WEB and Fake-TLS links plus per-key limits and traffic statistics. The existing tproxy engine stays selectable.

**Architecture:** `nodes.engine` selects the agent code path. New `internal/telemt` API client used by the agent; `internal/nodeinstall` gets a telemt script variant; the driver/worker layer stays engine-agnostic (desired state gains limits). Frontend adds engine choice, link kinds, limit fields and per-key stats.

**Tech Stack:** unchanged. telemt pinned `TELEMT_VERSION=3.5.5` (release asset `telemt-x86_64-linux-gnu.tar.gz` + `.sha256`).

**Spec:** `docs/superpowers/specs/2026-09-06-telemt-engine.md` (binding). Research: `docs/research/2026-09-06-telemt-and-meko.md`. telemt docs are in the scratchpad clone `telemt/docs` (Config_params, Architecture/API, WEB) — implementers must read the relevant sections.

## Global Constraints
- No git. Each task ends with: `go build ./... && TEST_DATABASE_URL=… go test -p 1 ./... -race && gofumpt -l ./cmd ./internal ./deploy && golangci-lint run ./...`; web tasks add the web checks.
- The tproxy engine must keep working unchanged: every existing test stays green; e2e (`make e2e`) still passes.
- Secrets (key secrets, telemt API token) never logged; API token stored only on the node (`/etc/telemt/api.token`, agent env), never in the panel DB.
- Engine-specific behaviour lives behind `engine` switches in the agent and in `nodeinstall`; the panel API and workers stay engine-agnostic except where the spec says otherwise (links, limits, stats).
- i18n parity; buttons are verbs.

---

### Task 39: Schema, domain, API for engine, limits and link kinds
- Migration `00005_telemt.sql`: `node_engine` enum (`tproxy`,`telemt`), `nodes.engine` default `telemt` with existing rows updated to `tproxy`, `nodes.tls_domain text`, `nodes.classic_port int default 8443`, `nodes.telemt_version text default ''`, `access_keys.telemt_limits jsonb default '{}'`, table `key_stats_snapshots` per spec §2 + index `(access_key_id, taken_at desc)`.
- Domain: `domain.Engine`, `domain.TelemtLimits{DataQuotaBytes, RateLimitUpBps, RateLimitDownBps, MaxUniqueIPs, MaxTCPConns}` with `Validate()` (non-negative, quota ≤ 100 TB); `qrlink.TMeProxy(host, port, secret)`, `qrlink.TgProxy`, `qrlink.FakeTLSSecret(hex, domain) = "ee"+hex+hex(domain)`.
- Node API: create accepts `engine` (default telemt), `tls_domain` (default hostname), `classic_port` (1024–65535); telemt nodes require `public_ip` at registration (the register endpoint accepts `public_ip` from the script and stores it); node JSON exposes `engine, tls_domain, classic_port, telemt_version`; PATCH allows `tls_domain`, `classic_port` (marks dirty; for telemt these need a restart — the agent handles it).
- Keys API: create/patch accept `telemt_limits`; `GET /keys/{id}` returns `telemt_limits`; links become `{node_id,node_name,hostname,engine,links:[{kind,tme,tg}]}`; `GET /keys/{id}/qr?node=&kind=web|tls`; subscription page/json include both kinds.
- Tests: migration/queries; node create with engine; links for a telemt node contain `kind:"tls"` with an `ee…` secret and port; tproxy node only `web`; limits validation 422.

### Task 40: telemt API client and agent engine path
- `internal/telemt/client.go` (+ `types.go`, `client_test.go` with an httptest fake implementing the endpoints the agent uses; error envelope mapping; `If-Match` unused).
- Agent: `Config.Engine` from `TGWP_ENGINE`; `TGWP_TELEMT_API`, `TGWP_TELEMT_API_TOKEN_FILE`, `TGWP_TELEMT_CONFIG` (`/etc/telemt/telemt.toml`), `TGWP_TELEMT_SITE_DIR` (`/var/lib/telemt/public`).
- `init-node --engine telemt …` renders `telemt.toml` (spec §5, `text/template`, validated by starting nothing — pure render), writes the API token file and returns it for `agent.env`, writes the site directory, installs systemd unit `telemt.service` (`ExecStart=/usr/local/bin/telemt /etc/telemt/telemt.toml`, `AmbientCapabilities=CAP_NET_ADMIN` for synlimit).
- `Apply` telemt path per spec §3 (reconcile users → patch web profiles → site dir + reload → ready); idempotent (no API writes when nothing differs); result log lists created/updated/deleted users; `RestartedRelay=false`.
- Health/Stats/Metrics/Logs for telemt; `TProxyVersion` field carries `telemt <version>` from `/v1/system/info`.
- Tests: apply against the fake telemt server (create/patch/delete, profile patch payload shape, reload only when the site changed, error → no partial state beyond what telemt already accepted, reported in log).

### Task 41: Install script for telemt nodes
- `nodeinstall`: `Params.Engine`, `TelemtVersion`, `TelemtSHA256` (panel config `TELEMT_VERSION`, `TELEMT_SHA256_X86_64` from `.env`; render refuses empty sha), `TLSDomain`, `ClassicPort`.
- Script branch for telemt: apt deps + nftables, Caddy (same pinned download as tproxy branch) with Caddyfile `hostname { reverse_proxy 127.0.0.1:18080 { header_up X-Forwarded-For {remote_host} } }` and global `servers { trusted_proxies static private_ranges }`, telemt tarball from `https://github.com/telemt/telemt/releases/download/<ver>/telemt-x86_64-linux-gnu.tar.gz` verified against the sha, `/usr/local/bin/telemt`, firewall: allow 80/443/classic_port, drop 9090/9091/18080 from outside; `tgwp-agent init-node --engine telemt --public-ip "$(detected)"` where detection = `curl -4s https://api.ipify.org || ip route get 1.1.1.1`; register with `public_ip`; start telemt, wait `/v1/health/ready`, start agent.
- Tests: render tests for both branches (essentials, sq quoting, sha present), `bash -n` on the rendered script.
- Panel config: `TELEMT_VERSION=3.5.5`, `TELEMT_SHA256_X86_64=<from release>` (fetch the real value from the release asset `.sha256` and put it in `.env.example`; document).

### Task 42: Workers and stats for telemt nodes
- `DesiredState` includes `TelemtLimits` + `ExpiresAt` per profile (proto: add fields to `Profile` message: `data_quota_bytes, rate_limit_up_bps, rate_limit_down_bps, max_unique_ips, max_tcp_conns, expires_at_unix, enabled`); regenerate proto; nodedriver conversions; mock.
- Apply job kinds unchanged; for telemt nodes the job log states "no restart".
- Stats worker: for telemt nodes parse `telemt_*` metrics into the snapshot columns (connections → sessions_live, octets → bytes) and write `key_stats_snapshots` from `ConnectionsSummary` per user (map username → key via profiles); retention 30d; `GET /keys/{id}/stats?from&to` → per node series + totals; keys list gains `traffic_30d`.
- Tests: worker with the mock driver returning a telemt-style summary; API stats endpoint.

### Task 43: Frontend
- Create node dialog: engine radio (telemt default, tproxy), telemt fields; nodes list/detail show engine tag, Fake-TLS domain/port, telemt version; node detail "Профили" shows limits summary.
- Keys: create/edit limits (quota GB, up/down Mbit/s, max IPs, max connections) with unit conversion; link dialog with tabs WEB / Fake-TLS per node (QR each, copy, download), tproxy nodes show WEB only with a note; drawer stats block (traffic, connections per node, 30d); keys table "Трафик" column; subscription page shows both links.
- i18n parity; vitest for link-kind rendering and unit conversion helpers.

### Task 44: fakenode-telemt, e2e, docs
- `deploy/Dockerfile.fakenode-telemt`: downloads the pinned telemt musl tarball (sha verified), stub systemctl/journalctl shims reused, entrypoint runs `init-node --engine telemt`, registers, starts telemt + agent; compose service `fakenode-telemt` under profile `dev-telemt`; `deploy/e2e-smoke-telemt.sh` + `make e2e-telemt`: create telemt node (engine telemt, public_ip from container), wait online, create personal key with limits, wait active without any restart in the job log, `GET /keys/{id}/links` has both kinds, on the node `curl -H "Authorization: <token>" 127.0.0.1:9091/v1/users` lists the user, `GET /keys/{id}/stats` 200; `SMOKE OK`.
- Docs: setup guides (RU/EN) section "Движок ноды: telemt или tproxy", README, runbook (telemt API token location, reload semantics, version pinning/upgrade), research link.
- Both `make e2e` and `make e2e-telemt` green.

### Task 45: Final review and fix wave
- Whole-phase review (security of the API token path, reconcile correctness, link formats vs telemt link printing, RBAC walk, docs), one fix wave, re-review.
