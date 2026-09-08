#!/usr/bin/env bash
# End-to-end smoke test for the panel + fakenode-telemt stack (spec §6 test bench).
#
# Same two-phase shape as e2e-smoke.sh, because the container needs an install token that
# only exists once a node has been created:
#
#   PANEL_URL=... ADMIN_USER=... ADMIN_PASS=... deploy/e2e-smoke-telemt.sh
#     -> creates a telemt node, prints FAKENODE_INSTALL_TOKEN=<token> and exits
#   FAKENODE_INSTALL_TOKEN=<token> docker compose --profile dev-telemt up -d --build fakenode-telemt
#   PANEL_URL=... ADMIN_USER=... ADMIN_PASS=... deploy/e2e-smoke-telemt.sh --continue
#     -> waits for the node, creates a limited key, and verifies the REAL telemt process on
#        the node received it through the control API without a restart
#
# Requires: curl, jq, docker. Ends with the line "SMOKE OK" on success.
set -euo pipefail

PANEL_URL="${PANEL_URL:?PANEL_URL is required}"
ADMIN_USER="${ADMIN_USER:?ADMIN_USER is required}"
ADMIN_PASS="${ADMIN_PASS:?ADMIN_PASS is required}"
NODE_HOSTNAME="${NODE_HOSTNAME:-fakenode-telemt.local}"
CLASSIC_PORT="${CLASSIC_PORT:-8443}"
NODE_SERVICE=fakenode-telemt

# The key's limits, asserted end to end: the panel stores them, the agent pushes them over
# telemt's control API, and telemt reports them back on /v1/users.
QUOTA_BYTES=1073741824
MAX_UNIQUE_IPS=2

CONTINUE=0
if [[ "${1:-}" == "--continue" ]]; then
	CONTINUE=1
fi

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
STATE_FILE="${E2E_STATE_FILE:-$SCRIPT_DIR/.e2e-smoke-telemt-state}"
COOKIE_JAR="$(mktemp -t tgwp-e2e-telemt-cookies.XXXXXX)"
trap 'rm -f "$COOKIE_JAR"' EXIT

log() { echo "[e2e-telemt] $*"; }
die() {
	echo "[e2e-telemt] FAIL: $*" >&2
	exit 1
}

for bin in curl jq docker; do
	command -v "$bin" >/dev/null 2>&1 || die "$bin is required on PATH"
done

compose() { (cd "$SCRIPT_DIR" && docker compose "$@"); }

# node_exec runs a command inside the fakenode-telemt container.
node_exec() { compose exec -T "$NODE_SERVICE" "$@"; }

# telemt_api GETs one control-API path on the node, authenticating with the token file
# init-node wrote. The token never leaves the container.
telemt_api() {
	# The token goes in on stdin rather than as an argument, so it is not visible in
	# /proc/*/cmdline while the call is in flight.
	node_exec sh -c "curl -s -H @- http://127.0.0.1:9091$1 <<EOF
Authorization: \$(cat /etc/telemt/api.token)
EOF"
}

cookie_value() {
	awk -F'\t' -v name="$1" '$6==name{v=$7} END{print v}' "$COOKIE_JAR" 2>/dev/null
}

# http METHOD PATH [JSON_BODY] -> body in $LAST_BODY, status code in $LAST_STATUS
http() {
	local method="$1" path="$2" body="${3:-}" tmp status
	tmp="$(mktemp)"
	local args=(-sS -o "$tmp" -w '%{http_code}' -b "$COOKIE_JAR" -c "$COOKIE_JAR" -X "$method" "$PANEL_URL$path")
	if [[ -n "$body" ]]; then
		args+=(-H "Content-Type: application/json" -d "$body")
	fi
	if [[ "$method" != "GET" ]]; then
		local csrf
		csrf="$(cookie_value tgwp_csrf)"
		[[ -n "$csrf" ]] && args+=(-H "X-CSRF-Token: $csrf")
	fi
	status="$(curl "${args[@]}")"
	LAST_STATUS="$status"
	LAST_BODY="$(cat "$tmp")"
	rm -f "$tmp"
}

