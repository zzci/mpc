# 门限签名 SDK — 自托管共管钱包的签名内核

> 基于 `bnb-chain/tss-lib` v3 的门限签名(MPC)库 + 协调服务。
> 参考源码已 vendor 至 `docs/design/tss-lib/`(tag v3.0.0)。
>
> **范围边界**:本项目 = MPC 签名内核(keygen/signing/resharing)+ 传输 + **中心协调服务器** + **只读交易解码(安全闸门)**。
> **含**(只读、安全攸关):解析 ETH/BSC/TRON 的 unsignedTx → 结构化事实、从解析结果重算链摘要并断言 `==digest32`、产出 A 区、与带外 `businessInfo` 声明式核对。
> **不含**(链/业务逻辑,外部业务服务负责):交易构造、ABI/calldata 编码、nonce/gas/energy 估算、广播、质押业务决策(仅*解码展示*在内,*构造决策*在外)。
> 对外契约:外部服务提交「签名请求信封」,本系统返回链无关的 `{R, S, V}`(low-S 规范化)。

## 0. 文档索引

本文为总览 + 决策记录 + 索引;细节见各专题文档(按 owner 组织):

| 文档 | 归属 | 内容 |
|---|---|---|
| `PLAN.md`(本文) | 共享 | 范围/架构概要/决策记录/风险/分期/开放点 |
| `architecture.md` | 共享 | 组件/部署拓扑/时序/数据流/信任模型汇总 |
| `security.md` | 共享 | 威胁模型/信任边界/安全不变量/攻击对策矩阵 |
| `testing.md` | 共享 | mcp+server 测试策略/阶段门禁/覆盖率 |
| `P0-tasks.md` | 共享 | P0 详细任务分解 |
| `mcp/sdk.md` | mcp | 端上 SDK:mpc-core/mobile-api/tx-decode/keystore/transport/rn-bridge |
| `server/server.md` | server | node:relay+coord 角色、配置 |
| `server/database.md` | server | coord 持久化(SQLite;relay/mcp 无 DB) |
| `server/admin.md` | server | 单一运维管理员:交易/历史会话查看、防滥用、审计 |
| `contract/api.md` | 契约 | 外部服务↔coord、成员↔coord 接口 |
| `contract/protocol.md` | 契约 | 信封/libp2p/心跳/START/能力令牌/版本 |
| `tss-lib/` | 参考 | bnb-chain/tss-lib v3 源码(tag v3.0.0) |

## 1. 确认范围

| 项 | 决定 |
|---|---|
| 形态 | 自托管共管钱包的签名内核 + 协调服务;每成员手机持一片分片 |
| 职责 | keygen/signing/resharing + 多方协调 + 传输 + 待签编排 + **只读交易解码与 A/B 核对**;不碰交易构造/广播 |
| 门限 | 默认 2-of-3;t/n 可配;n 可经 resharing 扩容 |
| 打包路径 | **A:gomobile + tss-lib**(已定) |
| 客户端 | Go(tss-lib)经 gomobile 编译为 .aar/.xcframework,供 RN 集成 |
| 通信 | go-libp2p(Noise 端到端)经通用零信任 relay 节点跑 MPC 消息 |
| 编排 | 中心协调服务器 `coord-server`(信任最小化):接待签信息、追踪连通性、法定人数就绪发起会话 |
| 对接 | `coord-server` 与外部业务服务对接:收签名请求、回 `{R,S,V}` |
| 安全红线 | **禁止盲签**:设备侧验签 + 人审(WYSIWYS)后才进 MPC |
| 构建基线(硬约束) | **Go 1.25 线**为唯一基线(用户 2026-05-18 裁定方案 B：放宽至 go1.25 以解 protobuf↔libp2p↔x/sys 依赖矛盾)。`go.mod` 声明 `go 1.25`(toolchain go1.25.x 允许)。第三方依赖取**go1.25 兼容的稳定版**,不得无脑 `latest`,须 registry 校验并在 `go.mod` 注记原因。锚定(实测背书):`protobuf v1.36.6`(B2 实测 tss-lib v3.0.0 @v1.36.6 `go test ok`,已审计树无回归——原 v1.31.0 pin 解除)、`go-libp2p v0.48.0`（用户裁定 2026-05-18 安全升级,自 v0.43.0；连带 quic-go v0.59.0 + webtransport-go v0.10.0,清除 GO-2025-4233/GO-2026-4488/-4485/-4483,govulncheck 0 可达漏洞；transport/relay/mpc 测试 63 pass 零回归）/配套 `pubsub`/`multiaddr` 同期版、`x/crypto ≥v0.39.0`(N-002 实测 full repo `go test ./...` exit0);`x/sys` 不再 pin(go1.25 下自由)。`golang.org/x/mobile`(**L1 裁定 2026-05-18,RA-001/GM-001 YELLOW**):`gomobile bind` 需 `golang.org/x/mobile/bind` 在模块解析内(生成的 Android/iOS 桥胶水代码 import 之),否则报「no Go package in golang.org/x/mobile/bind」。据 §6 移动库生成已为发布交付物 + 本仓 reproducible/provenance/供应链纪律(release.yml 含 govulncheck/gitleaks/SHA256),**裁定:`x/mobile` 以 recorded-reason pin 正式纳入 go.mod**(锚定 GM-001 已实证成功 .aar 构建所解析的确切 x/mobile 伪版本,go.sum 锁定);**否决** CI 内浮动 `go install x/mobile@latest` 方案(非可复现、供应链风险、违 §1 pin 纪律)。落地经专项小 L3(go.mod/go.sum 独占,`go mod tidy` + 校准门 GREEN 零回归实证),不并入 FIX-004/GM/CI 范围。**端上 SDK 残留风险**:gomobile@go1.25 对 .aar/.xcframework 真机打包兼容性须移动环境实测(§6 裁定:真机=non-blocker;.aar Linux/CI、.xcframework GitHub macOS runner 均已为发布产物)。破基线(如 go.mod 退回 go1.23 致依赖矛盾复现)即 RED。 |

