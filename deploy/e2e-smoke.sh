#!/usr/bin/env bash
# End-to-end smoke test for the panel + fakenode stack.
#
# Two-phase flow, matching the fakenode container needing an install token
# that only exists after a node has been created:
#
#   PANEL_URL=... ADMIN_USER=... ADMIN_PASS=... deploy/e2e-smoke.sh
#     -> creates the node, prints FAKENODE_INSTALL_TOKEN=<token> and exits
#   FAKENODE_INSTALL_TOKEN=<token> docker compose --profile dev up -d --build fakenode
#   PANEL_URL=... ADMIN_USER=... ADMIN_PASS=... deploy/e2e-smoke.sh --continue
#     -> waits for the node to come online, assigns a key + site, applies,
#        and verifies the real relay picked it all up
#
# Requires: curl, jq. Ends with the line "SMOKE OK" on success.
set -euo pipefail

PANEL_URL="${PANEL_URL:?PANEL_URL is required}"
ADMIN_USER="${ADMIN_USER:?ADMIN_USER is required}"
ADMIN_PASS="${ADMIN_PASS:?ADMIN_PASS is required}"
NODE_HOSTNAME="${NODE_HOSTNAME:-fakenode.local}"

CONTINUE=0
if [[ "${1:-}" == "--continue" ]]; then
	CONTINUE=1
fi

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
STATE_FILE="${E2E_STATE_FILE:-$SCRIPT_DIR/.e2e-smoke-state}"
COOKIE_JAR="$(mktemp -t tgwp-e2e-cookies.XXXXXX)"
trap 'rm -f "$COOKIE_JAR"' EXIT

log() { echo "[e2e] $*"; }
die() {
	echo "[e2e] FAIL: $*" >&2
	exit 1
}

for bin in curl jq docker; do
	command -v "$bin" >/dev/null 2>&1 || die "$bin is required on PATH"
done

compose() { (cd "$SCRIPT_DIR" && docker compose "$@"); }

cookie_value() {
	awk -F'\t' -v name="$1" '$6==name{v=$7} END{print v}' "$COOKIE_JAR" 2>/dev/null
}

# http METHOD PATH [JSON_BODY] -> body on stdout, status code in $LAST_STATUS
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
	log "creating node $NODE_HOSTNAME"
	# The fakenode container runs the tproxy stack (tproxy-server + MTProxy), so the
	# node is created with that engine explicitly - the panel default is telemt.
	http POST /api/v1/nodes "$(jq -n --arg n "fakenode" --arg h "$NODE_HOSTNAME" --arg e "admin@example.com" \
		'{name:$n,hostname:$h,acme_email:$e,engine:"tproxy"}')"
	expect_status 201 "$LAST_STATUS" "create node"
	local node_id install_cmd token
	node_id="$(jq -r '.node.id' <<<"$LAST_BODY")"
	install_cmd="$(jq -r '.install_command' <<<"$LAST_BODY")"
	token="$(sed -n 's#.*/install/\([^.]*\)\.sh.*#\1#p' <<<"$install_cmd")"
	[[ -n "$node_id" && "$node_id" != "null" ]] || die "create node: missing node id in response: $LAST_BODY"
	[[ -n "$token" ]] || die "create node: could not parse install token from: $install_cmd"

	{
		echo "NODE_ID=$node_id"
		echo "NODE_HOSTNAME=$NODE_HOSTNAME"
	} >"$STATE_FILE"
	log "node created: id=$node_id"
	echo "FAKENODE_INSTALL_TOKEN=$token"
	log "now run (keep the same -f flags you used to start panel, or fakenode's 'up' will recreate it and drop its port mapping):"
	log "  FAKENODE_INSTALL_TOKEN=$token docker compose -f docker-compose.yml -f docker-compose.override.example.yml --profile dev up -d --build fakenode"
	log "then re-run this script with --continue"
}

wait_node_online() {
	local id="$1" deadline elapsed=0
	log "waiting for node to come online (up to 60s)"
	deadline=60
	while true; do
		http GET "/api/v1/nodes/$id"
		expect_status 200 "$LAST_STATUS" "get node"
		local status
		status="$(jq -r '.status' <<<"$LAST_BODY")"
		[[ "$status" == "online" ]] && {
			log "node is online"
			return 0
		}
		elapsed=$((elapsed + 2))
		[[ "$elapsed" -ge "$deadline" ]] && die "node did not come online within ${deadline}s (last status: $status)"
		sleep 2
	done
}

wait_apply_ok() {
	local id="$1" deadline=90 elapsed=0
	log "waiting for the apply job to finish (up to 90s)"
	while true; do
		http GET "/api/v1/nodes/$id/jobs?limit=1"
		expect_status 200 "$LAST_STATUS" "list jobs"
		local status log_text
		status="$(jq -r '.items[0].status // empty' <<<"$LAST_BODY")"
		case "$status" in
		ok)
			log "apply job ok"
			return 0
			;;
		failed | rolled_back)
			log_text="$(jq -r '.items[0].log // empty' <<<"$LAST_BODY")"
			die "apply job $status:
