#!/usr/bin/env bash
# scripts/ci/release-build.sh — reproducible release artifacts + checksums +
# build provenance. Pure pipeline packaging; no Go/product source is modified.
#
# Reproducibility scope (surfaced honestly in provenance.json):
#   - cmd/cli, internal/mobileapi : PURE GO -> fully reproducible. cli is
#     cross-compiled CGO_ENABLED=0 -trimpath with a pinned, buildid-stripped
#     ldflags set; identical inputs -> byte-identical outputs.
#   - cmd/server : imports mutecomm/go-sqlcipher/v4 (cgo + SQLCipher) -> NOT
#     cross-compilable / not bit-reproducible. Built linux/amd64 ONLY, native
#     CGO on the runner toolchain. This limitation is recorded in provenance.
#
# Inputs : $1 = version/tag string (e.g. v1.2.3 or a commit sha).
#          AAR_PATH (optional)         = prebuilt .aar (Android job artifact).
#          XCFRAMEWORK_PATH (optional) = prebuilt .xcframework.zip (iOS job).
# Output : dist/release/{binaries, mcpwallet.aar?, Mcpwallet.xcframework.zip?,
#          SHA256SUMS, provenance.json}

set -euo pipefail
source "$(dirname "${BASH_SOURCE[0]}")/_lib.sh"

VERSION="${1:?usage: release-build.sh <version>}"
OUT="${CI_REPO_ROOT}/dist/release"
SDE="$(source_date_epoch)"
export SOURCE_DATE_EPOCH="${SDE}"

rm -rf "${OUT}"
mkdir -p "${OUT}"
cd "${CI_REPO_ROOT}"

# Pinned, deterministic build flags. -buildid= drops the per-build VCS/build
# id; -trimpath removes absolute paths -> path-independent, reproducible.
LDFLAGS="-s -w -buildid="
export GOFLAGS="-trimpath"

# --- cmd/cli: pure Go, fully static, reproducible cross-matrix ------------
# CGO_ENABLED=0 -> no libc, statically linked by construction; identical
# inputs -> byte-identical outputs across all OS/arch.
for target in linux/amd64 linux/arm64 darwin/amd64 darwin/arm64; do
  goos="${target%/*}"; goarch="${target#*/}"
  bin="${OUT}/mcpwallet-cli-${goos}-${goarch}"
  log "build cmd/cli ${goos}/${goarch} (CGO_ENABLED=0, static, reproducible)"
  CGO_ENABLED=0 GOOS="${goos}" GOARCH="${goarch}" \
    go build -ldflags "${LDFLAGS}" -o "${bin}" ./cmd/cli
done

# --- cmd/server (server): cgo + SQLCipher, fully static, reproducible -------
# SQLCipher whole-DB encryption (security.md #9) requires cgo. We link it
# FULLY STATIC (-linkmode external -extldflags "-static") so the server is a
# single self-contained binary with no shared-library / glibc runtime
# dependency. -Wl,--build-id=none drops the GNU ld build-id and, together
# with -trimpath / -buildid= / SOURCE_DATE_EPOCH, makes the static build
# byte-reproducible on a pinned builder toolchain. linux/amd64 only (cgo
# cross-compile out of scope; the server is a Linux daemon).
# -tags 'osusergo netgo': force Go's pure-Go user/DNS resolvers so the static
# cgo binary makes NO glibc getaddrinfo/NSS call -> no runtime glibc
# dependency, fully self-contained networking (avoids the classic
# static-glibc NSS caveat).
NODE_LDFLAGS="${LDFLAGS} -linkmode external -extldflags \"-static -Wl,--build-id=none\""
log "build cmd/server linux/amd64 (CGO_ENABLED=1, static SQLCipher, netgo, reproducible)"
CGO_ENABLED=1 GOOS=linux GOARCH=amd64 \
  go build -tags 'osusergo netgo' -ldflags "${NODE_LDFLAGS}" \
    -o "${OUT}/mcpwallet-server-linux-amd64" ./cmd/server
