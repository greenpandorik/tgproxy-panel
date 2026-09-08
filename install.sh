#!/usr/bin/env bash
# install.sh - one-command installer and updater for the TGProxy panel host.
#
#   curl -fsSL https://raw.githubusercontent.com/greenpandorik/tgproxy-panel/main/install.sh | sudo bash
#   curl -fsSL .../install.sh | sudo bash -s -- --domain panel.example.com --email me@example.com --yes
#
# What it does, in order: checks it runs as root on Linux; runs a pre-flight (domain resolves
# to this host, ports 80/443 free, install directory state) before touching anything;
# installs Docker (get.docker.com) when `docker compose` is missing; downloads the compose
# files of the chosen release into the install directory (default /opt/tgproxy-panel);
# writes .env with freshly generated secrets; starts the stack; waits for /healthz; creates
# the first owner account; prints a summary. Re-running it on a host that already has .env
# is an update: the image is pulled again and the stack restarted, .env is kept (except the
# telemt engine pins, which move to the values the new release ships).
# --uninstall stops the stack.
#
# Every option can also come from the environment as TGWP_<NAME> (TGWP_DOMAIN, TGWP_YES, ...).
set -euo pipefail

REPO="greenpandorik/tgproxy-panel"
RAW_BASE="https://raw.githubusercontent.com/$REPO"
API_LATEST="https://api.github.com/repos/$REPO/releases/latest"
DEFAULT_IMAGE="ghcr.io/$REPO"
DEFAULT_DIR="/opt/tgproxy-panel"
HEALTH_BUDGET=120

# Noisy tools stay quiet: no debconf dialogs, no "pending kernel upgrade" screens from
# needrestart, no progress bars from apt (get.docker.com runs apt underneath).
export DEBIAN_FRONTEND=noninteractive
export NEEDRESTART_MODE=a

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
SKIP_PREFLIGHT="${TGWP_SKIP_PREFLIGHT:-}"
MODE="install" # install | update | uninstall

usage() {
	cat <<EOF
Usage: install.sh [options]

Installs the TGProxy panel (Docker Compose stack: panel + PostgreSQL + Caddy) on this
host, or updates / removes an existing installation. Run as root.

Mode:
  (none)                 Install; if $DEFAULT_DIR/.env already exists, update instead.
  --update               Pull the image for --version (default: latest release) and restart.
                         Keeps .env and all data, but moves TELEMT_VERSION and the two
                         TELEMT_SHA256_* pins to the values the release ships, so new node
                         installs get the engine this panel expects.
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
  --skip-preflight       Do not run the pre-flight checks (DNS, ports 80/443 or 8080,
                         install directory) before installing.
  --purge                With --uninstall: remove data volumes and the install directory.
  --yes                  Never prompt. Missing required values are an error (exit 2); a
                         failed pre-flight is an error (exit 1) unless --skip-preflight.
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

# ---------------------------------------------------------------------------------------
# Output. The same helpers as the node install script, so both installs look like one
# product: colors only on a terminal without NO_COLOR; ✔ ✘ … only under a UTF-8 locale,
# [ok] [x] [..] otherwise.
# ---------------------------------------------------------------------------------------
if [[ -t 1 && -z "${NO_COLOR:-}" ]]; then
	GREEN=$'\033[32m' RED=$'\033[31m' YELLOW=$'\033[33m' DIM=$'\033[2m' BOLD=$'\033[1m' RESET=$'\033[0m'
else
	GREEN="" RED="" YELLOW="" DIM="" BOLD="" RESET=""
fi
case "${LC_ALL:-${LC_CTYPE:-${LANG:-}}}" in
*[Uu][Tt][Ff]-8* | *[Uu][Tt][Ff]8*) SYM_OK='✔' SYM_FAIL='✘' SYM_WAIT='…' SYM_ARROW='→' ;;
*) SYM_OK='[ok]' SYM_FAIL='[x]' SYM_WAIT='[..]' SYM_ARROW='->' ;;
esac

step() { printf '\n%s==> %s%s\n' "$BOLD" "$*" "$RESET"; }
ok() { printf '  %s%s%s %s\n' "$GREEN" "$SYM_OK" "$RESET" "$*"; }
fail() { printf '  %s%s%s %s\n' "$RED" "$SYM_FAIL" "$RESET" "$*" >&2; }
warn() { printf '  %s!%s %s\n' "$YELLOW" "$RESET" "$*"; }
info() { printf '    %s%s%s\n' "$DIM" "$*" "$RESET"; }

