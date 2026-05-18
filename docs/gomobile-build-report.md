# gomobile 构建实测报告（GM-001）

> 日期：2026-05-18 ｜ 任务：GM-001（gomobile Android `.aar` 真实生成 + 冒烟）
> 结论：**Android `.aar` 已在本工作树真实生成并冒烟通过**（4 ABI JNI + 扁平 Java 桥
> + 零复杂类型泄漏）。生成依赖两项硬前置（见 §4），二者均落在 GM-001 的「禁改 Go
> 生产码 / go.mod」范围外，已作为后续任务建议上报。

---

## 1. 工具链版本（实测）

| 组件 | 版本 / 标识 |
|---|---|
| 宿主 Go | `go1.25.7 linux/amd64`（`go.mod` 基线 `go 1.25.7`） |
| gomobile/gobind | `golang.org/x/mobile v0.0.0-20260514233045-7de0a8fa7f4d`（`go install ...@latest`） |
| gomobile 实编译 Go | **`go1.25.10`**（gomobile 携带 `toolchain` 指令要求 `go>=1.25.0`，`go` 自动切换下载，宿主 1.25.7 未被改写） |
| JDK | Temurin OpenJDK `17.0.13+11`（apt 源损坏，改用官方 tar 便携安装至 `/opt/mobiletc`） |
| Android SDK Platform | `android-34` + `platform-tools`（cmdline-tools `11076708`） |
| Android NDK | `26.3.11579264`（release `r26d`），clang `17.0.2` |
| 运行环境 | Ubuntu 22.04.5（root，无预装 Java/NDK/gomobile，全部本任务安装） |

`gomobile init` 在 go1.25.10 切换下 `rc=0`，与 go1.25 基线兼容（此前
`scripts/README.md` 列为「移动环境实测项」的 go1.25 兼容性问题——**实测通过**）。

## 2. 命令（端到端）

```bash
# 前置（一次性，本工作流外）
go install golang.org/x/mobile/cmd/gomobile@latest
go install golang.org/x/mobile/cmd/gobind@latest
export JAVA_HOME=<jdk17> ANDROID_HOME=<sdk> ANDROID_NDK_HOME=<sdk>/ndk/26.3.11579264
gomobile init

# 必要前置（见 §4 阻断 A）：golang.org/x/mobile 须在模块依赖内
go get golang.org/x/mobile/bind

# 构建（脚本封装；BIND_PKG 见 §4 阻断 B）
scripts/build-android.sh        # → dist/mobile/mcpwallet.aar
```

实测 `gomobile bind` 全量交叉编译（含 tss-lib v3 / btcec / 全 MPC 依赖闭包）至 4
个 Android ABI 的 JNI，用时约 33s（首次，含 stdlib 交叉编译），CPU 935%。

## 3. 产物与冒烟实证

产物：`dist/mobile/mcpwallet.aar`（**16,061,619 bytes**，sha256
`b4cc096bea17350d5b62dd84ce7afad727ff3c87398aed4a0982a792be819c54`）
+ `mcpwallet-sources.jar`。**不提交**（`.gitignore` 已含 `/dist/`，本任务追加显式条目，见 §6）。

### 3.1 结构（unzip）

```
AndroidManifest.xml      package=go.<pkg>.gojni  minSdkVersion=21
classes.jar              Java 桥接类
jni/arm64-v8a/libgojni.so    9,568,720 B
jni/armeabi-v7a/libgojni.so  9,596,744 B
jni/x86/libgojni.so          9,733,392 B
jni/x86_64/libgojni.so      10,271,504 B
proguard.txt  R.txt  res/
```

四 ABI 齐全（真机 arm64-v8a/armeabi-v7a + 模拟器 x86/x86_64），minSdk 21
与 `ANDROID_API` 默认对齐。

### 3.2 扁平符号（javap，关键安全实证）

`SDK` 类的导出方法签名**全部为扁平类型**：

```
byte[]      exportShare(String, String)        throws Exception
String      importShare(byte[], String)        throws Exception
void        keyGen(String, KeyGenCallback)
void        onWireMessage(byte[])              throws Exception
void        reshare(String, ReshareCallback)
SignSession sign(String, SignCallback)
SignSession() / SignSession.approve() / .reject()
KeyGenCallback  : onProgress(String) onResult(String) onError(String,String)
ReshareCallback : onProgress(String) onResult(String) onError(String,String)
SignCallback    : onDecoded(String,String,String) onResult(byte[]) onError(String,String)
```

**复杂类型泄漏检查**：对 `classes.jar` 全部桥接类 `javap -p` grep
`tss|keygen.|mpc.|btcec|LocalPreParams|big.Int` → **零命中**。
`docs/design/mcp/sdk.md §2` 的「仅 string/[]byte/callback/opaque，无 tss-lib/泛型外泄」
契约**经 gomobile 桥真实成立**。

## 4. 两项硬前置（阻断 → 已绕过取证 → 后续建议）

> 这两项是 GM-001 派发已预见的「移动环境实测项」（见 `scripts/README.md`、
> `scripts/_common.sh` 范围声明）。本任务**实测确认其确为阻断**，并在不改动受限
> 文件的前提下绕过以取得真实产物与冒烟实证。

### 阻断 A — `golang.org/x/mobile` 必须在 go.mod 依赖内

直跑 `gomobile bind` 报 `unable to import bind: no Go package in
golang.org/x/mobile/bind`。`gomobile` 生成的桥接代码 `import
golang.org/x/mobile/bind`，需模块可解析该依赖。

