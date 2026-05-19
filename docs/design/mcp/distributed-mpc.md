# 分布式 MPC 引擎设计(网络化 keygen / sign / reshare)

> 性质:设计文档(L1 权威,评审通过前不写引擎代码)。中文。
> 关联 `mcp/sdk.md`、`contract/protocol.md`、`contract/api.md`、`server/server.md`。
> 用户裁定 2026-05-19:**彻底分布式才安全 —— 任何单机永不持 >1 份分片**。

## 0. 问题

现状(已核实):`internal/mpc.Keygen/Sign/Reshare` 在**单进程**内模拟全 n 方
(Go channel,注释明示 "no network, relay, or coordinator — that lands in
later tasks");`internal/mobileapi.runKeyGen` 一台客户端造出**全部 N 份**并
全封进本机 keystore。`internal/cli/mpcnet.go` 是**唯一**真网络化 tss pump,
但被文件 rendezvous 驱动、属测试载体。`sdk.OnWireMessage` 仅收侧 gate stub。
coord 无 keygen 协调 API。

→ 当前不是门限自托管:一机持全部分片 = 零门限保护。本设计定义把它做成
**真分布式**:每设备跑一方、只持己份、全程经 relay 交换、coord 永不见分片。

## 1. 安全模型与红线(不可协商,实现强制 + 测试守卫)

- R1 **单份持有**:一个设备进程在其生命周期内只生成并持有**自己那一份**
  share_i;任何代码路径不得使任何单机同时持 ≥2 份。`mpc` 单方入口不得返回
  他方分片;keystore 仅封 share_i。
- R2 **本地 PreParams**:PreParams 每设备本地生成(`mpc.KeygenConfig`
  注释红线),绝不由 coord/relay/任何对端下发或预生成。
- R3 **coord 零分片 / keygen 零参与**:coord 不参与 keygen 仪式、不见任何
  分片/PreParams;仅在 keygen 完成后由组**记录**结果组公钥(对齐
  `spec/group-provisioning.md` §5.1 设计意图)。签名沿用既有 coord B 侧。
- R4 **relay 仅传输**:两方间 Noise 端到端会话,不在 relay 终结;relay 转发
  密文,不可读不可改(`server/server.md` 既定)。pnet PSK 隔离网络。
- R5 **会话隔离**:每 keygen/sign/reshare 会话独立 `sessionID`;跨会话/重放
  消息按 `contract.AcceptInbound` 无条件丢弃(`contract/protocol.md`:53)。
- R6 **全 n 在线方可 keygen**:keygen 是 n 方交互协议,缺任一方即不可完成
  (失败/超时干净中止,无降级);签名仅需 t+1 子集(既有 coord START)。

## 2. Rendezvous 决策:B(relay 自举,coord keygen 阶段零参与)

用户裁定取信任最小化:**B**。keygen 阶段 coord 完全不参与;设备经 libp2p
relay 的 rendezvous(namespace 派生自 groupId)互相发现并跑仪式。A(coord
中介 keygen 会话)作为 robust 化**备选**记录:若 B 的去中心化协调在生产中
鲁棒性不足,可加 coord keygen-provisioning 契约(api.md 增项,L1 级),
**但 R1–R5 红线在 A/B 下完全一致**(A 中 coord 仍零分片、仅协调连通元数据)。

## 3. 分布式 keygen 仪式(B)

参与者:一个**发起方**(initiator,组内任一成员设备)+ 其余 n−1 成员。

1. **会话声明**:发起方生成 `sessionID`(随机,全局唯一),声明
   `{groupId, sessionID, t, n, memberSet[], deadline}`,经成员身份私钥签名
   (复用 `contract` senderAuth)。
2. **发现/汇合**:全 n 方在 relay 注册 rendezvous,namespace =
   `H(groupId || "keygen" || sessionID)`(复用 `internal/transport`
   Advertise/FindPeers —— 当前未接线,本设计接活)。各方互建 Noise 会话
   (pnet + circuit-relay v2)。
3. **集合定版**:全 n 方在场且签名校验通过 → 冻结**有序 party 集**
   (party_i 索引 = 成员在 `memberSet` 的确定性序,各方独立可算,无需信任
   发起方排序)。任一不一致/缺员 → 全体干净中止(R6)。
4. **本地 PreParams**(R2)→ 跑 tss-lib keygen 多轮:出站 tss 消息经引擎
   → Noise/relay 投递指定对端;入站经 `contract.AcceptInbound`(R5)喂入。
5. **产出**:每方得**仅 share_i**(R1),封入本机 keystore;各方独立算
   `groupPubKey` 并交叉校验一致(不一致中止)。
6. **组记录**(R3):keygen 成功后,成员经既有 coord B 侧/组开通路径
   登记 `groupPubKey`(coord 仅存公开组公钥,永不见分片)。

中止/超时:任一阶段失败 → 全体终止会话、清理半态、不留分片;可换新
`sessionID` 重试。防捣乱:成员签名 + 集合定版要求全员一致,异常成员致
会话失败(keygen 本就需全 n,无可用降级,与 R6 一致)。

## 4. 生产网络引擎(复用 mpcnet)

抽取 `internal/cli/mpcnet.go` 的 tss-over-libp2p pump 为**生产包**
(建议 `internal/mpcnet`),与文件 rendezvous **解耦**:

- 输入:本方索引 / 全 party 集 / sessionID / relay 接入(peerIDs+multiaddrs)
  / 角色(keygen|sign|reshare)/(sign/reshare 时)本方 share。
- 内部:tss-lib party 驱动 + 出/入站消息路由(Noise/relay)+ 会话隔离。
- 输出:keygen→share_i;sign→{R,S,V};reshare→新 share_i。
- `internal/cli`(E2E 载体)保持现状不动(零回归);新引擎为独立包。

## 5. SDK 契约变更(`mcp/sdk.md` 同步修订)

单方化 + host 拥有传输(与移动端 RN host 一致;PC CLI 即 host):

- `KeyGen(configJSON)`:configJSON 增 `{groupId, sessionID, partyIndex,
  n, t, memberSet, relay{peerID,addrs[]}, role}`;**只产 share_i**,
  `OnResult` 摘要含 groupPubKey + 本方 moniker(单数)。
- `Sign`/`Reshare`:同样单方网络化(sign 仍由 coord START 提供 signers +
  relayHints;reshare 类 keygen 仪式)。
- **新增出站 wire 回调**(Go→host):引擎产出的 tss 消息经回调交 host
  传输(移动原生桥 / PC CLI libp2p)。`OnWireMessage`(host→Go)由
  stub 接为**活的入站喂入**(R5 gate 不变)。
- keystore:仅封 share_i(R1)。

> gomobile 约束不变:回调/参数仍 flat(string/[]byte/接口);新增回调
> 经 `sdk` 1:1 再导出。

## 6. 分层交付(每层独立零回归门:build/vet/lint/-race/E2E;首笔提交排在
当前后台 CI 门 `bcpe8mbb5` 绿之后)

