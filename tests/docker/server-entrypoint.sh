#!/bin/sh
# E2E-002 server container entrypoint. The harness `docker compose cp`s the
# per-run server config (relay group_pubkeys + secrets are per-run) to
# /server.yaml *after* the container is created and *before* it is started, so
# the file is already present here; we still poll defensively, then exec the
# real `node`. stdout/stderr are inherited (tini is PID1) so the harness can
# read libp2p's ephemeral relay peer id from the "relay: started" JSON line
# via `docker compose logs` — the same line e2e/src/lib/server-process.ts
# parses for §3.1.
set -eu

: "${SERVER_CONFIG:?SERVER_CONFIG must be set by docker-compose}"

i=0
while [ ! -f "$SERVER_CONFIG" ]; do
  i=$((i + 1))
  if [ "$i" -gt 600 ]; then
    echo "server-entrypoint: $SERVER_CONFIG was never provided by the harness" >&2
    exit 1
  fi
  sleep 0.2
done

exec /usr/local/bin/server
