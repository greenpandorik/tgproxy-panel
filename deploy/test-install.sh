#!/usr/bin/env bash
# End-to-end test of install.sh (make test-install).
#
# Builds the panel image from this checkout, starts a privileged docker:27-dind container
# with the repository mounted, loads the image into it and runs the installer there in
# --local --yes --from-checkout mode, exactly as a root shell on a fresh host would. Then
# asserts: the output has the ✔ step lines and the final banner, .env has every key and
# mode 0600, the stack is up, /healthz answers, the admin can log in via
# POST /api/v1/auth/login, an --update pass keeps it healthy, and --uninstall --purge
# leaves no containers, volumes or install directory behind. Around that, two pre-flight
# runs in domain mode: one against a domain that does not resolve must stop with the
# ✘ dns line before creating the install directory, one with --skip-preflight must get
# past the pre-flight. Also runs bash -n and shellcheck over install.sh (and this script).
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

# The installer prints ✔ ✘ only under a UTF-8 locale (sudo on a real host keeps LANG; a
# bare docker exec has none), so every command inside dind runs with LANG=C.UTF-8.
in_dind() { docker exec -e LANG=C.UTF-8 "$DIND_NAME" "$@"; }

# assert_line FILE FIXED-STRING WHAT: FILE must contain the string.
assert_line() {
	grep -qF -- "$2" "$1" || die "$3 (no line containing '$2')"
}

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
# postgres and caddy are loaded into dind from the host, so the installer needs no registry
# (the --skip-preflight run below is in domain mode and starts caddy).
for img in postgres:16-alpine caddy:2-alpine; do
	if ! docker image inspect "$img" >/dev/null 2>&1; then
		log "pulling $img on the host"
		docker pull -q "$img" >/dev/null
	fi
done

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

log "loading $IMAGE, postgres:16-alpine and caddy:2-alpine into dind"
docker save "$IMAGE" postgres:16-alpine caddy:2-alpine | docker exec -i "$DIND_NAME" docker load >/dev/null

INSTALL_LOG="$(mktemp -t tgwp-test-install.XXXXXX)"
trap 'rm -f "$INSTALL_LOG"; cleanup' EXIT

# --- 4a. pre-flight must stop a domain that does not resolve ----------------------------
# No tty and --yes: the failed dns check is fatal (exit 1) and nothing has been created.
log "running install.sh --domain nonexistent-host.invalid --yes (pre-flight must stop it)"
rc=0
in_dind bash /repo/install.sh --domain nonexistent-host.invalid --yes --from-checkout \
	--version "$VERSION" --image "$IMAGE" 2>&1 | tee "$INSTALL_LOG" || rc=$?
[[ "$rc" -eq 1 ]] || die "expected exit 1 from the failed pre-flight, got $rc"
assert_line "$INSTALL_LOG" '✘ dns: nonexistent-host.invalid' "pre-flight did not report the dns failure"
in_dind test ! -e "$INSTALL_DIR" || die "$INSTALL_DIR was created although the pre-flight failed"
if grep -qF '==> Docker' "$INSTALL_LOG"; then
	die "the installer went past the failed pre-flight"
fi

# --- 4. run the installer ---------------------------------------------------------------
log "running install.sh --local --yes --from-checkout inside dind"
: >"$INSTALL_LOG"
if ! docker exec -e LANG=C.UTF-8 -e TGWP_ADMIN_PASSWORD="$ADMIN_PASS" "$DIND_NAME" \
	bash /repo/install.sh --local --yes --from-checkout \
	--version "$VERSION" --image "$IMAGE" --admin-user "$ADMIN_USER" 2>&1 | tee "$INSTALL_LOG"; then
	die "install.sh exited non-zero"