# die MESSAGE: a fail line, then exit 1. usage_error is the same with exit 2 and a --help hint.
die() {
	fail "$@"
	exit 1
}
usage_error() {
	fail "$@"
	info "run with --help for the options"
	exit 2
}

# wait_for LABEL SECONDS CMD...: polls CMD until it succeeds or SECONDS pass. On a terminal
# the line is rewritten in place with the elapsed time; in a log it is one line at the start
# and one at the end. Returns CMD's final status.
wait_for() {
	local label="$1" budget="$2" start elapsed shown=-1 rc=1
	shift 2
	start=$SECONDS
	if [[ -t 1 ]]; then
		printf '  %s %s' "$SYM_WAIT" "$label"
	else
		printf '  %s %s (up to %ss)\n' "$SYM_WAIT" "$label" "$budget"
	fi
	while :; do
		if "$@" >/dev/null 2>&1; then
			rc=0
			break
		fi
		elapsed=$((SECONDS - start))
		[[ $elapsed -lt $budget ]] || break
		if [[ -t 1 && $((elapsed / 5)) -ne $shown ]]; then
			shown=$((elapsed / 5))
			printf '\r\033[K  %s %s (%ss)' "$SYM_WAIT" "$label" "$elapsed"
		fi
		sleep 2
	done
	elapsed=$((SECONDS - start))
	if [[ -t 1 ]]; then
		printf '\r\033[K'
	fi
	if [[ $rc -eq 0 ]]; then
		ok "$label (${elapsed}s)"
	else
		fail "$label: still not ready after ${elapsed}s"
	fi
	return $rc
}

# banner_ok TITLE LINE...: the final summary box. banner_fail is the same in red, on stderr.
banner() {
	local color="$1" sym="$2" title="$3" rule line
	shift 3
	rule="$(printf '%*s' 60 '' | tr ' ' '=')"
	printf '\n%s%s%s\n' "$color" "$rule" "$RESET"
	printf '%s %s %s%s%s%s\n' "$color" "$sym" "$BOLD" "$title" "$RESET" "$RESET"
	printf '%s%s%s\n' "$color" "$rule" "$RESET"
	for line in "$@"; do
		printf '  %s\n' "$line"
	done
	printf '%s%s%s\n' "$color" "$rule" "$RESET"
}
banner_ok() { banner "$GREEN" "$SYM_OK" "$@"; }
banner_fail() { banner "$RED" "$SYM_FAIL" "$@" >&2; }

# show_log_tail LABEL FILE: the last 30 lines of a captured output, on stderr.
show_log_tail() {
	printf '%s--- %s: last 30 lines of output ---%s\n' "$DIM" "$1" "$RESET" >&2
	tail -n 30 "$2" >&2
	printf '%s--- end of output ---%s\n' "$DIM" "$RESET" >&2
}

# quietly LABEL CMD...: run CMD with its output captured; on failure show the last 30 lines
# so the terminal only fills up when something actually went wrong.
quietly() {
	local label="$1" log rc=0
	shift
	log="$(mktemp "${TMPDIR:-/tmp}/tgwp-install.XXXXXX")"
	"$@" >"$log" 2>&1 || rc=$?
	if [[ $rc -ne 0 ]]; then
		show_log_tail "$label" "$log"
	fi
	rm -f "$log"
	return $rc
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
	--skip-preflight)
		SKIP_PREFLIGHT=1
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
		command -v "$c" >/dev/null 2>&1 || die "$c is required but not installed; install it (apt-get install $c) and re-run"
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