expect_status() {
	local want="$1" got="$2" what="$3"
	[[ "$got" == "$want" ]] || die "$what: expected HTTP $want, got $got: $LAST_BODY"
}

login() {
	log "logging in as $ADMIN_USER"
	http POST /api/v1/auth/login "$(jq -n --arg u "$ADMIN_USER" --arg p "$ADMIN_PASS" '{username:$u,password:$p}')"
	expect_status 200 "$LAST_STATUS" "login"
}

phase_create_node() {
	login
	log "creating telemt node $NODE_HOSTNAME"
	http POST /api/v1/nodes "$(jq -n --arg h "$NODE_HOSTNAME" --arg e "admin@example.com" --argjson p "$CLASSIC_PORT" \
		'{name:"fakenode-telemt",hostname:$h,acme_email:$e,engine:"telemt",tls_domain:$h,classic_port:$p}')"
	expect_status 201 "$LAST_STATUS" "create node"
	local node_id engine install_cmd token
	node_id="$(jq -r '.node.id' <<<"$LAST_BODY")"
	engine="$(jq -r '.node.engine' <<<"$LAST_BODY")"
	install_cmd="$(jq -r '.install_command' <<<"$LAST_BODY")"
	token="$(sed -n 's#.*/install/\([^.]*\)\.sh.*#\1#p' <<<"$install_cmd")"
	[[ -n "$node_id" && "$node_id" != "null" ]] || die "create node: missing node id in response: $LAST_BODY"
	[[ "$engine" == "telemt" ]] || die "create node: engine is $engine, not telemt: $LAST_BODY"
	[[ -n "$token" ]] || die "create node: could not parse install token from: $install_cmd"

	{
		echo "NODE_ID=$node_id"
		echo "NODE_HOSTNAME=$NODE_HOSTNAME"
	} >"$STATE_FILE"
	log "node created: id=$node_id engine=telemt"
	echo "FAKENODE_INSTALL_TOKEN=$token"
	log "now run (keep the same -f flags you used to start panel):"
	log "  FAKENODE_INSTALL_TOKEN=$token docker compose -f docker-compose.yml -f docker-compose.override.example.yml --profile dev-telemt up -d --build $NODE_SERVICE"
	log "then re-run this script with --continue"
}

wait_node_online() {
	local id="$1" deadline=120 elapsed=0 status version
	log "waiting for the node to come online and report its telemt version (up to ${deadline}s)"
	while true; do
		http GET "/api/v1/nodes/$id"
		expect_status 200 "$LAST_STATUS" "get node"
		status="$(jq -r '.status' <<<"$LAST_BODY")"
		version="$(jq -r '.telemt_version' <<<"$LAST_BODY")"
		if [[ "$status" == "online" && -n "$version" && "$version" != "null" ]]; then
			log "node is online, telemt $version"
			return 0
		fi
		elapsed=$((elapsed + 3))
		[[ "$elapsed" -ge "$deadline" ]] && die "node did not come online with a telemt version within ${deadline}s (status=$status version=$version)"
		sleep 3
	done
}

# wait_dc_data waits for the node's health to carry telemt's view of Telegram's datacenters:
# the agent reads GET /v1/stats/upstreams from the real telemt in every heartbeat, so once the
# node is online the next heartbeat must say dc_data_available=true with a non-empty dcs list
# (a DC telemt has not measured yet is still listed, with known=false).
wait_dc_data() {
	local id="$1" deadline=90 elapsed=0 available dcs
	log "waiting for the node's health to report Telegram DC connectivity (up to ${deadline}s)"
	while true; do
		http GET "/api/v1/nodes/$id"
		expect_status 200 "$LAST_STATUS" "get node"
		available="$(jq -r '.health.dc_data_available // false' <<<"$LAST_BODY")"
		dcs="$(jq -r '.health.dcs | length' <<<"$LAST_BODY")"
		if [[ "$available" == "true" && "$dcs" -gt 0 ]]; then
			log "health carries DC data: $(jq -c '.health | {dc_data_available, upstream_healthy, effective_latency_ms, connect_success_total, dcs}' <<<"$LAST_BODY")"
			return 0
		fi
		elapsed=$((elapsed + 3))
		[[ "$elapsed" -ge "$deadline" ]] && die "node health never reported DC data within ${deadline}s (dc_data_available=$available dcs=$dcs): $(jq -c '.health' <<<"$LAST_BODY")"
		sleep 3
	done
}