## 2. 关键技术结论:签名内核链无关

ETH / BSC / TRON 全部使用 **secp256k1**:一次 DKG 出单一主公钥,同一密钥三链通用。库对传入的 32 字节摘要门限签名,输出 `{R,S,V}`(含 recovery id,low-S 规范化)。**新增内置只读解码器**(`tx-decode`)从 unsignedTx 解析事实并**重算链摘要断言 `==digest32`**,作为 A 区权威来源;交易构造/calldata 编码/广播仍在外部业务服务。可选 `addr` 便利函数:公钥→ETH/TRON 地址(纯派生)。

## 3. 架构

```
外部业务服务 ("另外一个服务",不在本项目)
  └ 产生交易/签名需求 · 持有链逻辑 · 负责广播
        │  ① 提交签名请求(信封)         ⑥ 接收 {R,S,V}
        ▼                                  ▲
┌────────────────────────────────────────────────┐
│  中心协调服务器 coord-server  (信任最小化)        │
│  · 持久化待签信封 = 「待签列表」                  │
│  · 追踪成员在线/连 relay 状态(成员签名心跳上报)  │
│  · 法定人数 ≥ t 且已审批 → ② 通知 ③ 发起会话      │
│  · 无分片 · 不参与 MPC · 偷不走钱、伪造不了审批    │
└───────────┬────────────────────────────────────┘
            │ ② 推送/拉取通知 + ③ 开始信号
     ┌──────┼───────────────┐
     ▼      ▼               ▼
   手机A   手机B            手机C   (SDK = MPC + 加密keystore + ④验签后才签)
     └──── go-libp2p Noise 经 零信任 relay 跑 ⑤MPC 消息 ────┘
              relay:circuit-relay v2 哑管道,读不到内容/不持分片
```

流程:① 外部服务提交信封 → `coord-server` 入「待签列表」并通知成员 → ② 成员上线、连 relay、签名心跳上报状态 → ③ 法定人数就绪,`coord-server` 选定签名子集并发「开始」 → ④ 各设备用**内置 `tx-decode`** 重算链摘要断言 `==digest32`、A/B 分区展示、用户审核 → ⑤ 选定成员经 relay 跑 tss 签名(`coord-server` 不参与)→ ⑥ `{R,S,V}` 回传外部服务广播。

### 签名请求信封 & 禁止盲签(安全红线)

```
SigningRequest { requestId, chain, unsignedTx/intent(库视为不透明),
                 digest32, proposer, expiry,
                 businessInfo?(带外业务说明,结构化,可选),
                 metaHash = H(businessInfo),
                 proposerSig(覆盖以上全部字段,含 metaHash) }
```
不变量:任一共签设备进入 MPC **前**必须 (1) 由**内置 `tx-decode` 解码器**解析 `unsignedTx` 并**重算链摘要断言 `==digest32`**(失败即拒签);(2) 展示 A 区可读事实 + B 区业务说明,并做声明式 A/B 核对;(3) 用户审核通过。库定义信封结构 + 强制「验证后才签」;解码器内置三链(安全最优),允许可插拔覆盖,但覆盖实现仍须满足同一「重算摘要 == digest32」断言。