is_ipv4() {
	[[ "${1:-}" =~ ^[0-9]{1,3}(\.[0-9]{1,3}){3}$ ]]
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
	[[ ${#pw} -eq 20 ]] || die "could not generate a password (openssl rand failed)"
	printf '%s' "$pw"
}

# public_ip: this host's public IPv4 as the internet sees it (api.ipify.org), else the
# address of the default route, else the first address of hostname -I. Prints nothing when
# none of them yields an IPv4.
public_ip() {
	local ip=""
	ip="$(curl -4fsS --max-time 5 https://api.ipify.org 2>/dev/null || true)"
	if ! is_ipv4 "$ip"; then
		ip="$(ip -4 route get 1.1.1.1 2>/dev/null | sed -n 's/.* src \([0-9.]*\).*/\1/p' | head -n 1 || true)"
	fi
	if ! is_ipv4 "$ip"; then
		ip="$(hostname -I 2>/dev/null | awk '{print $1}' || true)"
	fi
	if is_ipv4 "$ip"; then
		printf '%s' "$ip"
	fi
}

# resolve_ipv4 DOMAIN: the A records of DOMAIN, one per line, with whatever resolver tool
# the host has (getent, dig, host, nslookup). Empty when it does not resolve.
resolve_ipv4() {
	local d="$1" out=""
	if command -v getent >/dev/null 2>&1; then
		out="$(getent ahostsv4 "$d" 2>/dev/null | awk '{print $1}' || true)"
		[[ -n "$out" ]] || out="$(getent hosts "$d" 2>/dev/null | awk '{print $1}' || true)"
	fi
	if [[ -z "$out" ]] && command -v dig >/dev/null 2>&1; then
		out="$(dig +short +time=3 +tries=2 A "$d" 2>/dev/null || true)"
	fi
	if [[ -z "$out" ]] && command -v host >/dev/null 2>&1; then
		out="$(host -t A "$d" 2>/dev/null | awk '/has address/ {print $NF}' || true)"
	fi
	if [[ -z "$out" ]] && command -v nslookup >/dev/null 2>&1; then
		out="$(nslookup "$d" 2>/dev/null | awk '/^Name:/ {found = 1} found && /^Address/ {print $2}' | sed 's/#.*//' || true)"
	fi
	printf '%s\n' "$out" | grep -E '^[0-9]{1,3}(\.[0-9]{1,3}){3}$' | sort -u || true
}

# port_listener PORT: who listens on TCP PORT (process name, or the bound address when the
# name is not available); empty when the port is free. ss, then netstat, then a plain
# connect to 127.0.0.1 as the last resort.
port_listener() {
	local port="$1" out=""
	if command -v ss >/dev/null 2>&1; then
		out="$(ss -ltnp "( sport = :$port )" 2>/dev/null | tail -n +2 |
			sed -n 's/.*users:(("\([^"]*\)".*/\1/p' | sort -u | tr '\n' ' ' | sed 's/ $//')"
		[[ -n "$out" ]] || out="$(ss -ltn "( sport = :$port )" 2>/dev/null | tail -n +2 | awk '{print "a listener on " $4; exit}')"
	elif command -v netstat >/dev/null 2>&1; then
		out="$(netstat -ltnp 2>/dev/null | awk -v p=":$port\$" '$4 ~ p { if ($7 == "" || $7 == "-") print "a listener on " $4; else print $7; exit }')"
	elif (exec 3<>"/dev/tcp/127.0.0.1/$port") 2>/dev/null; then
		out="something answering on 127.0.0.1:$port"
	fi
	printf '%s' "$out"
}

# containers_on_port PORT: names of the Docker containers publishing PORT, space separated.
containers_on_port() {
	docker_ready || return 0
	docker ps --filter "publish=$1" --format '{{.Names}}' 2>/dev/null | tr '\n' ' ' | sed 's/ $//' || true
}

# only_panel_containers "name name": 0 when every name belongs to this install directory's
# compose project (an earlier tgproxy-panel caddy or panel container), which `up` replaces.
only_panel_containers() {
	local project name
	project="$(basename "$DIR" | tr '[:upper:]' '[:lower:]')"
	for name in $1; do
		case "$name" in
		"$project"-caddy-* | "$project"_caddy_* | "$project"-panel-* | "$project"_panel_*) ;;
		*) return 1 ;;
		esac
	done
	return 0
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
	local arch mem_kb
	step "Host"
	[[ "$(uname -s)" == "Linux" ]] || die "this installer runs on Linux only (found $(uname -s))"
	[[ "$(id -u)" -eq 0 ]] || die "run as root: sudo bash install.sh ..."
	arch="$(uname -m)"
	case "$arch" in
	x86_64 | aarch64 | arm64) ok "Linux $arch, running as root" ;;
	*)
		ok "Linux, running as root"
		warn "architecture $arch: the published image is built for amd64 and arm64 only"
		;;
	esac
	# A "1 GB" VPS reports ~950 MB after what the kernel keeps, so 0.9 GiB counts as 1 GB.
	mem_kb="$(awk '/^MemTotal:/ {print $2}' /proc/meminfo 2>/dev/null || true)"
	if [[ "$mem_kb" =~ ^[0-9]+$ ]]; then
		if [[ $mem_kb -ge 943718 ]]; then
			ok "memory: $(awk -v k="$mem_kb" 'BEGIN { printf "%.1f", k / 1048576 }') GB"
		else
			warn "memory: $((mem_kb / 1024)) MB; 1 GB or more is recommended for panel + PostgreSQL"
		fi
	fi
	require_cmd curl openssl awk sed mktemp
	ok "curl, openssl, awk, sed present"
}

