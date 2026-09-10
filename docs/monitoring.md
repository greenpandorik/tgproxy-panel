# Monitoring

The panel UI groups monitoring by operator task: **Overview** for fleet health,
**Problems** for active alerts and their next action, **Nodes** for historical series,
and **WEB Transport** for carrier and overload observations. The Problems view uses
the same alert actions as the dashboard, so resolving or retrying an issue is consistent.

## WEB runtime and carriers

WEB Transport reports runtime/admission, endpoint, last certificate check and
supported negotiation/learning state. The UI carrier window is 24 hours, backed by
`GET /api/v1/nodes/{id}/web/carriers?from&to`; fleet data uses
`GET /api/v1/monitoring/web/carriers?from&to`.

Selections are not unique users. Reported failures/rejections are counters, not a
general client failure percentage. Missing metric families, offline nodes and
unsupported capabilities stay distinct from zero. Do not derive setup-latency
P50/P95 from panel-to-node probe timings. Diagnostics history/JSON preserves
executed and not-run results, but does not certify all Telegram clients and
fallback paths; see [the acceptance matrix](vnext-acceptance.md).

The panel exposes fleet-level metrics for Prometheus at `/metrics`, and the Monitoring
page in the UI (`/monitoring`) already covers per-node charts without any of this setup
— read this doc only if you want the panel's own metrics in Grafana/Prometheus, or want
to understand what a relay node exposes and how to reach it.

## 1. Enable `/metrics`

Set `METRICS_TOKEN` in the panel's environment to a random secret:

```bash
openssl rand -hex 32   # put the result in .env as METRICS_TOKEN=<value>
```

`/metrics` sits outside the panel's session-auth group (Prometheus can't do cookie
login), so it is instead gated by this bearer token:

```
GET /metrics
Authorization: Bearer <METRICS_TOKEN>
```

`METRICS_TOKEN` is **required** when `NODE_DRIVER=gateway` — the panel refuses to start
without it, because in that mode `/metrics` is reachable on the public domain. In other
node-driver modes it's optional, but still recommended: without it, `/metrics` is open to
anyone who can reach the panel.

Keep the token out of the browser and out of version control — it only ever goes to your
scraper.

## 2. Scrape it with Prometheus

Copy [`deploy/prometheus.example.yml`](../deploy/prometheus.example.yml)'s `tgwp-panel`
job into your `prometheus.yml`, put the token in a file Prometheus can read, and replace
the target placeholder with your panel's hostname:

```bash
echo -n "<METRICS_TOKEN value>" > /etc/prometheus/tgwp-token
chmod 600 /etc/prometheus/tgwp-token
```

```yaml
scrape_configs:
  - job_name: tgwp-panel
    metrics_path: /metrics
    scheme: https
    scrape_interval: 60s
    authorization:
      credentials_file: /etc/prometheus/tgwp-token
    static_configs:
      - targets: ["panel.example.com"]
```

A 60s scrape interval matches the dashboard's `1m` refresh; there's no need to scrape
more often — the underlying node/key counts and live-session gauges are refreshed from
the database once per scrape, not streamed.

## 3. Import the dashboard

1. In Grafana: **Dashboards → New → Import**.
2. Upload [`deploy/grafana/tgwp-panel.json`](../deploy/grafana/tgwp-panel.json), or
   paste its contents.
3. When prompted for the `Prometheus` datasource input, pick the datasource you
   configured for the scrape job above.
4. Import. The dashboard is pinned to `now-24h` with a 1-minute auto-refresh; both are
   adjustable from the dashboard's own time/refresh controls after import.

The dashboard's uid is `tgwp-panel` — re-importing the same file updates it in place
rather than creating a duplicate.

## 4. Metric reference

All of these come from the panel's own `/metrics` endpoint (job `tgwp-panel` above).
Prometheus adds its usual `job`/`instance` labels to every one of them on top of what's
listed here.

| Metric | Type | Labels | Meaning |
| --- | --- | --- | --- |
| `tgwp_nodes` | gauge | `status` (`pending`, `online`, `offline`, `degraded`) | Number of nodes currently in each status. All four labels are always present, even at 0. |
| `tgwp_keys` | gauge | `status` (`pending`, `active`, `revoked`) | Number of access keys currently in each status. All three labels are always present, even at 0. |
| `tgwp_node_sessions_live` | gauge | `node` (node UUID) | Live MTProto sessions on that node, from its latest stats snapshot. |
| `tgwp_node_streams_live` | gauge | `node` (node UUID) | Live relay streams on that node, from its latest stats snapshot. |
| `go_*`, `process_*` | various | — | Standard Go runtime and process collectors (goroutines, GC, memory, open FDs, CPU). Useful for panel-process health, not for the relay fleet. |

`tgwp_node_sessions_live` and `tgwp_node_streams_live` are only emitted for nodes that
have at least one stats snapshot — a brand-new node with no successful check yet won't
have a series until its first snapshot lands.

The `node` label is the node's UUID, not its hostname or display name, because labels on
a `const` metric can't join against the database at scrape time. The table panel in the
dashboard (`Sessions live by node`) is the fastest way to map a UUID to a live count; to
map a UUID to a hostname, open that node's page in the panel UI (its URL contains the
same UUID).

## 5. Per-node relay metrics (not a Prometheus target)

Each relay node's own `tproxy_*` metrics (bytes up/down, sessions created, limit hits, in
addition to the two live gauges mirrored into `tgwp_node_*_live` above) are **not**
scraped by Prometheus — nodes aren't reachable from the internet and don't carry the
panel's bearer-token auth. Instead, the panel proxies them through its own
session-authenticated API:

```
GET /api/v1/nodes/{id}/metrics
```

This requires a logged-in panel session (cookie auth, any role) — it is not a
Prometheus scrape target and has no bearer-token option. The panel's own Monitoring page
(`/monitoring`) already renders these per-node series as charts, so in practice you
rarely need to call this endpoint directly; it exists mainly so the UI has somewhere to
read from. If you do call it yourself (e.g. to script a check), you're reading raw
Prometheus text format — the same `tproxy_sessions_live`, `tproxy_streams_live`,
`tproxy_bytes_up_total`, `tproxy_bytes_down_total`, `tproxy_sessions_created_total` and
`tproxy_limit_hits_total` names the relay exposes, unlabeled, one node per request.

## 6. Per-key statistics are not available

There is no `tgwp_key_*` metric, and none is planned under the current relay: the
`tproxy-server` relay's own metrics carry no per-profile (per-key) label, only fleet-wide
counters per node. The panel cannot attribute sessions, streams or bytes to an individual
access key — it only ever counts nodes and keys by status. If you need per-key usage
attribution, it has to come from a relay change upstream; there is no workaround on the
panel side today.
