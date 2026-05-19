#!/usr/bin/env bash
# scripts/ci/setup-android-ndk.sh — pinned Android NDK + gomobile for the
# .aar build job. Pure pipeline setup: it installs the mobile toolchain and
# then DELEGATES to scripts/build-android.sh (GM-001 exclusive — call-only,
# never modified here). internal/mobileapi is pure Go (no cgo SQLCipher),
# so the NDK clang toolchain is sufficient for `gomobile bind`.

set -euo pipefail
source "$(dirname "${BASH_SOURCE[0]}")/_lib.sh"

ndk_zip="android-ndk-${ANDROID_NDK_VERSION}-linux.zip"
ndk_url="https://dl.google.com/android/repository/${ndk_zip}"
ndk_root="${RUNNER_TEMP:-/tmp}/android-ndk"

if [ ! -d "${ndk_root}/android-ndk-${ANDROID_NDK_VERSION}" ]; then
  log "downloading Android NDK ${ANDROID_NDK_VERSION}"
  mkdir -p "${ndk_root}"
  curl -fsSL --retry 3 -o "${ndk_root}/${ndk_zip}" "${ndk_url}"
  unzip -q "${ndk_root}/${ndk_zip}" -d "${ndk_root}"
  rm -f "${ndk_root}/${ndk_zip}"
fi

ANDROID_NDK_HOME="${ndk_root}/android-ndk-${ANDROID_NDK_VERSION}"
export ANDROID_NDK_HOME ANDROID_NDK_ROOT="${ANDROID_NDK_HOME}"
if [ -n "${GITHUB_ENV:-}" ]; then
  echo "ANDROID_NDK_HOME=${ANDROID_NDK_HOME}" >>"${GITHUB_ENV}"
  echo "ANDROID_NDK_ROOT=${ANDROID_NDK_HOME}" >>"${GITHUB_ENV}"
fi

log "installing gomobile + initializing"
go install golang.org/x/mobile/cmd/gomobile@latest
go install golang.org/x/mobile/cmd/gobind@latest
GOBIN="$(go env GOPATH)/bin"
export PATH="${GOBIN}:${PATH}"
[ -n "${GITHUB_PATH:-}" ] && echo "${GOBIN}" >>"${GITHUB_PATH}"
gomobile init

log "delegating to scripts/build-android.sh (GM-001, call-only)"
cd "${CI_REPO_ROOT}"
# gomobile bind needs a NON-internal target (Go internal/ rule forbids the
# generated gobind glue from importing internal/mobileapi). sdk is the
# 1:1 public re-export; build-android.sh honors MCPWALLET_BIND_PKG.
export MCPWALLET_BIND_PKG="github.com/zzci/mpc/sdk"
bash scripts/build-android.sh