- 绕过（取证用，已回滚）：`go get golang.org/x/mobile/bind`。副作用：连带升级
  `golang.org/x/{net,sync,sys,text,tools}`（如 `sys v0.43.0→v0.44.0`）。
- **后续建议**：将 `golang.org/x/mobile` 作为受控依赖纳入 `go.mod`（gomobile 官方
  要求），并复核上述 x/* 连带升级对 `-race` 校准门的影响。**属 go.mod 变更，GM-001
  范围外，需 owner / CI-001 / L1 裁决**。本任务取证后已 `go.mod`/`go.sum` 还原，
  提交树零变更。

### 阻断 B — `internal/mobileapi` 无法被 gomobile 直接绑定

`gomobile bind .../internal/mobileapi` 报 `use of internal package
github.com/zzci/mpc/internal/mobileapi not allowed`：gomobile 在模块外的
合成 `gobind` 包生成桥接，Go 的 internal 可见性规则禁止其导入 `internal/`。

- 绕过（取证用，已删除）：在仓库根建临时**非 internal 转发垫片包**
  `mcpwalletbind/`（仅 string/[]byte/error/opaque/callback 转发，零逻辑），
  `gomobile bind` 指向它 → 即得本报告全部实证。该文件**已在 finalize 前删除**，
  提交树无任何 Go 改动。
- **后续建议**：新增一个**受控的非 internal re-export 包**（如
  `mobile/` 或 `mobilesdk/`，转发 `internal/mobileapi` 扁平面），`gomobile bind`
  指向它。**属新增 Go 生产码，GM-001 明确范围外**（派发 FORBIDDEN: any Go
  production code），需独立任务 + L1 裁决。垫片实现见本报告 git 历史思路，可直接
  采纳为正式包骨架。

### 脚本侧已落地的配套（本任务 owner 文件内）

`scripts/build-android.sh` / `scripts/build-ios.sh`（详见 §5）已加：
1. `MCPWALLET_BIND_PKG` 环境覆盖钩子——后续正式非 internal 包就位后，仅需
   `export MCPWALLET_BIND_PKG=github.com/zzci/mpc/<pkg>` 即可，**无需再改
   脚本或 `_common.sh`**（`_common.sh` 非本任务 owner，未改）；
2. `golang.org/x/mobile` 依赖**快速失败前置**——缺失时打印精确指引并退出，取代
   gobind 的晦涩报错。
默认行为不变（仍默认 `internal/mobileapi`），零回归。

## 5. iOS 脚本参数核查（CI-001 macOS runner 自动产出）

> L1 裁定（2026-05-18，P0-report §6 已在 main，commit 1eade7f）：iOS
> `.xcframework` 由 **CI-001 release.yml 的 GitHub Actions macOS runner 自动产出**
> （本仓发布交付物），**非用户外部步骤**；本地 macOS 仅可选离线复现。故
> `build-ios.sh` 的定位是被 **CI-001 的 CI macOS job 调用**，核查须确保参数对
> `macos-14` runner 上 `gomobile bind` 可用。

`scripts/build-ios.sh` 参数**经核查正确，对 `macos-14` runner 可用**，无需改动逻辑：

| 项 | 值 | 评估（针对 `macos-14` GitHub Actions runner） |
|---|---|---|
| `-target` | `ios,iossimulator`（`IOS_TARGETS` 可覆盖） | 正确：现代 gomobile 合法目标；`macos-14`（Apple Silicon，预装 Xcode）产设备 arm64 + 模拟器切片合并 `.xcframework` |
| `-iosversion` | `13.0`（`IOS_VERSION` 可覆盖） | 合法 flag；低于 runner Xcode 最低部署目标下限，安全 |
| `-o` | `dist/mobile/Mcpwallet.xcframework` | 正确：现代 gomobile iOS 产物即 `.xcframework` |
| `-ldflags` | `-s -w` | 与 Android 一致，体积裁剪 |

iOS `.xcframework` 由 **CI-001 release.yml GitHub Actions macOS runner 自动产出
（本仓发布交付物），`build-ios.sh` 参数已核对就位；非用户外部步骤**。同样适用
§4 阻断 A、B（go.mod 依赖 + 非 internal 包前置）——本任务为 `build-ios.sh` 同步
落地的 `MCPWALLET_BIND_PKG` 钩子 + x/mobile 依赖快速失败，正是 CI macOS job 接入
所需。iOS 上 Go runtime/信号栈无崩溃（`scripts/README.md` T4）由 CI 产物经真机/
模拟器消费侧验证，不在 `build-ios.sh` 参数核查范围内。

## 6. .gitignore 与零回归

- `.gitignore`：原有 `/dist/` 已忽略全部移动产物；本任务在该处追加显式注释条目
  `*.aar` / `*.xcframework`（防 `dist/` 外误提交二进制）。**不提交任何 `.aar`/
  `.so`/`.xcframework`**。
- 临时垫片 `mcpwalletbind/` 已删除；`go.mod`/`go.sum` 已按取证前 sha256 还原
  （`go.mod` `a522c4e1…`、`go.sum` `5fc01a3b…`）。
- 零回归核验：还原后 `go build ./...` PASS（见任务回报实证）。
- 提交树净变更仅限本任务 owner 文件：`scripts/build-android.sh`、
  `scripts/build-ios.sh`、`docs/gomobile-build-report.md`、`.gitignore`。
