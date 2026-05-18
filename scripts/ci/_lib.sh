# shellcheck shell=bash
# scripts/ci/_lib.sh — shared helpers + pinned tool versions for CI/release.
#
# CI-001 file ownership: scripts/ci/** (new) + .github/**. This layer is pure
# pipeline tooling: it MUST NOT modify scripts/build-*.sh (GM-001 exclusive,
# call-only) or any Go/product code. Sourced by the other scripts/ci/*.sh.

set -euo pipefail

# Repo root: this file is <root>/scripts/ci/, derive without trusting CWD.
CI_REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
export CI_REPO_ROOT

# Pinned tool versions (supply-chain hardening — bump deliberately, never float).
GOVULNCHECK_VERSION="${GOVULNCHECK_VERSION:-v1.1.4}"
GITLEAKS_VERSION="${GITLEAKS_VERSION:-8.21.2}"
BUN_VERSION="${BUN_VERSION:-1.2.21}"
ANDROID_NDK_VERSION="${ANDROID_NDK_VERSION:-r26d}"
export GOVULNCHECK_VERSION GITLEAKS_VERSION BUN_VERSION ANDROID_NDK_VERSION

log()  { printf '[ci] %s\n' "$*" >&2; }
fail() { printf '[ci] error: %s\n' "$*" >&2; exit 1; }

# Deterministic source timestamp for reproducible builds (commit time, UTC).
source_date_epoch() {
  git -C "${CI_REPO_ROOT}" log -1 --pretty=%ct 2>/dev/null || date -u +%s
}
