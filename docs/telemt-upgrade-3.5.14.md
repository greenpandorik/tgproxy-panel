# telemt 3.5.14 upgrade

The panel now pins [telemt 3.5.14](https://github.com/telemt/telemt/releases/tag/3.5.14),
tag commit `9d5b896bb695c55e2905b82f276da84e45a0a2ec`. This release includes
[3.5.12's autonomous Middle Proxy recovery from Direct fallback](https://github.com/telemt/telemt/pull/943)
and [3.5.14's pool convergence changes](https://github.com/telemt/telemt/pull/949).
These upstream changes address engine recovery; they do not guarantee availability
through an external network outage.

## Verified release pins

| Archive | SHA256 |
| --- | --- |
| `telemt-x86_64-linux-gnu.tar.gz` (production nodes) | `b10f86c6851f5c422a49d98deea55e8ef4aea7879605e59c1d4aa43c8f4c70ce` |
| `telemt-x86_64-linux-musl.tar.gz` (real-engine bench) | `71c1454f79cf755edea0985500629daba553002f9863be754e8e38a418822773` |

On 2026-10-08, both downloads were hashed locally and matched both the official
[GNU checksum](https://github.com/telemt/telemt/releases/download/3.5.14/telemt-x86_64-linux-gnu.tar.gz.sha256)
and [musl checksum](https://github.com/telemt/telemt/releases/download/3.5.14/telemt-x86_64-linux-musl.tar.gz.sha256),
and the archive digests in the
[GitHub release API](https://api.github.com/repos/telemt/telemt/releases/tags/3.5.14).
The `.sha256` file's own digest is different from the archive digest: installers
must verify the archive against the values above.
The verified GNU binary also reported `telemt 3.5.14` in a disposable Ubuntu 24.04
container; the full real-agent regression suite used the musl binary.

The pins are kept together in `.env.example`, the panel's default version, the
bench Dockerfile and its Compose defaults. An explicit `TELEMT_VERSION` still
requires its matching `TELEMT_SHA256_X86_64`; the panel fails closed without it.
Tests that exercise arbitrary versions and malformed checksums remain fixtures.

## Compatibility and conntrack

The pinned upstream source retains the routes used by the agent: health/readiness,
system info, config GET/PATCH, reload/status, users GET/POST/PATCH/DELETE,
`POST /v1/users/{name}/reset-quota`, statistics and upstream statistics.
The [readiness response](https://github.com/telemt/telemt/blob/3.5.14/src/api/handler/read_routes.rs)
still reports `admission_closed` or `no_healthy_upstreams` with HTTP 503;
a reachable but unready engine is different from an unreachable control API.
Quota patches retain JSON Merge Patch semantics, including `null` to clear a limit.

3.5.14's [firewall command classifier](https://github.com/telemt/telemt/blob/3.5.14/src/conntrack_control/firewall/command.rs)
recognizes the iptables-nft missing-chain diagnostic that caused startup trouble
with 3.5.9. It limits that exception to the engine's owned chains and cleanup/check
operations. The panel's existing `10-tgwp-conntrack.conf` systemd drop-in remains
installed: its best-effort empty-chain creation supports nodes still on 3.5.9 and
rollback. Removing it is unnecessary for the pin upgrade. The drop-in does not
append filtering rules or change listener configuration.

The unprivileged Docker bench disables the listener SYN limiter and TLS fingerprint
emulation because it has no netfilter capability and uses a synthetic domain. It
does not exercise production systemd drop-ins or production firewall permissions.

## Real-engine regression checks

Run `make e2e-telemt` from the repository with Docker Compose, curl, jq and openssl
available. The runner creates a disposable Compose project, downloads and verifies
the pinned musl binary, and removes its containers and volumes on exit. Its default
panel endpoint is `http://localhost:18080`, separate from the development panel on
8080. `E2E_PANEL_PORT` and `E2E_TLS_PORT` can change its local ports.

The smoke exercises the real engine and real agent, including:

- pinned version and upstream/DC telemetry;
- key creation, WEB profile and Fake-TLS link delivery;
- quota creation and quota edits with an unchanged telemt PID;
- one real reset-quota call after backdating the disposable agent's remembered
  quota period by one week, then no repeated reset on reapply;
- agent reconnect after a panel restart, proven by a fresh heartbeat;
- readiness and persisted user/quota state after a telemt process restart;
- backup TLS-domain apply and restart-free key revocation.

Each apply waits for a newly created job; a previous successful job cannot satisfy
the check. The quota-period simulation changes only bench state and does not wait
for a calendar boundary. This suite checks control-plane integration and persisted
state; it does not measure production throughput or quota exhaustion by real traffic.
Authenticated Telegram transport checks are documented with the bundled probe.
The complete suite passed locally on 2026-10-08 against the pinned 3.5.14 musl
engine, including Telegram upstream/DC telemetry and the reconnect/restart checks.

`.github/workflows/telemt-e2e.yml` runs this suite manually and weekly on Linux.
It intentionally runs outside the PR gate because release downloads and Telegram
connectivity depend on external services. A failure must be investigated as either
an integration regression or network failure; it is never reported as a pass.

For inspection, set `E2E_KEEP_STACK=1` and an explicit `COMPOSE_PROJECT_NAME`.
Remove only that disposable project afterward:

```sh
cd deploy
COMPOSE_PROJECT_NAME=<the-test-project> \
  docker compose -f docker-compose.yml -f docker-compose.e2e-telemt.yml \
  --profile dev-telemt down -v
```

## Operator rollout

Deploy the panel release through the normal update procedure so its environment
receives the new version and matching checksum together. Existing nodes continue
running their installed engine until an operator requests the telemt update; this
pin change does not restart a fleet automatically. Upgrade one node first through
the panel's serialized engine-update flow, check readiness, key apply, external
authenticated probe results and DC detail, then expand the rollout. Retain the
engine update journal and backup for rollback. Existing maintenance and pause
settings still apply.

No production node was upgraded as part of this source change.
