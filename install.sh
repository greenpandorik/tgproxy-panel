#!/usr/bin/env bash
# install.sh - one-command installer and updater for the TGProxy panel host.
#
#   curl -fsSL https://raw.githubusercontent.com/greenpandorik/tgproxy-panel/main/install.sh | sudo bash
#   curl -fsSL .../install.sh | sudo bash -s -- --domain panel.example.com --email me@example.com --yes
#
# What it does, in order: checks it runs as root on Linux; installs Docker (get.docker.com)
# when `docker compose` is missing; downloads the compose files of the chosen release into
# the install directory (default /opt/tgproxy-panel); writes .env with freshly generated
# secrets; starts the stack; waits for /healthz; creates the first owner account; prints a
# summary. Re-running it on a host that already has .env is an update: the image is pulled
# again and the stack restarted, .env is kept. --uninstall stops the stack.
#
# Every option can also come from the environment as TGWP_<NAME> (TGWP_DOMAIN, TGWP_YES, ...).
set -euo pipefail

REPO="greenpandorik/tgproxy-panel"
RAW_BASE="https://raw.githubusercontent.com/$REPO"
API_LATEST="https://api.github.com/repos/$REPO/releases/latest"
DEFAULT_IMAGE="ghcr.io/$REPO"
DEFAULT_DIR="/opt/tgproxy-panel"
HEALTH_BUDGET=120

# ---------------------------------------------------------------------------------------
# Options. Flags win over TGWP_* environment variables, which win over the defaults.
# ---------------------------------------------------------------------------------------
DOMAIN="${TGWP_DOMAIN:-}"
LOCAL="${TGWP_LOCAL:-}"
EMAIL="${TGWP_EMAIL:-}"
ADMIN_USER="${TGWP_ADMIN_USER:-}"
ADMIN_PASSWORD="${TGWP_ADMIN_PASSWORD:-}"
VERSION="${TGWP_VERSION:-}"
DIR="${TGWP_DIR:-$DEFAULT_DIR}"
IMAGE="${TGWP_IMAGE:-}"
YES="${TGWP_YES:-}"
PURGE="${TGWP_PURGE:-}"
FROM_CHECKOUT="${TGWP_FROM_CHECKOUT:-}"
MODE="install" # install | update | uninstall

usage() {
	cat <<EOF
Usage: install.sh [options]

Installs the TGProxy panel (Docker Compose stack: panel + PostgreSQL + Caddy) on this
host, or updates / removes an existing installation. Run as root.

Mode:
  (none)                 Install; if $DEFAULT_DIR/.env already exists, update instead.
  --update               Pull the image for --version (default: latest release) and restart.
                         Keeps .env and all data.
  --uninstall            Stop and remove the containers. Asks before removing the data
                         volumes; --purge removes them (and the install directory) without
                         asking.

Options:
  --domain <fqdn>        Public domain of the panel. Caddy obtains a TLS certificate for it;
                         ports 80 and 443 must be free and reachable.
  --local                No domain: no Caddy, no TLS, panel on http://<ip>:8080.
  --email <address>      ACME account e-mail (certificate expiry notices). Optional.
  --admin-user <name>    First admin account (role owner). Default: admin.
  --admin-password <pw>  Its password. Generated (20 characters) and printed once if omitted.
  --version <tag>        Panel release to install, e.g. 1.0.0. Default: the latest release.
  --dir <path>           Install directory. Default: $DEFAULT_DIR.
  --image <ref>          Use this image reference instead of $DEFAULT_IMAGE:<version>
                         (for mirrors and tests). Saved to .env as PANEL_IMAGE.
  --from-checkout        Use the deploy files next to this script instead of downloading
                         them. Detected automatically when run from a git checkout.
  --purge                With --uninstall: remove data volumes and the install directory.
  --yes                  Never prompt. Missing required values are an error (exit 2).
  --help                 This text.

Every option is also read from the environment as TGWP_<NAME> in upper case, e.g.
TGWP_DOMAIN=panel.example.com TGWP_YES=1.

Examples:
  curl -fsSL $RAW_BASE/main/install.sh | sudo bash
  curl -fsSL $RAW_BASE/main/install.sh | sudo bash -s -- \\
      --domain panel.example.com --email me@example.com --yes
  sudo $DEFAULT_DIR/install.sh --update
  sudo $DEFAULT_DIR/install.sh --uninstall --purge
EOF
}