# apply_and_wait triggers an apply and returns once the newest job is terminal. The job log is
# left in $JOB_LOG so the caller can assert on what the agent actually did.
apply_and_wait() {
	local id="$1" deadline=120 elapsed=0 status
	http POST "/api/v1/nodes/$id/apply" ""
	expect_status 202 "$LAST_STATUS" "trigger apply"
	while true; do
		http GET "/api/v1/nodes/$id/jobs?limit=1"
		expect_status 200 "$LAST_STATUS" "list jobs"
		status="$(jq -r '.items[0].status // empty' <<<"$LAST_BODY")"
		JOB_LOG="$(jq -r '.items[0].log // empty' <<<"$LAST_BODY")"
		case "$status" in
		ok)
			return 0
			;;
		failed | rolled_back)
			die "apply job $status:
$JOB_LOG"
			;;
		esac
		elapsed=$((elapsed + 3))
		[[ "$elapsed" -ge "$deadline" ]] && die "apply job did not finish within ${deadline}s (last status: $status)"
		sleep 3
	done
}

phase_continue() {
	[[ -f "$STATE_FILE" ]] || die "no state file at $STATE_FILE; run this script once without --continue first"
	# shellcheck disable=SC1090
	. "$STATE_FILE"
	[[ -n "${NODE_ID:-}" ]] || die "state file $STATE_FILE has no NODE_ID"

	login
	wait_node_online "$NODE_ID"
	wait_dc_data "$NODE_ID"

	log "creating a personal key with telemt limits, bound to the node"
	http POST /api/v1/keys "$(jq -n --arg id "$NODE_ID" --argjson q "$QUOTA_BYTES" --argjson ips "$MAX_UNIQUE_IPS" \
		'{label:"e2e-telemt",type:"PERSONAL",carrier_mode:"https",node_ids:[$id],
		  telemt_limits:{data_quota_bytes:$q,max_unique_ips:$ips}}')"
	expect_status 201 "$LAST_STATUS" "create key"
	local key_id stored_quota
	key_id="$(jq -r '.id' <<<"$LAST_BODY")"
	stored_quota="$(jq -r '.telemt_limits.data_quota_bytes // 0' <<<"$LAST_BODY")"
	[[ -n "$key_id" && "$key_id" != "null" ]] || die "create key: missing id in response: $LAST_BODY"
	[[ "$stored_quota" == "$QUOTA_BYTES" ]] || die "create key: telemt_limits.data_quota_bytes is $stored_quota, want $QUOTA_BYTES: $LAST_BODY"
	log "key created: id=$key_id"

	log "applying the key to the node"
	apply_and_wait "$NODE_ID"
	# The whole point of the telemt engine: users and profiles are pushed over the control
	# API, so a key reaching the node must never have cost a relay restart.
	grep -q "no restart" <<<"$JOB_LOG" || die "apply job log does not say the apply was restart-free:
