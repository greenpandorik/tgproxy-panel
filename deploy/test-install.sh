#!/usr/bin/env bash
# End-to-end test of install.sh (make test-install).
#
# Builds the panel image from this checkout, starts a privileged docker:27-dind container
# with the repository mounted, loads the image into it and runs the installer there in
# --local --yes --from-checkout mode, exactly as a root shell on a fresh host would. Then
# asserts: .env has every key and mode 0600, the stack is up, /healthz answers, the admin
# can log in via POST /api/v1/auth/login, an --update pass keeps it healthy, and
# --uninstall --purge leaves no containers, volumes or install directory behind.
# Also runs bash -n and shellcheck over install.sh (and this script).
#
# Needs Docker on the host (Docker Desktop's CLI dir is added to PATH) and shellcheck
# (installed with brew when missing). Ends with "TEST-INSTALL OK" on success.
set -euo pipefail

export PATH="/Applications/Docker.app/Contents/Resources/bin:/opt/homebrew/bin:$PATH"

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_DIR="$(cd "$SCRIPT_DIR/.." && pwd)"
IMAGE="${TEST_INSTALL_IMAGE:-tgproxy-panel:test-install}"
VERSION="test-install"
DIND_NAME="tgwp-test-install-dind-$$"
DIND_IMAGE="docker:27-dind"
INSTALL_DIR="/opt/tgproxy-panel"
ADMIN_USER="root"
ADMIN_PASS="$(openssl rand -hex 12)"
KEEP="${TEST_INSTALL_KEEP:-0}"

log() { echo "[test-install] $*"; }
die() {
	echo "[test-install] FAIL: $*" >&2
	exit 1
}

cleanup() {
	if [[ "$KEEP" == "1" ]]; then
		log "TEST_INSTALL_KEEP=1: leaving $DIND_NAME running"
		return
	fi
	log "removing $DIND_NAME"
	docker rm -f -v "$DIND_NAME" >/dev/null 2>&1 || true
}
trap cleanup EXIT

in_dind() { docker exec "$DIND_NAME" "$@"; }

# --- 1. static checks -----------------------------------------------------------------
for bin in docker openssl; do
	command -v "$bin" >/dev/null 2>&1 || die "$bin is required on PATH"
done
if ! command -v shellcheck >/dev/null 2>&1; then
	log "shellcheck missing; installing with brew"
	brew install shellcheck
fi
log "bash -n install.sh"
bash -n "$REPO_DIR/install.sh"
log "shellcheck install.sh deploy/test-install.sh"
shellcheck "$REPO_DIR/install.sh" "$SCRIPT_DIR/test-install.sh"

# --- 2. build the panel image on the host ---------------------------------------------
log "building $IMAGE from deploy/Dockerfile.panel"
docker build -q -f "$REPO_DIR/deploy/Dockerfile.panel" -t "$IMAGE" --build-arg VERSION="$VERSION" "$REPO_DIR" >/dev/null
if ! docker image inspect postgres:16-alpine >/dev/null 2>&1; then
	log "pulling postgres:16-alpine on the host (loaded into dind, so the installer needs no registry)"
	docker pull -q postgres:16-alpine >/dev/null
fi

# --- 3. start dind ----------------------------------------------------------------------
log "starting $DIND_NAME ($DIND_IMAGE)"
docker rm -f -v "$DIND_NAME" >/dev/null 2>&1 || true
docker run -d --privileged --name "$DIND_NAME" \
	-e DOCKER_TLS_CERTDIR= \
	-v "$REPO_DIR:/repo:ro" \
	"$DIND_IMAGE" >/dev/null
for _ in $(seq 1 60); do
	if in_dind docker info >/dev/null 2>&1; then
		break
	fi
	sleep 1
done
in_dind docker info >/dev/null 2>&1 || die "dockerd inside $DIND_NAME did not come up"
log "installing bash curl openssl inside dind"
in_dind apk add --no-cache bash curl openssl >/dev/null

log "loading $IMAGE and postgres:16-alpine into dind"
docker save "$IMAGE" postgres:16-alpine | docker exec -i "$DIND_NAME" docker load >/dev/null

# --- 4. run the installer ---------------------------------------------------------------
log "running install.sh --local --yes --from-checkout inside dind"
INSTALL_LOG="$(mktemp -t tgwp-test-install.XXXXXX)"
trap 'rm -f "$INSTALL_LOG"; cleanup' EXIT
if ! docker exec -e TGWP_ADMIN_PASSWORD="$ADMIN_PASS" "$DIND_NAME" \
	bash /repo/install.sh --local --yes --from-checkout \
	--version "$VERSION" --image "$IMAGE" --admin-user "$ADMIN_USER" 2>&1 | tee "$INSTALL_LOG"; then
	die "install.sh exited non-zero"
fi
grep -q 'TGProxy panel is running' "$INSTALL_LOG" || die "no summary block in the installer output"
if grep -q "$ADMIN_PASS" "$INSTALL_LOG"; then
	die "the provided admin password was echoed by the installer"
fi