docker_ready() {
	docker compose version >/dev/null 2>&1 && docker info >/dev/null 2>&1
}

docker_report() {
	ok "Docker $(docker version --format '{{.Server.Version}}' 2>/dev/null || echo present)"
	ok "Compose $(docker compose version --short 2>/dev/null || echo v2)"
}

ensure_docker() {
	step "Docker"
	if docker_ready; then
		docker_report
		return 0
	fi
	if command -v docker >/dev/null 2>&1 && ! docker info >/dev/null 2>&1; then
		# Installed but not running: try to start it before reinstalling anything.
		if command -v systemctl >/dev/null 2>&1; then
			systemctl enable --now docker >/dev/null 2>&1 || true
		fi
		if docker_ready; then
			ok "Docker service started"
			docker_report
			return 0
		fi
	fi
	warn "Docker with Compose v2 is not available on this host"
	if [[ "$INTERACTIVE" -eq 1 ]]; then
		confirm "Install Docker now from https://get.docker.com?" y ||
			die "Docker is required; install it (https://docs.docker.com/engine/install/) and run this script again"
	elif ! truthy "$YES"; then
		usage_error "Docker is not installed; re-run with --yes to let the installer set it up"
	fi
	local script
	script="$(mktemp "${TMPDIR:-/tmp}/get-docker.XXXXXX.sh")"
	info "installing Docker from get.docker.com (takes a minute; output shown only on failure)"
	if ! curl -fsSL https://get.docker.com -o "$script"; then
		rm -f "$script"
		die "could not download https://get.docker.com; check the network (curl -I https://get.docker.com) and re-run"
	fi
	if ! quietly "get.docker.com" sh "$script"; then
		rm -f "$script"
		die "the Docker install script failed (output above); fix the cause or install Docker by hand (https://docs.docker.com/engine/install/) and re-run"
	fi
	rm -f "$script"
	if command -v systemctl >/dev/null 2>&1; then
		systemctl enable --now docker >/dev/null 2>&1 || true
	fi
	docker_ready || die "Docker was installed but 'docker compose version' still fails; check: systemctl status docker"
	ok "Docker installed (get.docker.com)"
	docker_report
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
	step "Deploy files"
	mkdir -p "$DIR"
	local -a pairs=(
		"deploy/docker-compose.release.yml:docker-compose.yml"
		"deploy/docker-compose.local.yml:docker-compose.local.yml"
		"deploy/Caddyfile:Caddyfile"
		".env.example:.env.example"
	)
	local pair src dest
	if truthy "$FROM_CHECKOUT"; then
		info "source: $SCRIPT_DIR (checkout)"
		for pair in "${pairs[@]}"; do
			src="${pair%%:*}"
			dest="${pair#*:}"
			cp -f "$SCRIPT_DIR/$src" "$DIR/$dest"
			ok "$dest"
		done
		cp -f "$SCRIPT_DIR/install.sh" "$DIR/install.sh"
		ok "install.sh (copy for --update and --uninstall)"
	else
		info "source: $RAW_BASE/$REF"
		for pair in "${pairs[@]}"; do
			src="${pair%%:*}"
			dest="${pair#*:}"
			if fetch "$RAW_BASE/$REF/$src" "$DIR/$dest"; then
				ok "$dest"
				continue
			fi
			[[ "$REF" != "main" ]] || die "could not download $src from $RAW_BASE/$REF; check the network and re-run"
			fetch "$RAW_BASE/main/$src" "$DIR/$dest" || die "could not download $src from $RAW_BASE/$REF nor from main; check the network and re-run"
			ok "$dest (not in ref $REF; took the copy from main)"
		done
		if fetch "$RAW_BASE/$REF/install.sh" "$DIR/install.sh" || fetch "$RAW_BASE/main/install.sh" "$DIR/install.sh"; then
			ok "install.sh (copy for --update and --uninstall)"
		else
			warn "could not save a copy of install.sh into $DIR"
		fi
	fi
	if [[ -f "$DIR/install.sh" ]]; then
		chmod 0755 "$DIR/install.sh"
	fi
	chmod 0644 "$DIR/docker-compose.yml" "$DIR/docker-compose.local.yml" "$DIR/Caddyfile" "$DIR/.env.example"
}