$log_text"
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

	log "creating a personal key bound to the node"
	http POST /api/v1/keys "$(jq -n --arg id "$NODE_ID" \
		'{label:"e2e-smoke",type:"PERSONAL",carrier_mode:"https",node_ids:[$id]}')"
	expect_status 201 "$LAST_STATUS" "create key"
	local key_id
	key_id="$(jq -r '.id' <<<"$LAST_BODY")"
	[[ -n "$key_id" && "$key_id" != "null" ]] || die "create key: missing id in response: $LAST_BODY"
	log "key created: id=$key_id"

	log "assigning the studio preset site to the node"
	http GET /api/v1/site-templates
	expect_status 200 "$LAST_STATUS" "list site templates"
	local template_id
	template_id="$(jq -r '.items[] | select(.name=="studio") | .id' <<<"$LAST_BODY" | head -n1)"
	[[ -n "$template_id" ]] || die "no 'studio' preset found in site-templates: $LAST_BODY"
	http POST "/api/v1/nodes/$NODE_ID/site" "$(jq -n --arg t "$template_id" '{template_id:$t}')"
	expect_status 200 "$LAST_STATUS" "assign site"

	log "triggering apply"
	http POST "/api/v1/nodes/$NODE_ID/apply" ""
	expect_status 202 "$LAST_STATUS" "trigger apply"

	wait_apply_ok "$NODE_ID"

	log "checking key status is active"
	http GET "/api/v1/keys/$key_id"
	expect_status 200 "$LAST_STATUS" "get key"
	local key_status
	key_status="$(jq -r '.status' <<<"$LAST_BODY")"
	[[ "$key_status" == "active" ]] || die "expected key status active, got $key_status: $LAST_BODY"

	log "checking key links"
	http GET "/api/v1/keys/$key_id/links"
	expect_status 200 "$LAST_STATUS" "key links"
	local tme kind kinds
	# Links are grouped per node; a tproxy node offers the WEB kind only.
	kinds="$(jq -r '[.items[0].links[].kind] | join(",")' <<<"$LAST_BODY")"
	[[ "$kinds" == "web" ]] || die "expected only the web link kind on a tproxy node, got: $kinds (body: $LAST_BODY)"
	kind="$(jq -r '.items[0].links[0].kind // empty' <<<"$LAST_BODY")"
	[[ "$kind" == "web" ]] || die "unexpected link kind: $kind (body: $LAST_BODY)"
	tme="$(jq -r '.items[0].links[0].tme // empty' <<<"$LAST_BODY")"
	case "$tme" in
	"https://t.me/webproxy?server=$NODE_HOSTNAME&secret="*) log "link ok: $tme" ;;
	*) die "unexpected t.me link: $tme (body: $LAST_BODY)" ;;
	esac

	log "creating a subscription link for the key"
	http POST "/api/v1/keys/$key_id/subscription" ""
	expect_status 200 "$LAST_STATUS" "create subscription"
	local sub_url sub_token
	sub_url="$(jq -r '.url' <<<"$LAST_BODY")"
	[[ -n "$sub_url" && "$sub_url" != "null" ]] || die "create subscription: missing url in response: $LAST_BODY"
	sub_token="${sub_url##*/s/}"
	[[ -n "$sub_token" ]] || die "create subscription: could not parse token from url: $sub_url"

	log "checking the public subscription page"
	http GET "/s/$sub_token"
	expect_status 200 "$LAST_STATUS" "subscription page"
	grep -q "$NODE_HOSTNAME" <<<"$LAST_BODY" || die "subscription page missing $NODE_HOSTNAME: $LAST_BODY"
	grep -q "e2e-smoke" <<<"$LAST_BODY" && die "subscription page leaks the key label: $LAST_BODY"
	log "subscription page ok (has $NODE_HOSTNAME, no key label)"

	log "checking the QR endpoint returns a PNG"
	local qr_file
	qr_file="$(mktemp)"
	local qr_status
	qr_status="$(curl -sS -o "$qr_file" -w '%{http_code}' -b "$COOKIE_JAR" -c "$COOKIE_JAR" \
		"$PANEL_URL/api/v1/keys/$key_id/qr?node=$NODE_ID")"
	[[ "$qr_status" == "200" ]] || die "qr endpoint: expected HTTP 200, got $qr_status"
	if [[ "$(head -c8 "$qr_file" | od -An -tx1 | tr -d ' \n')" != "89504e470d0a1a0a" ]]; then
		rm -f "$qr_file"
		die "qr endpoint did not return a PNG"
	fi
	rm -f "$qr_file"
	log "qr endpoint ok"

	log "checking the fakenode relay is ready"
	local readyz
	readyz="$(compose exec -T fakenode curl -s http://127.0.0.1:8081/readyz)"
	[[ "$(tr -d '\r\n' <<<"$readyz")" == "ready" ]] || die "fakenode /readyz did not report ready: $readyz"
	log "fakenode relay is ready"

	log "checking profiles.json contains the key's profile"
	local profile_name profiles_json
	profile_name="k$(tr -d '-' <<<"$key_id" | cut -c1-12)"
	profiles_json="$(compose exec -T fakenode cat /etc/tproxy-server/profiles.json)"
	if ! grep -q "$profile_name" <<<"$profiles_json"; then
		die "profiles.json does not contain profile $profile_name: $profiles_json"
	fi
	log "profiles.json contains $profile_name"

	log "checking the real relay serves the preset site"
	local site_html
	site_html="$(compose exec -T fakenode curl -s -H "Host: $NODE_HOSTNAME" http://127.0.0.1:8080/)"
	if ! grep -q "Ferrule Studio" <<<"$site_html"; then
		die "relay did not serve the studio preset site: $site_html"
	fi
	log "relay serves the preset site"

	log "running node prerequisite check"
	http POST "/api/v1/nodes/$NODE_ID/check" ""
	expect_status 200 "$LAST_STATUS" "node check"
	local has_dns_a
	has_dns_a="$(jq -r '[.results[].name] | any(. == "dns_a")' <<<"$LAST_BODY")"
	[[ "$has_dns_a" == "true" ]] || die "node check: expected a dns_a result, got: $LAST_BODY"
	log "node check ok (dns_a present; outcome not asserted)"

	log "checking the monitoring overview lists the node"
	http GET /api/v1/monitoring/overview
	expect_status 200 "$LAST_STATUS" "monitoring overview"
	local has_node
	has_node="$(jq -r --arg id "$NODE_ID" '[.nodes[].node_id] | any(. == $id)' <<<"$LAST_BODY")"
	[[ "$has_node" == "true" ]] || die "monitoring overview: expected node $NODE_ID in nodes list, got: $LAST_BODY"
	log "monitoring overview lists the node"

	log "checking the audit log has key.* entries"
	http GET "/api/v1/audit?action=key."
	expect_status 200 "$LAST_STATUS" "audit filter"
	local audit_total
	audit_total="$(jq -r '.total' <<<"$LAST_BODY")"
	[[ "$audit_total" -ge 1 ]] || die "audit?action=key.: expected total >= 1, got $audit_total: $LAST_BODY"
	log "audit log has $audit_total key.* entries"

	log "checking site-templates has 5 presets"
	http GET /api/v1/site-templates
	expect_status 200 "$LAST_STATUS" "list site templates"
	local preset_count
	preset_count="$(jq -r '[.items[] | select(.is_preset == true)] | length' <<<"$LAST_BODY")"
	[[ "$preset_count" == "5" ]] || die "site-templates: expected 5 presets, got $preset_count: $LAST_BODY"
	log "site-templates has 5 presets"

	log "creating a backup (owner)"
	http POST /api/v1/backups ""
	expect_status 201 "$LAST_STATUS" "create backup"
	local backup_id
	backup_id="$(jq -r '.id' <<<"$LAST_BODY")"
	[[ -n "$backup_id" && "$backup_id" != "null" ]] || die "create backup: missing id in response: $LAST_BODY"
	log "backup created: id=$backup_id"

	log "checking the backups list has at least one item"
	http GET /api/v1/backups
	expect_status 200 "$LAST_STATUS" "list backups"
	local backup_count
	backup_count="$(jq -r '.items | length' <<<"$LAST_BODY")"
	[[ "$backup_count" -ge 1 ]] || die "backups list: expected >= 1 item, got $backup_count: $LAST_BODY"
	log "backups list has $backup_count item(s)"

	log "downloading the backup"
	local backup_file backup_status backup_size
	backup_file="$(mktemp)"
	backup_status="$(curl -sS -o "$backup_file" -w '%{http_code}' -b "$COOKIE_JAR" -c "$COOKIE_JAR" \
		"$PANEL_URL/api/v1/backups/$backup_id/download")"
	backup_size="$(wc -c <"$backup_file" | tr -d ' ')"
	rm -f "$backup_file"
	[[ "$backup_status" == "200" ]] || die "backup download: expected HTTP 200, got $backup_status"
	[[ "$backup_size" -gt 0 ]] || die "backup download: expected size > 0, got $backup_size"
	log "backup download ok (size=$backup_size bytes)"

	log "deleting the backup"
	http DELETE "/api/v1/backups/$backup_id"
	expect_status 204 "$LAST_STATUS" "delete backup"
	log "backup deleted"

	rm -f "$STATE_FILE"
	echo "SMOKE OK"
}

if [[ "$CONTINUE" -eq 1 ]]; then
	phase_continue
else
	phase_create_node
fi
