# P0 报告 —— 端上 MPC 打包验证(路径 A)go/no-go

> 权威依据:`docs/design/P0-tasks.md`(T1–T8 退出标准)、`docs/design/PLAN.md` §1(构建基线 §1-B)、`docs/design/mcp/sdk.md` §8。
> 性质:P0 Gate 决策与报告(对应 P0-tasks **T8**)。
> 范围声明:本工作流交付至 **Go 侧全绿 + gomobile bind 脚本就位**;真机/移动环境实测项明示「需 mobile 环境,本工作流范围外」。
> 测量环境:宿主机,`go.mod` 基线 `go 1.25.7`(§1-B 合规);绑定门为 L1 类型B 裁定的权威校准命令
> `CGO_ENABLED=1 rtk test go test -race -count=1 -timeout=1200s -p 1 ./...`(全树/-race/串行/检真 race)。

## 1. 结论(go/no-go)

**GO(限定:工作流范围内的密码学 + 运行时 + 打包就位门通过;移动环境真机子门为待续做的后继门,不构成路径 A 失败)。**

- **进 GO 的依据**:tss-lib v3 keygen/signing/resharing 进程内多 party 仿真在权威绑定门(`-race -p 1` 全树)下端到端**全绿**;扁平 SDK 面(`internal/mobileapi`,仅 string/[]byte/callback)绿;`go build`/`go vet` 全树 rc=0;基线 `go 1.25.7` §1-B 合规;gomobile bind 脚本(B-003)就位并固化参数/路径。PLAN.md §5 风险 3 已记 tss-lib v3 纯 Go 无 cgo、gomobile 无原生依赖,**P0 编译风险低**;本阶段无任何 gomobile 编译/运行失败被观察到(系未在移动环境执行,**非失败**)。
- **未触发失败处置(路径 B/C)**:`docs/design/P0-tasks.md`「失败处置」的触发条件为 *gomobile 编译/运行不通* 或 *PreParams 端上不可接受*。两者均**未发生**——前者未在移动环境执行(范围外,非失败),后者端上耗时尚未实测;代码侧 PreParams 安全不变量(全程设备内、后台、严禁后端预生成下发)已满足(见 §3 T6)。故**不触发**改道,路径 A 维持「已定」。
- **后继门(必须在移动环境闭合,P0 方算完全签收)**:T3/T4/T5/T6/T7 真机/模拟器实测 + **gomobile@go1.25 工具链兼容性**。在移动环境侧任一项实测失败,方按 P0-tasks「失败处置」复议路径 B/C(纯端上 MPC 始终为硬约束,任何分支不得退化为远端签名)。

## 2. 测量(本工作流范围内,实测)

权威绑定门命令实跑,P0 核心两包(进程内仿真 + 扁平 SDK):

| 包 | 校准绑定门结果 | 墙钟(`-race`,串行 `-p 1`) | 内容 |
|---|---|---|---|
| `internal/mpc` | **ok**(无 race / 无 FAIL) | 81.967s | keygen/signing/resharing/recover 进程内 N 方仿真 |
| `internal/mobileapi` | **ok**(无 race / 无 FAIL) | 15.465s | 扁平 SDK `KeyGen`/`Sign`/`Reshare`(gomobile 友好面) |

- `CGO_ENABLED=1 rtk err go build ./...` → rc=0;`go vet ./...` → rc=0。
- `go.mod`:`go 1.25.7`(§1-B 唯一基线;无 `go1.23` 回退,未破基线)。
- 与 B-001 合并记录一致(权威):B-001 retry1 真修后,post-merge 含 H-001 与 mobileapi 同树下,**全树** `rtk test -race` rc=0,全包 ok,无 race/FAIL,lint 0,tidy 幂等,go1.25.7(`docs/changelog.md` B-001 GREEN 合并条目)。本报告 §2 为当前 HEAD 对 P0 核心包的复测复核,结论一致。
- `-race` 插桩约 ~3x 放慢(系统性已记);上表墙钟为**带 race 插桩 + 串行**的悲观上界,非端上代表值。**端上真机耗时不在此测量内**(见 §4)。