log() { printf '[install] %s\n' "$*"; }
warn() { printf '[install] warning: %s\n' "$*" >&2; }
die() {
	printf '[install] error: %s\n' "$*" >&2
	exit 1
}
usage_error() {
	printf '[install] error: %s\n' "$*" >&2
	printf '[install] run with --help for the options\n' >&2
	exit 2
}

# truthy VALUE: how TGWP_* booleans and the internal flags are read.
truthy() {
	case "${1:-}" in
	1 | true | TRUE | yes | YES | y | Y) return 0 ;;
	*) return 1 ;;
	esac
}

# ---------------------------------------------------------------------------------------
# Argument parsing
# ---------------------------------------------------------------------------------------
need_value() {
	[[ $# -ge 2 && -n "$2" ]] || usage_error "$1 needs a value"
}

while [[ $# -gt 0 ]]; do
	case "$1" in
	--domain)
		need_value "$@"
		DOMAIN="$2"
		shift 2
		;;
	--domain=*)
		DOMAIN="${1#*=}"
		shift
		;;
	--local)
		LOCAL=1
		shift
		;;
	--email)
		need_value "$@"
		EMAIL="$2"
		shift 2
		;;
	--email=*)
		EMAIL="${1#*=}"
		shift
		;;
	--admin-user)
		need_value "$@"
		ADMIN_USER="$2"
		shift 2
		;;
	--admin-user=*)
		ADMIN_USER="${1#*=}"
		shift
		;;
	--admin-password)
		need_value "$@"
		ADMIN_PASSWORD="$2"
		shift 2
		;;
	--admin-password=*)
		ADMIN_PASSWORD="${1#*=}"
		shift
		;;
	--version)
		need_value "$@"
		VERSION="$2"
		shift 2
		;;
	--version=*)
		VERSION="${1#*=}"
		shift
		;;
	--dir)
		need_value "$@"
		DIR="$2"
		shift 2
		;;
	--dir=*)
		DIR="${1#*=}"
		shift
		;;
	--image)
		need_value "$@"
		IMAGE="$2"
		shift 2
		;;
	--image=*)
		IMAGE="${1#*=}"
		shift
		;;
	--from-checkout)
		FROM_CHECKOUT=1
		shift
		;;
	--yes | -y)
		YES=1
		shift
		;;
	--update)
		MODE="update"
		shift
		;;
	--uninstall)
		MODE="uninstall"
		shift
		;;
	--purge)
		PURGE=1
		shift
		;;
	--help | -h)
		usage
		exit 0
		;;
	*)
		usage_error "unknown option: $1"
		;;
	esac
done

# ---------------------------------------------------------------------------------------
# Prompting. `curl | bash` leaves stdin busy with the script itself, so prompts go through
# /dev/tty when there is one. No tty or --yes means non-interactive: never ask, fail on a
# missing required value.
# ---------------------------------------------------------------------------------------
INTERACTIVE=0
if ! truthy "$YES" && { : </dev/tty; } 2>/dev/null; then
	INTERACTIVE=1
fi

# ask VAR "prompt" "default": reads a line from the terminal into VAR; empty keeps default.
ask() {
	local __var="$1" prompt="$2" default="${3:-}" reply=""
	if [[ -n "$default" ]]; then
		printf '%s [%s]: ' "$prompt" "$default" >/dev/tty
	else
		printf '%s: ' "$prompt" >/dev/tty
	fi
	IFS= read -r reply </dev/tty || true
	[[ -n "$reply" ]] || reply="$default"
	printf -v "$__var" '%s' "$reply"
}

# ask_secret VAR "prompt": same, without echo.
ask_secret() {
	local __var="$1" prompt="$2" reply=""
	printf '%s: ' "$prompt" >/dev/tty
	IFS= read -rs reply </dev/tty || true
	printf '\n' >/dev/tty
	printf -v "$__var" '%s' "$reply"
}