**带外业务说明(`businessInfo`)**:可选结构化字段(title/summary/items/refs 如发票/订单号/requester/memo/displayHints),供签名方理解业务意图。安全定位 —— **仅辅助展示,不绑定链上效果**:经 `proposerSig`+`metaHash` 防 coord 篡改,但不能替代 `tx-decode` 解析。**展示契约(强制分区)**:审批界面必须分两块 —— **A「已校验交易事实」**(`tx-decode` 从 `unsignedTx` 解码的 to/value/chain/合约/方法,且摘要已绑定,资金安全**唯一**权威依据)、**B「业务说明(带外,proposer 签名,非链上校验)」**;B 不得视觉冒充 A;**A/B 声明式核对**(如金额/收款方一致性,按 `displayHints` 规则)由 `tx-decode` 执行,不一致显著告警;未识别合约调用不臆造标签,展示原始 selector/calldata + 「谨慎审核」告警。

### 信任边界

- **零信任 relay**:Noise 两 party 端到端、不在 relay 终结;relay 仅哑管道,读不到内容/无法伪造发件人/不持分片;可第三方运营。
- **信任最小化 coord-server**:可 DoS/审查、获知交易隐私;但**偷不走钱**(无分片、伪造签名需 ≥ t 方且它不参与 MPC)、**无法盲签**(设备侧 WYSIWYS)、**无法让各方签不同内容**(TSS 要求同一摘要,篡改即失败)、**改不动 `businessInfo`**(proposerSig+metaHash 覆盖)、重放被 requestId/expiry + 链上 nonce 挡。
- **带外业务说明仅辅助**:`businessInfo` 经 coord 防篡改但**不绑定链上效果**;仅 A 区(unsignedTx 解码)保护资金,B 区被钓鱼时由「A 权威 + A/B 显著告警 + 人审」兜底。不破坏禁止盲签不变量。
- **先例**:THORChain go-tss(libp2p 包 tss-lib);Berty/Status(libp2p + gomobile 移动端实战)。

## 4. 模块拆分

| 模块 | 职责 | 语言 |
|---|---|---|
| `mpc-core` | tss-lib v3 封装:keygen/signing/resharing 编排,消息收发循环 | Go |
| `mobile-api` | gomobile 友好扁平接口:`KeyGen/Sign/Reshare`;`Sign` 接收信封,经 `tx-decode` 验证后才进 MPC,仅 string/[]byte/callback | Go |
| `tx-decode` | **内置只读三链解码器**:ETH/BSC(legacy+EIP-1559+ERC20/合约调用)、TRON(原生+TRC20+Stake2.0 系统合约);解析 → 重算链摘要断言 `==digest32` → 产出 A 区 → A/B 声明式核对;可插拔覆盖(覆盖须满足同一断言);未识别调用不臆造标签 | Go |
| `keystore` | 分片落盘加密(设备 keychain + 口令),备份/恢复 | Go |
| `transport` | `Transport` 接口;首实现 = libp2p(Noise + GossipSub + circuit-relay 客户端) | Go |
| `node`(单程序,双角色) | 同一二进制,配置 `relay.enable` / `coord.enable` 各自开关(可单开/双开):<br>· **relay 角色**:通用零信任 circuit-relay v2 + rendezvous;无状态、无明文<br>· **coord 角色**:收信封/待签列表、连通性追踪、法定人数发起、推送、外部对接 API、`{R,S,V}` 回传<br>详见 `docs/design/server/server.md` | Go |
| `admin-api` | coord 内管理接口:交易/历史会话只读查询 + 防滥用控制(配额/封禁/PSK 轮换);全操作入 `admin_audit`;不签发成员准入 | Go |
| `admin-ui` | 运维 Web 控制台,**htmx + tailwindcss 服务端渲染**,与 `admin-api` 同进程(`//go:embed uiassets`,无 Node 构建链);非公网暴露 | Go (htmx + tailwindcss) |
| `walletcli` | PC 钱包成员端(`cli serve`):JSON `/v1/*` API + htmx UI(`/ui/*`,WYSIWYS sign approval / import / fetch / xpub / address);非生产工具,运维与调试用 | Go (htmx + tailwindcss) |
| `addr` (可选) | 公钥→ETH/TRON 地址纯派生;非核心 | Go |
| `rn-bridge` | React Native 原生模块,桥接 gomobile lib ↔ JS | Kotlin/Swift/TS |
| `sample-app` (可选) | RN 集成示例:keygen/sign/reshare + 多方审批演示;非产品 | TS/RN |

