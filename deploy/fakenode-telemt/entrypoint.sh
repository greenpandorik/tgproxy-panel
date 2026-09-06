#!/usr/bin/env bash
# Entry point for the fakenode-telemt image: brings up the REAL telemt binary under the
# systemctl/journalctl shims and registers the container with the panel as a telemt node.
#
# There is no Caddy here. On a real node Caddy only terminates TLS on 443 and forwards to
# telemt's loopback WEB listener; the panel never talks to it, and the e2e drives the node
# through the agent, so the container would gain nothing but a certificate it cannot get.
#
# The node's first profile secret is the one the panel already baked into the install script,
# so instead of inventing one the entrypoint fetches GET /api/v1/install/<token>.sh and reads
# the init-node arguments back out of it. Fetching the script does not consume the install
# token (only .../register does), so this is exactly the sequence a real node performs, minus
# the apt/download half the image has already done at build time.
set -euo pipefail

PANEL_URL="${PANEL_URL:?PANEL_URL is required}"
TELEMT_VERSION="${TELEMT_VERSION:-unknown}"

SITE_SRC=/srv/fakenode-telemt-site
AGENT_ENV=/etc/tgwp-agent/agent.env
TOKEN_FILE=/etc/telemt/api.token

echo "fakenode-telemt: waiting for panel at $PANEL_URL"
until curl -sf "$PANEL_URL/healthz" >/dev/null 2>&1; do
	sleep 1
done
echo "fakenode-telemt: panel is up"

# telemt needs a concrete address for web.vhosts.public_addr. On a VPS the install script
# asks ipify; in compose the container's address on the bridge network is the honest answer,
# and it is what the panel stores as the node's public_ip.
PUBLIC_IP="$(hostname -i | awk '{print $1}')"
[[ -n "$PUBLIC_IP" ]] || {
	echo "fakenode-telemt: could not determine the container IP" >&2
	exit 1
}
echo "fakenode-telemt: container ip $PUBLIC_IP"

: "${INSTALL_TOKEN:?INSTALL_TOKEN is required}"
echo "fakenode-telemt: fetching the install script for its init-node arguments"
SCRIPT="$(curl -fsS "$PANEL_URL/api/v1/install/$INSTALL_TOKEN.sh")"

# Every value in the script is a single-quoted shell word (nodeinstall.sq). None of the
# fields read here can contain a quote - the panel validates hostname, user name, secret and
# port before rendering - so matching the whole line is enough and nothing is eval'd.
script_var() { sed -n "s/^$1='\\(.*\\)'\$/\\1/p" <<<"$SCRIPT" | head -n1; }

NODE_HOSTNAME="$(script_var NODE_HOSTNAME)"
WEB_USER="$(script_var WEB_USER)"
WEB_SECRET="$(script_var WEB_SECRET)"
TLS_DOMAIN="$(script_var TLS_DOMAIN)"
CLASSIC_PORT="$(script_var CLASSIC_PORT)"
for var in NODE_HOSTNAME WEB_USER WEB_SECRET TLS_DOMAIN CLASSIC_PORT; do
	if [[ -z "${!var}" ]]; then
		echo "fakenode-telemt: install script has no $var - is the node's engine telemt?" >&2
		exit 1
	fi
done
echo "fakenode-telemt: node $NODE_HOSTNAME (tls $TLS_DOMAIN, classic port $CLASSIC_PORT, user $WEB_USER)"

mkdir -p "$SITE_SRC" /etc/tgwp-agent /var/lib/tgwp-agent
cat >"$SITE_SRC/index.html" <<'EOF'
<!doctype html>
<html lang="en"><head><meta charset="utf-8"><title>fakenode-telemt</title></head>
<body><h1>fakenode-telemt placeholder site</h1></body></html>
EOF

