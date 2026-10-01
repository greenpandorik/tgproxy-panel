# From scratch: from renting a server to your first link

**English** · [Русский](start.ru.md)

This guide is for people who have never rented a server. We will go all the way, step by step:
rent two servers, buy a domain, install the panel and send a friend a link to your own Telegram
proxy. If a word is unclear, check the [glossary](#glossary) at the end.

You don't need to know how to program. You need a computer with Windows 10 or later or macOS, a
bank card to pay for the servers and the domain, and about an hour. Most of that hour is waiting
for the domain to start working.

## How it fits together

You need two servers. The first one runs the panel: a website you open in your browser to manage
everything else. The second one runs the proxy itself, which people use to reach Telegram. The
panel sets up the proxy server on its own; you only log in to it to install and upgrade.

```
You, browser ─────────> Panel (server 1)
                            │ sets up
                            ▼
Friends, Telegram ────> Proxy (server 2) ─────> Telegram
```

You can connect as many proxy servers as you like, for example in different countries. We start
with one.

## 1. Rent two servers

A server you rent is called a VPS: a computer in a data centre that you manage over the internet.
Almost every hosting company sells them; search for "VPS" or "virtual server". The cheapest VPS
plans cost a few dollars a month, and that is enough for both the panel and the proxy.

When you order, pick this for both servers:

- the Ubuntu 24.04 or 22.04 operating system (Debian 12 works too);
- the x86_64 architecture, also called amd64 or just x86. ARM servers won't work for the proxy;
- a public IPv4 address. It is usually included;
- the smallest plan: 1 CPU and 1 GB of RAM.

Rent the proxy server in a country where Telegram works without restrictions. The panel server
can be anywhere.

After you pay, the hosting company shows in your account, or sends by e-mail, the server's IP
address (four numbers separated by dots, such as `203.0.113.10`) and the root password. Write down
the IP addresses of both servers and note which one is for the panel and which one for the proxy.

## 2. Buy a domain and point it at the servers

A domain is a website name, such as `example.com`. You buy it from a domain registrar; a search
will find plenty. Simple domains start at a couple of dollars a year.

The panel and the proxy need names of their own. The easy way is to make them subdomains of one
domain: `panel.example.com` for the panel and `proxy1.example.com` for the proxy.

> It is better to use a separate domain for the proxy, one not connected to the panel. Website
> certificates are published in public logs, so one domain is enough to find all of its
> subdomains. If the panel's sign-in page sits next to the proxy, the proxy is easier to spot. One
> domain is fine for a first try; for real use, buy a second one.

Now tell the internet which IP address is behind each name. A domain has DNS settings for that,
with records of type A. At your registrar, open the section called "DNS", "DNS management" or
"DNS records" and add two records:

| Type | Name (host) | Value |
|---|---|---|
| A | `panel` | IP address of the panel server |
| A | `proxy1` | IP address of the proxy server |

Put only the first part in the "Name" field, without the domain: the registrar adds
`.example.com` itself.

New records start working within a few minutes, sometimes a couple of hours. You can check from
your computer. On Windows, open "Terminal" (right-click the Start button → "Terminal"); on macOS,
open the Terminal app. Then run:

```
nslookup panel.example.com
```

If the `Address` line shows your server's IP, the record works. Check `proxy1.example.com` the
same way. Don't install anything until both records work: the installers check them first and
will stop.

## 3. Open the ports

Some hosting companies have a network filter in your account, called "Firewall" or "Security
groups". If it exists and is turned on, allow incoming TCP connections:

- on the panel server: ports 22, 80 and 443;
- on the proxy server: ports 22, 80, 443 and 8443.

Port 22 lets you log in to the server, 80 and 443 are for the website and its certificate, and
8443 is for Fake-TLS links. If your account has no such section, there is nothing to do.

## 4. Connect to the server

You work with a server over SSH: you type commands in a terminal on your computer, and they run on
the server. There is nothing to install: SSH is built into Windows 10 and later and into macOS.

Open a terminal the same way as in step 2 and type `ssh root@` followed by the panel server's IP
address:

```
ssh root@203.0.113.10
```

On the first connection the terminal asks whether you trust this server
(`Are you sure you want to continue connecting`). Type `yes` and press Enter. Then type the root
password from the hosting company's e-mail. The characters don't show while you type the
password, and that is normal: just type it and press Enter.

When you see something like `root@server-name:~#`, you are on the server. Pasting commands is
easier than typing them: copy a command from this guide and paste it into the terminal with
Ctrl+V or a right-click on Windows, or Cmd+V on macOS. The `exit` command logs you out.

If the hosting company gave you another user instead of root, such as `ubuntu`, connect as
`ssh ubuntu@203.0.113.10`. The install commands below start with `sudo`, so they work that way
too.

## 5. Install the panel

Connect to the panel server and run:

```bash
curl -fsSL https://raw.githubusercontent.com/greenpandorik/tgproxy-panel/main/install.sh | sudo bash
```

The installer asks a few questions:

| Question | What to answer |
|---|---|
| `Panel domain` | The panel name from step 2, such as `panel.example.com` |
| `E-mail for Let's Encrypt` | Your e-mail. You get a message there if something goes wrong with the site's certificate |
| `Admin username` | The login for the panel. Press Enter to keep `admin` |
| `Admin password` | At least 10 characters; they don't show while you type. The installer asks you to repeat it. You can also just press Enter and it makes up a password for you |

The installer does the rest: it checks that the domain points at this server and the ports are
free, installs what it needs and gets a certificate for the site. That takes a few minutes. If a
check fails, it explains why and offers to retry (`r`), continue (`c`) or quit (`q`). Usually it
is enough to wait until the DNS record works and press `r`.

At the end the installer prints the panel address and, if you didn't set a password, the one it
generated. It is shown only once, so save it right away.

Open the panel address in your browser, such as `https://panel.example.com`, and sign in.

We recommend turning on two-factor sign-in right away: signing in to the panel will then need a
code from an app on your phone as well as the password. Open the user menu in the top right
corner → "Password and 2FA" and click "Turn on". The panel shows a QR code for your authenticator app and
recovery codes: save them, because without them you can't sign in if you lose your phone.

## 6. Connect the proxy server

In the panel, open "Servers" and click "Add server". Fill in three fields:

- "Name": what the server is called in the panel, such as "Amsterdam";
- "Hostname": the proxy name from step 2, such as `proxy1.example.com`;
- "Let's Encrypt e-mail": your e-mail.

Leave the other fields as they are and click "Create server". The panel shows the install
command. It works once and is valid for 24 hours. Copy it with the button next to it.

Keep the panel window open. Open a second terminal, connect to the proxy server as in step 4 and
paste the command. The installer checks DNS, the ports and the server, installs the proxy and
gets a certificate. A couple of minutes later the panel window changes to "Agent connected", and
the server appears in the list with a green dot.

## 7. Create your first user

In the panel, access to the proxy is given to users. A user can be one person or a group of
people. Open "Users" and click "New user". On the "One person" tab, fill in:

- "Name", so you can find the person in the list later, such as "Anna";
- "Servers" in the "Access" card: tick your proxy server.

You can leave the other fields alone. Click "Create". The user's window opens with their
subscription link. The user starts working in under a minute, once the panel sends them to the
server.

The subscription link leads to a page where every server has "Connect via Fake-TLS" and "Connect
via WEB" buttons. You can copy it or show it with "QR code". The separate proxy links are under
"Direct proxy links", and there are two of them:

- Fake-TLS works in every Telegram app. If in doubt, send this one.
- The WEB proxy is harder to block, but for now it only works in Telegram Desktop and recent
  versions of Telegram for Android.

## 8. Connect from Telegram

Send the subscription link to the person in any messenger, or show them the QR code. When they
open the link or scan the QR code with the phone camera, a page with the steps for their device
opens. There they tap "Connect" next to the server, and Telegram asks whether to connect the
proxy. All that's left is to tap "Connect" once more.

Try it yourself: open the link on your phone. In the mobile Telegram app the connected proxy
shows in the settings, under "Data and Storage" → "Proxy".

That's it: you have your own panel and your own proxy.

## What next

Save a copy of the panel's secrets file. It holds the master key that encrypts the secrets in the
database: without it they can't be recovered even from a backup. If you connected as root,
run this on your computer, not on the server:

```
scp root@203.0.113.10:/opt/tgproxy-panel/.env ./tgproxy-panel.env
```

The file `tgproxy-panel.env` appears in the folder the terminal is open in. Keep it somewhere safe
and don't show it to anyone.

Turn on database backups: "Settings" → "Backups".

Upgrade the panel with a command on its server:

```bash
sudo /opt/tgproxy-panel/install.sh --update
```

and a proxy server with a command on that server:

```bash
sudo tgwp-agent upgrade
```

If something doesn't work, open the server in the panel and the "Server checks" row on its
Health tab: "Full server check" shows what is wrong. The [full installation guide](setup.en.md) has more detail, and you
can ask questions in [Discussions](https://github.com/greenpandorik/tgproxy-panel/discussions).

## Glossary

| Term | What it means |
|---|---|
| VPS | A server you rent from a hosting company. It runs around the clock in a data centre, and you manage it over the internet. |
| IP address | The server's number on the internet, such as `203.0.113.10`. |
| Domain | A website name used instead of an IP address, such as `example.com`. A subdomain is a name inside a domain, such as `panel.example.com`. |
| DNS and A record | DNS is the internet's directory that turns a name into an IP address. An A record in it says "the name `panel.example.com` is the server `203.0.113.10`". |
| Port | A numbered "door" on the server. Different programs accept connections on different ports: a website on 443, SSH on 22. |
| SSH and terminal | A terminal is a window where you type commands. SSH is the way to run those commands on a remote server. |
| root | The server's main user, allowed to do anything. The installation needs its rights. |
| Certificate | What lets a website work over `https://`. The panel and the proxy get one on their own, for free, from Let's Encrypt. |
| MTProxy | A proxy that Telegram understands by itself, with no extra apps. You connect with a link. |
| Fake-TLS | A kind of MTProxy that looks like an ordinary website visit from the outside. |
| WEB proxy | A newer kind of Telegram proxy. It works over plain HTTPS on port 443. |
| Cover site | An ordinary website that opens on the proxy's domain. A stranger who visits the address sees it instead of the proxy. |
| User | Access to the proxy for one person or a group. Each user has their own subscription link and can be turned off or revoked without touching the others. |
| Subscription link | One link to a page with connect buttons for all of the user's servers. |
