#!/usr/bin/env bash
# Entry point for the fakenode image: brings up the real tproxy-server relay
# and the stub MTProxy backend under the systemctl/journalctl shims, then
# registers with the panel (or reuses NODE_TOKEN) and execs the real agent.
set -euo pipefail

PANEL_URL="${PANEL_URL:?PANEL_URL is required}"
NODE_HOSTNAME="${NODE_HOSTNAME:-fakenode.local}"
TPROXY_COMMIT="${TPROXY_COMMIT:-unknown}"

CONFIG_DIR=/etc/tproxy-server
CONFIG_FILE="$CONFIG_DIR/config.json"
PROFILES_FILE="$CONFIG_DIR/profiles.json"
MTPROXY_ENV=/etc/mtproxy/mtproxy.env
SITE_DIR=/srv/tproxy-site
AGENT_ENV=/etc/tgwp-agent/agent.env

echo "fakenode: waiting for panel at $PANEL_URL"
until curl -sf "$PANEL_URL/healthz" >/dev/null 2>&1; do
	sleep 1
done
echo "fakenode: panel is up"

mkdir -p "$CONFIG_DIR" /etc/mtproxy "$SITE_DIR" /etc/tgwp-agent /var/lib/tgwp-agent

# Seed a placeholder profile so the real relay has something valid to serve
# before the panel ever applies the node's real profiles. The secret must be
# 16 bytes hex to satisfy tproxy-server's DecodeSecret.
SECRET="${INIT_SECRET:-$(head -c16 /dev/urandom | od -An -tx1 | tr -d ' \n')}"
cat >"$PROFILES_FILE.tmp" <<EOF
{"profiles":[{"name":"default","secret":"$SECRET","backend":"127.0.0.1:2398","carrier_mode":"https"}]}
EOF
mv "$PROFILES_FILE.tmp" "$PROFILES_FILE"
chmod 0400 "$PROFILES_FILE"

cat >"$MTPROXY_ENV" <<EOF
MTPROXY_WORKERS=1
MTPROXY_MAX_CONNECTIONS=4096
MTPROXY_SECRET=$SECRET
MTPROXY_SECRETS=-S $SECRET
EOF

cat >"$SITE_DIR/index.html" <<'EOF'
<!doctype html>
<html><head><title>fakenode</title></head>
<body><h1>fakenode placeholder site</h1></body></html>
EOF

echo "fakenode: validating relay configuration"
tproxy-server -config "$CONFIG_FILE" -profiles-file "$PROFILES_FILE" -check

echo "fakenode: starting mtproxy, tproxy-server, caddy"
systemctl daemon-reload
systemctl start mtproxy
systemctl start tproxy-server
systemctl start caddy

echo "fakenode: waiting for the relay to become ready"
ready=0
for _ in $(seq 1 60); do
	if curl -sf http://127.0.0.1:8081/healthz >/dev/null 2>&1 && curl -sf http://127.0.0.1:8081/readyz >/dev/null 2>&1; then
		ready=1
		break
	fi
	sleep 1
done
if [[ "$ready" -ne 1 ]]; then
	echo "fakenode: relay did not become ready in time; tproxy-server log follows" >&2
	tail -n 200 /var/log/fake/tproxy-server.log >&2 || true
	exit 1
fi
echo "fakenode: relay is ready"

if [[ -n "${NODE_TOKEN:-}" ]]; then
	echo "fakenode: NODE_TOKEN provided, skipping registration"
	TOKEN="$NODE_TOKEN"
else
	: "${INSTALL_TOKEN:?INSTALL_TOKEN or NODE_TOKEN is required}"
	echo "fakenode: registering with the panel"
	AGENT_VERSION="$(tgwp-agent version)"
	REG="$(curl -fsS -X POST -H 'Content-Type: application/json' \
		-d "{\"hostname\":\"$NODE_HOSTNAME\",\"tproxy_version\":\"$TPROXY_COMMIT\",\"agent_version\":\"$AGENT_VERSION\"}" \
		"$PANEL_URL/api/v1/install/$INSTALL_TOKEN/register")"
	TOKEN="$(printf '%s' "$REG" | sed -n 's/.*"token":"\([^"]*\)".*/\1/p')"
	if [[ -z "$TOKEN" ]]; then
		echo "fakenode: registration failed: $REG" >&2
		exit 1
	fi
fi

umask 077
cat >"$AGENT_ENV" <<EOF
TGWP_PANEL_URL=$PANEL_URL
TGWP_TOKEN=$TOKEN
TGWP_TPROXY_VERSION=$TPROXY_COMMIT
EOF
umask 022

echo "fakenode: starting tgwp-agent"
set -a
# shellcheck disable=SC1090
. "$AGENT_ENV"
set +a
exec tgwp-agent
