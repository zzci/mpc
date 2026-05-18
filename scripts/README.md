# scripts/ —— gomobile bind 打包脚本（B-003 / PLAN-002 simple）

将 B-001 的扁平 SDK 面 `internal/mobileapi` 经 [gomobile](https://pkg.go.dev/golang.org/x/mobile/cmd/gomobile)
绑定为移动端原生库，供 rn-bridge（B-004）/ sample-app（B-005）/ P0·P4 真机验证消费。

权威依据：`docs/design/mcp/sdk.md` §8（gomobile bind → Android `.aar`、iOS
`.xcframework`；纯 Go 无 cgo）、`docs/design/P0-tasks.md` T1/T3/T4。

## 文件

| 文件 | 作用 |
|---|---|
| `_common.sh` | 共用前置：仓库根/绑定包/输出目录推导、工具就位校验（被 `source`，非独立执行） |
| `build-android.sh` | `gomobile bind -target=android` → `dist/mobile/mcpwallet.aar` |
| `build-ios.sh` | `gomobile bind -target=ios,iossimulator` → `dist/mobile/Mcpwallet.xcframework` |

绑定目标固定为 `github.com/zzci/mpc/internal/mobileapi`；产物输出至
`dist/mobile/`（`dist/` 已被 `.gitignore` 忽略，不污染工作树）。

## 用法（需 mobile 环境）

```bash
# 前置（范围外，移动环境执行一次）：
#   go install golang.org/x/mobile/cmd/gomobile@latest
#   gomobile init
scripts/build-android.sh   # 产 dist/mobile/mcpwallet.aar
scripts/build-ios.sh       # 产 dist/mobile/Mcpwallet.xcframework
```

可经环境变量覆盖默认参数：`OUT_DIR`、`GO_LDFLAGS`；Android 另有 `ANDROID_API`
（默认 21）、`JAVA_PKG`（默认 `cc.mcpwallet.mobileapi`）；iOS 另有 `IOS_TARGETS`
（默认 `ios,iossimulator`）、`IOS_VERSION`（默认 `13.0`）。这些默认值最终在移动
环境与 RN 工程对齐后确定。

## 范围声明：真机/实际 bind 编译验证「需 mobile 环境，本工作流范围外」

本任务（B-003，simple）**仅交付脚本骨架并固化参数/路径，做静态就位检查**。
以下均需移动环境实测，**不在本工作流范围内**：

- 实际 `gomobile bind` 编译（需 gomobile、Android NDK/SDK、Xcode、iOS SDK）；
- 真机/模拟器加载并跑通 keygen/sign（P0 T3/T4、P4，含 iOS 上 Go runtime/信号栈无崩溃）；
- **gomobile 与 go1.25 工具链兼容性**：`go.mod` 基线为 `go 1.25.7`，gomobile 在该
  工具链下的可用性以移动环境实测为准；
- **`internal/` 包经 gomobile 生成桥接包导入的可行性**：gomobile 在仓库外生成桥接
  包时对 `internal/mobileapi` 的可见性，同属移动环境实测项（若受限，由移动环境侧
  决定是否提供 re-export 外层包，不在本任务改动 Go 生产码的范围内）；
- 产物体积与冷启动增量（P0 T7）。

P0 测量、go/no-go 结论与上述范围外项的正式声明，由 **B-006** 汇总至
`docs/design/P0-report.md`（本任务不写 `docs/design/`）。
