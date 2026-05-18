# P0 任务分解 —— 端上 MPC 打包验证(路径 A)

> 目标:验证 **gomobile + tss-lib v3** 能在 **真实 iOS + 真实 Android** 编译并跑通 **keygen**,产出 `.aar`/`.xcframework`,并确认端上 PreParams 方案可接受。
> 性质:硬门槛(P0 不过,路径 A 不成立)。本阶段**仅 keygen + 进程内多 party**,不涉及网络/relay/coord-server(那是 P1/P2/P3)。
> 状态:规划。下方每项含可验证标准(verify)。

## 退出标准(Gate)

全部满足才算 P0 通过,否则按「失败处置」改道:

1. keygen 在**真实 iOS 设备**与**真实 Android 设备**经 gomobile lib 跑到完成,产出可用 save data。
2. 最小 RN App 能加载 lib、调用 keygen、跨 JS 桥拿回 save data(提前给 P4 兜底)。
3. 端上 PreParams **全程设备内**后台生成,App 不卡 UI,实测耗时 ≤ 验收阈值。
4. lib 体积与冷启动增量在预算内。

## 任务

### T1 工具链基线
- 安装校验 Go 1.23、`gomobile`/`gobind`、`gomobile init`;Android NDK + SDK;Xcode + iOS SDK。
- **verify**:`gomobile version` 正常;官方 `golang.org/x/mobile/example/bind` 能分别产出 `.aar` 与 `.xcframework`。

### T2 tss-lib keygen 最小封装(gomobile 友好)
- 设计扁平 API(仅 string/[]byte/callback,无泛型/复杂结构体):启动 n 方 keygen、经 callback 泵消息、回 save data 字节;复杂类型全封装 Go 侧。
- 进程内 N 方仿真(复用 `docs/design/tss-lib/ecdsa/keygen/local_party_test.go` 的接线模式),无需网络。
- **verify**:宿主机 `go build` + `go vet` 通过;进程内 3 方 keygen 测试跑到 `endCh`,save data 非空且各方公钥一致。

### T3 Android 绑定与真机 keygen
- `gomobile bind -target=android` 产 `.aar`;塞进一次性 Android 壳工程;在壳内进程内跑 3 方 keygen。
- **verify**:`.aar` 生成;真机 + 模拟器各跑完 keygen;记录 PreParams 与总 keygen 墙钟耗时;无崩溃/ANR。

### T4 iOS 绑定与真机 keygen
- `gomobile bind -target=ios` 产 `.xcframework`;塞进一次性 iOS 壳工程;模拟器 + 真机跑 3 方 keygen。
- **verify**:`.xcframework` 生成;真机 + 模拟器跑完 keygen;记录耗时;确认无 Go runtime/信号相关崩溃(gomobile 路径,验证而非假设)。

### T5 RN 集成冒烟(路径 A 桥接早验)
- 最小 RN App 经原生模块/自动链接载入 `.aar`/`.xcframework`,JS 调 keygen 取回 save data。
- **verify**:iOS + Android RN App 各成功调用 keygen 并跨桥拿回 save data;确认扁平 API 在 RN 桥下不丢类型。

### T6 端上 PreParams 策略验证(安全红线)
- 跨设备档位实测 `GeneratePreParams` 耗时;后台线程生成 + 进度回调;严禁 UI 线程;**严禁后端预生成下发**(含 Paillier 私钥)。
- 调参:`GeneratePreParams` 超时与并发(`runtime.NumCPU`)在手机上的取值。
- **verify**:PreParams 全程设备内后台生成、App 保持响应、耗时入表;设定验收阈值(建议:中端机 + 进度 UI 下 ≤ 60s;超出则触发路径 C 复议)。

### T7 体积与冷启动
- 记录 `.aar`/`.xcframework` 体积、Go runtime 引入的冷启动增量。
- **verify**:数值入表并对照预算(预算值在 T7 开始前与需求方确定)。

### T8 Gate 决策与报告
- 汇总 T3/T4/T6/T7 实测,对照退出标准,出 `docs/design/P0-report.md`(测量值 + go/no-go + 若失败的改道建议)。
- **verify**:报告产出并给出明确结论。

## 失败处置(不接受降级为远端签名)

- gomobile 编译/运行不通 → 改 **路径 B**(Go `c-shared` + 自写桥,注意 iOS 信号栈)。
- PreParams 端上不可接受且 B 无改善 → 触发 **路径 C**(Rust + Dfns CGGMP21,替换基座)用户决策。
- 纯端上 MPC 始终为硬约束,任何分支均不得退化为远端签名服务。

## 依赖与边界

- 不依赖:relay、coord-server、libp2p、外部业务服务(均 P2/P3)。
- 多 party 一律**进程内仿真**(单设备内多 goroutine),验证密码学 + 运行时 + 打包,不验证网络。
- 产物:`.aar`、`.xcframework`、最小壳工程、RN 冒烟工程、`docs/design/P0-report.md`。
