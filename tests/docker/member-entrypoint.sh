#!/bin/sh
# E2E-002 member-device container entrypoint. /work is this device's PRIVATE
# bind (no other member container mounts it), so the secret memberKeyHex /
# groupKeyHex in config.json and the result are physically isolated per device
# (docs/design/testing.md §3.3). The harness writes /work/config.json only after it
# has parsed the relay peer id; we poll for it, then exec the unmodified
# CLI-001 member harness. Peer discovery + barriers go through the shared,
# secret-free /rz rendezvous volume; the actual MPC traffic goes over the real
# libp2p Noise + circuit-relay v2 network across containers.
set -eu

: "${MEMBER_INDEX:?MEMBER_INDEX must be set by docker-compose}"

CFG="/work/config.json"
i=0
while [ ! -f "$CFG" ]; do
  i=$((i + 1))
  if [ "$i" -gt 900 ]; then
    echo "member-entrypoint: $CFG not provided for member $MEMBER_INDEX" >&2
    exit 1
  fi
  sleep 0.2
done

exec /usr/local/bin/cli member "$CFG"
