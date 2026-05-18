#!/usr/bin/env bash
# scripts/build-ios.sh —— gomobile bind iOS .xcframework(B-003 / PLAN-002 simple)。
#
# 权威:docs/design/mcp/sdk.md §8、docs/design/P0-tasks.md T4。
# 产出:dist/mobile/Mcpwallet.xcframework(设备 + 模拟器切片)。
#
# 调用方(L1 P0-report §6,2026-05-18):本脚本由 CI-001 release.yml 的 GitHub
# Actions macOS(macos-14)job 调用,自动产出 .xcframework 作为本仓发布交付物,
# 非用户外部步骤;本地 macOS 仅可选离线复现。GM-001 已核对参数对 macos-14
# runner 上 gomobile bind 可用(见 docs/gomobile-build-report.md §5)。Go
# runtime/信号栈在 gomobile 路径下无崩溃(T4)由 CI 产物经真机/模拟器消费侧验证。

set -euo pipefail
source "$(dirname "${BASH_SOURCE[0]}")/_common.sh"

# iOS 目标平台:device + simulator 合并为单一 .xcframework;可经 env 覆盖
# (如仅 ios 或追加 macos)。最终矩阵由 CI-001 release.yml macOS job 固化。
IOS_TARGETS="${IOS_TARGETS:-ios,iossimulator}"

# 最低 iOS 版本:与 RN sample-app(B-005)部署目标对齐,可经 env 覆盖。
IOS_VERSION="${IOS_VERSION:-13.0}"

# -s -w:剥符号表/调试信息,压缩产物体积(T7 体积预算关切),可经 env 覆盖。
GO_LDFLAGS="${GO_LDFLAGS:--s -w}"

preflight

# 绑定包覆盖钩子 + gomobile 依赖前置:与 build-android.sh 同源(GM-001 实测,
# 见 docs/gomobile-build-report.md §4/§5)。本脚本由 CI-001 release.yml 的
# GitHub Actions macOS(macos-14)job 调用自动产出 .xcframework(本仓发布交付物,
# 非用户外部步骤,L1 P0-report §6);同样适用 internal 包与 x/mobile 两项前置。
BIND_PKG="${MCPWALLET_BIND_PKG:-${BIND_PKG}}"
go list -m golang.org/x/mobile >/dev/null 2>&1 || fail \
  "go.mod 缺少 golang.org/x/mobile;gomobile bind 需该依赖:执行 go get golang.org/x/mobile/bind(属 go.mod 变更,见 docs/gomobile-build-report.md §4)"

XCF_OUT="${OUT_DIR}/Mcpwallet.xcframework"
log "目标       : ${IOS_TARGETS} (iosversion ${IOS_VERSION})"
log "产物       : ${XCF_OUT}"

set -x
gomobile bind \
  -target="${IOS_TARGETS}" \
  -iosversion "${IOS_VERSION}" \
  -ldflags "${GO_LDFLAGS}" \
  -o "${XCF_OUT}" \
  "${BIND_PKG}"
set +x

log "完成:${XCF_OUT}"
