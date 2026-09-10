# vNext implementation and infrastructure acceptance

Documentation review: 2026-09-10. Scope: user-facing surfaces from
`TGProxy_Panel_vNext_TZ.md` and `TGProxy_Panel_New_Decoy_Websites_TZ.md`.
Implemented means the corresponding UI/backend path exists in this checkout.
This documentation review did not run infrastructure acceptance or certify every
P0/P1/P2 criterion. Fixtures cannot establish compatibility across real Telegram
clients, networks and deployed Telemt versions.

| Surface | Implemented | Requires real infrastructure acceptance |
|---|---|---|
| WEB Transport | Runtime/capability states, carrier distribution/counters, policy/lifecycle controls, overload presets and live capacity resources (`NodeWebTab.tsx`) | Supported/older versions, real carrier selection/fallback, sustained saturation and pause/drain/resume with active clients |
| Diagnostics | Public probes plus node/runtime readings, TLS emulation/cache/error checks, grouped results, stored history and JSON export (`internal/nodediag`, `nodes_diagnostics.go`, `WebDiagnosticsCard.tsx`) | Public DNS/TLS/HTTP and forwarding; actual client authentication, TLS-front bootstrap and carrier paths; unavailable checks must remain not-run |
| Monitoring | Task-oriented Overview, Problems, Nodes and WEB Transport views; the Problems view reuses the dashboard alert actions (`MonitoringPage.tsx`) | Alert generation and resolution under real node, certificate, DNS, capacity and carrier failures |
| Telemt updates | Per-node jobs/history, capability preflight, SHA256, drain, binary/config backup, restart/verification, admission recovery and rollback outcomes (`internal/agent/telemtupdate.go`, `internal/worker/telemtupdate.go`) | Real artifact delivery/systemd permissions, active sessions/reconnect, failed restart/rollback, interrupted agent/panel and admission recovery |
| 15 websites | Embedded HTML/manifests, gallery/filter/preview/customize/assign (`internal/sitekit/presets`, `web/src/pages/sites`) | Every design on mobile/desktop; deployed root/assets, cache/MIME, per-node output and WEB on the same hostname |
| Custom ZIP | Root index, optional enclosing directory, size/entry limits, unsafe/duplicate path and non-regular entry rejection (`internal/sitekit/import.go`) | Representative multi-asset archives, deployed relative paths/content types, rendering and apply/recovery; not a server-side application installer |
| HTTP upstream | Capability-gated node Website configuration and origin test/apply path (`NodeSiteTab.tsx`) | Loopback/private service, rejected/unreachable origins, shared Telemt front and concurrent WEB clients |
| Node wizard | Identity/DNS, proxy settings, install command, agent connection/readiness and diagnostics (`CreateNodeDialog.tsx`, `InstallCommandDialog.tsx`) | Fresh Ubuntu/Debian, real DNS/ACME, token expiry/retry, delayed registration, service failures and successful Telegram connection |

The wizard is a guided creation/install flow. Website selection and key distribution
remain separate surfaces rather than additional wizard steps.

For acceptance evidence record commit, panel/agent/Telemt versions, OS, client,
network, scenario, expected/observed result and timestamp. Attach diagnostics JSON
and update outcomes after removing credentials/user identifiers. Mark each scenario
passed, failed or not run; leave unexecuted criteria open.

The two supplied specifications remain requirements. Current guidance lives in
README, PRODUCT, DESIGN, setup guides, monitoring and the runbook.
