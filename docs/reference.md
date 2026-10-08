# Reference

**English** · [Русский](reference.ru.md)

This is the detailed layer of the documentation. It explains how the panel and its servers
work, what each screen is for, and every setting, environment variable and command. You don't
have to read it from start to finish: pick a section from the list below.

If you are just starting, the [from-scratch guide](start.en.md) walks through the first install
step by step, and its [glossary](start.en.md#glossary) explains the basic terms. The
[setup guide](setup.en.md) covers every way to install, the [runbook](runbook.md) covers
upgrades, backups and fixing problems, and [monitoring](monitoring.md) covers Prometheus and
Grafana.

## Contents

- [How it works](#how-it-works)
- [Server engines](#server-engines)
- [Panel screens](#panel-screens)
- [Users and links](#users-and-links)
- [How cover sites work](#how-cover-sites-work)
- [Checks, incidents and alerts](#checks-incidents-and-alerts)
- [Accounts and security](#accounts-and-security)
- [Backups and the master key](#backups-and-the-master-key)
- [Installation details](#installation-details)
- [Environment variables](#environment-variables)
- [Command line](#command-line)
- [API for scripts](#api-for-scripts)
- [Development and tests](#development-and-tests)

## How it works

The system has two parts: the panel and the proxy servers.

The panel is one program written in Go, with the web interface built in. It runs in Docker on
its own server, next to a PostgreSQL database that holds everything: servers, users, sites,
settings and history. Caddy stands in front of the panel and gets its HTTPS certificate.

Each proxy server runs a proxy engine and a small program called the agent, `tgwp-agent`. The
engine is what Telegram users connect to: telemt on new servers, or the older tproxy stack (see
[Server engines](#server-engines)). The agent carries out the panel's orders on the server and
reports back.

```
admin's browser ──HTTPS──> Caddy ──> panel ──> PostgreSQL
                                       ▲
                                       │ gRPC, connection opened by the agent
                                       │
proxy server:  tgwp-agent ──> telemt: WEB proxy behind Caddy on 443, Fake-TLS on 8443,
                              cover site, control API on 127.0.0.1:9091
          or:  tgwp-agent ──> tproxy-server + MTProxy, Caddy on 443
```

The agent opens the connection to the panel itself and keeps it open. The panel never connects
to a server, so a server needs outgoing internet access and its own public ports, nothing more.
If the connection breaks, the agent reconnects, waiting from 1 to 30 seconds before each
attempt.

Every 30 seconds the agent sends a heartbeat with the state of the services and the load. Once a
minute the panel also collects statistics from every online server. A server has one of four
statuses:

| Status | Meaning |
|---|---|
| `pending` | Added in the panel, but its agent has not connected yet |
| `online` | The agent is connected and the services answer |
| `degraded` | The agent answers, but a service on the server is not fully ready |
| `offline` | No heartbeat for `OFFLINE_AFTER` seconds (90 by default) |

### How a change reaches a server

When you add, turn off or revoke a user, change their limits or assign a cover site, the panel
saves the change and marks the affected servers as having unapplied changes. Every `APPLY_INTERVAL`
seconds (45 by default) a background task sends the full configuration to each marked server.
Both intervals can also be changed without a restart under Settings → Notifications.
"Apply changes" on the server page sends the changes at once.

The panel applies background changes to at most four servers at once. Failed attempts retry
with randomized delays from 45 seconds up to 15 minutes. A new configuration revision does not
inherit an older revision's retry delay, so new changes, including access revocation, can start
at the next opportunity. Manual applies run immediately; other servers wait in the background
queue.

On a telemt server the agent applies the changes through telemt's control API, and connected
users stay connected. On a tproxy server the agent rewrites the relay's files and restarts it,
which drops live connections; Telegram clients reconnect by themselves. A new user starts
working after this step, usually within a minute.

If an apply fails, the agent puts the previous configuration back. The runbook explains how to
read such a job: [Changes do not reach a server](runbook.md#changes-do-not-reach-a-server).

Once a minute the panel also finds users whose end date has passed and takes them off the
servers. Such a user is not revoked: extend the date and access comes back.

## Server engines

The engine is the proxy software on a server. You choose it when you add the server, and it
can't be changed afterwards. telemt is the default and the one to pick for new servers.

| | telemt | tproxy |
|---|---|---|
| Direct links per user | WEB and Fake-TLS, plus one Fake-TLS link per backup masking domain | WEB only |
| User limits: traffic quota, speed, unique IPs, connections | Yes, telemt enforces them | No |
| Traffic per user | Yes | No |
| Applying changes | Without a restart | Restarts the relay |
| Public ports | 80, 443 and the Fake-TLS port (8443 by default) | 80 and 443 |
| Latency to Telegram data centres | Yes | No |
| Settings tab, full server check, WEB transport data | Yes | No |
| Telemt updates from the panel | Yes | No |

### telemt

One telemt process serves the WEB proxy, Fake-TLS, the cover site and a control API that the
agent uses. The server install script sets up:

- Caddy from its GitHub release, version and sha256 pinned by the panel. It holds the certificate on 443 and
  passes requests to telemt's WEB listener on `127.0.0.1:18080`, adding `X-Forwarded-For`.
  Caddy serves no files itself. telemt answers the cover site, so a visitor without a valid key
  gets a reply from the same process as a proxy user.
- The telemt release named by `TELEMT_VERSION`, downloaded from
  `https://github.com/telemt/telemt/releases` and checked against `TELEMT_SHA256_X86_64`
  before it is installed to `/usr/local/bin/telemt`.
- A `telemt` system account, so the service runs without root rights.
- `tgwp-agent init-node --engine telemt`, which writes `/etc/telemt/telemt.toml`, the control
  API token in `/etc/telemt/api.token` (mode 0600), the unit `/etc/systemd/system/telemt.service`
  and the cover site in `/var/lib/telemt/public`.
- An nftables table `tgwp_telemt` (`/etc/nftables.d/tgwp-telemt.nft`, included from
  `/etc/nftables.conf`) that closes the internal ports to the outside.
- The agent: `/usr/local/bin/tgwp-agent`, its settings in `/etc/tgwp-agent/agent.env` and the
  unit `tgwp-agent.service`.

| Port | Open to | What uses it |
|---|---|---|
| 80 | Internet | Caddy, certificate issuing |
| 443 | Internet | Caddy: the WEB proxy and the cover site |
| 8443 (the Fake-TLS port, `classic_port`) | Internet | Fake-TLS |
| 9090 | Loopback only | telemt metrics |
| 9091 | Loopback only | telemt control API |
| 18080 | Loopback only | telemt WEB listener behind Caddy |

The Fake-TLS listener runs with telemt's `synlimit` rules, the SYN flood fix taken from
MTPROTO_FIX_By_MEKO (see [Credits](../README.md#credits)).

telemt needs the server's public IPv4 address. The install script takes it from
`TGWP_PUBLIC_IP` if that is set, then from the public IP entered for the server in the panel.
Otherwise it compares two candidates, the address of the outgoing network interface and the
address `api.ipify.org` sees, and picks the one the server's A record points at. If neither
matches, it uses the interface address when that address is public. If no address can be
found, the install stops before changing anything: enter the public IP in the panel and run the
command again. You can correct the address later on the server's Settings tab.

The server must be able to reach its own domain on port 443. telemt passes a Fake-TLS
connection with an unknown domain name on to the Fake-TLS domain on port 443, and that domain
usually points at the server itself. Some hosting networks block a server from connecting to
its own public address. Then such connections fail, and anyone probing the server gets an error
where the cover site should be. To check, run `curl -sSI https://<server domain>/` on the server
itself: it must return the cover site's headers.

### tproxy

The older stack: `tproxy-server` from `https://github.com/telegramdesktop/tproxy-server` at
the commit `TPROXY_COMMIT`, the official MTProxy, Caddy and the agent. The install script runs
the upstream `deploy/install.sh` for the first three. A tproxy server uses ports 80 and 443 and
gives each user a WEB link only.

Every apply restarts `tproxy-server`, and MTProxy too when its secrets change, so live sessions
drop. The runbook has the details:
[On a tproxy server](runbook.md#on-a-tproxy-server). Moving an installed
tproxy server to a new `TPROXY_COMMIT` needs a reinstall with a fresh install command.

### What restarts telemt

Most changes reach telemt without a restart. The table lists the ones that restart it.
Restarting drops the server's live connections, and Telegram clients reconnect by themselves.

| Change | Restarts telemt | What happens to links |
|---|---|---|
| Users, their limits, the cover site | No | New users get their links; the rest stay as they are |
| Fake-TLS domain or port | Yes | Every Fake-TLS link issued for this server stops working. WEB links keep working |
| Backup masking domains | Yes | Issued links keep working; each domain adds one Fake-TLS link per user |
| Public IP | Yes | Nothing changes, because links contain the domain name |
| "Restart telemt" on the Maintenance tab | Yes | Nothing changes |
| A telemt update | Yes, after draining WEB sessions | Nothing changes |

The first three changes live on the Settings tab of the server page, in the "Addresses and
Fake-TLS" card, and reach the server with the next apply. The Fake-TLS domain and port are part
of every Fake-TLS link's secret, so after changing either one, send people new links.

Backup masking domains help when a provider starts blocking the main Fake-TLS domain: people
switch to a link with another domain, and the server needs no reinstall. A server takes up to
eight of them, and the agent must be version 2.9.2 or later. Use only real sites that answer
HTTPS on port 443 and are reachable from the server: telemt copies the TLS fingerprint of each
domain at startup, and if one does not answer, telemt refuses the configuration and the apply
rolls back.

### Network tuning

For both engines the install script writes `/etc/sysctl.d/90-tgwp.conf`, network settings for a
proxy server taken from MTPROTO_FIX_By_MEKO: BBR with the `fq` qdisc, larger accept and SYN
queues (`somaxconn`, `tcp_max_syn_backlog`, `netdev_max_backlog` = 65535), TCP Fast Open, and
short keepalives (45 and 15 seconds, 3 probes), so dead clients drop off in about a minute. The
script applies the file with `sysctl --system`. If the kernel rejects a key, for example in a
container or on an old kernel, the script says so and carries on. The file can be deleted if a
server needs its own values.

### Updating servers

The panel decides which telemt a server should run: the pair `TELEMT_VERSION` and
`TELEMT_SHA256_X86_64` in its `.env`. `sudo /opt/tgproxy-panel/install.sh --update` moves this
pair to the values the new panel release ships. Servers keep what they have until you update
them. There are three ways to do it.

On the server itself, over SSH as root:

```bash
tgwp-agent upgrade --check   # what would change; changes nothing
tgwp-agent upgrade           # prints the plan and asks
tgwp-agent upgrade --yes     # unattended; required when there is no terminal
```

The command asks the panel what the server should run (`GET /api/v1/node/upgrade`, sent with
the server's own token from `/etc/tgwp-agent/agent.env`) and replaces only what differs: the
telemt binary, the agent binary or both (`--telemt` and `--agent` narrow it down). It checks
every download against the panel's sha256, keeps the previous binary, restarts the service and
waits for it to report healthy. If it does not, the previous binary goes back. No install token
is needed.

In the panel, for one server: the server page → Maintenance → the Versions block → "Update
telemt". The agent checks the download's SHA256, pauses new WEB sessions, lets the current ones
finish for up to 120 seconds, swaps the binary with a backup copy, restarts telemt, checks the
version and readiness, and opens WEB sessions again. The result is one of `ok`, `refused`,
`failed`, `rolled_back`, `rollback_failed` or `needs_attention`. Only `ok` means the update
finished.

In the panel, for several servers: Servers → "Update telemt…". The window names the version it
installs and lists every telemt server with the version it would move from and to. Search finds
a server by name or host, and "Select outdated" ticks every online server on an older version.
The servers are updated one by one in list order, so the first one works as a test. After each
update the panel checks the server and watches it for a minute before moving on. An error stops
the queue, and "Stop subsequent updates" lets the update in progress finish.

Restarting telemt drops the server's live sessions, so go one server at a time. The runbook
covers the details and the outcomes: [Upgrading servers](runbook.md#upgrading-servers) and
[A telemt update failed](runbook.md#a-telemt-update-failed).

## Panel screens

The side menu has eight sections: Overview and Monitoring for watching, Servers and Cover
websites for the infrastructure, Users in the Access group, Page in the Subscription group, and
then Activity log and Settings.

### Overview

The home page shows whether everything works and what needs your attention. "Needs attention"
lists open problems: which server, what happened and how long ago, with the action that fixes
it ("Open", "Apply" or "Restart") and "Mark read". Owners and admins can tick several problems
and mark them read together, or mark all read; a read problem leaves the list until it clears
and happens again. When nothing is wrong the box says so. Beside it are four numbers: people
online with a 24-hour line and the connections under it, servers online naming the one that is
down, active users out of all, and the traffic of the last 24 hours with the change against the
day before when the panel holds that whole day. Below are cards for all servers in the shared
order, and a folded block "People online chart and recent operations". The "New user" button
opens user creation.

People online is an estimate refreshed every minute. A personal user with at least one
connection is one person, a shared user counts as many as its distinct IP addresses, and on
tproxy servers WEB sessions stand in for people. One Telegram app holds several connections, so
the connections are always the bigger number. The [monitoring guide](monitoring.md) has the
details.

### Monitoring

The Monitoring page holds charts and history. Its views:

| View | What it shows |
|---|---|
| Overview | Servers online, people online with the connections under it, the slowest route to Telegram, the certificate that expires first, current traffic, and a card for each server: people online, processor, memory, latency to Telegram, days left on the certificate, time since the last reboot, how long unapplied changes have waited, an available telemt update |
| By server | For each server: sessions and streams, upload and download rate, and CPU, RAM and disk. A telemt server has lines for people online and connections instead, and one for traffic |
| WEB transport | WEB carriers across all telemt servers |

How to collect the panel's metrics with Prometheus is shown in Settings → Integrations; it used to
be a Metrics export view here.

The period switch on "By server" offers 1, 6 and 24 hours and 7 days. Problems are listed on
Overview only. Collecting the panel's metrics with Prometheus is described in Settings →
Integrations; old links to the metrics export open that tab.

The charts come from snapshots: once a minute the panel records the state of every online
server, and keeps these records for 30 days.

For Prometheus the panel serves `/metrics` in Prometheus text format. The endpoint needs the
header `Authorization: Bearer <METRICS_TOKEN>`. It exports `tgwp_nodes` and `tgwp_keys` (counts
by status), `tgwp_node_sessions_live`, `tgwp_node_streams_live` and `tgwp_node_people_online` (per
server UUID), `tgwp_people_online` and `tgwp_people_online_15m` (the whole fleet), plus the
standard `go_*` and `process_*` metrics. A scrape job:

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

A ready Grafana dashboard is in [`deploy/grafana/tgwp-panel.json`](../deploy/grafana/tgwp-panel.json),
and a scrape example in [`deploy/prometheus.example.yml`](../deploy/prometheus.example.yml).
[Monitoring](monitoring.md) explains the setup, the metrics and what per-user statistics exist.

### Servers

The list shows each server's name and domain, people online with the connections under them,
CPU and RAM, and the time of its last response. A click anywhere in a row opens the server;
applying changes, the install command and deleting are on the server's page. Servers follow one
order across the panel: Overview, Monitoring, the server choice for a user and the subscription
page use it too. Owners and admins drag a row by its handle to change it, or from the keyboard
press Space on the handle, move with the arrows and press Space again. A new server goes last.
"Add server" opens a three-step wizard:

1. Server: a name, the domain, an e-mail for the Let's Encrypt certificate and, if needed, the
   public IP. "Check DNS" shows where the domain points right now.
2. Proxy: the engine and, under "Advanced: masking and ports", the Fake-TLS domain (the server's
   domain by default) and the Fake-TLS port (8443 by default; 1024 to 65535, except 80 and 443).
3. Install: the one-time install command. The window follows the agent until it connects.

"Update telemt…" on the same page updates several servers in a row (see
[Updating servers](#updating-servers)).

### Server page

The header shows the server's status, domain, the versions of the engine and the agent, and the
"Apply changes" button, which is active while the server has unapplied changes. There are five
tabs:

| Tab | What is there |
|---|---|
| Health | What is wrong, if anything (for an offline server, also the commands to check it). A line of key numbers: status, people online linking to this server's users, processor and memory, latency to Telegram, the result of the checks. People online, processor and traffic over a day or a week. Rows that open on click: "Telegram datacenters" (the route, each data centre's latency now and over time, the routes to Telegram), "Services" (each service's state, the server's resources and their history, resource headroom, proxy counters), "Server checks" ("Full server check" on telemt, "Check from the panel", "External network checks" and the "Check now" button) and, on telemt, "WEB transport" (status, capacity, carrier learning over 24 hours and the buttons to pause, drain and resume WEB sessions). A row with a problem is already open |
| Settings | telemt only: "Addresses and Fake-TLS" (public IP, Fake-TLS domain and port, backup masking domains, sponsor channel tag), then the folded "Transport strategy" with overload protection and "Recovery and egress" |
| Cover site | The current template, choosing another one, and an existing HTTP website on telemt servers that support it |
| Blocklist | Addresses and subnets the server refuses connections from |
| Maintenance | The telemt and agent versions with the telemt update, the actions (restart telemt or the relay, show the install command), apply history with logs, the users on the server with "Compare with the server" and, set apart, deleting the server |

The panel shows no service logs: they are read on the server itself, and the commands are in the
operations guide, under [Server logs](runbook.md#server-logs).

The "Telegram datacenters" row shows, for each data centre, the latency telemt measures with
its own health checks, the IPv4 or IPv6 preference and the state of the route. Latency under
150 ms is green, under 400 ms amber, higher is red. tproxy servers have no such data.

The sponsor channel tag comes from @MTProxybot in Telegram. With a tag set, clients connected
through this server see the sponsored channel. The card has buttons that copy the address and
the secret the bot asks for. Don't hand out the link the bot prints afterwards: give people the
links from the Users section.

"Recovery and egress" controls two things. Recovery decides what the agent does when
telemt stops answering: only watch, or restart after several failed checks in a row. Egress can
send telemt's traffic to Telegram through a local SOCKS5 proxy, for example WARP, with a reserve
route. The runbook describes both in
[Automatic restarts and the route to Telegram](runbook.md#automatic-restarts-and-the-route-to-telegram).

### Cover websites

The gallery of cover sites: 15 built-in ones, your own copies and imported sites, with a filter
by category, a preview on computer, tablet and phone sizes, "Customize website", "Import ZIP" and
"Custom website" for a site written from scratch in the editor. A site is assigned to a server
from the gallery or from the server's Cover site tab. How it works inside:
[How cover sites work](#how-cover-sites-work).

### Users

Above the list are the search by name, contact, note and short address, and the state buttons
with counts: All, Active, Expiring soon (within the next 7 days), Expired, Turned off, plus Setting
up and Revoked when there are any. "Filters" narrows the list by type and server, and an active
filter shows as its own button with a cross. The address `/users?node=<id>` opens the users of one
server. Bulk actions: extend, turn off, turn on, revoke access, delete. When nobody matches the
filters, there is a "Reset filters" button. "Columns" hides and shows activity, expiry, traffic
over 30 days, servers, type, the link (with a copy button) and creation date, and the choice is
remembered in the browser. Under the list it says which rows are shown, and you can pick 25, 50 or
100 rows per page; the choice is remembered and the page number is in the address. The "⋯" menu of
a row has "Copy link", "Extend by a month", "Turn off" or "Turn on", and "Delete". Clicking
anywhere on a row opens the user's window with the tabs Main, Access and servers, Limits and
Statistics. The section used to be called Access keys;
`/keys` and `/keys?key=<id>` lead to `/users` and `/users?user=<id>`. Everything about users is in
[Users and links](#users-and-links).

### Subscription

The Subscription group has two items. Page sets up the public subscription page: the language,
the first tab, the title and greeting, which links and servers to show. It has a live preview,
which always runs on the panel. These settings used to be a tab in Settings. Service decides where
pages open: on the panel's domain or on a domain of their own, on the panel server or on a
separate one. The owner and admins can see it, only the owner can change it. The details are in
[Subscription page](#subscription-page) and
[Subscription pages on their own domain](#subscription-pages-on-their-own-domain).

### Activity log

Every change made in the panel, and every sign-in including the failed ones, with who did it,
when, from which IP and what changed. Filters: action, user and dates. The action filter matches
the beginning of the name, so `key.` finds `key.create`, `key.revoke` and the rest. A 2FA reset
from the command line appears here as `auth.totp_reset`.

### Settings

The tabs come in two groups. "Whole panel" affects everyone, "Just for you" only your account.

| Tab | Group | What is there | Who can change it |
|---|---|---|---|
| Notifications | Whole panel | How often to apply changes (10 to 3600 s), when to treat a server as down (30 to 3600 s), Telegram alerts | Owner |
| Integrations | Whole panel | Metrics export for Prometheus and Grafana with a scrape example, whether `METRICS_TOKEN` is set, whether the alert webhook is configured and how to set it | Read only: both are set in `.env` |
| Branding | Whole panel | Branding variants: panel name, logo, logo for the dark theme, favicon, login background, colours, default theme, texts, custom CSS | Owner and admin |
| Accounts | Whole panel | Panel accounts and their roles | Owner |
| Backups | Whole panel | Database backups and the nightly schedule | Owner |
| My interface | Just for you | Theme (light, dark, system), density, collapsed or expanded menu. Stored in this browser | Everyone |
| Password and 2FA | Just for you | Password change, two-factor login | Everyone |

Branding changes apply at once, without a rebuild. The active variant styles the login page, the
interface and every public subscription page. Images upload immediately (PNG, JPEG, ICO or SVG,
up to 2 MB); name, colours and texts are saved with "Save". The default colours are cyan
`#3fc0d6` and teal `#20c997`, and they adjust to the light theme by themselves.

### Top bar, search and help

The top bar shows the panel version, the project on GitHub with its star count, and the number
of servers online. Once an hour the panel reads the latest release from GitHub. When a newer
version exists, the version chip is highlighted and links to the release. The panel sends
nothing about your installation, and `UPDATE_CHECK=false` turns the check off.

`⌘K` or `Ctrl+K` opens the command palette: jump to a section, find a server or a user, create a
user, apply changes everywhere, switch the theme or the language.

The `?` button in the header of every page and dialog opens help on that screen: what each field
means, an example, what happens if it stays empty. The `?` key does the same.

Forms remember what you typed. If a dialog closes by accident, the next time it opens the panel
offers to restore the draft. A draft lives in the browser for 24 hours, and passwords and tokens
are never saved in it.

## Users and links

A user gives one person or a group of people access to the proxy. The API and the database still
call a user a key: the `/api/v1/keys` routes, the `access_keys` table, the `key.*` actions in the
activity log. The "New user" dialog has three tabs:

| Tab | What it creates |
|---|---|
| One person | A personal user (`PERSONAL`). You can turn them off or revoke them without touching anyone else |
| Shared access | One user (`SHARED`) for a group of people sharing one link. It is turned off or revoked for everyone at once, and only shared access can have a short address |
| Several | Up to 100 personal users at once, named `prefix-1`, `prefix-2` and so on. In this mode the dialog is called "Several users", and the prefix and count go into the "Names" card. The panel checks that every server has room and creates either all of them or none. The result has a copy button for each user's subscription link, and "Download all links (.txt)" saves a file with the subscription links first and then the direct links for each server |

The fields: "Name", so you can find the user in the list later (required); "Contact", such as a
Telegram username, phone or e-mail (optional); "Short address" for shared access; "Note"; "Access
until" with "+1 month", "+3 months", "+1 year" or "No expiry"; the servers; and limits. A user can
be on several servers, and the person then gets links for each of them. A server holds up to 128
users by default. Every new user gets a subscription link straight away.

A user's state shows in the list and in their window:

| State | What it means |
|---|---|
| Setting up (`pending`) | The panel is setting up the servers, usually within a minute. There is nothing to press |
| Active (`active`) | Access works |
| Turned off (`disabled`) | Access is paused: the user is taken off the servers, the links and settings are kept. The "Access is on" switch in the user window, or "Turn on", brings access back |
| Expired (`expired`) | The end date has passed and the user is taken off the servers. Move the date forward and save, and access comes back with the same links |
| Revoked (`revoked`) | Access is closed for good. The user stays in the list for the record, and their settings are read-only |

A telemt server keeps a turned-off or expired user in its configuration but disabled, and a
tproxy server drops their secret. Users that earlier versions of the panel revoked on expiry stay
revoked.

"Change secret" gives the user a new secret: the direct links change and the old ones stop
working, while the subscription link stays the same. "Revoke access" closes access for good, and
"Delete" removes the user with their links and statistics.

### Links

Each server gives a user their own direct links. The "Direct proxy links" row in the user window opens them,
with a QR code for each and a copy button for the `t.me` and `tg://` forms.

| Link | Format | Where it works |
|---|---|---|
| WEB | `https://t.me/webproxy?server=<domain>&secret=<secret>` | Telegram Desktop; Android is experimental, iOS is planned |
| Fake-TLS | `https://t.me/proxy?server=<domain>&port=<Fake-TLS port>&secret=ee…` | Every Telegram app |

A tproxy server gives the WEB link only. A telemt server gives both, plus one more Fake-TLS link
for each backup masking domain.

### Limits

On telemt servers a user can have a traffic quota in GB, upload and download speed in Mbit/s, a
maximum of unique IP addresses and a maximum of connections. telemt enforces them itself. An
empty field means no limit. The advanced limits (sessions, streams and the like) mostly apply to
tproxy servers; telemt takes only the session and stream limits from them. On tproxy servers a
user also has a carrier mode: HTTPS (the default), HTTPS lanes, WebSocket or WebSocket lanes. On
telemt the WEB carrier is chosen automatically for each connection.

A quota can reset on a schedule: "Quota resets" is "Never", "Every week" or "Every month". A week
starts on Monday and a month on the 1st, both at 00:00 UTC. When a period starts the panel sends
the servers a new apply, and the agent zeroes the user's consumed traffic once; the quota itself
stays the same. If the panel or the server was down when the period changed, the reset happens
on the first apply after that. Turning the schedule on in the middle of a period does not zero
the quota: the first reset comes at the start of the next period.

On telemt servers the panel also counts traffic per user. The user window shows the traffic
over 30 days and whether the person is online, from about how many devices (distinct IP
addresses across all servers) and over how many connections, or when they last connected.
The Statistics tab draws a chart over 24 hours or 7 days.

### Subscription page

Every user has a public subscription link, which the panel creates together with the user. It opens
a page that walks the person through connecting: tabs for Android, iPhone and iPad, and a computer,
and on each one three steps (install Telegram, press "Connect" next to a server, confirm in
Telegram). The "Connect" button uses the link the device can open: Fake-TLS on iPhone, Fake-TLS
with the WEB proxy as a fallback on Android, the WEB proxy with Fake-TLS as a fallback on a
computer. On iPhone the second choice is the Fake-TLS link on a backup masking domain, when the
server has one. Below, "All links and QR codes" lists every link. The server cards on the page do
not show the servers' domains. When the Computer tab is opened on a phone, the page suggests
sending the link to yourself and opening it on a computer, with a "Copy the link to this page"
button. When a phone tab is opened on a computer and QR codes are on, the page shows a QR code of
itself so it can be opened on a phone. The page shows how long the access lasts and never shows the
user's name, contact or note. The RU/EN switch at the top lets the visitor change the language, and
the page remembers their language and tab in that browser.

What the page shows is set under Subscription → Page, with a live preview. The old address
`/settings?section=subscription` leads there too.

| Setting | What it does |
|---|---|
| Page language | The visitor's browser language (default), Russian or English |
| Tab shown first | Detected from the visitor's device (default), or always Android, iPhone and iPad, or Computer |
| Title and greeting | Text at the top, separately in Russian and in English; empty fields use "Connect Telegram" and a standard greeting in the page language |
| Which links to show | Fake-TLS, WEB proxy, backup domains; at least one of the first two stays on |
| What else to show | The step-by-step guide, the access end date, QR codes |
| Servers on the page | An unticked server is left off every subscription page; its users keep working |

The same filters apply to the JSON view.

- The panel keeps the subscription link in two forms: a SHA-256 hash, which it uses to find the
  user when the page is opened, and a copy encrypted with `MASTER_KEY`. That is why the link can
  be copied or shown as a QR code at any time.
- Links issued by earlier versions of the panel are stored as a hash only. They work but cannot
  be shown. "Issue a new link" in the user window, after a confirmation, replaces such a link with
  a new one, and the old one stops opening.
- "New subscription link" in the "More actions" menu replaces the link, and the old one stops
  opening at once. Proxies already added in Telegram keep working.
- "Revoke link" turns it off, and "Create link" then takes its place.
- "Change secret" does not change the subscription link: to get the new direct links, the person
  opens it and presses "Connect" again.

Shared access can have a short address `/s/<slug>`: 3 to 32 characters, lowercase Latin letters,
digits and dashes, not starting or ending with a dash. As you type, the panel turns Cyrillic into
Latin letters and spaces into dashes. The address must be free. It opens the same
page as the long link while the user has an active subscription link.

The page lives at `/s/<token>` or `/s/<slug>`, and adding `.json` gives the same data as JSON.
These addresses are public, limited to 60 requests a minute per IP address, and never cached. When
the link gives no access, the page explains why, and the JSON answers with the same status code:

| Case | Code | What the page says |
|---|---|---|
| Unknown or revoked subscription link | 404 | "This link was not found." |
| Access revoked | 410 | "This link is no longer valid." |
| Access turned off | 403 | "Access through this link is turned off for now. Message whoever sent you the link." |
| Access expired | 410 | "Access through this link has expired. Ask whoever sent you the link to extend it." |

### Subscription pages on their own domain

By default subscription links are built on the panel's domain (`PANEL_PUBLIC_URL`). Under
Subscription → Service you can choose "On my own domain" and enter another domain, just the domain
without a path, such as `sub.example.com`. It must differ from the panel's domain, or the panel
answers 422. Once it is saved, every subscription link, short address and QR code in the panel is
built on that domain. Old `/s/…` links on the panel's domain answer with a 302 redirect to the same
path on the new domain. The "Do not open pages on the panel domain" checkbox replaces the redirect
with a "This link was not found" page and code 404: the panel's domain is never shown, but old
links stop working. "Check the domain" opens `https://<domain>/healthz`.

The domain can be served in two ways:

| Way | How it works |
|---|---|
| On the panel server | `install.sh --update --sub-domain sub.example.com` writes `sites/subpage.caddy` into the panel directory, and the panel's Caddy picks it up and gets a certificate. From then on the panel serves only `/s/*`, `/healthz` and `robots.txt` there and answers 404 to the rest (it learns the domain from `SUBPAGE_DOMAIN` in `.env`). `--sub-domain off` removes the file |
| On another server | `install.sh --subpage` sets up two containers in `/opt/tgproxy-subpage`: `subpage` (the panel's own image, run with the `subpage` command) and Caddy with a certificate for the domain. "Get the install command" gives the command with the token |

The service on another server keeps no database. For every link it asks the panel
(`GET /api/v1/subpage/pages/{token}` with `Authorization: Bearer <service token>`), keeps the
answer for 30 seconds, and when the panel is unreachable shows the saved copy for up to 6 hours.
With no copy, it answers 503 and asks to try again in a minute. It takes at most 60 requests a
minute from one IP address. `/healthz` answers `{"ok":true,"panel_reachable":…,"version":…}`,
where `panel_reachable` says whether the panel answered in the last three minutes. Once a minute
the service sends the panel `POST /api/v1/subpage/heartbeat` with its version. Service in the panel
shows "online" when such a signal arrived within the last three minutes, the time of the last
one, the service version (with a warning when it differs from the panel's) and the address the
signal came from. The panel keeps the service token only as a hash and shows it once, inside the
install command. "New token", after a confirmation, invalidates the old one. "Disconnect the
service" revokes the token: the panel starts answering the service with 401, and it lives on saved
copies for a while.

## How cover sites work

Anyone who opens a proxy server's domain in a browser sees an ordinary website. This is the
cover site. On a telemt server telemt serves it itself; on a tproxy server the relay does.

The panel ships 15 built-in sites: `acorn`, `atlasdocs`, `cloudmetrics`, `corporate`,
`dailybrief`, `frame`, `kansocoffee`, `lumanotes`, `maison`, `nomad`, `northstar`, `orbitcdn`,
`personal`, `pixelforge` and `status`. On top of them you can:

- customize a built-in site: this makes an editable copy with your company name, headline,
  description, contact e-mail and footer text;
- import a static site as a ZIP with `index.html` in the root (one enclosing folder is fine). It
  may have HTML, CSS, JavaScript and local files, up to 512 files and 2 MB after unpacking. Paths
  and file types are checked, and nothing from the archive runs on the panel;
- write a site in the editor. It rejects external resources, forms and inline event handlers,
  and moves inline styles and scripts into files.

Before a site goes to a server, the panel makes a unique copy of it for that server: it shuffles
the order of the page blocks, renames CSS classes and file names, and picks among wording
variants where a template offers them. The copy depends on the server's ID, so two servers with
the same template serve different HTML, while the same server always gets the same result.
Assigning the same template again changes nothing and does not restart anything. This stops an
outside observer from finding all your servers by comparing their pages.

A telemt server whose telemt reports support for it can show an existing HTTP website instead of
a template: "HTTP website" on the Cover site tab. Only `http://` addresses with a loopback or
private IP are accepted. The agent checks the address on the server before switching.

After changing the site, check the server's domain in a browser, including images and styles.
The editor's preview doesn't show what was deployed.

## Checks, incidents and alerts

### Check from the panel

"Check now" by the "Server checks" row on the Health tab makes the panel test the server from the
outside, and on telemt servers runs the full check too. The result is saved with the server. The checks run in order and share a 15-second
budget:

| Check | What it verifies |
|---|---|
| `dns_a` | The domain resolves, and one of its addresses is the server's IP if the panel knows it |
| `tcp_80` | Port 80 answers. It is needed for the certificate and the HTTP redirect |
| `tcp_443` | Port 443 answers. If it doesn't, the next three are skipped |
| `tls_cert` | The certificate is valid for the domain and does not expire within 7 days |
| `pq_kex` | The TLS front agrees on the post-quantum key exchange `X25519MLKEM768`. For information only, it never fails the check |
| `http_root` | `https://<domain>/` answers 200 with a non-empty page. Redirects are not followed |
| `mask` | telemt only: the Fake-TLS port, asked with the Fake-TLS domain and no key, answers with a certificate valid for that domain |

What to do when a check fails is in the runbook:
[A check found a problem](runbook.md#a-check-found-a-problem).

### Full server check

On telemt servers "Full server check" looks at the server from both sides: from the outside, as
above, and from the inside, through the agent. The checks are grouped: DNS, ports and TLS, Caddy,
WEB transport, telemt, Telegram, and the availability of each public IP address. Each check
passes, gives a warning, fails or could not run. Checks that could not run are counted
separately and never count as passed. Results keep their history, and "Export report" saves one
as JSON.

The panel also runs this check by itself, at most once every 15 minutes per online server.

### External checks

A check from the panel's own network does not show how the server is seen from other networks.
For that there is `tgwp-probe`, a small program you run in other networks on a timer. It makes
a real TLS handshake with the server and opens the cover site. With a private test-secret
configuration it also performs authenticated Fake-TLS and WEB exchanges with Telegram; a custom
client adapter remains supported. Results appear under "External network checks" in
the "Server checks" row. A report older than three minutes counts as missing and opens an incident.

It is turned on with `PROBE_TOKEN` and `PROBE_LOCATIONS` in the panel's `.env`. The systemd
service and timer are in `deploy/probe`, and the runbook describes the setup in
[Checks from other networks](runbook.md#checks-from-other-networks).

### Incidents

An incident is a problem the panel found by itself. It appears in "Needs attention" on Overview.
Most incidents close by themselves when the problem goes away. "Mark read" takes one off the
list without closing it: it stays hidden while the problem lasts, and the check that found it
does not raise it again, so it shows up only when the problem clears and comes back. A failing
apply that keeps retrying is one incident until an apply succeeds. Incidents come from:

- a server going offline, and an apply that failed;
- the agent's reports: disk at least 90% full, memory at least 95% full, the engine not ready,
  WEB sessions closed, running out of file descriptors or of the connection table, too many
  failed connections to Telegram, an egress route down;
- the scheduled full server checks;
- external checks that failed or stopped reporting;
- a backup that could not be encrypted or uploaded.

### Telegram alerts and webhook

Settings → Notifications: enter the bot token and the chat ID, turn on "Enable alerts" and save.
"Send test message" works before saving: it uses what the form holds, and the stored values for
empty fields. The token is stored encrypted and never sent back to the browser.

Server health notifications combine datacenter, route and scheduled-check findings into one
problem episode per server. A problem must persist for 5 minutes before one warning is sent.
Recovery is sent only after 10 minutes of continuously healthy observations. Missing or stale
observations do not prove recovery and interrupt the confirmation period. Details remain in
the server's Health tab; a failing Middle Proxy writer alone does not prove that Telegram is
unavailable, because direct routing may still work.

The panel stores notification state in PostgreSQL, so restarting it does not start the same
notification episode again. Telegram and webhook delivery are acknowledged separately; failed
attempts are retried. Brief server outages under 3 minutes stay silent. Longer outages have a
separate offline/recovery notification. Failed applies are reported at most once per hour.

Detailed health findings still use their own confirmation rules (normally three bad or three
good observations, and two runs for scheduled checks); these remain visible in the panel
without producing a separate chat message for each datacenter.

If messages don't arrive, see [Telegram alerts do not arrive](runbook.md#telegram-alerts-do-not-arrive).

The same messages can go to your own HTTPS address: set `ALERT_WEBHOOK_URL` and
`ALERT_WEBHOOK_SECRET`. The webhook works whether or not the Telegram bot is on. The panel sends
a JSON body with `event_id`, `node_id`, `kind`, `message` and `at`, makes up to three attempts
and does not follow redirects. Each request is signed with HMAC-SHA256 in the
`X-TGWP-Signature` header, computed over the `X-TGWP-Timestamp` value, a dot and the body.
Settings → Integrations shows whether the webhook is configured; the address and the secret never
leave the server.

## Accounts and security

### Roles

| Role | What it can do |
|---|---|
| `owner` | Everything, including panel settings, the Telegram test message, accounts and backups |
| `admin` | Everything else: servers, users, cover sites, applies, checks, updates, resolving problems, branding |
| `viewer` | Only look. The viewer cannot see users' subscription links, direct links or secrets, and the API does not send them to this role. The viewer can change only their own password and second factor |

The installer creates the first account with the `owner` role. Other accounts are added under
Settings → Accounts, with a password of at least 10 characters.

### Two-factor login

Two-factor login is available by default; `FEATURE_TOTP=false` in `.env` hides it. Settings →
Password and 2FA shows "Turn on":

1. The panel shows a QR code and the key for manual entry. Any standard authenticator app works:
   Google Authenticator, Aegis, 1Password and others.
2. Enter the code from the app and your current password. The password stops someone who got
   hold of your session from enrolling their own app. Turning 2FA on ends your other sessions.
3. The panel shows eight recovery codes of the form `xxxxx-xxxxx`, once. Each works once. Save
   them before closing the window.

From then on, sign-in asks for the code after the password. The second step is valid for five
minutes. Turning 2FA off needs the password and a code or a recovery code.

If someone loses both the app and the recovery codes, an owner resets their second factor on the
panel's server:

```bash
docker compose exec panel /app/panel admin totp-reset <username>
```

The runbook describes it: [Two-factor authentication lockout](runbook.md#two-factor-authentication-lockout).

### How secrets are stored

- User and profile secrets, subscription links, TOTP secrets and the Telegram bot token are
  encrypted in the database with `MASTER_KEY` (AES-256-GCM). Without this key they cannot be
  decrypted, not even from a backup.
- Agent tokens, install tokens and the subscription page service token are stored only as SHA-256
  hashes. The token itself is shown
  once, so a copy of the database gives no working token. A subscription link also has its hash
  stored next to the encrypted copy: the panel uses it to find the user when the page is opened.
- Session cookies are signed with `SESSION_SECRET` and are `HttpOnly`. Every change needs the
  `X-CSRF-Token` header to match the CSRF cookie.
- `/metrics` shows server UUIDs and counts of servers and users. It is protected by
  `METRICS_TOKEN`, which is required when `NODE_DRIVER=gateway`. Give the token only to your
  Prometheus.
- Uploaded SVG images are checked and rejected if they contain scripts. Branding files are served
  with `Content-Security-Policy: default-src 'none'; style-src 'unsafe-inline'; sandbox` and
  `X-Content-Type-Options: nosniff`.
- On its own the panel calls out only to GitHub for the update check. Telegram, the webhook and
  backup upload are used only when you set them up.
- Never commit `.env`, `MASTER_KEY` or `SESSION_SECRET` to a repository.

## Backups and the master key

### Backups

Settings → Backups (owner only). "Create backup" makes a dump of the whole database with
`pg_dump`. "Nightly backup" makes one a day at the chosen hour (UTC) and keeps the chosen number
of them, 1 to 60; older ones are deleted. Backups made by hand are never deleted automatically.
Files are stored in `DATA_DIR/backups`, which is `/data/backups` on the `paneldata` volume in the
Docker setup. Each file can be downloaded from the table. One backup runs at a time.

A backup can also be made on the panel's server with
`docker compose exec panel /app/panel db backup`.
Optional protection is set in `.env`: encrypt each backup with age (`BACKUP_AGE_RECIPIENT`),
upload it to your storage (`BACKUP_UPLOAD_URL`), and test that it restores (`BACKUP_VERIFY`). See
[Environment variables](#environment-variables).

Restoring replaces the whole database, so it is a command-line operation with the panel stopped:
[Backups and restore](runbook.md#backups-and-restore).

### The master key

`MASTER_KEY` in `.env` encrypts the secrets in the database. A backup contains the database but
not this key. Keep a copy of `.env` somewhere other than the panel's server: without
`MASTER_KEY`, and the old `MASTER_KEY_V<n>` values if there are any, a restored database cannot
decrypt a single secret.

The key can be changed. `panel keys rotate` re-encrypts every secret, subscription links
included, with a new key in one transaction, with the panel stopped:

```bash
docker compose stop panel
docker compose run --rm panel keys rotate --dry-run   # counts rows, writes nothing
docker compose run --rm panel keys rotate             # re-encrypts, prints the lines to put in .env
docker compose up -d panel                            # after updating .env; start would keep the old key
```

It reads the new key from `MASTER_KEY_NEW` and refuses if it equals the current one. It never
prints keys. The full procedure: [Master key rotation](runbook.md#master-key-rotation).

## Installation details

The step-by-step install is in the [from-scratch guide](start.en.md) and the
[setup guide](setup.en.md). This section collects the details.

### Panel installer

```bash
curl -fsSL https://raw.githubusercontent.com/greenpandorik/tgproxy-panel/main/install.sh | sudo bash
```

The script asks for the domain, the e-mail for the certificate and the admin account, or takes
them as options. It installs Docker if needed, puts the files into `/opt/tgproxy-panel`,
generates `.env` with fresh secrets, starts the stack and creates the first administrator. At the
end it prints the panel address and, if you gave no password, the generated one, once. The
directory holds `docker-compose.yml`, `Caddyfile`, `.env` (mode 0600) and a copy of the script.

```bash
sudo /opt/tgproxy-panel/install.sh --update              # move to the latest release (or --version 1.2.0)
sudo /opt/tgproxy-panel/install.sh --uninstall           # stop the stack; asks before removing the data volumes
sudo /opt/tgproxy-panel/install.sh --uninstall --purge   # remove containers, volumes and the directory
cd /opt/tgproxy-panel && docker compose logs -f panel    # logs
```

Before changing anything, the script checks that the domain points at this server, that ports
80 and 443 are free (8080 in local mode) and that the install directory can be used. On a
terminal a failed check offers to re-run, continue or quit; without a terminal the script stops.
The script does not touch the firewall, so open the ports yourself. The full list of options is
in [Command line](#installsh). The published image is built for `linux/amd64` and `linux/arm64`.

Each release also ships `panel-linux-{amd64,arm64}` and `tgwp-agent-linux-{amd64,arm64}`
binaries with a `SHA256SUMS` file.

### Manual setup with Docker Compose

You need Docker with Compose v2 (`docker compose ...`) on the panel's server.

```bash
cd deploy
cp ../.env.example .env
# fill in MASTER_KEY and SESSION_SECRET (32 random bytes, base64 each):
sed -i.bak "s|^MASTER_KEY=.*|MASTER_KEY=$(openssl rand -base64 32)|" .env
sed -i.bak "s|^SESSION_SECRET=.*|SESSION_SECRET=$(openssl rand -base64 32)|" .env
sed -i.bak "s|^METRICS_TOKEN=.*|METRICS_TOKEN=$(openssl rand -hex 32)|" .env && rm .env.bak

# behind Caddy, on a real domain, with TLS:
docker compose up -d --build postgres panel caddy

# or, for local use without Caddy/TLS, expose the panel directly on :8080:
docker compose -f docker-compose.yml -f docker-compose.override.example.yml up -d --build postgres panel
```

For a real domain, set `PANEL_DOMAIN=panel.example.com` and
`PANEL_PUBLIC_URL=https://panel.example.com` in `.env`. With the default `PANEL_DOMAIN=localhost`
Caddy issues its own self-signed certificate. Either trust its local CA
(`docker compose exec caddy caddy trust`) or use `curl --insecure`.

To run a published image instead of building from the source, use
`deploy/docker-compose.release.yml`, the file the installer deploys. There the panel image is
`ghcr.io/greenpandorik/tgproxy-panel:${PANEL_VERSION:-latest}`, Caddy runs under the `caddy`
profile, and `deploy/docker-compose.local.yml` publishes port 8080 for local mode. Data lives in
the `pgdata`, `paneldata` and `caddydata` volumes.

### First administrator

The installer creates the first administrator itself. After a manual setup, create one in the
running panel container:

```bash
docker compose exec panel /app/panel admin create <username> <password>
```

A fourth argument sets the role: `owner` (the default), `admin` or `viewer`.

### Server install script

The "Add server" wizard ends with a command like this (`POST /api/v1/nodes` returns the same
command as `install_command`):

```
curl -fsSL https://panel.example.com/api/v1/install/<token>.sh | sudo bash
```

Run it as root on a fresh Ubuntu 22.04+ or Debian 12+ x86_64 server with a public IPv4 address.
The token works once and for 24 hours; a new command is on the Maintenance tab ("Show install
command"), and the old one stops working.

The script starts with read-only checks: the architecture is x86_64, systemd is running, the
panel's `/healthz` answers from this server, the public IP is known, the server's A record points
at it, and ports 80 and 443 are free. On a telemt server it also checks that the Fake-TLS port
is free and, as a warning only, that the Fake-TLS domain resolves. A failed DNS or port check
shows a menu:

```
  What now?  [r] re-run the checks   [c] continue anyway   [q] quit
```

Nothing is installed at that point. Without a terminal the script stops instead. Three variables
change its behaviour: `TGWP_SKIP_PREFLIGHT=1` continues despite failed checks,
`TGWP_DRY_RUN=1` stops right after them, and `TGWP_PUBLIC_IP=a.b.c.d` sets the public address.
They go before `bash`: `curl … | sudo TGWP_SKIP_PREFLIGHT=1 bash`.

Then the script installs the engine (see [Server engines](#server-engines)). Caddy must get a
certificate within 120 seconds, and on a telemt server telemt must report ready within 60
seconds. The server is registered with the panel only after that, as the last step, because
registration uses up the token. So if the script fails before the "Registration" step, fix the
cause and run the same command again.

## Environment variables

The panel reads its settings from `.env`. The file `.env.example` lists them all.

### Read by the panel

| Variable | Default | Meaning |
|---|---|---|
| `DATABASE_URL` | none, required | PostgreSQL connection string. In the Docker setup the compose file sets it |
| `MASTER_KEY` | none, required | 32 random bytes in base64. Encrypts secrets in the database |
| `MASTER_KEY_VERSION` | `1` | The version of the current master key |
| `MASTER_KEY_V<n>` | none | Old master keys, for every version below the current one |
| `MASTER_KEY_NEW` | none | Only for `panel keys rotate`: the new key |
| `SESSION_SECRET` | none, required | 32 random bytes in base64. Signs session cookies |
| `PANEL_HTTP_ADDR` | `:8080` | Where the panel listens for HTTP and gRPC |
| `PANEL_PUBLIC_URL` | `http://localhost:8080` | The panel's public address, used in install commands and by the agents. No trailing slash |
| `DATA_DIR` | `./data` (`/data` in the image) | The agent binary served to servers, and backups |
| `NODE_DRIVER` | `gateway` | `gateway` for real servers, `mock` for demos and tests |
| `METRICS_TOKEN` | none | Protects `/metrics`. Required with `gateway` |
| `TPROXY_COMMIT` | `52a5feb7fac38f68da5afef9cedd9b3bfc8473ca` | The `tproxy-server` commit for new tproxy servers, 7 to 40 lowercase hex characters |
| `TELEMT_VERSION` | `3.5.14` | The telemt release for telemt servers, like `3.5.7` |
| `TELEMT_SHA256_X86_64` | none, filled in `.env.example` | sha256 of `telemt-x86_64-linux-gnu.tar.gz` for that release. Required with `gateway`. Change it together with `TELEMT_VERSION` |
| `FEATURE_TOTP` | `true` | `false` hides two-factor login |
| `TRUST_FORWARDED_FOR` | `true` | Take the visitor's IP from `X-Forwarded-For`. Right when Caddy sits in front, since it rewrites the header. Local mode without Caddy sets `false`, otherwise the login attempt limit could be bypassed with a forged header |
| `LOG_LEVEL` | `info` | `debug`, `info`, `warn` or `error` |
| `APPLY_INTERVAL` | `45` | Seconds between applies to servers with changes. Settings → Notifications can override it |
| `OFFLINE_AFTER` | `90` | Seconds without a heartbeat before a server is offline. Settings → Notifications can override it |
| `GITHUB_REPO` | `greenpandorik/tgproxy-panel` | Where the update check reads releases and stars |
| `GITHUB_TOKEN` | none | Optional GitHub token without scopes, raises the API limit. Never logged |
| `UPDATE_CHECK` | `true` | `false` turns off every call to GitHub |
| `PROBE_TOKEN` | none | Token for external checks, at least 32 characters. Needs `PROBE_LOCATIONS` |
| `PROBE_LOCATIONS` | none | Names of the external check locations, separated by commas or spaces: letters, digits, `_` and `-` |
| `ALERT_WEBHOOK_URL` | none | HTTPS address for alerts, without login, query or fragment |
| `ALERT_WEBHOOK_SECRET` | none | HMAC secret for the webhook, at least 32 characters |
| `BACKUP_AGE_RECIPIENT` | none | age X25519 public key; each backup is encrypted to it |
| `BACKUP_UPLOAD_URL` | none | HTTPS address that accepts `PUT /<name>.dump.age`. Needs `BACKUP_AGE_RECIPIENT` |
| `BACKUP_UPLOAD_TOKEN` | none | Optional bearer token for the upload |
| `BACKUP_VERIFY` | `false` | `true` restores each new backup into a temporary database to test it. The database role needs `CREATEDB` |

The panel refuses to start when a required value is missing or a value has the wrong format,
and says which one.

### Read by Compose and tests

| Variable | Meaning |
|---|---|
| `PANEL_DOMAIN`, `ACME_EMAIL` | For Caddy: the domain to get a certificate for (`localhost` by default) and the e-mail for certificate notices |
| `POSTGRES_PASSWORD` | Password of the bundled PostgreSQL. The installer generates it, and `DATABASE_URL` is built from it |
| `PANEL_VERSION`, `PANEL_IMAGE` | For `deploy/docker-compose.release.yml`: the image tag (`1.0.0`, `latest`), or a full image reference for mirrors and tests. Written by `install.sh` |
| `TELEMT_SHA256_MUSL_X86_64` | sha256 of `telemt-x86_64-linux-musl.tar.gz`, used only by the `fakenode-telemt` demo image |
| `TEST_DATABASE_URL` | PostgreSQL connection string for `go test` |

### Read by the agent

The install script writes `/etc/tgwp-agent/agent.env` on each server.

| Variable | Meaning |
|---|---|
| `TGWP_PANEL_URL` | The panel's address |
| `TGWP_TOKEN` | The server's token. `TGWP_TOKEN_FILE` can point at a file instead |
| `TGWP_ENGINE` | `telemt` or `tproxy` (the default when unset) |
| `TGWP_TPROXY_VERSION` | On tproxy servers: the installed commit |
| `LOG_LEVEL` | Log level, as for the panel |

Paths and addresses have defaults and are changed only on test benches: `TGWP_STATE_DIR`,
`TGWP_TELEMT_API`, `TGWP_TELEMT_API_TOKEN`, `TGWP_TELEMT_API_TOKEN_FILE`, `TGWP_TELEMT_CONFIG`,
`TGWP_TELEMT_SITE_DIR`, `TGWP_TELEMT_BIN`, `TGWP_TELEMT_METRICS`, `TGWP_TPROXY_BIN`,
`TGWP_CONFIG`, `TGWP_PROFILES`, `TGWP_MTPROXY_ENV`, `TGWP_SITE_DIR`, `TGWP_RELAY_ADMIN`,
`TGWP_MTPROXY_STATS`.

### Read by install scripts and the probe

| Variable | Meaning |
|---|---|
| `TGWP_<NAME>` | Any option of the panel's `install.sh`, for example `TGWP_DOMAIN=panel.example.com TGWP_YES=1` |
| `TGWP_SKIP_PREFLIGHT`, `TGWP_DRY_RUN`, `TGWP_PUBLIC_IP` | The server install script, see [Server install script](#server-install-script) |
| `TGWP_PROBE_TOKEN` | For `tgwp-probe`: the value of the panel's `PROBE_TOKEN` |

### Read by the subscription page service

`install.sh --subpage` writes them into `/opt/tgproxy-subpage/.env`.

| Variable | Meaning |
|---|---|
| `SUBPAGE_PANEL_URL` | The panel address, such as `https://panel.example.com`. Required |
| `SUBPAGE_TOKEN` | The service token from Subscription → Service. Required |
| `SUBPAGE_LISTEN` | The address to listen on, `:8080` by default |
| `SUBPAGE_DOMAIN`, `ACME_EMAIL` | The page domain and the certificate e-mail, read by the service's Caddy |
| `SUBPAGE_VERSION`, `SUBPAGE_IMAGE` | Which image to run. The version should match the panel's |

## Command line

### `panel`

The panel program. In the Docker setup it is `/app/panel` inside the `panel` container: use
`docker compose exec panel /app/panel …` while the panel runs, and
`docker compose run --rm panel …` for commands that need it stopped. Every command first applies
pending database migrations.

| Command | What it does |
|---|---|
| `panel serve` | Runs the panel. This is the default. A second panel on the same database refuses to start |
| `panel migrate` | Applies database migrations and exits |
| `panel admin create <username> <password> [owner\|admin\|viewer]` | Creates an account, `owner` by default. The password needs at least 10 characters |
| `panel admin totp-reset <username>` | Turns off two-factor login for one account and deletes its recovery codes |
| `panel db backup` | Makes a backup, same as the button in Settings |
| `panel db restore <file> --yes` | Replaces the database with a backup. Refuses while the panel is running |
| `panel db verify <file>` | Restores a backup into a temporary database to test it; the working database is untouched |
| `panel keys rotate [--dry-run]` | Re-encrypts every secret with `MASTER_KEY_NEW`. Refuses while the panel is running |
| `panel subpage` | Runs the subscription page service instead of the panel. It needs no database and reads its settings from the `SUBPAGE_*` variables, see [Subscription pages on their own domain](#subscription-pages-on-their-own-domain) |

### `tgwp-agent`

The agent on each server. The `tgwp-agent.service` unit runs it without arguments.

| Command | What it does |
|---|---|
| `tgwp-agent` | Runs the agent with the settings from the environment |
| `tgwp-agent version` | Prints the version |
| `tgwp-agent upgrade [--telemt\|--agent] [--check] [--yes]` | Updates the server to what the panel expects, see [Updating servers](#updating-servers). `--env` sets another settings file; `--agent-bin` and `--telemt-bin` other binary paths |
| `tgwp-agent init-node --engine telemt\|tproxy …` | Writes the engine's configuration. The install script calls it |

`init-node` has two flags for test benches only: `--no-synlimit` (no nftables rules, for
containers without `CAP_NET_ADMIN`) and `--no-tls-emulation` (for a Fake-TLS domain that does not
resolve). Real servers run without them.

### `tgwp-probe`

The external check. Build it with `go build -o tgwp-probe ./cmd/probe` and run it in another
network, usually from the systemd timer in `deploy/probe`:

```bash
TGWP_PROBE_TOKEN=<PROBE_TOKEN> tgwp-probe --panel https://panel.example.com \
  --host proxy1.example.com --node <server UUID> --location isp-a
```

`--protocol-config /path/to/private.json` enables the built-in authenticated Fake-TLS and WEB
checks: a successful result requires a Telegram `resPQ` response matching the request nonce.
Keep the proxy test secret in a private file, not in arguments. See
[Authenticated probes](authenticated-probes.md) for the configuration and systemd setup.
Without configured credentials, authenticated checks remain `not_run`.

`--client-check /absolute/path` adds your own adapter that tests Fake-TLS and WEB with a real
client. It gets the domain on standard input and has 45 seconds to print up to 4096 bytes of
JSON. The server's UUID is in the address of its page in the panel.

### `install.sh`

The panel installer. Every option can also be given as an environment variable `TGWP_<NAME>`.

| Option | What it does |
|---|---|
| (none) | Install. If `/opt/tgproxy-panel/.env` exists, update instead |
| `--update` | Pull the image for `--version` (the latest release by default) and restart. Keeps `.env` and data, and moves the telemt pins to the release's values |
| `--uninstall` | Stop and remove the containers. Asks before removing the data volumes |
| `--purge` | With `--uninstall`: remove the volumes and the install directory without asking |
| `--domain <fqdn>` | The panel's domain |
| `--local` | No domain: no Caddy, no TLS, the panel on `http://<ip>:8080` |
| `--email <address>` | E-mail for certificate notices |
| `--admin-user <name>` | Name of the first account (role `owner`), `admin` by default |
| `--admin-password <pw>` | Its password. Generated (20 characters) and printed once if omitted |
| `--version <tag>` | Release to install, for example `1.0.0` |
| `--dir <path>` | Install directory, `/opt/tgproxy-panel` by default |
| `--image <ref>` | Another image reference, for mirrors and tests |
| `--from-checkout` | Use the files next to the script instead of downloading them |
| `--skip-preflight` | Skip the checks of DNS, ports and the install directory |
| `--sub-domain <fqdn>` | Also serve subscription pages on this domain, from the panel server. Its A record must point at this server too. `off` removes it |
| `--subpage` | Install, update (`--update`) or remove (`--uninstall`, `--purge`) the subscription page service instead of the panel. The default directory is `/opt/tgproxy-subpage` |
| `--panel-url <url>`, `--token <token>` | With `--subpage`: the panel address and the service token from Subscription → Service. With `--subpage`, `--domain` sets the page domain |
| `--yes` | Never ask. A missing required value is an error |
| `--help` | List the options |

## API for scripts

The web interface uses the same HTTP API as scripts. Requests go to `/api/v1/…` with the
session cookie that `POST /api/v1/auth/login` sets, and every change needs the `X-CSRF-Token`
header equal to the `tgwp_csrf` cookie. Some routes are useful on their own:

| Route | Access | What it returns |
|---|---|---|
| `GET /healthz` | Public | `ok` while the panel runs |
| `GET /metrics` | `METRICS_TOKEN` | Prometheus metrics, see [Monitoring](#monitoring) |
| `GET /s/{token}`, `GET /s/{token}.json` | Public | The subscription page, see [Subscription page](#subscription-page). A short address works in place of the token |
| `GET /api/v1/subscription-service` | Owner, admin | Settings and state of the subscription page service |
| `PUT /api/v1/subscription-service` | Owner | Save `{public_url, hide_on_panel}` |
| `POST /api/v1/subscription-service/token` | Owner | A new service token, answers `{token, command}` |
| `DELETE /api/v1/subscription-service/token` | Owner | Revoke the service token |
| `GET /api/v1/subpage/pages/{token}`, `POST /api/v1/subpage/heartbeat` | Service token | One page's data and the "online" signal, for the subscription page service |
| `GET /api/v1/keys` | Any role | The list of users. Parameters `q`, `type`, `node`, `state`, `page`, `per_page` |
| `GET /api/v1/keys/summary` | Any role | The counts for the state buttons above the list: `{total, active, expiring, expired, disabled, revoked}` |
| `POST /api/v1/keys`, `PATCH /api/v1/keys/{id}` | Owner, admin | Create or change a user |
| `POST /api/v1/keys/{id}/disable`, `POST /api/v1/keys/{id}/enable` | Owner, admin | Turn access off or on |
| `POST /api/v1/keys/bulk` | Owner, admin | One action on several users: `extend`, `disable`, `enable`, `revoke` or `delete` |
| `POST /api/v1/keys/import` | Owner, admin | Users from another proxy with their own secrets, all or none |
| `POST /api/v1/keys/{id}/subscription` | Owner, admin | A new subscription link in place of the old one, answers `{url, qr_data_uri}` |
| `GET /api/v1/keys/{id}/subscription/qr` | Owner, admin | A PNG with the QR code of the current subscription link |
| `GET /api/v1/keys/{id}/links` | Owner, admin | The user's direct links, grouped by server |
| `GET /api/v1/monitoring/overview?from&to&step` | Any role | Series for the Monitoring charts |
| `GET /api/v1/dashboard/trends` | Any role | The last day for Overview: `{people: [{t, people_online}], traffic_24h, traffic_prev_24h}` |
| `PUT /api/v1/nodes/order` | Owner, admin | Set the server order: `{ids: [...]}` |
| `GET /api/v1/alerts` | Any role | Open problems nobody has marked read |
| `POST /api/v1/alerts/resolve` | Owner, admin | Mark problems read: `{ids: [...]}`, answers `{resolved}` |
| `GET /api/v1/audit` | Any role | The activity log |
| `GET /api/v1/nodes/{id}/metrics` | Any role | The server's own proxy metrics in Prometheus format |
| `GET /api/v1/nodes/{id}/blocklist` | Any role | The server's blocklist, its state on the server and dropped-packet counters |
| `PUT /api/v1/nodes/{id}/blocklist` | Owner, admin | Replace the blocklist: `{revision, entries: [{prefix, note}]}`, up to 2000 entries; 409 when the list changed after `revision` |
| `GET /api/v1/status/update` | Any role | The update check result |
| `GET /api/v1/node/upgrade` | Server token | What a server should run, for `tgwp-agent upgrade` |

`GET /api/v1/keys/{id}/links` answers
`{items: [{node_id, node_name, hostname, engine, links: [{kind, domain, tme, tg}]}], client_support}`.
`kind` is `web` or `tls`, and `domain` names the masking domain of a `tls` link.

In API answers a user is a key object. Besides the fields it always had (`id`, `label`, `type`,
`owner_label`, `status`, `expires_at`, `nodes` and others) it has:

| Field | Meaning |
|---|---|
| `state` | `active`, `pending`, `disabled`, `expired` or `revoked`, as in the list |
| `disabled_at` | When access was turned off, or `null` |
| `last_seen_at` | When the person last connected, or `null` |
| `sub_slug` | The short address of shared access, or `null` |
| `subscription_url`, `subscription_short_url` | The subscription link and the short link, or `null`. Sent only to the owner and admins |
| `subscription_legacy` | `true` when the link was issued by an earlier version of the panel and cannot be shown. Also only for the owner and admins |
| `live` | `{online, connections, devices, devices_15m, ips}` across all telemt servers: `online` when there is at least one connection, `devices` the distinct IP addresses right now, `devices_15m` over the last 15 minutes; `ips` repeats `devices` |

The `state` filter of `GET /api/v1/keys` takes `active`, `pending`, `expiring` (the end date is
within the next 7 days), `expired`, `disabled` and `revoked`. `POST /api/v1/keys` and
`PATCH /api/v1/keys/{id}` take `sub_slug`, for shared access only; an address that is taken
answers 422 with `sub_slug: "taken"` in `error.fields`. `POST /api/v1/keys/bulk` takes
`{action, ids, expires_at}` with 1 to 500 ids, and `expires_at` is needed only for `extend`.
`POST /api/v1/keys/import` takes the fields of `POST /api/v1/keys` plus
`items: [{label, owner_label, secret}]`, 1 to 500 of them, each secret 32 hex characters. A bad
line answers 422 with `items.<index>` in `error.fields`, including a secret another user already
has; nothing is created then.
`GET /api/v1/keys/{id}/subscription/qr` takes `size` from 128 to 1024 (256 by default), and with
`short=1` it draws the short address.

`GET /api/v1/monitoring/overview` returns
`{nodes: [{node_id, node_name, hostname, status, cert_expires_at}], series: {<node_id>: [{t, sessions_live, streams_live, people_online, bytes_up_rate, bytes_down_rate}]}, fleet: {people_online, people_online_15m, connections}}`.
`from` and `to` are RFC 3339 times, the last 24 hours by default. The span is at most 31 days,
since snapshots are kept for 30; a wider span answers 400. Rates are bytes per second, computed
from neighbouring snapshots, and a counter reset (a relay restart) gives 0. A `step` above 60
seconds makes the database group the data into buckets of that size. Each server gets at most
600 points. The same span limit applies to `GET /api/v1/monitoring/nodes/{id}/series`, whose
points carry `people_online` too. `people_online` is `null` for snapshots taken before the panel
counted people, and the `fleet` numbers are `null` when the newest count is older than three
minutes. `GET /api/v1/dashboard/summary` has the same `people_online` and `people_online_15m`
next to `sessions_live`, and every server in `GET /api/v1/nodes` and `GET /api/v1/nodes/{id}`
has `people_online` and `connections` from its latest snapshot of the last three minutes, or
`null`. Servers marked offline have none, and the summary's `sessions_live`, `streams_live`,
`bytes_up` and `bytes_down` add up only the servers that do. `cert_expires_at` is when the TLS
certificate on port 443 expires, from the newest diagnostics pass that read it, or `null`. A
server in `GET /api/v1/nodes` also has `dirty_since`, when its unapplied changes were first made,
or `null` when none wait or the time is not known.

`PUT /api/v1/nodes/order` puts the listed servers first, in the order given, and keeps the rest
after them in their order; ids that name no server are skipped. An empty list, more than 1000
ids or the same id twice answers 400. It answers `{ids}` with every server in the new order.

`GET /api/v1/dashboard/trends` gives people online over the last 24 hours in 20-minute steps,
and the traffic of all servers over the last 24 hours as the sum of their counter steps, so a
restart does not lose it. `traffic_prev_24h` is the same sum for the 24 hours before and is
`null` unless the stored history covers that whole day; `traffic_24h` is `null` until a server
has two readings.

`POST /api/v1/alerts/resolve` takes 1 to 500 ids and marks those that are still open and unread;
it answers how many it marked. A problem raised after the list was loaded is not in it and stays.
A read problem leaves `GET /api/v1/alerts` and the summary but stays open until it clears.
`POST /api/v1/alerts/{id}/resolve` does the same for one id. `GET /api/v1/fleet/updates` also
gives `version`, the telemt build a rollout installs.

`GET /api/v1/audit` takes these parameters:

| Parameter | Meaning |
|---|---|
| `action` | The beginning of the action name: `action=key.` matches `key.create`, `key.revoke` and so on |
| `user` | Exact user name |
| `from`, `to` | RFC 3339 times, both ends included |
| `page`, `per_page` | Paging. `per_page` is at most 200, `page` at most 100000 |

## Development and tests

You need Go 1.26+, Node.js 22+ and PostgreSQL 16. Create the `tgwp` and `tgwp_test` databases and
copy `.env.example` to `.env`. [CONTRIBUTING.md](../CONTRIBUTING.md) lists the checks to run
before sending changes, the conventions and the release procedure.

| Command | What it does |
|---|---|
| `make tools` | Installs sqlc, gofumpt, golangci-lint and the protoc plugins |
| `make web` | Builds the web interface into `web/dist`, which is built into the binary |
| `make run` | `go run ./cmd/panel serve` with your local `.env` |
| `cd web && npm run dev` | The web interface with hot reload; `/api` goes to `:8080` |
| `make test` | `go test -p 1 ./...` |
| `make lint` | `golangci-lint run ./...` |
| `make build` | The panel binary for this machine. `make build VERSION=v1.2.3` sets the version |
| `make e2e`, `make e2e-telemt` | End-to-end tests with a demo server, see below |
| `make test-install` | Tests `install.sh` inside a `docker:27-dind` container |
| `make test-node-preflight` | Tests the server install script's checks inside `ubuntu:24.04` |
| `make test-agent-upgrade` | Tests `tgwp-agent upgrade` against a stub panel |

Go tests run one package at a time (`-p 1`) because all packages share one test database.

### Demo servers and the end-to-end test

Both engines have a demo server in a container, so the whole path from the panel through the
agent to the proxy can be tested without a VPS:

```bash
make e2e          # tproxy engine: deploy/Dockerfile.fakenode (real tproxy-server + a stub MTProxy)
make e2e-telemt   # telemt engine: deploy/Dockerfile.fakenode-telemt (the real telemt release binary)
```

Each target starts PostgreSQL and the panel on `:8080`, creates an administrator and a server,
runs the demo server with a fresh install token, creates a user, applies it and checks that the
proxy received it. The tproxy run also assigns the `corporate` site. At the end it prints
`SMOKE OK` and removes the stack with its volumes.

To keep the telemt stack running and look around, run the steps yourself:
`deploy/e2e-smoke-telemt.sh` prints an install token,
`docker compose --profile dev-telemt up -d --build fakenode-telemt` starts the server, and
`deploy/e2e-smoke-telemt.sh --continue` runs the checks. `docker compose logs fakenode-telemt`
shows the server's start, and `docker compose exec fakenode-telemt journalctl -u telemt` its
telemt log.

The telemt image downloads `telemt-x86_64-linux-musl.tar.gz` and checks it against
`TELEMT_SHA256_MUSL_X86_64`. telemt is built for x86_64 only, so on an arm64 machine the
container runs emulated and slower. The demo server differs from a real one in three ways, all
because of the container: it has no Caddy, and it runs `init-node` with `--no-synlimit` and
`--no-tls-emulation`.

`make e2e-telemt` needs outgoing access to Telegram's data centres. Until telemt reaches one, its
`/v1/health/ready` answers `no_healthy_upstreams` and the server never leaves `pending`.
`docker compose exec fakenode-telemt journalctl -u telemt` shows the cause.
