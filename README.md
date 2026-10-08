<picture>
  <source media="(prefers-color-scheme: dark)" srcset="docs/brand/logo-dark.png">
  <img src="docs/brand/logo-light.png" alt="TGProxy Panel" height="32">
</picture>

# TGProxy Panel

[![CI](https://github.com/greenpandorik/tgproxy-panel/actions/workflows/ci.yml/badge.svg)](https://github.com/greenpandorik/tgproxy-panel/actions/workflows/ci.yml)
[![Release](https://img.shields.io/github/v/release/greenpandorik/tgproxy-panel?sort=semver)](https://github.com/greenpandorik/tgproxy-panel/releases)
[![Go](https://img.shields.io/github/go-mod/go-version/greenpandorik/tgproxy-panel)](go.mod)
[![License](https://img.shields.io/badge/license-AGPL--3.0-blue)](LICENSE)

**English** · [Русский](README.ru.md) · [Website](https://tgproxypanel.com/en/)

TGProxy Panel lets you run your own Telegram proxies and manage them from a browser. You install
the panel on one server, and it sets up the proxies on the others: it gives people ready-made
links and QR codes, hides every proxy behind an ordinary-looking website and keeps an eye on all
of it.

The panel is free and open source. It installs with one command, and the interface is in English
and Russian.

<p align="center">
  <img src="docs/screenshots/issue-a-key.gif" width="90%" alt="Giving access: name the user, bind them to two servers, and get a WEB link and a Fake-TLS link with QR codes">
</p>

<p align="center"><sub>Giving access: enter a name, pick the servers and get the links with QR codes.</sub></p>

## What it does

- Gives access with one link. Every user has a subscription link: the person opens it, picks
  their device and connects the proxy step by step. The proxy links themselves come in two
  kinds. Fake-TLS works in every Telegram app. The WEB proxy runs over plain HTTPS and is harder
  to block, but for now only Telegram Desktop and recent Android versions understand it. Each
  link has a QR code. Subscription pages can open on a domain of their own, even from a separate
  server, so people never see the panel's domain.
- Disguises the proxy as a website. The domain of every proxy server opens an ordinary site, such
  as a coffee shop or a blog. Fifteen ready-made sites are included, and each server gets its own
  slightly rearranged copy, so servers can't be matched by identical pages.
- Limits access. Every user can have an expiry date, and on telemt servers also a traffic quota,
  a speed limit and a cap on unique IP addresses. Access can be turned off for a while, and an
  expired date can be extended, with the link staying the same.
- Keeps people connected. New users and access changes are applied without restarting the proxy,
  so connected people don't notice.
- Watches the servers. The overview tells you straight away what is broken and where. The panel
  checks DNS, ports and certificates, sends alerts to Telegram and exports metrics for Prometheus
  and Grafana.
- Upgrades with one command. If a new proxy version fails to start, the server goes back to the
  previous one on its own.
- Protects access. Two-factor sign-in, an activity log of every admin action and scheduled daily
  database backups.

<p align="center">
  <img src="docs/screenshots/dashboard.png" width="49%" alt="Overview: what needs attention, people online and every server at a glance">
  <img src="docs/screenshots/websites.png" width="49%" alt="Cover websites: fifteen ready-made sites">
</p>

## What you need

- Two VPS with Ubuntu 22.04+ or Debian 12+: one for the panel and one for the proxy. The panel
  is fine with 1 CPU and 1 GB of RAM. The proxy server needs ports 80 and 443, so it can't share
  a VPS with the panel. You can add as many proxy servers later as you like.
- A domain such as `example.com`, with DNS records that point at your servers.
- SSH access to the servers as root.

If some of these words are new to you, start with the [From scratch](docs/start.en.md) guide. It
walks through everything step by step: which server to rent, how to buy a domain, how to connect
to a server and how to send a friend your first link.

## Quick install

1. On the panel server, run:

   ```bash
   curl -fsSL https://raw.githubusercontent.com/greenpandorik/tgproxy-panel/main/install.sh | sudo bash
   ```

   The script asks for the panel's domain, an e-mail for the certificate and the admin
   credentials. At the end it prints the panel address, and the password if you didn't set one.

2. In the panel, open Servers → Add server and enter the proxy server's domain. The panel gives
   you a command: run it on the proxy server. A couple of minutes later the server shows up in the
   list.

3. Open Users → New user, pick the servers and send the person the subscription link or its QR
   code.

To upgrade the panel: `sudo /opt/tgproxy-panel/install.sh --update`. To upgrade a proxy server:
`tgwp-agent upgrade` on that server.

## Documentation

| Document | Who it is for |
|---|---|
| [From scratch](docs/start.en.md) | Renting a server for the first time: every step, plus a glossary |
| [Installation](docs/setup.en.md) | Comfortable with Linux: installing the panel and servers in detail, every option |
| [Reference](docs/reference.md) | How it all works: engines, panel screens, environment variables |
| [Runbook](docs/runbook.md) | Upgrades, backups and what to do when something breaks |
| [Monitoring](docs/monitoring.md) | Prometheus metrics and the Grafana dashboard |
| [Contributing](CONTRIBUTING.md) | Building the project, running the tests and sending changes |
| [Security](SECURITY.md) | How to report a vulnerability |

Questions and ideas go to [Discussions](https://github.com/greenpandorik/tgproxy-panel/discussions).

## Credits

[telemt](https://github.com/telemt/telemt) is the proxy that runs on the servers. One telemt
process serves the WEB proxy, Fake-TLS, the cover site and the control API. The panel installs a
specific vetted release and checks its checksum. telemt is distributed under its own TELEMT PL 3
licence, whose text ships with the program.

[MTPROTO_FIX_By_MEKO](https://github.com/Mekotofeuka/MTPROTO_FIX_By_MEKO) is the fix for the
SYN-flood connection problems Telegram proxies started hitting in June 2026. telemt carries it as
`synlimit`, and the panel turns it on for every server. The network settings the installer writes
to `/etc/sysctl.d/90-tgwp.conf` come from the same place.

[tproxy-server](https://github.com/telegramdesktop/tproxy-server) and the official MTProxy run on
servers with the older tproxy engine.

[Remnawave](https://github.com/remnawave/panel) is the panel whose look this interface borrows:
the floating sectioned menu, the metric cards and the dark palette with a cyan accent. The
interface code is our own.

## Give it a star

If the panel is useful to you, [give it a star](https://github.com/greenpandorik/tgproxy-panel):
that is how other people setting up Telegram proxies find the project.

## Licence

[AGPL-3.0](LICENSE).
