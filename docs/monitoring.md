# Monitoring

The Monitoring page is for charts and history: **Whole fleet** for the fleet's totals,
**Servers** for each server's series, **WEB transport** for carrier and overload observations
and **Metrics export** for Prometheus. Open incidents and what to do about them are on
the Overview page, the one place the panel lists problems.

## WEB runtime and carriers

WEB Transport reports runtime and admission, the endpoint, the last certificate check and
whether negotiation and learning are supported. The UI's carrier window is 24 hours, read from
`GET /api/v1/nodes/{id}/web/carriers?from&to`; fleet data comes from
`GET /api/v1/monitoring/web/carriers?from&to`.

Selections count choices, not unique users. Reported failures and rejections are counters,
so they cannot be read as a general client failure rate. Missing metric families, offline
servers and unsupported capabilities stay distinct from zero. Do not derive setup-latency
P50/P95 from the timings of panel-to-node external checks. Diagnostics history and its JSON export
keep both executed and not-run results, but they do not vouch for every Telegram client
and fallback path.

The panel exposes fleet-level metrics for Prometheus at `/metrics`. The Monitoring
page in the UI (`/monitoring`) already has per-node charts with no setup at all, so
you only need this document to get the panel's own metrics into Grafana or Prometheus,
or to understand what a relay server exposes and how to reach it.

## 1. Enable `/metrics`

Set `METRICS_TOKEN` in the panel's environment to a random secret:

```bash
openssl rand -hex 32   # put the result in .env as METRICS_TOKEN=<value>
```

`/metrics` sits outside the panel's session-authenticated routes, since Prometheus cannot
log in with a cookie. This bearer token protects it instead:

```
GET /metrics
Authorization: Bearer <METRICS_TOKEN>
```

`METRICS_TOKEN` is **required** when `NODE_DRIVER=gateway`. In that mode `/metrics` is
reachable on the public domain, so the panel will not start without the token. With other
server drivers it is optional but still recommended, because without it `/metrics` is open to
anyone who can reach the panel.

Keep the token out of the browser and out of version control. The only thing that needs
it is your scraper.

## 2. Scrape it with Prometheus

Copy the `tgwp-panel` job from [`deploy/prometheus.example.yml`](../deploy/prometheus.example.yml)
into your `prometheus.yml`, put the token in a file Prometheus can read, and replace
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

A 60s scrape interval matches the dashboard's `1m` refresh. Scraping more often gains
nothing: the server and key counts and the live-session gauges are read from the database
once per scrape, not streamed.

## 3. Import the dashboard

1. In Grafana: **Dashboards → New → Import**.
2. Upload [`deploy/grafana/tgwp-panel.json`](../deploy/grafana/tgwp-panel.json), or
   paste its contents.
3. When asked for the `Prometheus` datasource input, pick the datasource you
   configured for the scrape job above.
4. Import. The dashboard opens on `now-24h` with a 1-minute auto-refresh; you can
   change both with the dashboard's own time and refresh controls afterwards.

The dashboard's uid is `tgwp-panel`, so importing the same file again updates it in place
instead of creating a duplicate.

## 4. Metric reference

All of these come from the panel's own `/metrics` endpoint (job `tgwp-panel` above).
Prometheus adds its usual `job`/`instance` labels to each of them on top of what is
listed here.

| Metric | Type | Labels | Meaning |
| --- | --- | --- | --- |
| `tgwp_nodes` | gauge | `status` (`pending`, `online`, `offline`, `degraded`) | Number of servers currently in each status. All four labels are always present, even at 0. |
| `tgwp_keys` | gauge | `status` (`pending`, `active`, `revoked`) | Number of access keys currently in each status. All three labels are always present, even at 0. |
| `tgwp_node_sessions_live` | gauge | `node` (server UUID) | Live MTProto sessions on that server, from its latest stats snapshot. |
| `tgwp_node_streams_live` | gauge | `node` (server UUID) | Live relay streams on that server, from its latest stats snapshot. |
| `go_*`, `process_*` | various | — | Standard Go runtime and process collectors (goroutines, GC, memory, open FDs, CPU). Useful for panel-process health, not for the relay fleet. |

`tgwp_node_sessions_live` and `tgwp_node_streams_live` appear only for servers that
have at least one stats snapshot. A brand-new server with no successful check yet has no
series until its first snapshot lands.

The `node` label is the server's UUID rather than its hostname or display name, because labels
on a `const` metric cannot join against the database at scrape time. The dashboard's table
panel (`Sessions live by node`) is the quickest way to match a UUID with a live count. To
match a UUID with a hostname, open that server's page in the panel UI; its URL contains the
same UUID.

## 5. Per-node relay metrics (not a Prometheus target)

Each relay server's own metrics (on a tproxy server, the `tproxy_*` family: bytes up and down,
sessions created, limit hits, plus the two live gauges mirrored into `tgwp_node_*_live`
above) are **not** scraped by Prometheus. Servers are not reachable from the internet and
do not carry the panel's bearer-token auth. The panel proxies them through its own
session-authenticated API instead:

```
GET /api/v1/nodes/{id}/metrics
```

This needs a logged-in panel session (cookie auth, any role). It is not a
Prometheus scrape target and has no bearer-token option. The panel's own Monitoring page
(`/monitoring`) already draws these per-node series as charts, so you will rarely call
the endpoint directly; it exists mainly as something for the UI to read. If you do call it
yourself, for example from a script, you get raw Prometheus text format: the same
`tproxy_sessions_live`, `tproxy_streams_live`, `tproxy_bytes_up_total`,
`tproxy_bytes_down_total`, `tproxy_sessions_created_total` and `tproxy_limit_hits_total`
names the relay exposes, without labels, one server per request.

## 6. Per-key statistics are not a Prometheus metric

There is no `tgwp_key_*` metric; the panel's metrics only count servers and keys by status. On a
telemt server the panel does record traffic per key, and shows it in the keys list and on
each key's page. On a tproxy server there is nothing to record: the `tproxy-server` relay's
own metrics carry no per-profile (per-key) label, only counters per server, so per-key usage
there would need a change in the relay upstream.
