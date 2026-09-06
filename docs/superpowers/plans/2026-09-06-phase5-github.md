# Phase 5 — Topbar status chips, MEKO tuning, GitHub publication Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add Remnawave-style status chips to the topbar (panel version with update highlight, GitHub stars, nodes online), adopt the two MEKO items (sysctl tuning in the node installer; post-quantum key-exchange check in node readiness), publish the project to GitHub (`greenpandorik/tgproxy-panel`, public, AGPL-3.0) with a plain README, CI and a release workflow, and cut `v1.0.0`.

**Decisions (user, 2026-09-06):** repo `greenpandorik/tgproxy-panel`; public; AGPL-3.0; chips: version + update highlight, GitHub stars, nodes online. Git is now used for this project (previous "no git" decision is superseded); commits are made by the agent, pushes only when the plan says.

**Spec references:** UI language `docs/superpowers/specs/2026-09-05-ui-redesign.md`; telemt engine spec; research doc for the MEKO items.

## Global Constraints
- Checks as before (Go suite `-p 1 -race`, gofumpt, golangci-lint; web typecheck/lint/test/i18n/build); both e2e stay green.
- GitHub calls from the panel are unauthenticated, cached server-side (1h), fail soft (chips degrade to version-only); optional `GITHUB_TOKEN` env raises the rate limit; `GITHUB_REPO` env defaults to `greenpandorik/tgproxy-panel`; `UPDATE_CHECK=false` disables outbound calls.
- Version comes from `internal/version.Version` injected with `-ldflags "-X tgwebproxy/internal/version.Version=<tag>"`; dev builds show `dev`.
- README: plain language, no marketing adjectives, real screenshots from `docs/screenshots/`, credits telemt and MEKO with links.
- Never commit `.env`, `deploy/.env`, `data/`, `.superpowers/`, `web/dist`, `web/node_modules` (already in .gitignore); verify with `git status --ignored` before the first commit.

---

### Task 46: Update check + topbar chips + MEKO items
- Backend: `internal/updates` package: `Checker{Repo, Token, HTTP}` fetching `GET https://api.github.com/repos/<repo>/releases/latest` (tag_name, html_url, published_at) and `GET https://api.github.com/repos/<repo>` (stargazers_count), cached 1h with jitter, single-flight, fail-soft (returns last good + `stale:true`); semver compare (`v` prefix tolerant; `dev` never outdated). API `GET /api/v1/status/update` (auth, all roles) → `{current, latest, latest_url, published_at, stars, repo_url, update_available, checked_at, stale}`. Config: `GITHUB_REPO`, `GITHUB_TOKEN` (optional, never logged), `UPDATE_CHECK` (default true). Tests with httptest (rate limit 403 → stale; newer tag → update_available; `dev` → false).
- Frontend: topbar right group (before language): `VersionChip` (mono `v1.0.0`; when update available: brand-colored hairline + dot, tooltip "Доступна vX.Y.Z", click opens release page), `GitHubChip` (GitHub mark + star count formatted 6096 → "6.1k", link to repo), `NodesChip` (dot + `2/3`, link to /nodes, status color by offline count). Hooks `useUpdateStatus` (staleTime 1h). Mobile: chips collapse into the user menu. i18n. Vitest for the chips (update available styling, stars formatting, chip hidden when update check disabled).
- MEKO items: installer (both engine branches, after apt deps): write `/etc/sysctl.d/90-tgwp.conf` with `net.core.default_qdisc=fq`, `net.ipv4.tcp_congestion_control=bbr`, `net.core.somaxconn=65535`, `net.ipv4.tcp_max_syn_backlog=65535`, `net.core.netdev_max_backlog=65535`, `net.ipv4.tcp_fastopen=3`, `net.ipv4.tcp_keepalive_time=45`, `net.ipv4.tcp_keepalive_intvl=15`, `net.ipv4.tcp_keepalive_probes=3`, apply with `sysctl --system` (ignore failures for unavailable keys, log); render test asserts the file. Node check `pq_kex`: TLS handshake to `hostname:443` with `CurvePreferences` default and check `ConnectionState().CurveID == tls.X25519MLKEM768` (Go 1.24+); ok=true when negotiated, detail names the group; skipped when tcp_443 failed; i18n label "Постквантовый обмен ключами" / "Post-quantum key exchange"; test with httptest TLS server (Go's default server negotiates X25519MLKEM768 when the client offers it).
- Docs: README env table (new vars), runbook note on update checks and sysctl.

### Task 47: Repository bootstrap and publication
- Files: `LICENSE` (AGPL-3.0 full text, © 2026 greenpandorik), `README.md` rewrite (what it is, screenshots, features, quick start link to setup guides, node engines with credits: telemt (link, license note) and MEKO (link, what we adopted), architecture sketch, roadmap/unverified items, license), `CONTRIBUTING.md` (short: how to run tests, e2e, style), `.github/workflows/ci.yml` (Go 1.26 + Node 22, Postgres service for tests, golangci-lint, web checks; on push/PR), `.github/workflows/release.yml` (on tag `v*`: build panel + agent linux/amd64 (+arm64 panel) with ldflags version, upload assets with sha256, build and push Docker image to `ghcr.io/greenpandorik/tgproxy-panel:<tag>` and `:latest`), `deploy/docker-compose.yml` gains an `image:` alternative comment, `Makefile` `VERSION` injection for `make build`.
- Repo: `git init` (default branch `main`), verify ignored files, first commit "Initial import: TGProxy panel v1.0.0", `gh repo create greenpandorik/tgproxy-panel --public --source=. --remote=origin --description "Control panel for Telegram WEB proxy and telemt nodes" --push`; set topics (telegram, mtproxy, proxy, telemt, go, react); enable Issues; create tag `v1.0.0` + `gh release create v1.0.0` with notes (the release workflow builds assets); verify the update chip against the live API (`GET /status/update` returns stars ≥0 and latest v1.0.0).
- Screenshots of the new topbar added to docs/screenshots and the setup guides updated where the topbar is described.

### Task 48: Final review of Phase 5 and fix wave
- Whole-phase review (security of outbound calls, token handling, CI secrets, README accuracy vs code, license headers), fix wave, re-review; final commit + push.
