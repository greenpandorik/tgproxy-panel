# Telemt reliability and quiet notifications

> For agentic workers: implement the independent tasks with the parallel-agents workflow; use TDD for behavior changes and a final independent review.

**Goal:** improve real proxy availability and replace the user's repeated per-DC Telegram messages with useful, stable server notifications.

**Architecture:** retain the existing panel/agent/control-API architecture. Extend existing probes, bounded apply scheduling, recovery reason handling, and notification state rather than replacing these systems.

**Tech stack:** Go, PostgreSQL, telemt, existing TypeScript translations, shell/CI.

**Spec:** user's approval of the five recommendations in this conversation plus the supplied Germany 1 notification transcript. Prior quota fix is included in this integration branch.

## Constraints

- No live deployment, shared-branch push, real Telegram messages, or production credentials.
- Main checkout remains untouched. Worktree: `.worktrees/telemt-reliability`.
- Preserve existing per-DC detail in the panel; do not silently disable incident collection.
- Unknown/stale data must never be interpreted as recovery.
- Real protocol checks must prove an authenticated proxy exchange with Telegram, not merely HTTP/TLS acceptance. Retain custom client-check compatibility and protect probe secrets.
- Use verified upstream release checksums; target telemt 3.5.14 if compatibility checks pass.
- Existing per-server update serialization, operator maintenance/pause and stopped services must remain respected.

## Review focus

- DC flapping and rotating bad DCs must not create a stream of recovery/problem messages.
- Restarted panel and failed notification delivery must preserve pairing and retry without concurrent duplicates.
- External network outages must not cause futile telemt restarts.
- A disconnected fleet must not produce unbounded concurrent apply operations.
- Successful TLS/HTTP with invalid proxy credentials must fail the authenticated probe.

## Tasks

### 1. Quiet server notifications

Files: `internal/worker/alerts*.go`, `incidents*.go`, `stats.go` integration, new notification state helper/migration, `internal/alerttext`, relevant translation keys.

- [ ] Reproduce multiple DC warning/recovery oscillations with deterministic clock tests.
- [ ] Aggregate reliability notifications per server, persist delivery/debounce state, wait 5 minutes for a problem and 10 minutes for stable recovery (optional user preference can revise these defaults).
- [ ] Keep detailed incidents in the panel; no false recovery on missing data; paired recoveries only after successful problem delivery; serialize sends and retain retry state on failure. Offline handling remains timely and distinct.
- [ ] Explain DC writer loss as degraded Middle Proxy connectivity, not proof that all users cannot use Telegram.
- [ ] Test simultaneous DC changes, recovery flapping, restart, send failure, stale data and independent nodes. Run worker + alerttext integration tests against an isolated test DB.

### 2. Engine pin and real-engine checks

Files: `.env.example`, `internal/config`, release pins in `deploy`, `.github/workflows`, focused upgrade documentation.

- [ ] Verify official 3.5.14 gnu/musl assets and matching hashes; inspect compatibility and conntrack workaround behavior.
- [ ] Update all operational pins consistently, retaining arbitrary-version tests as fixtures.
- [ ] Add a runnable CI job for the existing real telemt E2E suite (manual/scheduled network-dependent checks are acceptable).
- [ ] Extend meaningful real-engine checks for apply/agent reconnect/quota changes where current harness supports them. Verify scripts and execute real E2E when runtime/network permit; report exact limits.

### 3. Built-in authenticated external probe

Files: `cmd/probe`, new focused protocol probe package, `deploy/probe`, dedicated probe documentation.

- [ ] Research pinned upstream Fake-TLS and WEB protocol definitions/source.
- [ ] Provide a bundled checker for configured test secrets, reading a private config file, with timeouts and no secrets in logs/argv.
- [ ] Keep old custom checker behavior and `not_run` for unconfigured transports.
- [ ] Test valid protocol reply, wrong key, TLS-only impostor, timeout and malformed response; run an end-to-end real-engine check when available.

### 4. Reason-aware recovery

Files: `internal/agent/recovery.go`, existing/new recovery tests.

- [ ] Test unavailable upstream readiness versus dead control API.
- [ ] Classify valid readiness reasons: network/upstream unavailability must not trigger restart; keep healthy reserve route failover and existing maintenance/rate gates.
- [ ] Verify behavior for unknown reasons, absent API, operator pause and stopped units.

### 5. Bounded apply retries

Files: `internal/worker/apply.go`, new scheduler helper/tests.

- [ ] Test global concurrency, node deduplication, retry suppression and successful reset of backoff.
- [ ] Bound background applies to 4 active nodes; reserve slots before spawning, increasing retry delay with jitter from 45 seconds to 15 minutes. Keep direct/manual ApplyNode behavior compatible.
- [ ] Ensure deferred dirty nodes remain eligible later, offline nodes do not exhaust slots, and Stop waits safely.

### 6. Integration and delivery

- [ ] Run complete Go suite with race checks and an isolated PostgreSQL instance, frontend checks for changed translations, and applicable E2E.
- [ ] Review full branch independently, address important findings, update operator documentation and record commits.
- [ ] Deliver local branch/worktree and exact validation results; explain any live rollout or external-network verification still outstanding.
