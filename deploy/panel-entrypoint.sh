#!/usr/bin/env sh
# Entry point for the panel image: stages the agent binary into $DATA_DIR so
# the install/register API can serve it, then execs the panel binary.
set -eu

mkdir -p "$DATA_DIR/agent"
cp /app/agent-dist/* "$DATA_DIR/agent/"

exec /app/panel "$@"