# Hard gate: the server MUST be statically linked (no dynamic interpreter).
if file "${OUT}/mcpwallet-server-linux-amd64" | grep -q "dynamically linked"; then
  fail "cmd/server is dynamically linked — static release invariant violated"
fi
log "cmd/server: $(file "${OUT}/mcpwallet-server-linux-amd64" | sed 's/.*: //; s/,.*//') (static OK)"

# --- mobile libs (built by the parallel Android/iOS jobs; passed through) -
if [ -n "${AAR_PATH:-}" ] && [ -f "${AAR_PATH}" ]; then
  log "including Android artifact $(basename "${AAR_PATH}")"
  cp "${AAR_PATH}" "${OUT}/mcpwallet.aar"
fi
if [ -n "${XCFRAMEWORK_PATH:-}" ] && [ -f "${XCFRAMEWORK_PATH}" ]; then
  log "including iOS artifact $(basename "${XCFRAMEWORK_PATH}")"
  cp "${XCFRAMEWORK_PATH}" "${OUT}/Mcpwallet.xcframework.zip"
fi

# --- SHA256SUMS -----------------------------------------------------------
( cd "${OUT}" && sha256sum -- * >SHA256SUMS )
log "checksums:"
cat "${OUT}/SHA256SUMS" >&2

# --- build provenance -----------------------------------------------------
go_version="$(go version | awk '{print $3}')"
commit="$(git -C "${CI_REPO_ROOT}" rev-parse HEAD)"
built_at="$(date -u -d "@${SDE}" +%Y-%m-%dT%H:%M:%SZ 2>/dev/null || date -u +%Y-%m-%dT%H:%M:%SZ)"

{
  echo '{'
  echo "  \"version\": \"${VERSION}\","
  echo "  \"commit\": \"${commit}\","
  echo "  \"goVersion\": \"${go_version}\","
  echo "  \"sourceDateEpoch\": ${SDE},"
  echo "  \"builtAt\": \"${built_at}\","
  echo "  \"builder\": \"${GITHUB_REPOSITORY:-local}@${GITHUB_RUN_ID:-manual}\","
  echo '  "static": {'
  echo '    "mcpwallet-cli-*": true,'
  echo '    "mcpwallet-server-linux-amd64": true'
  echo '  },'
  echo '  "reproducible": {'
  echo '    "mcpwallet-cli-*": true,'
  echo '    "mcpwallet-server-linux-amd64": "true (pinned builder toolchain)",'
  echo '    "mcpwallet.aar": "toolchain-dependent",'
  echo '    "Mcpwallet.xcframework.zip": "toolchain-dependent"'
  echo '  },'
  echo '  "notes": "cmd/cli: pure Go (CGO_ENABLED=0) -> fully static, byte-reproducible across all OS/arch (-trimpath, -buildid=, SOURCE_DATE_EPOCH pinned to commit time). cmd/server (server): links mutecomm/go-sqlcipher/v4 (cgo + SQLCipher, required by security.md #9 whole-DB encryption) FULLY STATIC via -linkmode external -extldflags \"-static -Wl,--build-id=none\"; linux/amd64; byte-reproducible on the pinned builder toolchain (cross-toolchain bit-identical requires the same digest-pinned builder image — the CI release-verify job rebuilds and diffs SHA256 as trust evidence). The .aar (gomobile + Android NDK) and .xcframework (gomobile + Xcode) are pure-Go bindings but pass through host mobile toolchains, so byte-identical reproduction is not guaranteed; SHA256 below pins the exact published bytes.",'
  printf '  "artifacts": ['
  first=1
  while IFS= read -r line; do
    h="${line%% *}"; f="${line##* }"
    [ "${f}" = "SHA256SUMS" ] && continue
    [ "${first}" -eq 1 ] && first=0 || printf ','
    printf '\n    {"name": "%s", "sha256": "%s"}' "${f}" "${h}"
  done <"${OUT}/SHA256SUMS"
  echo
  echo '  ]'
  echo '}'
} >"${OUT}/provenance.json"

log "release artifacts ready in ${OUT}"
ls -la "${OUT}" >&2
