# shellcheck shell=bash
# scripts/_common.sh —— gomobile bind 脚本共用前置(B-003 / PLAN-002 simple)。
#
# 权威:docs/design/mcp/sdk.md §8(gomobile bind → .aar/.xcframework;纯 Go 无 cgo)、
# docs/design/P0-tasks.md T1/T3/T4。被 build-android.sh / build-ios.sh 经 `source` 引入。
#
# 范围说明(本工作流不做,移动环境实测):
#   - 真机/模拟器实际 bind 编译与运行(需 gomobile + Android NDK/SDK + Xcode + 真机);
#   - gomobile @ go1.25 工具链兼容性(go.mod 基线 go 1.25.7,gomobile 实测以移动环境为准);
#   - internal/ 包经 gomobile 生成桥接包导入的可行性,同属移动环境实测项;
#   - P0 测量与 go/no-go 由 B-006 汇总至 docs/design/P0-report.md(本任务不写 docs/design/)。
# 上述均为「需 mobile 环境,本工作流范围外」,脚本仅做静态就位与参数固化。

set -euo pipefail

# 仓库根:本文件位于 <root>/scripts/ 下,据此推导,避免依赖调用方 CWD。
REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"

# 绑定目标:B-001 的扁平 SDK 面(仅 string/[]byte/callback,无泛型/复杂结构体)。
MODULE_PATH="github.com/royqta/mcp-wallet"
BIND_PKG="${MODULE_PATH}/internal/mobileapi"

# 产物输出目录:dist/ 已被 .gitignore 忽略(/dist/),不污染工作树。
OUT_DIR="${OUT_DIR:-${REPO_ROOT}/dist/mobile}"

log()  { printf '[gomobile-bind] %s\n' "$*" >&2; }
fail() { printf '[gomobile-bind] 错误:%s\n' "$*" >&2; exit 1; }

# 前置校验:仅检查工具就位,不在本工作流执行真实 bind(范围外)。
preflight() {
  command -v go >/dev/null 2>&1 || fail "未找到 go;需 go1.25.7 基线(go.mod)"
  command -v gomobile >/dev/null 2>&1 || fail \
    "未找到 gomobile;安装:go install golang.org/x/mobile/cmd/gomobile@latest 并执行 gomobile init(需 mobile 环境,范围外)"
  [ -d "${REPO_ROOT}/internal/mobileapi" ] || fail "缺少 internal/mobileapi(B-001 依赖未就位)"
  mkdir -p "${OUT_DIR}"
  log "仓库根    : ${REPO_ROOT}"
  log "绑定包    : ${BIND_PKG}"
  log "输出目录  : ${OUT_DIR}"
}
