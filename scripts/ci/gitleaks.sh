#!/usr/bin/env bash
# scripts/ci/gitleaks.sh — secret scan over the full git history (pinned).
#
# Downloads a pinned gitleaks release and runs `gitleaks detect` against the
# whole history (requires a full clone: actions/checkout fetch-depth: 0).
# Read-only; exits non-zero if any secret is found.

set -euo pipefail
source "$(dirname "${BASH_SOURCE[0]}")/_lib.sh"

os="linux"
case "$(uname -m)" in
  x86_64|amd64) arch="x64" ;;
  aarch64|arm64) arch="arm64" ;;
  *) fail "unsupported arch $(uname -m) for gitleaks" ;;
esac

tmp="$(mktemp -d)"
trap 'rm -rf "${tmp}"' EXIT
tarball="gitleaks_${GITLEAKS_VERSION}_${os}_${arch}.tar.gz"
url="https://github.com/gitleaks/gitleaks/releases/download/v${GITLEAKS_VERSION}/${tarball}"

log "downloading gitleaks ${GITLEAKS_VERSION} (${os}/${arch})"
curl -fsSL --retry 3 -o "${tmp}/${tarball}" "${url}"
tar -xzf "${tmp}/${tarball}" -C "${tmp}" gitleaks
chmod +x "${tmp}/gitleaks"

log "scanning full git history for secrets"
cd "${CI_REPO_ROOT}"
"${tmp}/gitleaks" detect --source . --redact --no-banner --verbose
