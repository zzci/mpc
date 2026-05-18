#!/usr/bin/env bash
# scripts/ci/install-bun.sh — install a pinned Bun and export it onto PATH.
#
# Bun runs the E2E-002 Docker isolated ring (e2e/, docs/design/testing.md §3.3) and
# the e2e static gate (lint + typecheck). Pinned for reproducibility.

set -euo pipefail
source "$(dirname "${BASH_SOURCE[0]}")/_lib.sh"

if command -v bun >/dev/null 2>&1 && [ "$(bun --version)" = "${BUN_VERSION}" ]; then
  log "bun ${BUN_VERSION} already present"
else
  log "installing bun ${BUN_VERSION}"
  curl -fsSL https://bun.sh/install | bash -s "bun-v${BUN_VERSION}"
fi

BUN_BIN="${HOME}/.bun/bin"
export PATH="${BUN_BIN}:${PATH}"
# Persist for subsequent GitHub Actions steps.
if [ -n "${GITHUB_PATH:-}" ]; then
  echo "${BUN_BIN}" >>"${GITHUB_PATH}"
fi
bun --version