# confirm "question" [default y|n]: returns 0 for yes.
confirm() {
	local prompt="$1" default="${2:-y}" reply=""
	if [[ "$default" == "y" ]]; then
		printf '%s [Y/n]: ' "$prompt" >/dev/tty
	else
		printf '%s [y/N]: ' "$prompt" >/dev/tty
	fi
	IFS= read -r reply </dev/tty || true
	[[ -n "$reply" ]] || reply="$default"
	case "$reply" in
	y | Y | yes | YES) return 0 ;;
	*) return 1 ;;
	esac
}

# ---------------------------------------------------------------------------------------
# Helpers
# ---------------------------------------------------------------------------------------
require_cmd() {
	local c
	for c in "$@"; do
		command -v "$c" >/dev/null 2>&1 || die "$c is required but not installed"
	done
}

# env_get FILE KEY: value of KEY=... in FILE (last one wins), empty when absent.
env_get() {
	local file="$1" key="$2"
	[[ -f "$file" ]] || return 0
	sed -n "s/^${key}=//p" "$file" | tail -n 1
}

# env_set FILE KEY VALUE: replace the KEY= line, or append one. Values go through the
# environment rather than awk -v, so backslashes and the like survive untouched.
env_set() {
	local file="$1" tmp
	tmp="$(mktemp "$file.XXXXXX")"
	ENV_KEY="$2" ENV_VAL="$3" awk '
		BEGIN { key = ENVIRON["ENV_KEY"]; val = ENVIRON["ENV_VAL"]; done = 0 }
		index($0, key "=") == 1 { if (!done) { print key "=" val; done = 1 }; next }
		{ print }
		END { if (!done) print key "=" val }
	' "$file" >"$tmp"
	chmod 0600 "$tmp"
	mv -f "$tmp" "$file"
}

# is_semver 1.2.3 -> 0
is_semver() {
	[[ "$1" =~ ^[0-9]+\.[0-9]+\.[0-9]+([.-][0-9A-Za-z.-]+)?$ ]]
}