1. `internal/mpc` 单方 keygen/sign/reshare 入口(只己份;`internal/mpc`
   既有全 n 模拟保留供测试,新增单方 API)。
2. 抽取泛化 `internal/cli/mpcnet` → `internal/mpcnet` 生产引擎(去文件
   rendezvous),`internal/cli` 不动。
3. SDK 单方化 + 出站 wire 回调 + `OnWireMessage` 接活;`sdk` 再导出;
   `mcp/sdk.md` 修订。
4. keygen 仪式(B):transport rendezvous 接活 + 会话声明/定版/中止子协议。
5. host 传输接线:PC CLI(libp2p,复用 transport)先行打通真三方
   keygen+sign E2E;移动原生桥同接口(同一 SDK,两端同获益)。
6. 组记录对齐既有 coord B 侧 / group-provisioning(coord 零分片校验)。

## 7. 风险与开放项(评审需定)

- B 的发现/定版子协议鲁棒性(NAT/掉线/部分到场);备选 A 触发条件。
- party 索引确定性序的权威来源(memberSet 来自何处:组开通契约 /
  group-provisioning §5.1 —— 需与 S-001/G-001 对齐)。
- reshare 的旧/新委员会重叠在线要求(类 keygen,需另列时序)。
- E2E:真多进程 n 方 keygen 验收(可由 PC CLI 充当 n 个独立 host 进程,
  非单进程模拟 —— 这是"彻底分布式"的验收判据)。

## 8. 验收判据(彻底分布式成立的硬证据)

- 跨 n 个**独立进程/设备**(各自 keystore)跑 keygen,事后每个 keystore
  **仅含 1 份** share;任意 t+1 进程可签、任意 ≤t 不可;任一进程的磁盘/
  内存全程无他方分片;coord 日志/库无任何分片或 PreParams;relay 仅见
  密文。任一条不满足即未达"安全的彻底三方"。