fi
assert_line "$INSTALL_LOG" '✔ TGProxy panel is running' "no summary banner in the installer output"
assert_line "$INSTALL_LOG" '✔ port 8080: free' "no ✔ pre-flight line for port 8080"
assert_line "$INSTALL_LOG" '✔ Docker ' "no ✔ Docker line"
assert_line "$INSTALL_LOG" '✔ Compose ' "no ✔ Compose line"
assert_line "$INSTALL_LOG" '✔ .env written' "no ✔ Configuration line"
assert_line "$INSTALL_LOG" '✔ containers started' "no ✔ Stack line"
assert_line "$INSTALL_LOG" '✔ panel healthy' "no ✔ line for the health wait"
assert_line "$INSTALL_LOG" "✔ owner account '$ADMIN_USER' created" "no ✔ Admin account line"
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
# The engine pins in .env have to follow the release: a panel updated to a release with a
# newer pinned telemt must hand out install scripts for that one, and until --update synced
# them it kept the pin the first install wrote. Stale the version here and check it moves.
PIN_VERSION="$(sed -n 's/^TELEMT_VERSION=//p' "$REPO_DIR/.env.example" | tail -n 1)"
[[ -n "$PIN_VERSION" ]] || die ".env.example carries no TELEMT_VERSION"
in_dind sed -i 's/^TELEMT_VERSION=.*/TELEMT_VERSION=0.0.1/' "$INSTALL_DIR/.env"

log "running install.sh --update inside dind"
: >"$INSTALL_LOG"
if ! in_dind bash /repo/install.sh --update --yes --version "$VERSION" 2>&1 | tee "$INSTALL_LOG"; then
	die "install.sh --update exited non-zero"
fi
assert_line "$INSTALL_LOG" '✔ TGProxy panel is running' "no summary banner after --update"
# The test image exists only inside dind, so the pull fails and the installer must fall
# back to the local copy with a single warning line, without dumping the pull output.
assert_line "$INSTALL_LOG" "! could not pull $IMAGE; using the copy already on this host" "no fallback line after the failed pull"
if grep -qF 'docker compose pull: last 30 lines' "$INSTALL_LOG"; then
	die "the failed pull dumped its output although the local copy was used"
fi
in_dind grep -q '^MASTER_KEY=.\+' "$INSTALL_DIR/.env" || die ".env lost MASTER_KEY after --update"
assert_line "$INSTALL_LOG" "TELEMT_VERSION: 0.0.1 → $PIN_VERSION" "--update did not sync the telemt engine pins"
in_dind grep -q "^TELEMT_VERSION=$PIN_VERSION\$" "$INSTALL_DIR/.env" ||
	die ".env still carries the stale TELEMT_VERSION after --update"
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

# --- 8. --skip-preflight must get past the pre-flight -------------------------------------
# Domain mode with a domain that does not resolve; only "it went on" is asserted (the run may
# fail later, e.g. at Caddy), then whatever it left behind is removed.
log "running install.sh --domain panel.test --skip-preflight --yes (must get past the pre-flight)"
: >"$INSTALL_LOG"
rc=0
docker exec -e LANG=C.UTF-8 -e TGWP_ADMIN_PASSWORD="$ADMIN_PASS" "$DIND_NAME" \
	bash /repo/install.sh --domain panel.test --skip-preflight --yes --from-checkout \
	--version "$VERSION" --image "$IMAGE" 2>&1 | tee "$INSTALL_LOG" || rc=$?
log "skip-preflight run exited with $rc (a later failure is allowed)"
assert_line "$INSTALL_LOG" 'skipped (--skip-preflight)' "no line saying the pre-flight was skipped"
assert_line "$INSTALL_LOG" '==> Docker' "the installer did not get past the pre-flight"
if grep -qF '✘ dns' "$INSTALL_LOG"; then
	die "the dns check ran despite --skip-preflight"
fi
log "removing the domain-mode install"
in_dind bash /repo/install.sh --uninstall --purge --yes >/dev/null 2>&1 || true
left="$(in_dind docker ps -aq)"
[[ -z "$left" ]] || die "containers left after the skip-preflight cleanup: $left"
in_dind test ! -e "$INSTALL_DIR" || die "$INSTALL_DIR still exists after the skip-preflight cleanup"

log "TEST-INSTALL OK"
