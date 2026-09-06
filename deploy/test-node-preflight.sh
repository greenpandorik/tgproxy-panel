#!/usr/bin/env bash
# Behaviour test of the node install script's pre-flight checks (make test-node-preflight).
#
# Renders the telemt install script for a node whose hostname does not resolve
# (TestRenderForPreflight writes it), then runs it in an ubuntu:24.04 container that fakes
# just enough of a VPS: a systemctl stub and /run/systemd/system, a busybox httpd answering
# the panel's /healthz on loopback, and an apt-get stub that records the call and fails. The
# container has no network and no tty, exactly the situation of `curl … | sudo bash` in a
# cloud console. Four runs:
#   1. no tty, hostname unresolvable          -> exit 1, the dns line is a cross, the
#      "nothing was installed" message, apt-get never called, /usr/bin/caddy absent
#   2. TGWP_SKIP_PREFLIGHT=1 TGWP_DRY_RUN=1   -> exit 0, the warning, "dry run: stopping"
#   3. hostname resolving to this server (/etc/hosts) -> exit 0 (dry run), the dns line is a check
#   4. a pseudo-tty (util-linux script) answering [r] then [c] to the menu -> exit 0 (dry run)
#
# Needs Docker on the host (Docker Desktop's CLI dir is added to PATH; on Apple Silicon the
# amd64 image runs under emulation, a node is x86_64 like the real thing), Go, and shellcheck
# for this script. Ends with "TEST-NODE-PREFLIGHT OK" on success.
set -euo pipefail

export PATH="/Applications/Docker.app/Contents/Resources/bin:/opt/homebrew/bin:$PATH"

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_DIR="$(cd "$SCRIPT_DIR/.." && pwd)"
IMAGE="${TEST_NODE_PREFLIGHT_IMAGE:-tgproxy-node-preflight:test}"
WORK="$(mktemp -d "${TMPDIR:-/tmp}/tgwp-node-preflight.XXXXXX")"
# The address the script is told is "this server" (TGWP_PUBLIC_IP), so the dns check has
# something to compare against without touching the network.
SERVER_IP="203.0.113.10"

log() { echo "[test-node-preflight] $*"; }
die() {
	echo "[test-node-preflight] FAIL: $*" >&2
	exit 1
}
trap 'rm -rf "$WORK"' EXIT

# --- 1. static checks and the rendered script -------------------------------------------
for bin in docker go; do
	command -v "$bin" >/dev/null 2>&1 || die "$bin is required on PATH"
done
if ! command -v shellcheck >/dev/null 2>&1; then
	log "shellcheck missing; installing with brew"
	brew install shellcheck
fi
log "bash -n / shellcheck deploy/test-node-preflight.sh"
bash -n "$SCRIPT_DIR/test-node-preflight.sh"
shellcheck "$SCRIPT_DIR/test-node-preflight.sh"

log "rendering the telemt install script (go test -run TestRenderForPreflight)"
(cd "$REPO_DIR" && go test ./internal/nodeinstall -count=1 -run 'TestRenderForPreflight$' -args -preflight-out "$WORK/node.sh" >/dev/null)
[[ -s "$WORK/node.sh" ]] || die "TestRenderForPreflight wrote no script"
grep -q "^NODE_HOSTNAME='node.test'$" "$WORK/node.sh" || die "rendered script is not for node.test"