# --- 5. assertions ----------------------------------------------------------------------
log "checking $INSTALL_DIR/.env"
mode="$(in_dind stat -c %a "$INSTALL_DIR/.env")"
[[ "$mode" == "600" ]] || die ".env mode is $mode, want 600"
for key in MASTER_KEY SESSION_SECRET METRICS_TOKEN POSTGRES_PASSWORD PANEL_DOMAIN \
	PANEL_PUBLIC_URL PANEL_VERSION PANEL_IMAGE DATA_DIR TELEMT_VERSION TELEMT_SHA256_X86_64; do
	in_dind grep -q "^$key=.\+" "$INSTALL_DIR/.env" || die ".env: $key is missing or empty"
done
in_dind grep -q '^ACME_EMAIL=' "$INSTALL_DIR/.env" || die ".env: ACME_EMAIL line is missing"
in_dind grep -q "^PANEL_VERSION=$VERSION\$" "$INSTALL_DIR/.env" || die ".env: PANEL_VERSION is not $VERSION"
in_dind grep -q "^PANEL_IMAGE=$IMAGE\$" "$INSTALL_DIR/.env" || die ".env: PANEL_IMAGE is not $IMAGE"
in_dind grep -q '^DATA_DIR=/data$' "$INSTALL_DIR/.env" || die ".env: DATA_DIR is not /data"
in_dind grep -q '^PANEL_PUBLIC_URL=http://.*:8080$' "$INSTALL_DIR/.env" || die ".env: PANEL_PUBLIC_URL is not a local http URL"
for f in docker-compose.yml docker-compose.local.yml Caddyfile install.sh; do
	in_dind test -f "$INSTALL_DIR/$f" || die "$INSTALL_DIR/$f is missing"
done

# wait_healthy CONTAINER: the installer probes /healthz itself and returns before Docker's
# own healthcheck (start_period 10s, interval 5s) has moved the container from "starting"
# to "healthy", so give the status a moment to catch up.
wait_healthy() {
	local name="$1" state=""
	for _ in $(seq 1 30); do
		state="$(in_dind docker inspect --format '{{.State.Status}} {{.State.Health.Status}} {{.RestartCount}}' "$name" 2>/dev/null || true)"
		[[ "$state" == "running healthy 0" ]] && return 0
		sleep 2
	done
	die "$name is not running+healthy without restarts (status/health/restarts: '$state')"
}

log "checking the stack"
wait_healthy tgproxy-panel-panel-1
wait_healthy tgproxy-panel-postgres-1
ps_out="$(in_dind docker ps -a --format '{{.Names}}')"
if echo "$ps_out" | grep -q caddy; then
	die "caddy is present in local mode: $ps_out"
fi
# shellcheck disable=SC2016  # $DATA_DIR is meant to expand inside the container
in_dind docker exec tgproxy-panel-panel-1 sh -c 'test "$DATA_DIR" = /data' || die "DATA_DIR inside the panel container is not /data"

log "checking /healthz"
in_dind curl -fsS http://127.0.0.1:8080/healthz >/dev/null || die "healthz failed"

log "checking admin login"
status="$(in_dind curl -sS -o /dev/null -w '%{http_code}' -H 'Content-Type: application/json' \
	-d "{\"username\":\"$ADMIN_USER\",\"password\":\"$ADMIN_PASS\"}" http://127.0.0.1:8080/api/v1/auth/login)"
[[ "$status" == "200" ]] || die "login returned HTTP $status, want 200"
status="$(in_dind curl -sS -o /dev/null -w '%{http_code}' -H 'Content-Type: application/json' \
	-d "{\"username\":\"$ADMIN_USER\",\"password\":\"wrong-$ADMIN_PASS\"}" http://127.0.0.1:8080/api/v1/auth/login)"
[[ "$status" == "401" ]] || die "login with a wrong password returned HTTP $status, want 401"

# --- 6. update pass ---------------------------------------------------------------------
log "running install.sh --update inside dind"
: >"$INSTALL_LOG"
if ! in_dind bash /repo/install.sh --update --yes --version "$VERSION" 2>&1 | tee "$INSTALL_LOG"; then
	die "install.sh --update exited non-zero"
fi
grep -q 'TGProxy panel is running' "$INSTALL_LOG" || die "no summary block after --update"
in_dind grep -q '^MASTER_KEY=.\+' "$INSTALL_DIR/.env" || die ".env lost MASTER_KEY after --update"
wait_healthy tgproxy-panel-panel-1
in_dind curl -fsS http://127.0.0.1:8080/healthz >/dev/null || die "healthz failed after --update"
status="$(in_dind curl -sS -o /dev/null -w '%{http_code}' -H 'Content-Type: application/json' \
	-d "{\"username\":\"$ADMIN_USER\",\"password\":\"$ADMIN_PASS\"}" http://127.0.0.1:8080/api/v1/auth/login)"
[[ "$status" == "200" ]] || die "login after --update returned HTTP $status, want 200"

# --- 7. uninstall -----------------------------------------------------------------------
log "running install.sh --uninstall --purge --yes inside dind"
in_dind bash /repo/install.sh --uninstall --purge --yes 2>&1 | sed 's/^/  /'
left="$(in_dind docker ps -aq)"
[[ -z "$left" ]] || die "containers left after uninstall: $left"
left="$(in_dind docker volume ls -q)"
[[ -z "$left" ]] || die "volumes left after uninstall: $left"
in_dind test ! -e "$INSTALL_DIR" || die "$INSTALL_DIR still exists after --purge"

log "TEST-INSTALL OK"