# Two flags are the only differences from a real node's config.
#
# --no-synlimit: telemt installs its own nftables rules for the Fake-TLS listener at startup
# and aborts before the accept loops if it cannot (CAP_NET_ADMIN plus a writable netfilter
# namespace, neither of which an unprivileged container has).
#
# --no-tls-emulation: telemt learns the TLS fingerprint of the real tls_domain by connecting
# to it on 443. The bench domain resolves nowhere, so the fetch falls back to a built-in fake
# profile - which telemt tolerates at startup but refuses on a runtime reload ("TLS-front
# profiles are not ready for domains: ..."), and every apply would then roll back.
#
# Everything else - listeners, vhost, decoy, control API, limits - is the config a real telemt
# node runs.
echo "fakenode-telemt: running init-node"
tgwp-agent init-node --engine telemt \
	--hostname "$NODE_HOSTNAME" --public-ip "$PUBLIC_IP" \
	--tls-domain "$TLS_DOMAIN" --classic-port "$CLASSIC_PORT" \
	--web-user "$WEB_USER" --web-secret "$WEB_SECRET" \
	--site-dir "$SITE_SRC" --no-synlimit --no-tls-emulation

echo "fakenode-telemt: starting telemt"
systemctl daemon-reload
systemctl start telemt

# Listening is not the same as able to serve, so the wait is on /v1/health/ready, exactly as
# the install script does it. The token is read from the file init-node wrote, is never echoed,
# and goes in on stdin (-H @-) rather than as an argument so it never appears in
# /proc/*/cmdline.
TELEMT_API_TOKEN="$(tr -d '\r\n' <"$TOKEN_FILE")"
ready=0
for _ in $(seq 1 90); do
	code="$(curl -s -o /dev/null -w '%{http_code}' -H @- \
		http://127.0.0.1:9091/v1/health/ready <<<"Authorization: $TELEMT_API_TOKEN")" || code=000
	if [[ "$code" == "200" ]]; then
		ready=1
		break
	fi
	if ! systemctl is-active telemt >/dev/null 2>&1; then
		echo "fakenode-telemt: telemt exited during startup; log follows" >&2
		tail -n 200 /var/log/fake/telemt.log >&2 || true
		exit 1
	fi
	sleep 1
done
if [[ "$ready" -ne 1 ]]; then
	echo "fakenode-telemt: telemt did not report ready within 90s; log follows" >&2
	tail -n 200 /var/log/fake/telemt.log >&2 || true
	exit 1
fi
unset TELEMT_API_TOKEN
echo "fakenode-telemt: telemt is ready"

if [[ -n "${NODE_TOKEN:-}" ]]; then
	echo "fakenode-telemt: NODE_TOKEN provided, skipping registration"
	TOKEN="$NODE_TOKEN"
else
	echo "fakenode-telemt: registering with the panel"
	AGENT_VERSION="$(tgwp-agent version)"
	REG="$(curl -fsS -X POST -H 'Content-Type: application/json' \
		-d "{\"hostname\":\"$NODE_HOSTNAME\",\"public_ip\":\"$PUBLIC_IP\",\"tproxy_version\":\"telemt $TELEMT_VERSION\",\"agent_version\":\"$AGENT_VERSION\"}" \
		"$PANEL_URL/api/v1/install/$INSTALL_TOKEN/register")"
	TOKEN="$(printf '%s' "$REG" | sed -n 's/.*"token":"\([^"]*\)".*/\1/p')"
	if [[ -z "$TOKEN" ]]; then
		echo "fakenode-telemt: registration failed: $REG" >&2
		exit 1
	fi
fi

umask 077
cat >"$AGENT_ENV" <<EOF
TGWP_PANEL_URL=$PANEL_URL
TGWP_TOKEN=$TOKEN
TGWP_ENGINE=telemt
EOF
umask 022

echo "fakenode-telemt: starting tgwp-agent"
set -a
# shellcheck disable=SC1090
. "$AGENT_ENV"
set +a
exec tgwp-agent
