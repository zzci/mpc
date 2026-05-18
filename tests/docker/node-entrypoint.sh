#!/bin/sh
# E2E-002 node container entrypoint. The harness `docker compose cp`s the
# per-run node config (relay group_pubkeys + secrets are per-run) to
# /node.yaml *after* the container is created and *before* it is started, so
# the file is already present here; we still poll defensively, then exec the
# real `node`. stdout/stderr are inherited (tini is PID1) so the harness can
# read libp2p's ephemeral relay peer id from the "relay: started" JSON line
# via `docker compose logs` — the same line e2e/src/lib/node-process.ts
# parses for §3.1.
set -eu

: "${NODE_CONFIG:?NODE_CONFIG must be set by docker-compose}"

i=0
while [ ! -f "$NODE_CONFIG" ]; do
  i=$((i + 1))
  if [ "$i" -gt 600 ]; then
    echo "node-entrypoint: $NODE_CONFIG was never provided by the harness" >&2
    exit 1
  fi
  sleep 0.2
done

exec /usr/local/bin/node