$JOB_LOG"
	log "apply ok, restart-free"

	log "checking the key is active"
	http GET "/api/v1/keys/$key_id"
	expect_status 200 "$LAST_STATUS" "get key"
	local key_status
	key_status="$(jq -r '.status' <<<"$LAST_BODY")"
	[[ "$key_status" == "active" ]] || die "expected key status active, got $key_status: $LAST_BODY"

	log "checking the key offers both link kinds"
	http GET "/api/v1/keys/$key_id/links"
	expect_status 200 "$LAST_STATUS" "key links"
	local kinds web_tme tls_tme tls_secret
	kinds="$(jq -r '[.items[0].links[].kind] | sort | join(",")' <<<"$LAST_BODY")"
	[[ "$kinds" == "tls,web" ]] || die "expected both web and tls link kinds on a telemt node, got: $kinds (body: $LAST_BODY)"
	web_tme="$(jq -r '.items[0].links[] | select(.kind=="web") | .tme' <<<"$LAST_BODY")"
	tls_tme="$(jq -r '.items[0].links[] | select(.kind=="tls") | .tme' <<<"$LAST_BODY")"
	case "$web_tme" in
	"https://t.me/webproxy?server=$NODE_HOSTNAME&secret="*) ;;
	*) die "unexpected web t.me link: $web_tme (body: $LAST_BODY)" ;;
	esac
	case "$tls_tme" in
	"https://t.me/proxy?server=$NODE_HOSTNAME&port=$CLASSIC_PORT&secret="*) ;;
	*) die "unexpected tls t.me link: $tls_tme (body: $LAST_BODY)" ;;
	esac
	# Fake-TLS secrets are the "ee" flavour: ee + the 16-byte secret + hex(SNI).
	tls_secret="${tls_tme##*secret=}"
	case "$tls_secret" in
	ee*) ;;
	*) die "tls link secret is not an ee secret: $tls_secret" ;;
	esac
	log "links ok (web + tls, tls secret is ee-prefixed)"

	local profile_name
	profile_name="k$(tr -d '-' <<<"$key_id" | cut -c1-12)"

	log "checking telemt itself knows the user (control API on the node)"
	local users
	users="$(telemt_api /v1/users)"
	jq -e --arg n "$profile_name" '[.data[]?.username] | index($n)' >/dev/null 2>&1 <<<"$users" ||
		die "telemt /v1/users does not list $profile_name: $users"
	local node_quota node_ips
	node_quota="$(jq -r --arg n "$profile_name" '.data[] | select(.username==$n) | .data_quota_bytes // 0' <<<"$users")"
	node_ips="$(jq -r --arg n "$profile_name" '.data[] | select(.username==$n) | .max_unique_ips // 0' <<<"$users")"
	[[ "$node_quota" == "$QUOTA_BYTES" ]] || die "telemt reports data_quota_bytes=$node_quota for $profile_name, want $QUOTA_BYTES: $users"
	[[ "$node_ips" == "$MAX_UNIQUE_IPS" ]] || die "telemt reports max_unique_ips=$node_ips for $profile_name, want $MAX_UNIQUE_IPS: $users"
	log "telemt has $profile_name with data_quota_bytes=$node_quota max_unique_ips=$node_ips"

	log "checking the key's WEB profile reached telemt's vhost config"
	local cfg
	cfg="$(telemt_api /v1/config)"
	jq -e --arg n "$profile_name" '[.data.web.vhosts[]?.profiles[]?.user] | index($n)' >/dev/null 2>&1 <<<"$cfg" ||
		die "telemt vhost profiles do not include $profile_name: $(head -c 2000 <<<"$cfg")"
	log "vhost profile present"

	log "checking the key stats endpoint"
	http GET "/api/v1/keys/$key_id/stats"
	expect_status 200 "$LAST_STATUS" "key stats"
	jq -e 'has("nodes")' >/dev/null <<<"$LAST_BODY" || die "key stats has no nodes field: $LAST_BODY"
	log "key stats ok"

	log "revoking the key"
	http POST "/api/v1/keys/$key_id/revoke" ""
	[[ "$LAST_STATUS" == "200" || "$LAST_STATUS" == "204" ]] || die "revoke key: expected HTTP 200/204, got $LAST_STATUS: $LAST_BODY"
	apply_and_wait "$NODE_ID"
	users="$(telemt_api /v1/users)"
	if jq -e --arg n "$profile_name" '[.data[]?.username] | index($n)' >/dev/null 2>&1 <<<"$users"; then
		die "telemt still lists $profile_name one apply after the key was revoked: $users"
	fi
	log "telemt dropped $profile_name after the revoke"

	rm -f "$STATE_FILE"
	echo "SMOKE OK"
}

if [[ "$CONTINUE" -eq 1 ]]; then
	phase_continue
else
	phase_create_node
fi