> 说明:本阶段进程内仿真**超出 T2 的最小 keygen 要求**——signing 与 resharing 亦进程内端到端绿(P1 核心提前于 P0 阶段验证),进一步加强 GO 判据(密码学 + 运行时在宿主侧已超额验证)。

## 3. P0-tasks T1–T8 退出标准对照

| 任务 | 退出标准(摘) | 状态 | 说明 |
|---|---|---|---|
| **T1** 工具链基线 | Go/gomobile/NDK/Xcode 就位;`example/bind` 产 `.aar`/`.xcframework` | **部分(范围内 ✓ / 范围外 待续)** | 宿主 Go `1.25.7` 就位;`gomobile`/`gobind`/`gomobile init`/Android NDK·SDK/Xcode·iOS SDK 与 `example/bind` 产物验证 = **需 mobile 环境(范围外)**。脚本 `preflight` 仅做工具就位静态校验,不执行真实 bind。 |
| **T2** keygen 最小扁平封装 + 进程内 N 方 | 宿主 `go build`+`go vet` 过;进程内 3 方 keygen 到 `endCh`,save data 非空、各方公钥一致 | **达成 ✓(并超额)** | `build`/`vet` rc=0;`internal/mpc` `TestSimulateKeygenInProcess`、`internal/mobileapi` `TestKeyGenInProcessAndCallbackOrder` 等在 `-race -p 1` 下绿。扁平面仅 string/[]byte/callback,复杂类型全封 Go 侧。**超出**:sign/reshare 进程内亦绿。 |
| **T3** Android 绑定 + 真机 keygen | `.aar` 生成;真机+模拟器跑完 keygen;记 PreParams/keygen 墙钟;无崩溃/ANR | **范围外(脚本就位 / 真机待续)** | `scripts/build-android.sh` 固化 `gomobile bind -target=android`(API/javapkg/ldflags 参数化,产 `dist/mobile/mcpwallet.aar`),**静态就位、不执行**。实际 bind 编译 + 真机/模拟器 keygen + 墙钟记录 = **需 mobile 环境(gomobile/NDK/SDK/真机),范围外**。 |
| **T4** iOS 绑定 + 真机 keygen | `.xcframework` 生成;真机+模拟器跑完 keygen;记耗时;无 Go runtime/信号相关崩溃 | **范围外(脚本就位 / 真机待续)** | `scripts/build-ios.sh` 固化 `gomobile bind -target=ios,iossimulator`(iosversion/ldflags 参数化,产 `dist/mobile/Mcpwallet.xcframework`),**静态就位、不执行**。实际 bind + 模拟器/真机 keygen + **Go runtime/信号栈在 gomobile 路径下无崩溃**(验证而非假设)= **需 mobile 环境(gomobile/Xcode/iOS SDK/真机),范围外**。 |
| **T5** RN 集成冒烟 | iOS+Android RN App 各调 keygen 跨桥拿回 save data;扁平 API 桥下不丢类型 | **范围外(待续)** | rn-bridge / sample-app 骨架属 B-004/B-005;**RN 真机冒烟(跨 JS 桥实际调用 + 类型不丢)= 需 mobile 环境,范围外**。扁平面设计(仅 string/[]byte/callback)为桥下不丢类型提供前置保障,实证待移动环境。 |
| **T6** 端上 PreParams 策略(安全红线) | 全程设备内后台生成、App 不卡 UI、耗时入表;阈值(建议中端机+进度 UI ≤ 60s) | **设计不变量满足 ✓ / 端上耗时实测 范围外** | 代码侧不变量已落:PreParams 全程设备内、后台、`OnProgress("preparams")` 进度阶段、严禁 UI 线程;**红线**——含 Paillier 私钥,**禁止后端预生成下发**(`mpc.KeygenConfig.PreParams`/`ResharingConfig.PreParams` 注记;`mobileapi.sdk.go` `preParams` 仅测试 seam,真机为 nil 由 mpc 端内生成)。**跨设备档位耗时实测 + 阈值(≤ 60s)验收 + `GeneratePreParams` 超时/并发调参 = 需 mobile 环境(真机),范围外**;超阈触发路径 C 复议的判定在移动环境侧执行。 |
| **T7** 体积与冷启动 | `.aar`/`.xcframework` 体积、Go runtime 冷启动增量入表对照预算 | **范围外(待续)** | 体积/冷启动增量须对实际 bind 产物测量;脚本已置 `-ldflags "-s -w"`(剥符号,体积关切)为预算友好默认。**数值测量 + 预算对照(预算值在 T7 开始前与需求方确定)= 需 mobile 环境,范围外**。 |
| **T8** Gate 决策与报告 | 汇总实测,对照退出标准,出 `docs/design/P0-report.md` 含 go/no-go(及失败改道建议) | **达成 ✓(本文)** | 本报告即 T8 产出;go/no-go 见 §1,范围外项与后继门见 §4,失败处置触发条件见 §1/§5。 |

