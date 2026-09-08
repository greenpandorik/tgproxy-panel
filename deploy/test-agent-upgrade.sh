#!/usr/bin/env bash
# Behaviour test of `tgwp-agent upgrade` against a stub panel (make test-agent-upgrade).
#
# Builds the real agent for linux/amd64 and runs it in an ubuntu:24.04 container that fakes
# just enough of a node: a busybox httpd on loopback answering GET /api/v1/node/upgrade with a
# manifest, a `telemt` stub that prints a version, a `systemctl` stub that records what it was
# asked to do, and /etc/tgwp-agent/agent.env with a panel URL and a node token. The container
# has no network, no tty and no systemd - the situation of an operator pasting one command
# over ssh, minus the parts only a real node has. Four runs:
#   1. stale     -> --check reports "3.5.5 → 3.5.6" and an agent upgrade, changes nothing,
#      restarts nothing, and never prints the node token
#   2. current   -> --check reports both components up to date and "nothing to do"
#   3. no-tty    -> upgrade without --check and without --yes stops with the --yes hint,
#      before downloading or restarting anything
#   4. bad-sha   -> the panel's sha256 does not match the file it serves: the run fails with
#      "checksum mismatch", /usr/local/bin/tgwp-agent is byte-for-byte unchanged, no unit was
#      restarted and no .prev copy was left behind
#
# Needs Docker on the host (Docker Desktop's CLI dir is added to PATH), Go, and shellcheck.
# Ends with "TEST-AGENT-UPGRADE OK" on success.
set -euo pipefail

export PATH="/Applications/Docker.app/Contents/Resources/bin:/opt/homebrew/bin:$PATH"

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_DIR="$(cd "$SCRIPT_DIR/.." && pwd)"
IMAGE="${TEST_AGENT_UPGRADE_IMAGE:-tgproxy-agent-upgrade:test}"
WORK="$(mktemp -d "${TMPDIR:-/tmp}/tgwp-agent-upgrade.XXXXXX")"
# The token the stub panel would have issued. It is in the env file the command reads, so it
# is exactly what must never appear in the output.
NODE_TOKEN="s3cr3t-node-token-must-not-be-printed"

log() { echo "[test-agent-upgrade] $*"; }
die() {
	echo "[test-agent-upgrade] FAIL: $*" >&2
	exit 1
}
trap 'rm -rf "$WORK"' EXIT

# --- 1. static checks -------------------------------------------------------------------
for bin in docker go; do
	command -v "$bin" >/dev/null 2>&1 || die "$bin is required on PATH"
done
if ! command -v shellcheck >/dev/null 2>&1; then
	log "shellcheck missing; installing with brew"
	brew install shellcheck
fi
log "bash -n / shellcheck deploy/test-agent-upgrade.sh"
bash -n "$SCRIPT_DIR/test-agent-upgrade.sh"
shellcheck "$SCRIPT_DIR/test-agent-upgrade.sh"

# --- 2. the agent binary under test -----------------------------------------------------
log "building the agent for linux/amd64"
(cd "$REPO_DIR" && CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -trimpath -o "$WORK/tgwp-agent" ./cmd/agent)
[[ -s "$WORK/tgwp-agent" ]] || die "the agent binary was not built"