# write_env: a fresh .env from .env.example with generated secrets. Built in a temp file
# and moved into place, so a failure never leaves a half-written .env behind.
write_env() {
	local tmp public_url ip
	tmp="$(mktemp "$DIR/.env.XXXXXX")"
	chmod 0600 "$tmp"
	cp -f "$DIR/.env.example" "$tmp"
	chmod 0600 "$tmp"
	if truthy "$LOCAL"; then
		ip="$(public_ip)"
		public_url="http://${ip:-127.0.0.1}:8080"
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
	ok ".env written: $DIR/.env (mode 0600)"
	ok "secrets generated: MASTER_KEY, SESSION_SECRET, METRICS_TOKEN, POSTGRES_PASSWORD"
	info "PANEL_PUBLIC_URL=$public_url"
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

# sync_engine_pins: on --update, move the node engine pins in .env to the values the release
# being installed ships in its .env.example (already downloaded by fetch_deploy_files, from
# the same ref as everything else). Without this a panel updated to a release with a newer
# pinned telemt keeps handing out install scripts for the old one, because .env was written
# once at install time and never touched again.
#
# Only these three keys are considered, and only when the release carries a value for them.
# Secrets, the domain, the version and everything else in .env are never touched.
sync_engine_pins() {
	local key cur new changed=0
	if [[ ! -f "$DIR/.env.example" ]]; then
		warn "no .env.example for this release; the telemt pins in .env were left as they are"
		return 0
	fi
	for key in TELEMT_VERSION TELEMT_SHA256_X86_64 TELEMT_SHA256_MUSL_X86_64; do
		new="$(env_get "$DIR/.env.example" "$key")"
		[[ -n "$new" ]] || continue
		cur="$(env_get "$DIR/.env" "$key")"
		[[ "$cur" != "$new" ]] || continue
		env_set "$DIR/.env" "$key" "$new"
		ok "$key: ${cur:-(unset)} $SYM_ARROW $new"
		changed=1
	done
	if [[ "$changed" -eq 0 ]]; then
		ok "engine pins already current (telemt $(env_get "$DIR/.env" TELEMT_VERSION))"
	else
		info "existing nodes are moved to the new engine with: tgwp-agent upgrade (on each node)"
	fi
}

# pull_panel_image: for updates. A mirror or a locally loaded image (--image) that cannot
# be pulled is still usable when a copy exists locally.
pull_panel_image() {
	local ref log rc=0
	ref="$(image_ref)"
	log="$(mktemp "${TMPDIR:-/tmp}/tgwp-install.XXXXXX")"
	compose pull --quiet panel >"$log" 2>&1 || rc=$?
	if [[ $rc -eq 0 ]]; then
		rm -f "$log"
		ok "image pulled: $ref"
		return 0
	fi
	if docker image inspect "$ref" >/dev/null 2>&1; then
		# The pull output is not worth the screen space when the local copy does the job.
		rm -f "$log"
		warn "could not pull $ref; using the copy already on this host"
		return 0
	fi
	show_log_tail "docker compose pull" "$log"
	rm -f "$log"
	die "could not pull $ref; check the registry access (docker pull $ref) and re-run"
}

healthz_ok() {
	compose exec -T panel wget -q -O- http://127.0.0.1:8080/healthz
}

health_failed() {
	printf '\n%s--- docker compose ps ---%s\n' "$DIM" "$RESET" >&2
	compose ps >&2 || true
	printf '\n%s--- last 50 lines of panel logs ---%s\n' "$DIM" "$RESET" >&2
	compose logs --tail 50 panel >&2 || true
	banner_fail "The panel did not become healthy within ${HEALTH_BUDGET}s" \
		"Status:  cd $DIR && docker compose ps" \
		"Logs:    cd $DIR && docker compose logs --tail 100 panel postgres" \
		"Re-run:  sudo $DIR/install.sh$([[ "$MODE" == "update" ]] && printf ' --update')" \
		"" \
		"The usual causes: the postgres container still starting on a slow disk, or an" \
		"image that does not match this host's architecture ($(uname -m))."
	exit 1
}

# create_admin: the password travels as an environment variable into the container (no
# argv on the host, nothing on disk); the CLI takes it as an argument inside the container.
create_admin() {
	step "Admin account"
	local out
	# The single quotes are deliberate: the variables must expand inside the container's
	# shell, not here, so the password never appears in a host-side command line.
	# shellcheck disable=SC2016
	if ! out="$(TGWP_ADMIN_USER="$ADMIN_USER" TGWP_ADMIN_PASSWORD="$ADMIN_PASSWORD" \
		compose exec -T -e TGWP_ADMIN_USER -e TGWP_ADMIN_PASSWORD panel \
		sh -c 'exec /app/panel admin create "$TGWP_ADMIN_USER" "$TGWP_ADMIN_PASSWORD" owner' 2>&1)"; then
		printf '%s\n' "$out" >&2
		die "could not create the owner account; the stack is running, create it by hand: cd $DIR && docker compose exec panel /app/panel admin create $ADMIN_USER '<password>' owner"
	fi
	ok "owner account '$ADMIN_USER' created"
}

firewall_hint() {
	local ports="80 and 443"
	truthy "$LOCAL" && ports="8080"
	if command -v ufw >/dev/null 2>&1 && ufw status 2>/dev/null | grep -q '^Status: active'; then
		warn "ufw is active: make sure ports $ports are allowed (this script does not change firewall rules)"
	fi
}

print_summary() {
	local -a lines=()
	lines+=("URL:            $PUBLIC_URL")
	if [[ "$MODE" == "install" ]]; then
		lines+=("Admin user:     $ADMIN_USER")
		if [[ "$PASSWORD_GENERATED" -eq 1 ]]; then
			lines+=("Admin password: $ADMIN_PASSWORD")
			lines+=("                (generated; shown only this once)")
		else
			lines+=("Admin password: the one you provided")
		fi
	fi
	lines+=("Version:        $VERSION")
	lines+=("Install dir:    $DIR")
	lines+=("")
	lines+=("Update:         sudo $DIR/install.sh --update")
	lines+=("Logs:           cd $DIR && docker compose logs -f panel")
	lines+=("Uninstall:      sudo $DIR/install.sh --uninstall")
	lines+=("")
	if truthy "$LOCAL"; then
		lines+=("Note: port 8080/tcp must be reachable from where you open the panel.")
	else
		lines+=("Note: ports 80 and 443/tcp must be reachable from the internet, and the DNS")
		lines+=("      A record of $DOMAIN must point at this host.")
	fi
	if [[ "$MODE" == "install" ]]; then
		lines+=("Keep a copy of $DIR/.env somewhere safe: MASTER_KEY encrypts")
		lines+=("everything in the database and cannot be recovered.")
	fi
	banner_ok "TGProxy panel is running" "${lines[@]}"
}

# ---------------------------------------------------------------------------------------
# Pre-flight: everything that commonly makes a first install fail, checked before anything
# is changed. Domain mode: the domain resolves to this host, 80/443 are free, the install
# directory is usable. Local mode: 8080 and the directory. A failure offers re-run /
# continue / quit on a terminal and is fatal otherwise (unless --skip-preflight).
# ---------------------------------------------------------------------------------------
PREFLIGHT_FAILED=0
pf_fail() {
	fail "$@"
	PREFLIGHT_FAILED=1
}

preflight_dns() {
	local me ips ip
	me="$(public_ip)"
	ips="$(resolve_ipv4 "$DOMAIN" | tr '\n' ' ' | sed 's/ $//')"
	if [[ -z "$ips" ]]; then
		pf_fail "dns: $DOMAIN does not resolve to an IPv4 address"
		info "create an A record for $DOMAIN pointing at ${me:-the public IP of this host} and wait for it to propagate"
		return 0
	fi
	if [[ -z "$me" ]]; then
		warn "dns: $DOMAIN resolves to $ips; could not detect this host's public IPv4 to compare"
		return 0
	fi
	for ip in $ips; do
		if [[ "$ip" == "$me" ]]; then
			ok "dns: $DOMAIN -> $me (this host)"
			return 0
		fi
	done
	pf_fail "dns: $DOMAIN resolves to $ips, but this host is $me"
	info "point the A record at $me (a CDN proxy in front of it must be off: Let's Encrypt has to reach this host directly)"
}

preflight_port() {
	local port="$1" who containers
	who="$(port_listener "$port")"
	if [[ -z "$who" ]]; then
		ok "port $port: free"
		return 0
	fi
	containers="$(containers_on_port "$port")"
	if [[ -n "$containers" ]] && only_panel_containers "$containers"; then
		ok "port $port: used by $containers (an earlier panel install; replaced by this one)"
		return 0
	fi
	[[ -z "$containers" ]] || who="container $containers"
	pf_fail "port $port: in use by $who"
	info "stop it first (systemctl stop <service> / docker stop <container>) or free the port some other way, then re-check"
}

preflight_dir() {
	if [[ ! -e "$DIR" ]]; then
		ok "panel dir: $DIR will be created"
	elif [[ -f "$DIR/.env" ]]; then
		ok "panel dir: $DIR holds an installation (its .env is kept)"
	elif [[ -z "$(ls -A "$DIR" 2>/dev/null)" ]]; then
		ok "panel dir: $DIR is empty"
	elif [[ -f "$DIR/docker-compose.yml" ]]; then
		ok "panel dir: $DIR has deploy files from an earlier run but no .env (fresh install)"
	elif truthy "$YES"; then
		warn "panel dir: $DIR is not empty and does not look like a panel installation; continuing because of --yes"
	else
		pf_fail "panel dir: $DIR is not empty and does not look like a panel installation"
		info "pick another --dir, empty it, or pass --yes to install into it anyway"
	fi
}

run_preflight() {
	local reply
	if truthy "$SKIP_PREFLIGHT"; then
		step "Pre-flight"
		warn "skipped (--skip-preflight)"
		return 0
	fi
	while :; do
		step "Pre-flight"
		PREFLIGHT_FAILED=0
		if truthy "$LOCAL"; then
			preflight_port 8080
		else
			preflight_dns
			preflight_port 80
			preflight_port 443
		fi
		preflight_dir
		if [[ "$PREFLIGHT_FAILED" -eq 0 ]]; then
			return 0
		fi
		if [[ "$INTERACTIVE" -ne 1 ]]; then
			fail "pre-flight failed: fix the points above and re-run, or pass --skip-preflight (TGWP_SKIP_PREFLIGHT=1) to continue anyway"
			exit 1
		fi
		printf '\n  [r] re-run  [c] continue anyway  [q] quit: ' >/dev/tty
		IFS= read -r reply </dev/tty || reply="q"
		case "$reply" in
		r | R) continue ;;
		c | C)
			warn "continuing despite the failed pre-flight checks"
			return 0
			;;
		*) die "aborted" ;;
		esac
	done
}