## 4. 「需 mobile 环境,本工作流范围外」明示

以下各项均需 **gomobile + Android NDK/SDK + Xcode + iOS SDK + 真机/模拟器**,**不在本工作流范围内**,移植/续做于移动环境:

1. **`.aar` / `.xcframework` 真实 bind 编译**:实际 `gomobile bind`(脚本就位但未执行)。
2. **真机/模拟器加载并跑通 keygen/sign**(T3/T4/P4),含 **iOS 上 Go runtime/信号栈在 gomobile 路径下无崩溃**(验证而非假设)。
3. **端上 PreParams 实测**:跨设备档位 `GeneratePreParams` 耗时、后台 + 进度 UI、阈值(中端机+进度 UI ≤ 60s)验收、超时/并发调参(T6)。
4. **体积 / 冷启动**:`.aar`/`.xcframework` 体积与 Go runtime 冷启动增量,对照预算(T7)。
5. **RN 桥真机冒烟**:RN App 跨 JS 桥实调 keygen 取回 save data、扁平 API 桥下类型不丢(T5)。
6. **gomobile @ go1.25 工具链兼容性**:`go.mod` 基线 `go 1.25.7`(§1-B);gomobile 在该工具链下对 .aar/.xcframework 打包的可用性以移动环境实测为准(PLAN.md §1「端上 SDK 残留风险」明列;本工作流交付至 Go 侧全绿 + bind 脚本就位)。
7. **`internal/` 包经 gomobile 生成桥接包导入的可行性**:gomobile 仓库外生成桥接包时对 `internal/mobileapi` 的可见性,亦属移动环境实测项(若受限,由移动环境侧决定是否提供 re-export 外层包,不在本工作流改 Go 生产码范围内)。

## 5. 失败处置触发边界(供移动环境续做参照)

按 `docs/design/P0-tasks.md`「失败处置」(本工作流**未触发**,记录触发条件供后继门):

- gomobile 编译/运行不通(移动环境实测出现)→ 改 **路径 B**(Go `c-shared` + 自写桥,注意 iOS 信号栈)。
- 端上 PreParams 不可接受(超 T6 阈值)且 B 无改善 → 触发 **路径 C**(Rust + Dfns CGGMP21,替换基座),用户决策。
- **纯端上 MPC 为硬约束**:任何分支均不得退化为远端签名服务。

## 6. 用户裁定(2026-05-18)—— 移动端范围重定义:真机验证不作发布阻断,交付物=库生成

> 权威级:用户显式裁定,覆盖 §1「后继门(必须在移动环境闭合,P0 方算完全签收)」与 §3/§4/§5 中将真机/移动环境实测项视为**发布阻断**的措辞。§3/§4/§5 作为「移动环境续做项的技术清单」仍准确保留;其**发布门禁含义**以本 §6 为准。