> 已移除:`chain-*`(链/业务逻辑,范围外)、`control`/Waku/Matrix(由 coord 角色推送+拉取 + FCM/APNs 取代)。
>
> **单程序双角色**:relay 与 coord 是同一二进制的两个角色,可同进程合并部署(默认,最简)或拆开独立部署/复制。**合并不削弱 relay 密码学零信任**:Noise 仍两 party 端到端、不在 relay 终结;coord 的明文信封经「外部服务→coord API」另一路径进入,不经 relay 转发。合并仅是两角色交同一运营方(信任域合并),设计保持角色内部解耦以支持随时拆分;relay 角色仍可独立复制/第三方运营。
>
> **配置**:`node` 经配置文件(默认 `./server.yaml`,`SERVER_CONFIG` 可改)+ 环境变量覆盖(前缀 `TSSSERVER_`,嵌套 `__`)控制;优先级 `默认 < 文件 < 环境变量`;secret 仅经 env/密钥文件注入,启动 fail-fast 校验。完整参数表见 `docs/design/server/server.md` 「配置」章节。

## 5. 重大风险与对策

1. **gomobile 类型限制**:不支持泛型/复杂结构体导出。→ `mobile-api` 扁平化(`KeyGen(json)`/`OnMessage(bytes)`/callback);tss-lib 复杂类型全封装 Go 侧。
2. **PreParams 慢(安全素数)**:手机 10–30s 且耗电。→ **端上后台生成 + 进度 UI**,严禁 UI 线程。**红线**:含 Paillier 私钥,**禁止后端预生成下发**。
3. **打包路径**:tss-lib v3 纯 Go 无 cgo(Go 1.23),gomobile 无原生依赖,P0 编译风险低;路径 A 已定。
4. **丢手机 = 丢分片**:2-of-3 丢 1 片仍可签,但须立即 resharing 重建,否则降级 2-of-2 无冗余。→ 内置「丢失成员 → 剩余 t 方协同 resharing 重置」。
5. **coord-server 单点(可用性 + 隐私)**:SQLite 单节点 → 宕机即不可发起;能获知交易详情。→ 文件备份 +(可选)Litestream/LiteFS 容灾;信封 proposerSig;expiry/requestId 防重放。安全不依赖其诚实(见信任边界)。资金安全由 TSS 门限 + 设备侧 WYSIWYS 保证;可用性降级可接受。
6. **连通性**:手机多在 NAT 后,实际几乎都经 relay 中转;多 relay 部署 + 客户端可配置自动故障转移。
7. **签名输出正确性**:V/low-S 错误致消费方 `ecrecover` 失败/地址错位。→ P3 用三链真实摘要交叉验证。
8. **盲签**(已列为红线):见 §3 信封不变量;`tx-decode` 重算摘要不通过则拒绝进入 MPC。
9. **`tx-decode` 解码器正确性(新增,安全攸关)**:A 区是资金安全唯一权威,解码器 bug = 错误展示 → 误签;三链各类交易/编码边界(EIP-1559、ERC20 非标实现、TRON protobuf、错误填充)易出错。→ 与摘要重算断言**双重绑定**(解码错则摘要对不上即拒签,降低误签为拒签);P3 必须以三链真实交易语料 + 模糊测试覆盖;未识别一律降级为「原始 + 告警」不臆造。
10. **coord 库整库加密 + 默认锁定**:专防 `.db` 文件泄露 —— 全库加密,口令 Argon2id 派生密钥仅内存,启动即 LOCKED、fail-closed(API `503 LOCKED`、不收交易、不泄数据)。**口令丢失则 coord 库不可恢复**(资金不受影响,分片在设备)→ 运维须安全离线备份口令;不防在线运营方(设计性接受)。详见 server/database.md §7、server.md C9b、server/admin.md §8。

## 6. 分阶段交付计划

