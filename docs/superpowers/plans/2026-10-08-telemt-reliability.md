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

- [x] Reproduce multiple DC warning/recovery oscillations with deterministic clock tests.
- [x] Aggregate reliability notifications per server, persist delivery/debounce state, wait 5 minutes for a problem and 10 minutes for stable recovery (optional user preference can revise these defaults).
- [x] Keep detailed incidents in the panel; no false recovery on missing data; paired recoveries only after successful problem delivery; serialize sends and retain retry state on failure. Offline handling remains timely and distinct.
- [x] Explain DC writer loss as degraded Middle Proxy connectivity, not proof that all users cannot use Telegram.
- [x] Test simultaneous DC changes, recovery flapping, restart, send failure, stale data and independent nodes. Run worker + alerttext integration tests against an isolated test DB.

### 2. Engine pin and real-engine checks

Files: `.env.example`, `internal/config`, release pins in `deploy`, `.github/workflows`, focused upgrade documentation.

- [x] Verify official 3.5.14 gnu/musl assets and matching hashes; inspect compatibility and conntrack workaround behavior.
- [x] Update all operational pins consistently, retaining arbitrary-version tests as fixtures.
- [x] Add a runnable CI job for the existing real telemt E2E suite (manual/scheduled network-dependent checks are acceptable).
- [x] Extend meaningful real-engine checks for apply/agent reconnect/quota changes where current harness supports them. Verify scripts and execute real E2E when runtime/network permit; report exact limits.

### 3. Built-in authenticated external probe

Files: `cmd/probe`, new focused protocol probe package, `deploy/probe`, dedicated probe documentation.

- [x] Research pinned upstream Fake-TLS and WEB protocol definitions/source.
- [x] Provide a bundled checker for configured test secrets, reading a private config file, with timeouts and no secrets in logs/argv.
- [x] Keep old custom checker behavior and `not_run` for unconfigured transports.
- [x] Test valid protocol reply, wrong key, TLS-only impostor, timeout and malformed response; run an end-to-end real-engine check when available.

### 4. Reason-aware recovery

Files: `internal/agent/recovery.go`, existing/new recovery tests.

- [x] Test unavailable upstream readiness versus dead control API.
- [x] Classify valid readiness reasons: network/upstream unavailability must not trigger restart; keep healthy reserve route failover and existing maintenance/rate gates.
- [x] Verify behavior for unknown reasons, absent API, operator pause and stopped units.

### 5. Bounded apply retries

Files: `internal/worker/apply.go`, new scheduler helper/tests.

- [x] Test global concurrency, node deduplication, retry suppression and successful reset of backoff.
- [x] Bound background applies to 4 active nodes; reserve slots before spawning, increasing retry delay with jitter from 45 seconds to 15 minutes. Keep direct/manual ApplyNode behavior compatible.
- [x] Ensure deferred dirty nodes remain eligible later, offline nodes do not exhaust slots, and Stop waits safely.

### 6. Integration and delivery

- [x] Run complete Go suite with race checks and an isolated PostgreSQL instance, frontend checks for changed translations, and applicable E2E.
- [x] Review full branch independently, address important findings, update operator documentation and record commits.
- [x] Deliver local branch/worktree and exact validation results; explain any live rollout or external-network verification still outstanding.

## Implementation notes and verification

- Worktree includes the previously reviewed quota reset checkpoint fix. Successful resets are
  saved before subsequent user updates can fail, preventing routine retries from zeroing the
  same quota period again.
- Real telemt 3.5.14 smoke passed: quota edits/reset/reapply, fresh apply jobs, restart-free
  key changes/revocation, panel reconnect and engine state after restart. Both release archive
  hashes were verified against official release metadata and downloaded bytes.
- Authenticated FakeTLS and all four WEB carriers passed against the real engine; wrong secrets
  failed. The probe checks one configured Telegram DC and stops after nonce-matched `resPQ`.
- Frontend build/typecheck, translation parity (2450 keys per language), and 378 tests passed.
  Frontend lint returned no errors and 18 warnings; Go lint returned zero issues.
- Independent review found and verified fixes for default-port WSS dialing, relapse after
  partial-channel recovery, stale queued recovery and legacy tproxy notification compatibility.
  No unresolved review findings remain.
- No production deployment, engine update, push or real Telegram notification was performed.
  The disposable Docker bench was removed after verification. External probe credentials and
  production network validation remain operator setup after deployment.
- Persistent acknowledgements prevent ordinary restart/concurrency duplicates. A crash after
  Telegram accepts a message but before the database acknowledgement commits can still repeat
  that message, since Telegram provides no delivery idempotency key. Changing destinations
  during an active incident retains the existing channel-based pairing semantics.

Final integration gate on 2026-10-08: `go test -p 1 -race ./... -count=1` passed against
fresh isolated PostgreSQL 16 database `tgwp_final_test`. Serialization avoids collisions
between packages whose fixtures truncate their test database. Final worker/alerttext
regressions also passed separately after review fixes. Shell syntax and shellcheck passed.
The source changes are retained locally on `improve/telemt-reliability`; no remote push or
production rollout is included.