valid_domain() {
	[[ "$1" =~ ^[a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?(\.[a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?)+$ ]]
}

valid_email() {
	[[ "$1" =~ ^[^[:space:]@]+@[^[:space:]@]+\.[^[:space:]@]+$ ]]
}

valid_username() {
	[[ "$1" =~ ^[A-Za-z0-9_.@-]{1,64}$ ]]
}

gen_password() {
	local pw
	pw="$(openssl rand -base64 64 | tr -dc 'A-Za-z0-9' | head -c 20)"
	[[ ${#pw} -eq 20 ]] || die "could not generate a password"
	printf '%s' "$pw"
}

# public_ip: best effort, for the summary URL in local mode.
public_ip() {
	local ip=""
	ip="$(curl -fsS --max-time 5 https://api.ipify.org 2>/dev/null || true)"
	if [[ ! "$ip" =~ ^[0-9]+\.[0-9]+\.[0-9]+\.[0-9]+$ ]]; then
		ip="$(hostname -I 2>/dev/null | awk '{print $1}' || true)"
	fi
	if [[ ! "$ip" =~ ^[0-9]+\.[0-9]+\.[0-9]+\.[0-9]+$ ]]; then
		ip="$(ip -4 route get 1.1.1.1 2>/dev/null | sed -n 's/.* src \([0-9.]*\).*/\1/p' | head -n 1 || true)"
	fi
	if [[ ! "$ip" =~ ^[0-9]+\.[0-9]+\.[0-9]+\.[0-9]+$ ]]; then
		ip="127.0.0.1"
	fi
	printf '%s' "$ip"
}

# fetch URL DEST: download to a temp file, then move into place.
fetch() {
	local url="$1" dest="$2" tmp
	tmp="$(mktemp "$dest.XXXXXX")"
	if curl -fsSL --retry 3 --retry-delay 2 -o "$tmp" "$url"; then
		mv -f "$tmp" "$dest"
		return 0
	fi
	rm -f "$tmp"
	return 1
}

# ---------------------------------------------------------------------------------------
# Checkout detection: run from a git clone, the deploy files next to the script are used
# instead of a download (that is also how the test harness runs it).
# ---------------------------------------------------------------------------------------
SCRIPT_DIR=""
if [[ -n "${BASH_SOURCE[0]:-}" && -f "${BASH_SOURCE[0]}" ]]; then
	SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
fi
if [[ -z "$FROM_CHECKOUT" ]]; then
	if [[ -n "$SCRIPT_DIR" && -f "$SCRIPT_DIR/deploy/docker-compose.release.yml" ]]; then
		FROM_CHECKOUT=1
	else
		FROM_CHECKOUT=0
	fi
fi
if truthy "$FROM_CHECKOUT"; then
	[[ -n "$SCRIPT_DIR" && -f "$SCRIPT_DIR/deploy/docker-compose.release.yml" ]] ||
		die "--from-checkout: deploy/docker-compose.release.yml not found next to $0"
fi

# ---------------------------------------------------------------------------------------
# Compose wrapper. The project lives in $DIR; local mode adds the port override, domain
# mode enables the caddy profile. Uninstall passes both so that everything is removed
# whichever mode was installed.
# ---------------------------------------------------------------------------------------
COMPOSE_ARGS=()
compose_setup() {
	COMPOSE_ARGS=(docker compose --project-directory "$DIR" -f "$DIR/docker-compose.yml")
	if truthy "$LOCAL" || [[ "$MODE" == "uninstall" ]]; then
		[[ -f "$DIR/docker-compose.local.yml" ]] && COMPOSE_ARGS+=(-f "$DIR/docker-compose.local.yml")
	fi
	if ! truthy "$LOCAL" || [[ "$MODE" == "uninstall" ]]; then
		COMPOSE_ARGS+=(--profile caddy)
	fi
}
compose() { "${COMPOSE_ARGS[@]}" "$@"; }

# ---------------------------------------------------------------------------------------
# Steps
# ---------------------------------------------------------------------------------------
check_host() {
	[[ "$(uname -s)" == "Linux" ]] || die "this installer runs on Linux only (found $(uname -s))"
	[[ "$(id -u)" -eq 0 ]] || die "run as root: sudo bash install.sh ..."
	require_cmd curl openssl awk sed mktemp
}

docker_ready() {
	docker compose version >/dev/null 2>&1 && docker info >/dev/null 2>&1
}

ensure_docker() {
	if docker_ready; then
		return 0
	fi
	if command -v docker >/dev/null 2>&1 && ! docker info >/dev/null 2>&1; then
		# Installed but not running: try to start it before reinstalling anything.
		if command -v systemctl >/dev/null 2>&1; then
			systemctl enable --now docker >/dev/null 2>&1 || true
		fi
		docker_ready && return 0
	fi
	log "Docker with Compose v2 is not available on this host."
	if [[ "$INTERACTIVE" -eq 1 ]]; then
		confirm "Install Docker now from https://get.docker.com?" y ||
			die "Docker is required; install it and run this script again"
	elif ! truthy "$YES"; then
		usage_error "Docker is not installed; re-run with --yes to let the installer set it up"
	fi
	log "installing Docker (get.docker.com)"
	curl -fsSL https://get.docker.com | sh
	if command -v systemctl >/dev/null 2>&1; then
		systemctl enable --now docker >/dev/null 2>&1 || true
	fi
	docker_ready || die "Docker was installed but 'docker compose version' still fails; check 'systemctl status docker'"
}

# resolve_version: sets VERSION (tag without v, or "latest") and REF (git ref the deploy
# files are downloaded from).
REF="main"
resolve_version() {
	if [[ -z "$VERSION" ]]; then
		local tag
		tag="$(curl -fsSL --max-time 15 "$API_LATEST" 2>/dev/null |
			sed -n 's/.*"tag_name"[[:space:]]*:[[:space:]]*"\([^"]*\)".*/\1/p' | head -n 1 || true)"
		if [[ -n "$tag" ]]; then
			VERSION="${tag#v}"
		else
			warn "could not determine the latest release from GitHub; using the 'latest' image tag"
			VERSION="latest"
		fi
	fi
	VERSION="${VERSION#v}"
	[[ "$VERSION" =~ ^[A-Za-z0-9][A-Za-z0-9._-]*$ ]] || usage_error "invalid --version: $VERSION"
	if is_semver "$VERSION"; then
		REF="v$VERSION"
	else
		REF="main"
	fi
}

image_ref() {
	if [[ -n "$IMAGE" ]]; then
		printf '%s' "$IMAGE"
	else
		printf '%s:%s' "$DEFAULT_IMAGE" "$VERSION"
	fi
}

# fetch_deploy_files: docker-compose.yml (from docker-compose.release.yml),
# docker-compose.local.yml, Caddyfile and .env.example into $DIR. A release tag that
# predates a file falls back to main, which carries the current copy.
fetch_deploy_files() {
	mkdir -p "$DIR"
	local -a pairs=(
		"deploy/docker-compose.release.yml:docker-compose.yml"
		"deploy/docker-compose.local.yml:docker-compose.local.yml"
		"deploy/Caddyfile:Caddyfile"
		".env.example:.env.example"
	)
	local pair src dest
	if truthy "$FROM_CHECKOUT"; then
		log "using the deploy files from $SCRIPT_DIR"
		for pair in "${pairs[@]}"; do
			src="${pair%%:*}"
			dest="${pair#*:}"
			cp -f "$SCRIPT_DIR/$src" "$DIR/$dest"
		done
		cp -f "$SCRIPT_DIR/install.sh" "$DIR/install.sh"
	else
		log "downloading the deploy files (ref $REF)"
		for pair in "${pairs[@]}"; do
			src="${pair%%:*}"
			dest="${pair#*:}"
			if ! fetch "$RAW_BASE/$REF/$src" "$DIR/$dest"; then
				[[ "$REF" != "main" ]] || die "could not download $src"
				warn "$src is not in ref $REF; taking the copy from main"
				fetch "$RAW_BASE/main/$src" "$DIR/$dest" || die "could not download $src"
			fi
		done
		fetch "$RAW_BASE/$REF/install.sh" "$DIR/install.sh" ||
			fetch "$RAW_BASE/main/install.sh" "$DIR/install.sh" ||
			warn "could not save a copy of install.sh into $DIR"
	fi
	if [[ -f "$DIR/install.sh" ]]; then
		chmod 0755 "$DIR/install.sh"
	fi
	chmod 0644 "$DIR/docker-compose.yml" "$DIR/docker-compose.local.yml" "$DIR/Caddyfile" "$DIR/.env.example"
}

# write_env: a fresh .env from .env.example with generated secrets. Built in a temp file
# and moved into place, so a failure never leaves a half-written .env behind.
write_env() {
	local tmp public_url
	tmp="$(mktemp "$DIR/.env.XXXXXX")"
	chmod 0600 "$tmp"
	cp -f "$DIR/.env.example" "$tmp"
	chmod 0600 "$tmp"
	if truthy "$LOCAL"; then
		public_url="http://$(public_ip):8080"
	else
		public_url="https://$DOMAIN"
	fi
	env_set "$tmp" MASTER_KEY "$(openssl rand -base64 32)"
	env_set "$tmp" SESSION_SECRET "$(openssl rand -base64 32)"
	env_set "$tmp" METRICS_TOKEN "$(openssl rand -hex 32)"
	env_set "$tmp" POSTGRES_PASSWORD "$(openssl rand -hex 16)"
	env_set "$tmp" PANEL_DOMAIN "${DOMAIN:-localhost}"
	env_set "$tmp" ACME_EMAIL "$EMAIL"
	env_set "$tmp" PANEL_PUBLIC_URL "$public_url"
	env_set "$tmp" PANEL_VERSION "$VERSION"
	# The image sets DATA_DIR=/data (the paneldata volume); .env.example carries the
	# bare-metal ./data, and env_file would override the image's value with it.
	env_set "$tmp" DATA_DIR "/data"
	if [[ -n "$IMAGE" ]]; then
		env_set "$tmp" PANEL_IMAGE "$IMAGE"
	fi
	chmod 0600 "$tmp"
	mv -f "$tmp" "$DIR/.env"
	PUBLIC_URL="$public_url"
}

# update_env: keep the existing .env; bump PANEL_VERSION when asked for explicitly or when
# the resolved release is newer; record --image when given.
update_env() {
	local current
	chmod 0600 "$DIR/.env"
	current="$(env_get "$DIR/.env" PANEL_VERSION)"
	if [[ -n "$VERSION_REQUESTED" ]]; then
		env_set "$DIR/.env" PANEL_VERSION "$VERSION"
	elif [[ -z "$current" || "$current" == "latest" ]]; then
		env_set "$DIR/.env" PANEL_VERSION "$VERSION"
	elif is_semver "$current" && is_semver "$VERSION" &&
		[[ "$current" != "$VERSION" && "$(printf '%s\n%s\n' "$current" "$VERSION" | sort -V | tail -n 1)" == "$VERSION" ]]; then
		env_set "$DIR/.env" PANEL_VERSION "$VERSION"
	else
		VERSION="$current"
	fi
	if [[ -n "$IMAGE" ]]; then
		env_set "$DIR/.env" PANEL_IMAGE "$IMAGE"
	else
		IMAGE="$(env_get "$DIR/.env" PANEL_IMAGE)"
	fi
	PUBLIC_URL="$(env_get "$DIR/.env" PANEL_PUBLIC_URL)"
}

# pull_panel_image: for updates. A mirror or a locally loaded image (--image) that cannot
# be pulled is still usable when a copy exists locally.
pull_panel_image() {
	local ref
	ref="$(image_ref)"
	log "pulling $ref"
	if compose pull panel; then
		return 0
	fi
	if docker image inspect "$ref" >/dev/null 2>&1; then
		warn "could not pull $ref; using the copy already on this host"
		return 0
	fi
	die "could not pull $ref"
}

wait_healthy() {
	local deadline=$((SECONDS + HEALTH_BUDGET))
	log "waiting for the panel to become healthy (up to ${HEALTH_BUDGET}s)"
	while [[ $SECONDS -lt $deadline ]]; do
		if compose exec -T panel wget -q -O- http://127.0.0.1:8080/healthz >/dev/null 2>&1; then
			log "panel is healthy"
			return 0
		fi
		sleep 3
	done
	printf '\n--- docker compose ps ---\n' >&2
	compose ps >&2 || true
	printf '\n--- last 50 lines of panel logs ---\n' >&2
	compose logs --tail 50 panel >&2 || true
	die "the panel did not become healthy within ${HEALTH_BUDGET}s"
}

# create_admin: the password travels as an environment variable into the container (no
# argv on the host, nothing on disk); the CLI takes it as an argument inside the container.
create_admin() {
	log "creating the owner account '$ADMIN_USER'"
	local out
	# The single quotes are deliberate: the variables must expand inside the container's
	# shell, not here, so the password never appears in a host-side command line.
	# shellcheck disable=SC2016
	if ! out="$(TGWP_ADMIN_USER="$ADMIN_USER" TGWP_ADMIN_PASSWORD="$ADMIN_PASSWORD" \
		compose exec -T -e TGWP_ADMIN_USER -e TGWP_ADMIN_PASSWORD panel \
		sh -c 'exec /app/panel admin create "$TGWP_ADMIN_USER" "$TGWP_ADMIN_PASSWORD" owner' 2>&1)"; then
		printf '%s\n' "$out" >&2
		die "could not create the admin account"
	fi
}

firewall_hint() {
	local ports="80 and 443"
	truthy "$LOCAL" && ports="8080"
	if command -v ufw >/dev/null 2>&1 && ufw status 2>/dev/null | grep -q '^Status: active'; then
		log "ufw is active: make sure ports $ports are allowed (this script does not change firewall rules)"
	fi
}

print_summary() {
	local proto_note
	printf '\n'
	printf '=========================================================\n'
	printf ' TGProxy panel is running\n'
	printf '=========================================================\n'
	printf ' URL:            %s\n' "$PUBLIC_URL"
	if [[ "$MODE" == "install" ]]; then
		printf ' Admin user:     %s\n' "$ADMIN_USER"
		if [[ "$PASSWORD_GENERATED" -eq 1 ]]; then
			printf ' Admin password: %s\n' "$ADMIN_PASSWORD"
			printf '                 (generated; shown only this once)\n'
		else
			printf ' Admin password: the one you provided\n'
		fi
	fi
	printf ' Version:        %s\n' "$VERSION"
	printf ' Install dir:    %s\n' "$DIR"
	printf '\n'
	printf ' Update:         sudo %s/install.sh --update\n' "$DIR"
	printf ' Logs:           cd %s && docker compose logs -f panel\n' "$DIR"
	printf ' Uninstall:      sudo %s/install.sh --uninstall\n' "$DIR"
	printf '\n'
	if truthy "$LOCAL"; then
		proto_note="port 8080/tcp must be reachable from where you open the panel"
	else
		proto_note="ports 80 and 443/tcp must be reachable from the internet, and the DNS A record of $DOMAIN must point at this host"
	fi
	printf ' Note: %s.\n' "$proto_note"
	if [[ "$MODE" == "install" ]]; then
		printf ' Keep a copy of %s/.env somewhere safe: MASTER_KEY encrypts everything in the database.\n' "$DIR"
	fi
	printf '=========================================================\n'
}

# ---------------------------------------------------------------------------------------
# Uninstall
# ---------------------------------------------------------------------------------------
do_uninstall() {
	check_host
	[[ -f "$DIR/docker-compose.yml" ]] || die "no installation found in $DIR"
	docker_ready || die "Docker is not running"
	compose_setup
	local remove_volumes=0
	if truthy "$PURGE"; then
		remove_volumes=1
	elif [[ "$INTERACTIVE" -eq 1 ]]; then
		if confirm "Also remove the data volumes (database, panel data, certificates)? This cannot be undone" n; then
			remove_volumes=1
		fi
	fi
	log "stopping the stack in $DIR"
	if [[ "$remove_volumes" -eq 1 ]]; then
		compose down --remove-orphans -v
		log "removing $DIR"
		rm -rf "$DIR"
		log "uninstalled; containers, volumes and $DIR are gone"
	else
		compose down --remove-orphans
		log "containers removed; data volumes and $DIR were kept"
		log "to remove them too: $DIR/install.sh --uninstall --purge"
	fi
}

# ---------------------------------------------------------------------------------------
# Install / update
# ---------------------------------------------------------------------------------------
PUBLIC_URL=""
PASSWORD_GENERATED=0
VERSION_REQUESTED="$VERSION"

collect_install_options() {
	# Domain or local mode.
	if truthy "$LOCAL" && [[ -n "$DOMAIN" ]]; then
		usage_error "--local and --domain exclude each other"
	fi
	if ! truthy "$LOCAL" && [[ -z "$DOMAIN" ]]; then
		if [[ "$INTERACTIVE" -eq 1 ]]; then
			ask DOMAIN "Panel domain (DNS A record pointing here; leave empty for local mode on :8080 without TLS)" ""
			[[ -n "$DOMAIN" ]] || LOCAL=1
		else
			usage_error "--domain <fqdn> or --local is required"
		fi
	fi
	if [[ -n "$DOMAIN" ]]; then
		DOMAIN="$(printf '%s' "$DOMAIN" | tr '[:upper:]' '[:lower:]')"
		valid_domain "$DOMAIN" || usage_error "not a valid domain: $DOMAIN"
		if [[ -z "$EMAIL" && "$INTERACTIVE" -eq 1 ]]; then
			ask EMAIL "E-mail for Let's Encrypt (certificate expiry notices; optional)" ""
		fi
	fi
	if [[ -n "$EMAIL" ]]; then
		valid_email "$EMAIL" || usage_error "not a valid e-mail address: $EMAIL"
	elif ! truthy "$LOCAL"; then
		warn "no ACME e-mail: Let's Encrypt will not be able to warn you about certificate problems"
	fi
	# Admin account.
	if [[ -z "$ADMIN_USER" ]]; then
		if [[ "$INTERACTIVE" -eq 1 ]]; then
			ask ADMIN_USER "Admin username" "admin"
		else
			ADMIN_USER="admin"
		fi
	fi
	valid_username "$ADMIN_USER" || usage_error "admin username may contain letters, digits, . _ @ - (1-64 chars)"
	if [[ -z "$ADMIN_PASSWORD" && "$INTERACTIVE" -eq 1 ]]; then
		local again=""
		ask_secret ADMIN_PASSWORD "Admin password (hidden; leave empty to generate one)"
		if [[ -n "$ADMIN_PASSWORD" ]]; then
			ask_secret again "Repeat the password"
			[[ "$ADMIN_PASSWORD" == "$again" ]] || die "the passwords do not match"
		fi
	fi
	if [[ -z "$ADMIN_PASSWORD" ]]; then
		ADMIN_PASSWORD="$(gen_password)"
		PASSWORD_GENERATED=1
	fi
	[[ ${#ADMIN_PASSWORD} -ge 8 ]] || usage_error "the admin password must be at least 8 characters"
}

check_dir() {
	[[ "$DIR" == /* ]] || usage_error "--dir must be an absolute path"
	if [[ -e "$DIR" && ! -d "$DIR" ]]; then
		die "$DIR exists and is not a directory"
	fi
	if [[ -d "$DIR" && ! -f "$DIR/.env" && ! -f "$DIR/docker-compose.yml" ]] && [[ -n "$(ls -A "$DIR" 2>/dev/null)" ]]; then
		if truthy "$YES"; then
			warn "$DIR is not empty and does not look like a panel installation; continuing because of --yes"
		else
			die "$DIR is not empty and does not look like a panel installation; pick another --dir or pass --yes"
		fi
	fi
}

show_plan() {
	printf '\n'
	printf 'About to %s the TGProxy panel:\n' "$MODE"
	if truthy "$LOCAL"; then
		printf '  Mode:        local (no domain, no TLS, panel on :8080)\n'
	else
		printf '  Domain:      %s\n' "$DOMAIN"
		printf '  ACME e-mail: %s\n' "${EMAIL:-(none)}"
	fi
	if [[ "$MODE" == "install" ]]; then
		printf '  Admin user:  %s\n' "$ADMIN_USER"
		if [[ "$PASSWORD_GENERATED" -eq 1 ]]; then
			printf '  Password:    generated, printed at the end\n'
		else
			printf '  Password:    as provided\n'
		fi
	fi
	printf '  Version:     %s\n' "$VERSION"
	printf '  Image:       %s\n' "$(image_ref)"
	printf '  Directory:   %s\n' "$DIR"
	printf '\n'
}

do_install_or_update() {
	check_host
	check_dir
	if [[ "$MODE" == "install" && -f "$DIR/.env" ]]; then
		log "an installation already exists in $DIR; switching to update mode"
		MODE="update"
	fi
	if [[ "$MODE" == "update" ]]; then
		[[ -f "$DIR/.env" ]] || die "nothing to update: $DIR/.env does not exist (run without --update to install)"
		# Mode comes from the existing .env unless given explicitly.
		if ! truthy "$LOCAL" && [[ -z "$DOMAIN" ]]; then
			DOMAIN="$(env_get "$DIR/.env" PANEL_DOMAIN)"
			if [[ -z "$DOMAIN" || "$DOMAIN" == "localhost" ]]; then
				DOMAIN=""
				LOCAL=1
			fi
		fi
		EMAIL="${EMAIL:-$(env_get "$DIR/.env" ACME_EMAIL)}"
	else
		collect_install_options
	fi
	ensure_docker
	resolve_version
	if [[ "$MODE" == "update" ]]; then
		update_env
	fi
	show_plan
	if [[ "$INTERACTIVE" -eq 1 ]]; then
		confirm "Proceed?" y || die "aborted"
	fi

	fetch_deploy_files
	if [[ "$MODE" == "install" ]]; then
		log "writing $DIR/.env with fresh secrets"
		write_env
	else
		# A domain or e-mail passed on the command line updates the stored values.
		if ! truthy "$LOCAL"; then
			env_set "$DIR/.env" PANEL_DOMAIN "$DOMAIN"
			[[ -z "$EMAIL" ]] || env_set "$DIR/.env" ACME_EMAIL "$EMAIL"
		fi
	fi
	compose_setup
	if [[ "$MODE" == "update" ]]; then
		pull_panel_image
	fi
	log "starting the stack ($(image_ref))"
	compose up -d --remove-orphans
	wait_healthy
	if [[ "$MODE" == "install" ]]; then
		create_admin
	fi
	firewall_hint
	print_summary
}

case "$MODE" in
uninstall) do_uninstall ;;
*) do_install_or_update ;;
esac
