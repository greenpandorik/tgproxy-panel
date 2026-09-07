#!/usr/bin/env bash
# Orchestrates the telemt end-to-end smoke: builds postgres+panel, creates an admin, runs
# e2e-smoke-telemt.sh once to get an install token, brings up the fakenode-telemt container
# (real telemt binary) with that token, then runs e2e-smoke-telemt.sh --continue. Used by
# `make e2e-telemt`. Tears the stack down (with volumes) when it's done, whether it passed or
# failed, so repeated runs start clean.
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
cd "$SCRIPT_DIR"

PANEL_URL="${PANEL_URL:-http://localhost:8080}"
ADMIN_USER="${ADMIN_USER:-root}"
ADMIN_PASS="${ADMIN_PASS:-change-me-now-1}"
COMPOSE=(docker compose -f docker-compose.yml -f docker-compose.override.example.yml)

log() { echo "[run-e2e-telemt] $*"; }

cleanup() {
	log "tearing down (docker compose down -v)"
	"${COMPOSE[@]}" --profile dev-telemt down -v || true
}
trap cleanup EXIT

if [[ ! -f .env ]]; then
	log "creating deploy/.env from .env.example"
	cp ../.env.example .env
fi
if ! grep -q '^MASTER_KEY=.\+' .env; then
	sed -i.bak "s|^MASTER_KEY=.*|MASTER_KEY=$(openssl rand -base64 32)|" .env && rm -f .env.bak
fi
if ! grep -q '^SESSION_SECRET=.\+' .env; then
	sed -i.bak "s|^SESSION_SECRET=.*|SESSION_SECRET=$(openssl rand -base64 32)|" .env && rm -f .env.bak
fi
# METRICS_TOKEN is mandatory in gateway mode (the panel refuses to start without it).
if ! grep -q '^METRICS_TOKEN=.\+' .env; then
	sed -i.bak "s|^METRICS_TOKEN=.*|METRICS_TOKEN=$(openssl rand -hex 32)|" .env && rm -f .env.bak
fi
# The telemt pins: TELEMT_VERSION and the gnu checksum are what the panel bakes into the
# install script (and it refuses to render one without them); the musl checksum is what the
# fakenode-telemt image verifies. They are pins, not local choices, so they are taken from
# .env.example on every run rather than only when missing: a .env left over from before a
# pin bump would otherwise make this smoke test quietly exercise the previous release.
for var in TELEMT_VERSION TELEMT_SHA256_X86_64 TELEMT_SHA256_MUSL_X86_64; do
	want="$(grep -E "^$var=" ../.env.example || true)"
	[[ -n "$want" ]] || continue
	if ! grep -qxF "$want" .env; then
		log "syncing $var from .env.example (${want#*=})"
		sed -i.bak "/^$var=/d" .env && rm -f .env.bak
		printf '%s\n' "$want" >>.env
	fi
done

log "building and starting postgres + panel"
"${COMPOSE[@]}" up -d --build postgres panel

log "creating admin $ADMIN_USER (ignored if it already exists)"
for _ in $(seq 1 30); do
	if "${COMPOSE[@]}" exec -T panel /app/panel admin create "$ADMIN_USER" "$ADMIN_PASS" 2>/tmp/admin-create-telemt.err; then
		break
	fi
	if grep -qi 'already exists\|duplicate' /tmp/admin-create-telemt.err; then
		break
	fi
	sleep 1
done

log "running e2e-smoke-telemt.sh (phase 1: create the telemt node)"
OUT="$(PANEL_URL="$PANEL_URL" ADMIN_USER="$ADMIN_USER" ADMIN_PASS="$ADMIN_PASS" ./e2e-smoke-telemt.sh | tee /dev/stderr)"
TOKEN="$(sed -n 's/^FAKENODE_INSTALL_TOKEN=//p' <<<"$OUT" | tail -n1)"
[[ -n "$TOKEN" ]] || {
	echo "[run-e2e-telemt] FAIL: no FAKENODE_INSTALL_TOKEN in e2e-smoke-telemt.sh output" >&2
	exit 1
}

log "building and starting fakenode-telemt with the install token (the image downloads telemt)"
FAKENODE_INSTALL_TOKEN="$TOKEN" "${COMPOSE[@]}" --profile dev-telemt up -d --build fakenode-telemt

log "running e2e-smoke-telemt.sh (phase 2: continue)"
PANEL_URL="$PANEL_URL" ADMIN_USER="$ADMIN_USER" ADMIN_PASS="$ADMIN_PASS" ./e2e-smoke-telemt.sh --continue