- **P0 端上 MPC 打包验证(路径 A)**:gomobile + tss-lib v3 在 iOS+Android 编译并跑通 keygen,出 .aar/.xcframework;实测端上 PreParams 耗时与后台方案。
- **P1 MPC 核心**:Go 侧 keygen/signing/resharing 端到端(参考 `docs/design/tss-lib/ecdsa/*/local_party_test.go`),进程内内存通道跑通。
- **P2 传输层(libp2p + relay)**:`Transport` + libp2p 替换内存通道;实现并部署 `node` 的 **relay 角色**(server/server.md R 部分);验证零信任(relay 抓包仅密文)。
- **P3 协调、解码与契约**:`node` 的 **coord 角色**(待签列表、连通性追踪、法定人数发起、外部对接 API、`{R,S,V}` 回传;server/server.md C 部分 + server/database.md + contract/api.md)+ 信封结构(contract/protocol.md)+ **`tx-decode` 三链内置解码器**(解析 → 重算摘要断言 → A 区 → A/B 核对,真实交易语料 + 模糊测试)+ 可插拔覆盖;V/low-S 用 ETH/BSC/TRON 真实摘要交叉验证;三机经 relay 端到端跑通 keygen+sign+reshare。
- **P3.5 管理面**:`admin-api` 只读查询(交易记录 + 历史签名会话 + 审计 + relay 计数)+ `admin-ui`;`admin_audit`(server/admin.md、server/database.md §6)。
- **P4 移动封装**:gomobile bind + `rn-bridge` 原生模块桥接。
- **P5 示例与韧性**:`sample-app`;keystore 备份/恢复;丢失成员 resharing 流程。
- **P6 加固**:keystore 加密、coord 鉴权/防滥用、relay 访问控制(威胁模型 A)、**管理面强鉴权/读控权限分离/非公网/审计不可篡改**、容灾、安全审查。

## 7. 开放点状态

1. ~~打包路径~~ → **已定:A(gomobile + tss-lib),纯端上 MPC 强制。**
2. ~~Relay~~ → **已定:通用零信任 relay 节点**(libp2p circuit-relay v2 + rendezvous)。
3. ~~链/质押~~ → **已定**:交易构造/广播/质押决策范围外(外部服务);**但新增内置只读 `tx-decode` 三链解码器**(安全最优 + 可插拔覆盖)用于 A 区与 A/B 核对 —— 这是对早先「不含链逻辑」的有意安全细化:解码*在内*,构造/广播*在外*。
4. ~~Relay 访问控制~~ → **已定:威胁模型 A**(仅阻止未授权,不隐藏成员元数据);信任锚默认自主式,P6 复议。
5. ~~待签来源 / 中心服务~~ → **已定:中心 `coord-server`** 接收待签信息并按连通性发起;`control`/Waku/Matrix 取消。
6. ~~假设 A1–A4~~ → **已确认**:A1 待签来源=外部业务服务,**结果必须回传**外部服务;A2 连通性由成员签名心跳上报(不耦合 relay);A3 coord-server 不参与 MPC;A4 离线=上线拉取 + 推送。
7. **过期(TTL)为一等公民**(A4 强调):信封 `expiry` 强制校验 ——(a) `coord-server` 到期即标记/剔除待签项并回报外部服务「已过期」;(b) 设备侧进入 MPC **前**与签名**前**各校验一次未过期,过期立即拒签;(c) 推送/拉取均携带剩余 TTL;(d) 已过期请求不得复用 requestId。
8. ~~管理面~~ → **已定**:单一全局**运维管理员** + Web UI,可查交易记录与历史签名会话、日志/指标、防滥用控制;**不签发成员准入**(自主式不变),看不到分片/MPC。隐私:coord 经 API 本就持信封明文,管理员可见不增信任增量,**交易隐私对运营方/管理员开放(设计性接受)**;静态加密降为仅防库被盗。详见 `server/admin.md`、`security.md` §8。

---

_状态(2026-05-20):P0–P3 + P3.5 已交付,真分布式 MPC 闭环(DM-1..DM-6)+ 地址派生(AD-1..AD-6 + H-005 GREEN)+ admin-ui + wallet-cli UI 全部 in main;P4 移动封装(gomobile bind 脚本就位,GM-001 .aar 实证可构;真机后继门待移动环境)、P5 sample-app/韧性场景、P6 持续加固为后续工作。所有开放点闭环;开发文档体系见 §0 索引;P0 详细任务分解见 `docs/design/P0-tasks.md`。数据库:**SQLite 单文件、整库加密 + 默认锁定(口令解锁,仅内存,fail-closed)**;coord 单节点 + 文件备份/Litestream 容灾;内存 SQLite 承载无状态在线集;历史长期保留以支撑管理面。_
