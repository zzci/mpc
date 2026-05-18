#!/usr/bin/env bash
# scripts/build-android.sh —— gomobile bind Android .aar(B-003 / PLAN-002 simple)。
#
# 权威:docs/design/mcp/sdk.md §8、docs/design/P0-tasks.md T3。
# 产出:dist/mobile/mcpwallet.aar(内含 Java/Kotlin 桥接 + JNI .so)。
#
# 「需 mobile 环境,本工作流范围外」:真实 bind 编译需 gomobile + Android
# NDK/SDK,并由 RN(B-004)/真机(P0/P4)验证;本脚本仅固化参数与路径,
# 静态就位,不在本工作流执行。详见 scripts/README.md 与 scripts/_common.sh。

set -euo pipefail
source "$(dirname "${BASH_SOURCE[0]}")/_common.sh"

# Android 最低 API:与 RN sample-app(B-005)minSdk 对齐,可经 env 覆盖。
# 21 为 gomobile 历史默认下限;具体取值在移动环境最终确定(范围外)。
ANDROID_API="${ANDROID_API:-21}"

# Java 包名:扁平 SDK 在宿主侧的命名空间(rn-bridge B-004 据此自动链接)。
JAVA_PKG="${JAVA_PKG:-cc.mcpwallet.mobileapi}"

# -s -w:剥符号表/调试信息,压缩产物体积(T7 体积预算关切),可经 env 覆盖。
GO_LDFLAGS="${GO_LDFLAGS:--s -w}"

preflight

# 绑定包覆盖钩子(GM-001 实测结论):gomobile 不能直接绑定 internal 包
# (`use of internal package ... not allowed`),正式方案是新增受控的非 internal
# re-export 包(属 Go 生产码,GM-001 范围外,见 docs/gomobile-build-report.md §4)。
# 该包就位后仅需 `export MCPWALLET_BIND_PKG=<module>/<pkg>`,无需改本脚本或
# _common.sh(_common.sh 非本任务 owner)。默认仍指向 internal/mobileapi,零回归。
BIND_PKG="${MCPWALLET_BIND_PKG:-${BIND_PKG}}"

# gomobile 依赖前置(GM-001 实测阻断 A):gomobile 生成的桥接代码 import
# golang.org/x/mobile/bind,模块须可解析该依赖,否则 gobind 报晦涩的
# "no Go package in golang.org/x/mobile/bind"。此处快速失败给出精确指引。
# 将 golang.org/x/mobile 纳入 go.mod 属依赖变更,GM-001 范围外(详见报告 §4)。
go list -m golang.org/x/mobile >/dev/null 2>&1 || fail \
  "go.mod 缺少 golang.org/x/mobile;gomobile bind 需该依赖:执行 go get golang.org/x/mobile/bind(属 go.mod 变更,见 docs/gomobile-build-report.md §4)"

AAR_OUT="${OUT_DIR}/mcpwallet.aar"
log "目标       : android (API ${ANDROID_API}, javapkg ${JAVA_PKG})"
log "产物       : ${AAR_OUT}"

set -x
gomobile bind \
  -target="android" \
  -androidapi "${ANDROID_API}" \
  -javapkg "${JAVA_PKG}" \
  -ldflags "${GO_LDFLAGS}" \
  -o "${AAR_OUT}" \
  "${BIND_PKG}"
set +x

log "完成:${AAR_OUT}"
