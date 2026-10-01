# Monitoring

**English** · [Русский](monitoring.ru.md)

The panel watches your servers on its own. The Monitoring page draws charts, the Overview page
lists what is broken, and alerts can come to Telegram. None of this needs extra software. This
document covers the optional part: sending the panel's numbers to Prometheus and Grafana. If a
term is unfamiliar, see the [glossary](start.en.md#glossary).

## Do you need Prometheus and Grafana?

Prometheus is a monitoring program. Every minute or so it visits a list of addresses, collects
numbers from them (metrics) and keeps their history. Grafana draws charts and dashboards from
those numbers. Both are free and installed separately from the panel.

You don't need them if this panel is all you watch. Set them up if you already run Prometheus
and Grafana for other systems and want the panel on the same screen, or if you want your own
alert rules.

The panel exports only a few numbers: servers and users by status, live sessions per server,
and people online per server and in total. Traffic, per-user statistics and WEB transport data
stay in the panel.

## What the panel shows without them

The Monitoring page has four tabs and a period switch (1h, 6h, 24h, 7d):

- "All servers" shows totals: servers online, healthy, running with errors, people online (with
  the number of connections under it) and current traffic.
- "Servers" shows a card of charts for each server: live sessions and streams, upload and
  download speed. A telemt server has two lines instead of sessions and streams, "People online"
  and "Connections", and one "Traffic" line.
- "WEB transport" shows which WEB carriers clients chose on all telemt servers over the last
  24 hours, plus counters of failures, rejected attempts, evicted sessions and bridge recoveries.
- "Metrics export" has a short Prometheus snippet and points to this document.

The panel reads each online server once a minute and keeps this history for 30 days. The
Overview page is the one place that lists problems, under "Needs attention". Details for one
server are on its page: the Health tab has the same charts, and on a telemt server also the
WEB carriers of that server. Telegram alerts are set up in "Settings" → "Notifications". The
panel sends a message when a server goes offline or comes back, when changes fail to apply,
and about other problems it finds on servers.

How the panel counts people online:

- One Telegram app holds several connections at once, usually 2 to 15, so there are always
  more connections than people. The panel counts people once a minute from what telemt reports.
- A personal user with at least one connection counts as one person. A shared user counts as
  many people as the distinct IP addresses connected to it.
- For a shared user, several people behind one router count as one, while one person's phone on
  mobile data and computer on Wi-Fi count as two. The users list and the user window call the
  same number devices.
- Someone connected to two servers counts once in the total, and on each server in that
  server's own number.
- tproxy servers do not report who is connected, so they count WEB sessions instead.
- The panel keeps addresses only in memory, to count people over the last 15 minutes. Only the
  counts reach the database.

How to read the WEB transport numbers:

- A selection is one choice of a carrier by a client. It does not count people or live
  sessions.
- Failures, rejections, evictions and recoveries are counters over the window. They are not a
  failure rate, since the panel does not know how many attempts succeeded.
- "Not available" is not zero. A server that is offline, did not report a counter or does not
  support the feature shows up as a gap.
- The timings in a server's "Server checks" row measure the path from the panel to the server. They do
  not tell you how long a user's connection takes to set up.

## 1. Get the token

The panel serves its metrics at `/metrics` on its own domain, for example
`https://panel.example.com/metrics`. Prometheus cannot sign in with a password, so this address
is protected by a separate token, `METRICS_TOKEN`. Every request must carry it in a header:

```
GET /metrics
Authorization: Bearer <METRICS_TOKEN>
```

Without the header, or with a wrong token, the panel answers `401`.

If you installed the panel with `install.sh`, the token already exists: the installer generated
it and wrote it to `/opt/tgproxy-panel/.env`. Show it on the panel's server:

```bash
sudo grep '^METRICS_TOKEN=' /opt/tgproxy-panel/.env
```

If you set up the panel by hand, generate a token with `openssl rand -hex 32`, put it in `.env`
as `METRICS_TOKEN=<value>` and restart the panel with `docker compose up -d` in the folder with
its `docker-compose.yml`.

With `NODE_DRIVER=gateway`, the default and the only mode that works with real servers, the
panel refuses to start without `METRICS_TOKEN`, because `/metrics` is on the public domain. Only
`NODE_DRIVER=mock` (tests and demos) allows an empty token, and then `/metrics` is open to
anyone.

Check that it works:

```bash
curl -H "Authorization: Bearer <METRICS_TOKEN>" https://panel.example.com/metrics
```

The answer is plain text with lines like `tgwp_nodes{status="online"} 3`.

Give the token only to Prometheus. Keep it out of the browser and out of git. To change it, put
a new value in `.env`, restart the panel and update the token file on the Prometheus side. On an
`install.sh` install the restart is:

```bash
cd /opt/tgproxy-panel && sudo docker compose up -d
```

## 2. Add the panel to Prometheus

Put the token in a file that Prometheus can read. The file must be readable by the user
Prometheus runs as:

```bash
echo -n "<METRICS_TOKEN>" > /etc/prometheus/tgwp-token
chmod 600 /etc/prometheus/tgwp-token
```

Copy the `tgwp-panel` job from [`deploy/prometheus.example.yml`](../deploy/prometheus.example.yml)
into the `scrape_configs` of your `prometheus.yml` and replace the target with your panel's
domain:

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

Reload Prometheus. On its "Status" → "Targets" page the `tgwp-panel` job should be `UP`. A
`401` there means the token in the file is wrong.

Keep the interval at 60 seconds. Server and user counts are read from the database at each
scrape, but the live session numbers change only once a minute, when the panel reads the
servers.

## 3. Import the Grafana dashboard

The ready dashboard is in [`deploy/grafana/tgwp-panel.json`](../deploy/grafana/tgwp-panel.json).
It needs Grafana 10 or newer.

1. In Grafana, open "Dashboards" → "New" → "Import".
2. Upload `tgwp-panel.json` or paste its contents.
3. For the `Prometheus` data source, pick the Prometheus that scrapes the panel.
4. Press "Import".

The dashboard shows the last 24 hours and refreshes every minute; both can be changed in Grafana. It
has tiles for servers and users by status (the dashboard labels them `Keys`), charts of live
sessions and streams per server, and the `Sessions live by node` table. Its uid is `tgwp-panel`, so
importing the file again updates the dashboard in place.

## 4. Metric reference

Everything below comes from the panel's `/metrics`. Prometheus adds its own `job` and
`instance` labels on top.

| Metric | Type | Labels | Meaning |
| --- | --- | --- | --- |
| `tgwp_nodes` | gauge | `status`: `pending`, `online`, `offline`, `degraded` | Number of servers in each status. All four series are always present, even at 0. |
| `tgwp_keys` | gauge | `status`: `pending`, `active`, `revoked` | Number of users in each status. All three series are always present, even at 0. |
| `tgwp_node_sessions_live` | gauge | `node`: server UUID | Live sessions on the server, from its latest snapshot. On telemt, the current connections of all its users added up. |
| `tgwp_node_streams_live` | gauge | `node`: server UUID | Live relay streams on the server, from its latest snapshot. On telemt, the same number as `tgwp_node_sessions_live`. |
| `tgwp_node_people_online` | gauge | `node`: server UUID | People online on the server, from its latest snapshot. Someone connected to two servers is in both series. Snapshots taken before the version that counts people have no series. |
| `tgwp_people_online` | gauge | | People online across all servers, each person once even when connected to several servers. How people are counted is described under "What the panel shows without them". |
| `tgwp_people_online_15m` | gauge | | People who were online at least once in the last 15 minutes. |
| `go_*`, `process_*` | various | | Standard Go runtime and process metrics of the panel itself: goroutines, memory, garbage collection, open files, CPU. |

What the statuses mean:

- A `pending` server ("Pending" in the panel) was created, but its install command has not been
  run yet.
- An `online` server ("Online") has an agent that keeps in touch.
- An `offline` server ("Offline") sent no heartbeat for longer than the offline threshold. The
  threshold is 90 seconds by default and can be changed in "Settings" → "Notifications".
- A `degraded` server ("Degraded") has an agent that answers, but one of the proxy's services is
  not working.
- A `pending` user ("Setting up" in the panel) has not yet reached all of their servers. They
  turn `active` once every server has them.
- A `revoked` user was revoked by hand. Users that earlier versions of the panel revoked on
  expiry stay here too.
- Turned-off and expired users are not revoked and count as `active` or `pending`. The tiles in
  the Users section and `GET /api/v1/keys/summary` show how many there are.

Things to know about the per-server metrics:

- The `node` label holds the server's UUID. The same UUID is in the address of the
  server's page in the panel: `https://panel.example.com/nodes/<uuid>`.
- A new server has no series until the panel takes its first snapshot.
- The value is the server's latest snapshot, as long as it is no more than three minutes old.
  A server's series disappear as soon as the panel marks it offline or it stops sending data,
  and come back with it.
- `tgwp_people_online` and `tgwp_people_online_15m` disappear too when the panel has not counted
  people in the last three minutes.
- When a server is deleted, its series disappear.

A few queries to start with:

```
tgwp_nodes{status="offline"} > 0      # some server is offline
tgwp_nodes{status="degraded"} > 0     # some server has a broken service
tgwp_people_online                    # people online across all servers
sum(tgwp_node_sessions_live)          # live sessions on all servers
up{job="tgwp-panel"} == 0             # Prometheus cannot reach the panel
```

## 5. Metrics of a single server

Prometheus cannot scrape the servers themselves. The agent reads the proxy engine's metrics on
the server itself and passes them to the panel over the connection it keeps open to the panel.
On telemt servers the metrics port listens only on the server's loopback interface, and a
firewall rule closes it from outside as well.

To see the raw metrics of one server, ask the panel:

```
GET /api/v1/nodes/{id}/metrics
```

This address needs a signed-in panel session (any role) and does not accept `METRICS_TOKEN`.
The panel fetches the text from the server at that moment and returns it unchanged, in
Prometheus text format:

- On a tproxy server, this is the `tproxy-server` metrics. The panel charts
  `tproxy_sessions_live`, `tproxy_streams_live`, `tproxy_bytes_up_total`,
  `tproxy_bytes_down_total`, `tproxy_sessions_created_total` and `tproxy_limit_hits_total`.
  These have no labels.
- On a telemt server, this is telemt's own metrics. The panel reads `telemt_connections_total`,
  `telemt_user_connections_current` and `telemt_user_unique_ips_current`. The last two have a
  `user` label: `k` followed by the first 12 hex digits of the user's UUID.

You will rarely need this address. The Monitoring page and the server's Health tab already
draw these numbers.

## 6. Per-user statistics

There is no `tgwp_key_*` metric. On telemt servers the panel records traffic, connections and
unique IP addresses for each user, and shows them in Users: the "Traffic, 30 days" and
"Activity" columns in the list ("Online, ≈ 2 devices" with the connections under it), and the
same numbers plus the "Traffic and connections" chart in the user window. tproxy servers do not
measure traffic per user: `tproxy-server` counts only per server.

How the panel is built is described in the [reference](reference.md), installation in the
[setup guide](setup.en.md). What to do when something breaks is in the [runbook](runbook.md).
