#!/usr/bin/env bash
# scripts/ci/govulncheck.sh — Go vulnerability scan (pinned govulncheck).
#
# Scans the module against the Go vulnerability database. Read-only; no source
# is modified. Fails the build on any known-vulnerable symbol actually reached.

set -euo pipefail
source "$(dirname "${BASH_SOURCE[0]}")/_lib.sh"

log "installing govulncheck ${GOVULNCHECK_VERSION}"
go install "golang.org/x/vuln/cmd/govulncheck@${GOVULNCHECK_VERSION}"

GOBIN="$(go env GOPATH)/bin"
log "running govulncheck ./... (CGO_ENABLED=1 — cmd/node uses cgo SQLCipher)"
cd "${CI_REPO_ROOT}"
CGO_ENABLED=1 "${GOBIN}/govulncheck" ./...