- **真机/模拟器实测(T3/T4/T5/T6/T7 真机子门 + gomobile@go1.25 真机兼容性)= 不构成发布阻断项(non-blocker)**。理由:产品形态为「移动协管钱包」,Go 内核 + 服务端 + 双 E2E 门(localhost E2E-001、Docker 隔离 E2E-002)均已证就绪;移动打包/真机层属设备侧集成,按用户裁定移出发布关键路径,作发布说明中的「移动环境续做项」披露,不阻断本仓发布决定。
- **本仓发布交付物 = gomobile 库生成(可在 CI/Linux 环境产出者须真实产出)**:
  - **Android `.aar`**:在 Linux/CI 环境经 `gomobile` + Android NDK 真实 `gomobile bind -target=android` 产出 `dist/mobile/mcpwallet.aar`(`scripts/build-android.sh` 已固化参数),并加载冒烟(解压校验 classes/jni 结构、`internal/mobileapi` 扁平面符号存在)。**纳入交付物 + 最终验收项**(在制:GM-001)。
  - **iOS `.xcframework`(用户裁定 2026-05-18:GitHub 自动产出,不再为用户外部步骤)**:`gomobile bind -target=ios,iossimulator` 须在 **GitHub Actions 托管 macOS runner**(`macos-14`/`macos-latest`,自带 Xcode/iOS SDK)由发布流水线**自动产出 `.xcframework` artifact + SHA256 校验和**(`scripts/build-ios.sh` 已固化参数,扁平 `internal/mobileapi`/B-001 gomobile 友好面就位)。**= 本仓发布交付物 + 最终验收项(在制:CI-001 release.yml macOS job)**;本地 macOS 仅作可选离线复现路径,**用户无需本地 Mac**。
- **PreParams 安全红线不变**(T6):全程设备内/后台/严禁后端预生成下发 —— 此为代码侧不变量,已满足,**与真机耗时实测解耦,继续硬约束**。
- 后续若用户提供移动环境,§4/§5 清单按原触发边界续做(路径 B/C 复议条件不变);在此之前发布决定不受其阻。

## 7. 用户范围裁定(2026-05-18)—— 本次发布 = 签名内核里程碑;移动 APP 全分布式签名为显式后继(解 RA-001 go/no-go 范围问)

> 权威级:用户显式择「方案 A」(推荐范围框定),解 RA-001 审计明示的「go/no-go 取决于移动是否本次范围」开放问。

- **本次发布范围 = 「MPC 签名内核 + 可信最小化 coord/relay 服务端 + 双 E2E 门(localhost E2E-001 / Docker E2E-002)+ 可构建移动库(Android `.aar` via CI、iOS `.xcframework` via GitHub macOS runner)」里程碑发布**。
- **移动 APP 的全分布式端上签名 + 生产级 RN 桥 = 显式披露的后继里程碑**,本次**不声称已交付**。据此:RA-001 **P1-3(移动 SDK 进程内签名非分布式)/ P1-4(RN 桥惰性桩)= 诚实记录的范围边界,非本次发布阻断项**(无 remediation L3;作后继里程碑披露)。RA-001「终端钱包成品 NO-GO」为本里程碑发布预期且正确的结论(本次非最终消费者 APP)。
- **里程碑 GO 完成条件**(达成即「签名内核里程碑 GO」,转交用户最终验收):① RA-001 P1-1+P1-2 经 FIX-004 修复并 finalize(带证明联网环 keygen/reshare/sign 活跑实证 + 生产护栏专测 + 校准门 GREEN + 零回归)② DEP-001(x/mobile pin)finalize ③ 二者 post-merge 校准门 GREEN 零回归 ④ docs/release-readiness-audit.md 已 IN_HEAD(已达成)。P2×7/P3 文档化供用户最终 go/no-go,不阻里程碑(用户择修则另派)。

---

_状态:P0 Gate = GO。密码学/运行时/扁平 SDK 全绿 + bind 脚本就位 + 双 E2E 门 GREEN;**真机/移动环境实测(§4/§5)= 用户裁定 non-blocker,作发布说明续做项披露(§6)**;发布交付物新增 Android `.aar`(Linux/CI 真实生成,GM-001 在制)与 iOS `.xcframework`(GitHub Actions macOS runner 自动产出,CI-001 release.yml 在制,用户无需本地 Mac)。用户裁定本次发布=签名内核里程碑(§7,方案 A),移动 APP 全分布式签名为显式后继;里程碑 GO 待 FIX-004+DEP-001 finalize。本报告仅汇总,不改其它 docs/design/ 与 docs/ 生产码。_
