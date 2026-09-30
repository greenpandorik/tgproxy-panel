#!/usr/bin/env sh
# Entry point for the panel image: stages the agent binary into $DATA_DIR so
# the install/register API can serve it, then execs the panel binary. The
# subscription page service ("subpage") needs neither, so it starts directly.
set -eu

if [ "${1:-}" = "subpage" ]; then
	exec /app/panel "$@"
fi

mkdir -p "$DATA_DIR/agent"
cp /app/agent-dist/* "$DATA_DIR/agent/"

exec /app/panel "$@"