# ---------------------------------------------------------------------------------------
# Uninstall
# ---------------------------------------------------------------------------------------
do_uninstall() {
	printf '%sTGProxy panel installer%s\n' "$BOLD" "$RESET"
	check_host
	[[ -f "$DIR/docker-compose.yml" ]] || die "no installation found in $DIR (pass --dir if it lives elsewhere)"
	docker_ready || die "Docker is not running; check: systemctl status docker"
	ok "installation found in $DIR"
	compose_setup
	local remove_volumes=0
	if truthy "$PURGE"; then
		remove_volumes=1
	elif [[ "$INTERACTIVE" -eq 1 ]]; then
		if confirm "Also remove the data volumes (database, panel data, certificates)? This cannot be undone" n; then
			remove_volumes=1
		fi
	fi
	step "Stack"
	if [[ "$remove_volumes" -eq 1 ]]; then
		quietly "docker compose down -v" compose down --remove-orphans -v ||
			die "docker compose down failed (output above); check: cd $DIR && docker compose ps"
		ok "containers removed"
		ok "data volumes removed"
		rm -rf "$DIR"
		ok "$DIR removed"
		banner_ok "TGProxy panel removed" \
			"Containers, data volumes and $DIR are gone."
	else
		quietly "docker compose down" compose down --remove-orphans ||
			die "docker compose down failed (output above); check: cd $DIR && docker compose ps"
		ok "containers removed"
		info "data volumes and $DIR were kept"
		banner_ok "TGProxy panel stopped" \
			"Containers removed; data volumes and $DIR were kept." \
			"" \
			"Remove them too:  sudo $DIR/install.sh --uninstall --purge" \
			"Start again:      sudo $DIR/install.sh --update"
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
	if [[ "$INTERACTIVE" -eq 1 ]]; then
		step "Settings"
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
			[[ "$ADMIN_PASSWORD" == "$again" ]] || die "the passwords do not match; run the installer again"
		fi
	fi
	if [[ -z "$ADMIN_PASSWORD" ]]; then
		ADMIN_PASSWORD="$(gen_password)"
		PASSWORD_GENERATED=1
	fi
	[[ ${#ADMIN_PASSWORD} -ge 8 ]] || usage_error "the admin password must be at least 8 characters"
}

# check_dir: the shape of --dir; whether its contents are acceptable is a pre-flight check.
check_dir() {
	[[ "$DIR" == /* ]] || usage_error "--dir must be an absolute path"
	if [[ -e "$DIR" && ! -d "$DIR" ]]; then
		die "$DIR exists and is not a directory; pick another --dir"
	fi
}

show_plan() {
	step "Plan"
	resolve_version
	if [[ "$MODE" == "update" ]]; then
		update_env
	fi
	info "$(tr '[:lower:]' '[:upper:]' <<<"${MODE:0:1}")${MODE:1} the TGProxy panel"
	if truthy "$LOCAL"; then
		info "Mode:        local (no domain, no TLS, panel on :8080)"
	else
		info "Domain:      $DOMAIN"
		info "ACME e-mail: ${EMAIL:-(none)}"
	fi
	if [[ "$MODE" == "install" ]]; then
		info "Admin user:  $ADMIN_USER"
		if [[ "$PASSWORD_GENERATED" -eq 1 ]]; then
			info "Password:    generated, printed at the end"
		else
			info "Password:    as provided"
		fi
	fi
	info "Version:     $VERSION"
	info "Image:       $(image_ref)"
	info "Directory:   $DIR"
}

do_install_or_update() {
	printf '%sTGProxy panel installer%s\n' "$BOLD" "$RESET"
	check_host
	check_dir
	if [[ "$MODE" == "install" && -f "$DIR/.env" ]]; then
		ok "existing installation in $DIR: update mode"
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
		run_preflight
	fi
	ensure_docker
	show_plan
	if [[ "$INTERACTIVE" -eq 1 ]]; then
		printf '\n'
		confirm "Proceed?" y || die "aborted"
	fi

	fetch_deploy_files
	step "Configuration"
	if [[ "$MODE" == "install" ]]; then
		write_env
	else
		# A domain or e-mail passed on the command line updates the stored values.
		if ! truthy "$LOCAL"; then
			env_set "$DIR/.env" PANEL_DOMAIN "$DOMAIN"
			[[ -z "$EMAIL" ]] || env_set "$DIR/.env" ACME_EMAIL "$EMAIL"
		fi
		ok ".env kept: $DIR/.env"
		ok "PANEL_VERSION: $VERSION"
		sync_engine_pins
	fi
	compose_setup
	step "Stack"
	info "image: $(image_ref)"
	if [[ "$MODE" == "update" ]]; then
		pull_panel_image
	fi
	quietly "docker compose up" compose up -d --remove-orphans --quiet-pull ||
		die "could not start the stack (output above); check: cd $DIR && docker compose ps && docker compose logs --tail 50"
	ok "containers started"
	wait_for "panel healthy" "$HEALTH_BUDGET" healthz_ok || health_failed
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
