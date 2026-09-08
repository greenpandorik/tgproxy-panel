#!/usr/bin/env bash
# Orchestrates the full docker-compose smoke flow described in the runbook:
# builds postgres+panel, creates an admin, runs e2e-smoke.sh once to get an
# install token, brings up fakenode with that token, then runs e2e-smoke.sh
# --continue. Used by `make e2e`. Tears the stack down (with volumes) when
# it's done, whether it passed or failed, so repeated runs start clean.
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
cd "$SCRIPT_DIR"

PANEL_URL="${PANEL_URL:-http://localhost:8080}"
ADMIN_USER="${ADMIN_USER:-root}"
ADMIN_PASS="${ADMIN_PASS:-change-me-now-1}"
COMPOSE=(docker compose -f docker-compose.yml -f docker-compose.override.example.yml)

log() { echo "[run-e2e] $*"; }

cleanup() {
	log "tearing down (docker compose down -v)"
	"${COMPOSE[@]}" --profile dev down -v || true
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
# So is the telemt release pin. A .env copied from .env.example already carries it; a .env
# left over from before the telemt engine existed does not.
if ! grep -q '^TELEMT_SHA256_X86_64=.\+' .env; then
	log "adding the telemt pin from .env.example"
	sed -i.bak '/^TELEMT_VERSION=/d;/^TELEMT_SHA256_X86_64=/d' .env && rm -f .env.bak
	grep -E '^TELEMT_(VERSION|SHA256_X86_64)=' ../.env.example >> .env
fi

log "building and starting postgres + panel"
"${COMPOSE[@]}" up -d --build postgres panel

# The panel migrates on start and `admin create` migrates too. Wait for the panel's
# healthcheck (it answers only once its own migration has run) before creating the
# admin, so the two never race on a fresh database.
log "waiting for the panel to become healthy"
for _ in $(seq 1 60); do
	if "${COMPOSE[@]}" exec -T panel wget -q -O- http://127.0.0.1:8080/healthz >/dev/null 2>&1; then
		break
	fi
	sleep 2
done

log "creating admin $ADMIN_USER (ignored if it already exists)"
ADMIN_OK=0
for _ in $(seq 1 30); do
	if "${COMPOSE[@]}" exec -T panel /app/panel admin create "$ADMIN_USER" "$ADMIN_PASS" 2>/tmp/admin-create.err; then
		ADMIN_OK=1
		break
	fi
	# Only the admin's own unique constraint means "already there". Any other duplicate-key
	# error (a migration racing another migrator on CREATE EXTENSION, say) is a real failure
	# and must not be mistaken for it.
	if grep -q 'admin_users_username_key' /tmp/admin-create.err; then
		ADMIN_OK=1
		break
	fi
	sleep 1
done
if [[ "$ADMIN_OK" -ne 1 ]]; then
	log "FAIL: could not create admin $ADMIN_USER:"
	cat /tmp/admin-create.err >&2
	exit 1
fi

log "running e2e-smoke.sh (phase 1: create node)"
OUT="$(PANEL_URL="$PANEL_URL" ADMIN_USER="$ADMIN_USER" ADMIN_PASS="$ADMIN_PASS" ./e2e-smoke.sh | tee /dev/stderr)"
TOKEN="$(sed -n 's/^FAKENODE_INSTALL_TOKEN=//p' <<<"$OUT" | tail -n1)"
[[ -n "$TOKEN" ]] || {
	echo "[run-e2e] FAIL: no FAKENODE_INSTALL_TOKEN in e2e-smoke.sh output" >&2
	exit 1
}

log "building and starting fakenode with the install token"
FAKENODE_INSTALL_TOKEN="$TOKEN" "${COMPOSE[@]}" --profile dev up -d --build fakenode

log "running e2e-smoke.sh (phase 2: continue)"
PANEL_URL="$PANEL_URL" ADMIN_USER="$ADMIN_USER" ADMIN_PASS="$ADMIN_PASS" ./e2e-smoke.sh --continue