# --- 2. the container image -------------------------------------------------------------
# ubuntu:24.04 plus what a real Ubuntu server has and the base image lacks: curl (the script
# arrives through it), iproute2 (ss, for the ports check), busybox (the panel stub). The
# build is cached after the first run.
log "building $IMAGE"
docker build -q --platform linux/amd64 -t "$IMAGE" - >/dev/null <<'EOF'
FROM ubuntu:24.04
RUN apt-get update \
 && DEBIAN_FRONTEND=noninteractive apt-get install -y --no-install-recommends ca-certificates curl iproute2 busybox \
 && rm -rf /var/lib/apt/lists/*
EOF

# --- 3. the in-container wrapper --------------------------------------------------------
cat >"$WORK/run.sh" <<'EOF'
#!/usr/bin/env bash
# Runs inside the container as root. Fakes the parts of a VPS the pre-flight looks at, runs
# the rendered script, then reports what happened as RESULT lines for the host to grep.
set -u
mkdir -p /run/systemd/system /www
echo ok >/www/healthz
printf '#!/bin/sh\nexit 0\n' >/usr/local/bin/systemctl
printf '#!/bin/sh\ntouch /tmp/apt-get-called\necho "apt-get called during pre-flight" >&2\nexit 100\n' >/usr/local/bin/apt-get
chmod 0755 /usr/local/bin/systemctl /usr/local/bin/apt-get
busybox httpd -p 127.0.0.1:8080 -h /www
# The "resolving" case: an /etc/hosts entry is what getent ahostsv4 sees on a real host too
# (docker run --add-host does not write it for a --network none container).
if [[ -n "${ADD_HOST:-}" ]]; then echo "$ADD_HOST" >>/etc/hosts; fi
if [[ "${WITH_TTY:-}" == "1" ]]; then
	# util-linux script gives the child a pseudo-terminal, so /dev/tty exists and the menu
	# reads the answers forwarded from our stdin.
	printf '%s' "${TTY_INPUT:-}" | script -qec 'bash /t/node.sh' /dev/null
	rc=$?
else
	bash /t/node.sh
	rc=$?
fi
echo "RESULT rc=$rc"
if [[ -e /tmp/apt-get-called ]]; then echo "RESULT apt-get=called"; else echo "RESULT apt-get=not-called"; fi
if [[ -e /usr/bin/caddy ]]; then echo "RESULT caddy=present"; else echo "RESULT caddy=absent"; fi
EOF

# run_case <name> <docker run args...>: runs the wrapper, keeps the output in $WORK/<name>.log.
run_case() {
	local name=$1
	shift
	log "case $name"
	docker run --rm --platform linux/amd64 --network none \
		-e LANG=C.UTF-8 -e RES_OPTIONS='timeout:1 attempts:1' -e TGWP_PUBLIC_IP="$SERVER_IP" \
		-v "$WORK:/t:ro" "$@" "$IMAGE" bash /t/run.sh >"$WORK/$name.log" 2>&1 || true
	sed 's/^/    | /' "$WORK/$name.log"
}
expect() { # <name> <pattern>...: every pattern must match a line of the case's log
	local name=$1 pat
	shift
	for pat in "$@"; do
		grep -qE -- "$pat" "$WORK/$name.log" || die "case $name: expected /$pat/ in the output above"
	done
}
reject() { # <name> <pattern>...: no line may match
	local name=$1 pat
	shift
	for pat in "$@"; do
		if grep -qE -- "$pat" "$WORK/$name.log"; then die "case $name: did not expect /$pat/ in the output above"; fi
	done
}

# --- 4. the cases -----------------------------------------------------------------------
run_case unresolvable
expect unresolvable '^RESULT rc=1$' '✘ dns +no A record for node\.test' \
	'✘ nothing was installed; fix the record and run the same command again' \
	'TGWP_SKIP_PREFLIGHT=1 bash' '✔ panel +http://127\.0\.0\.1:8080 reachable' \
	"✔ public_ip +$SERVER_IP \\(TGWP_PUBLIC_IP\\)" '✔ ports +80, 443, 8443 free' \
	'^RESULT apt-get=not-called$' '^RESULT caddy=absent$'
reject unresolvable 'What now\?' 'dry run'

run_case skip -e TGWP_SKIP_PREFLIGHT=1 -e TGWP_DRY_RUN=1
expect skip '^RESULT rc=0$' '✘ dns +no A record' 'TGWP_SKIP_PREFLIGHT=1: continuing despite the failed checks' \
	'dry run: stopping before installation' '^RESULT apt-get=not-called$'

run_case resolving -e ADD_HOST="$SERVER_IP node.test" -e TGWP_DRY_RUN=1
expect resolving '^RESULT rc=0$' "✔ dns +node\\.test → $SERVER_IP \\(this server\\)" \
	'! tls_domain +sni\.test does not resolve' 'dry run: stopping before installation' '^RESULT apt-get=not-called$'
reject resolving '✘'

# The menu: [r] re-runs the checks (the block prints twice), [c] goes on to the dry-run exit.
run_case menu -e WITH_TTY=1 -e TTY_INPUT=rc -e TGWP_DRY_RUN=1
expect menu '^RESULT rc=0$' 'What now\?  \[r\] re-run the checks   \[c\] continue anyway   \[q\] quit' \
	'continuing anyway' 'dry run: stopping before installation' '^RESULT apt-get=not-called$'
[[ "$(grep -c 'no A record for node.test' "$WORK/menu.log")" -ge 2 ]] || die "case menu: [r] did not re-run the checks"

log "TEST-NODE-PREFLIGHT OK"