# --- 3. the container image -------------------------------------------------------------
# ubuntu:24.04 plus busybox, which plays the panel: httpd serving a directory is enough for
# a manifest and a binary download.
log "building $IMAGE"
docker build -q --platform linux/amd64 -t "$IMAGE" - >/dev/null <<'EOF'
FROM ubuntu:24.04
RUN apt-get update \
 && DEBIAN_FRONTEND=noninteractive apt-get install -y --no-install-recommends ca-certificates busybox \
 && rm -rf /var/lib/apt/lists/*
EOF

# --- 4. the in-container wrapper --------------------------------------------------------
cat >"$WORK/run.sh" <<'EOF'
#!/usr/bin/env bash
# Runs inside the container as root. Builds the node's world, serves the manifest the case
# asked for, runs the agent, and reports RESULT lines the host greps.
set -u
install -m 0755 /t/tgwp-agent /usr/local/bin/tgwp-agent
AGENT_VERSION="$(/usr/local/bin/tgwp-agent version)"
BEFORE="$(sha256sum /usr/local/bin/tgwp-agent | cut -d' ' -f1)"

# The panel: a directory served on loopback. The agent binary it hands out is a different
# file from the installed one, so a successful replacement would be visible.
mkdir -p /www
printf 'this is the new agent binary\n' >/www/agent
AGENT_SHA="$(sha256sum /www/agent | cut -d' ' -f1)"
if [[ "${BAD_SHA:-}" == "1" ]]; then
	AGENT_SHA="0000000000000000000000000000000000000000000000000000000000000000"
fi
# busybox httpd serves files by path, so the manifest is simply the file the agent GETs.
TELEMT_SHA="aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
mkdir -p /www/api/v1/node
cat >/www/api/v1/node/upgrade <<JSON
{"engine":"telemt",
 "telemt":{"version":"${WANT_TELEMT}","sha256":"$TELEMT_SHA","url":"http://127.0.0.1:8080/telemt.tar.gz"},
 "agent":{"version":"${WANT_AGENT:-$AGENT_VERSION}","sha256":"$AGENT_SHA","url":"http://127.0.0.1:8080/agent"}}
JSON
busybox httpd -p 127.0.0.1:8080 -h /www

# The node's own files. 0600 like the installer writes it: it holds the node token.
mkdir -p /etc/tgwp-agent
umask 077
cat >/etc/tgwp-agent/agent.env <<ENV
TGWP_PANEL_URL=http://127.0.0.1:8080
TGWP_TOKEN=$NODE_TOKEN
TGWP_ENGINE=telemt
ENV
umask 022

# The installed telemt, and a systemctl that records rather than acts.
printf '#!/bin/sh\necho "telemt %s"\n' "$HAVE_TELEMT" >/usr/local/bin/telemt
chmod 0755 /usr/local/bin/telemt
printf '#!/bin/sh\necho "systemctl $*" >>/tmp/systemctl.log\nexit 0\n' >/usr/local/bin/systemctl
chmod 0755 /usr/local/bin/systemctl
: >/tmp/systemctl.log

# shellcheck disable=SC2086  # ARGS is a deliberate word list
/usr/local/bin/tgwp-agent upgrade $ARGS
rc=$?
echo "RESULT rc=$rc"
AFTER="$(sha256sum /usr/local/bin/tgwp-agent | cut -d' ' -f1)"
if [[ "$BEFORE" == "$AFTER" ]]; then echo "RESULT binary=unchanged"; else echo "RESULT binary=replaced"; fi
if [[ -s /tmp/systemctl.log ]]; then
	echo "RESULT systemctl=called"
	sed 's/^/RESULT systemctl-call: /' /tmp/systemctl.log
else
	echo "RESULT systemctl=not-called"
fi
if [[ -e /usr/local/bin/tgwp-agent.prev ]]; then echo "RESULT prev=present"; else echo "RESULT prev=absent"; fi
echo "RESULT agent-version=$AGENT_VERSION"
EOF

# run_case <name> <docker run args...>: runs the wrapper, keeps the output in $WORK/<name>.log.
run_case() {
	local name=$1
	shift
	log "case $name"
	docker run --rm --platform linux/amd64 --network none \
		-e LANG=C.UTF-8 -e NODE_TOKEN="$NODE_TOKEN" \
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

# --- 5. the cases -----------------------------------------------------------------------
# 1. Both components behind the panel's pins: --check says so and does nothing.
run_case stale -e ARGS="--check" -e HAVE_TELEMT=3.5.5 -e WANT_TELEMT=3.5.6 -e WANT_AGENT=9.9.9
expect stale '^RESULT rc=0$' 'the panel pins: telemt 3\.5\.6, agent 9\.9\.9' \
	'! telemt +3\.5\.5 → 3\.5\.6' '! agent +[0-9.]+ → 9\.9\.9' \
	'--check: 2 component\(s\) would be replaced; nothing was changed' \
	'^RESULT binary=unchanged$' '^RESULT systemctl=not-called$' '^RESULT prev=absent$'
reject stale "$NODE_TOKEN" '✘'

# 2. A node already at the pins: every line is a check and there is nothing to do.
run_case current -e ARGS="--check" -e HAVE_TELEMT=3.5.6 -e WANT_TELEMT=3.5.6
expect current '^RESULT rc=0$' '✔ telemt +up to date \(3\.5\.6\)' '✔ agent +up to date' \
	'--check: nothing to do' '^RESULT binary=unchanged$' '^RESULT systemctl=not-called$'
reject current "$NODE_TOKEN" '✘'

# 3. Unattended and unconfirmed: the one combination that must refuse, before touching
#    anything. (This is `curl … | bash`'s situation, and an upgrade is not a fresh install.)
run_case no-tty -e ARGS="--agent" -e HAVE_TELEMT=3.5.6 -e WANT_TELEMT=3.5.6 -e WANT_AGENT=9.9.9
expect no-tty '^RESULT rc=1$' '✘ not a terminal: re-run with --yes' \
	'^RESULT binary=unchanged$' '^RESULT systemctl=not-called$' '^RESULT prev=absent$'
reject no-tty "$NODE_TOKEN"

# 4. The checksum gate: the panel's sha256 does not match the file it serves. Nothing is
#    replaced, nothing is restarted, and no half-finished state is left on disk.
run_case bad-sha -e ARGS="--agent --yes" -e BAD_SHA=1 -e HAVE_TELEMT=3.5.6 -e WANT_TELEMT=3.5.6 -e WANT_AGENT=9.9.9
expect bad-sha '^RESULT rc=1$' '✘ checksum mismatch for http://127\.0\.0\.1:8080/agent' \
	'Nothing was replaced' '^RESULT binary=unchanged$' '^RESULT systemctl=not-called$' '^RESULT prev=absent$'
reject bad-sha "$NODE_TOKEN" 'RESULT binary=replaced'

log "TEST-AGENT-UPGRADE OK"
