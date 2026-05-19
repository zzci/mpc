#!/usr/bin/env bash
# scripts/ci/build-ios.sh — macOS-runner iOS .xcframework build wrapper.
#
# Runs on a GitHub-hosted macOS runner (macos-14, Xcode + iOS SDK
# preinstalled) so the user needs no local Mac (docs/design/P0-report.md §6, L1
# ruling 2026-05-18). Pure pipeline setup: installs gomobile, then DELEGATES
# to scripts/build-ios.sh (GM-001 exclusive — call-only, never modified).
# internal/mobileapi is pure Go, so gomobile bind needs no extra C deps
# beyond the Xcode toolchain already on the runner.
#
# Output: dist/mobile/Mcpwallet.xcframework (a bundle dir) -> zipped to
# dist/mobile/Mcpwallet.xcframework.zip for a stable, hashable release asset.

set -euo pipefail
source "$(dirname "${BASH_SOURCE[0]}")/_lib.sh"

log "installing gomobile + initializing"
go install golang.org/x/mobile/cmd/gomobile@latest
go install golang.org/x/mobile/cmd/gobind@latest
GOBIN="$(go env GOPATH)/bin"
export PATH="${GOBIN}:${PATH}"
[ -n "${GITHUB_PATH:-}" ] && echo "${GOBIN}" >>"${GITHUB_PATH}"
gomobile init

log "delegating to scripts/build-ios.sh (GM-001, call-only)"
cd "${CI_REPO_ROOT}"
# gomobile bind needs a NON-internal target (Go internal/ rule forbids the
# generated gobind glue from importing internal/mobileapi). sdk is the
# 1:1 public re-export; build-ios.sh honors MCPWALLET_BIND_PKG.
export MCPWALLET_BIND_PKG="github.com/zzci/mpc/sdk"
bash scripts/build-ios.sh

xcf="dist/mobile/Mcpwallet.xcframework"
[ -d "${xcf}" ] || fail "expected ${xcf} (build-ios.sh output)"
log "zipping ${xcf} -> ${xcf}.zip (stable release asset)"
( cd dist/mobile && rm -f Mcpwallet.xcframework.zip \
  && zip -qry Mcpwallet.xcframework.zip Mcpwallet.xcframework )
log "done: ${xcf}.zip"
