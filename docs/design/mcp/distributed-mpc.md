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
- R3 **coord 零私密 + 仅事件引导**(精修 2026-05-19,用户两轮裁定):
  coord 永不见任何分片 / PreParams / tss-lib party 内部状态 / 任何 keygen
  私密产物;coord **不持**任何 tss-lib party、**不经手**任何份额、**不在**
  密码学消息路径(连密文中转都不做,relay 才负责)。coord 角色 = **纯事件
  引导**:发布 keygen / reshare / state-reconciliation 事件、校验 identity、
  记录事后公开元数据(组公钥 / chaincode / member identity)。所有密码学
  仪式(包括 commit-reveal chaincode)在成员设备之间经 relay+Noise 直接
  交换。签名沿用既有 coord B 侧(START dispatch 同属事件引导)。
- R7 **coord 公钥 append-only**(用户裁 2026-05-19):一旦 `groups.ecdsa_pubkey`
  写入,coord **不可删除、不可置空、不可由他值覆盖**(reshare 由设计保证
  Q_master 不变,故无合法 update 路径)。`coorddb` 应以 CHECK 约束或事务
  级守卫强制(详 §4)。运维误操作 / 恶意管理员均不可越此不变量。
- R4 **relay 仅传输**:两方间 Noise 端到端会话,不在 relay 终结;relay 转发
  密文,不可读不可改(`server/server.md` 既定)。pnet PSK 隔离网络。
- R5 **会话隔离**:每 keygen/sign/reshare 会话独立 `sessionID`;跨会话/重放
  消息按 `contract.AcceptInbound` 无条件丢弃(`contract/protocol.md`:53)。
- R6 **全 n 在线方可 keygen**:keygen 是 n 方交互协议,缺任一方即不可完成
  (失败/超时干净中止,无降级);签名仅需 t+1 子集(既有 coord START)。

## 2. Rendezvous 决策:A(coord 中介 keygen 会话)— 用户裁定 2026-05-19

**A:由 coord 启动 keygen 请求并协调身份/连通,各 MPC 方据 coord dispatch
开始密钥生成。** 用户裁定取**身份/路由复用既有 B-side 机制 + 运维可控
robust**。R3(coord 零私密材料)在 A 下完整保留:coord 只触协调层
(initiate / identity 校验 / START dispatch / 事后组公钥+chaincode 记录),
**永不**触 tss-lib party 内部状态、份额、PreParams。密码学仪式本体在成员
之间经 relay+Noise 直接交换,coord 不在转发路径。

**身份模型(§2.1)= 复用既有 `group_members.identity_pubkey` 自证机制**:
每 MPC 方先在 coord 注册自身身份(secp256k1 公钥);**运营方以强制配置
预声明组的成员公钥集**;coord 在 keygen/sign/reshare 发起时校验请求者与
拟邀请方均在该强制集中,且各方对 START 的回应皆经其 identity 私钥签名
(沿 api.md B-side memberGate 同款机制)。**新键 identity 永远不会自动
注册**——必须经运营方配置匹配后方接受,防恶意方自加入组。

B(relay 自举,coord 完全不参与)曾为 L1 推荐备选,**未采纳**;若未来需
最大去中心化可重启评估,但本设计据 A 实施。

## 3. 分布式 keygen 仪式(A,coord 中介)

参与者:**发起方**(组内某成员设备,经其 identity 私钥签名)+ 其余 n−1
预注册成员 + **coord**(协调层,零私密)。

1. **身份预注册**(一次性 / 前置):每方经既有 B-side `PUT /v1/members/self/
   register` 等(或新等价端点)向 coord 注册其 `identity_pubkey`;**运营方
   强制配置**(`coord.groups.<gid>.expected_members = [pubkey1,pubkey2,…]`)
   声明该组允许成员公钥集;coord 启动校验配置/库一致。**未在强制集中的
   identity 永不可参与 keygen/sign/reshare**(R1/防自加入)。
2. **发起**:发起方调 coord 新端点 `POST /v1/groups/{groupId}/keygen`
   (api.md L1 改),携 `{sessionID(随机),t,n,memberSet=expected pubkey 集,
   deadline,proposerSig}`;coord 校验:(a) 发起方 identity 在强制集中
   (b) memberSet ⊆ 强制集且 |memberSet|=n (c) signature 有效 (d) 该 group
   尚无 group_pubkey(防重 keygen,既有 group 仅可 reshare)。
3. **dispatch**:coord 经既有 dispatchHub 范式向**全 n 方**派发 keygen-START
   `{sessionID, groupId, partyIndex(=memberSet 确定性序), peerMap(各方
   relay 接入点), deadline}`;每方经身份签名信道接收。
4. **互联**:各方经 relay 直接互建 Noise+pnet+circuit-relay v2 会话(coord
   不在转发路径,R3)。
5. **本地 PreParams**(R2)→ 跑 tss-lib keygen 多轮 + §3.6 commit-reveal
   chaincode(`docs/design/mcp/address-derivation.md` §3:SHA-256 承诺 +
   HKDF-SHA256 派生 + DST 域分隔 + group_id 绑定 + 严格 abort)。出站 tss
   消息经引擎 → Noise/relay 投递指定对端;入站经 `contract.AcceptInbound`
   (R5)喂入。
6. **产出**:每方得**仅 share_i**(R1),封入本机 keystore;各方独立算
   `groupPubKey` + `chaincode` 并各自经身份签名向 coord 上报。
7. **组记录**:coord 校验**全 n 方**上报的 `(groupPubKey, chaincode)`
   完全一致 → 同一事务持久化 `groups{ecdsa_pubkey, chaincode, evm/tron 派生}`
   + `group_members{identity_pubkey}` 锁定;任一不一致或缺员 → coord 拒
   持久化、整 keygen abort。

**中止/超时**:任一阶段失败 → coord 标记 sessionID FAILED、各方清理半态
不留分片;以新 sessionID 重试(R6:keygen 需全 n,无降级)。

**防捣乱**:强制配置匹配 + 各方身份签名 + commit-reveal 不可偏置
+ 上报一致性校验,异常方致会话失败 - 不影响他组。

## 3.ter 客户端状态上报与协商(用户裁定 2026-05-19)

**动机**:分片只在客户端 keystore,coord 不知任何客户端的真实持有状态。
在(a)coord 库重建/迁移、(b)成员设备重装/重新登录、(c)keygen 异常中止后
部分成员可能已持分片但 coord 状态不一致 等情形下,需要"**客户端为真**"的
状态对账机制。

**协议**:每成员设备在加入组 / 重连 / 主动触发时,向 coord 上报 attestation:

```
{ groupId, identityPubkey, holdsShare: bool, groupPubkeyHex?, chaincodeHex?, ts, sig }
```

其中 `groupPubkeyHex` / `chaincodeHex` 仅在 `holdsShare=true` 时附,且经
本机 keystore 直接读出(无 MPC、无网络);`sig` = 该成员 identity 私钥
对上述字段签名。

**coord 聚合 + 协商**:
- 全 n 强制集成员都上报 `holdsShare=true` 且 (groupPubkey, chaincode) 全员
  一致 → 该 group **REGISTERED**,可接受 sign / reshare 请求(协调层使能)。
- 任一成员 `holdsShare=false` 或 (pubkey, chaincode) 不一致 → coord 标
  group 为 **NEEDS_RESHARE / NEEDS_KEYGEN / INCONSISTENT**(下游 client 可
  据此决策):
  - 全 n 都 `holdsShare=false` 且 coord 库无 `ecdsa_pubkey` → **需 keygen**
    (走 §3 流程)。
  - 部分 `holdsShare=true` 且与已有 `ecdsa_pubkey` 一致 + 部分 `holdsShare=
    false` → **需 reshare**(走 §3.bis;新方需进强制集)或拒服务直至齐。
  - 任一上报与已记录 pubkey 矛盾 → **INCONSISTENT 警告 + 拒服务**,运维介入。
- coord 仅**记录 attestation 元数据 + 校验签名**,**永不**索取/见 share 本身
  (R3 不变)。
- attestation 重放防护:沿 B-side memberGate ts+nonce 模式(`contract.AcceptInbound`)。

**与既有 D-001 / S-002 ProvisionGroup 关系**:本协议是 ProvisionGroup
的**补充非替代**——ProvisionGroup 仍是 keygen 完成后首次写公钥的入口;
attestation 是后续运行期的对账层。

## 3.bis Reshare(同身份层,用户裁定 #3)

reshare 沿用 §2.1 同一身份模型:旧委员会 + 新委员会的 identity_pubkey 集
**均**需在强制配置中预声明;coord 经新端点 `POST /v1/groups/{groupId}/
reshare` 校验旧/新委员会签名后 dispatch reshare-START(类 keygen-START,
但新方 partyIndex 由新 memberSet 序定);tss-lib reshare 多轮 + 新方
PreParams 本地生成;旧方退出后 keystore 必须**抹掉旧份额**(R1 衍生:
不可同时持新旧份额)。reshare 完成后 coord 更新 `groups.ecdsa_pubkey`
(若 t/n 变 → 同时改 `group_members`);**chaincode 不变**(reshare 不动
xpub,与 `address-derivation.md` §8 「reshare 不再生成新 c」一致)。

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

## 6. 分层交付(每层独立零回归门:build/vet/lint/-race/E2E;A 模型据用户
裁定 2026-05-19)

1. **mpc 单方入口**:`internal/mpc` 单方 keygen/sign/reshare API(只己份;
   既有全 n 模拟保留供测试)。
2. **生产网络引擎**:抽取泛化 `internal/cli/mpcnet` → `internal/mpcnet`
   (去文件 rendezvous,改由 START.peerMap 驱动连接);`internal/cli` 不动。
3. **SDK 单方化** + 出站 wire 回调 + `OnWireMessage` 接活;`sdk` 再导出;
   `mcp/sdk.md` 修订。
4. **coord 事件引导契约**(coord 仅事件、不参与生成,R3 收窄):`coord.external`
   配置加 `expected_members` 强制集;`coorddb` 加 identity 注册;新 api.md
   端点(L1 权威 L1 改):`POST /v1/groups/{groupId}/keygen` 发布 **new-group
   事件**(用户裁 #2),`…/reshare` 发布 reshare 事件,`PUT /v1/groups/
   {groupId}/attestation` 客户端状态上报(§3.ter,用户裁 #1)。dispatchHub
   扩展 keygen-START / reshare-START / attestation-ACK 事件类型。**R7
   append-only**:`groups.ecdsa_pubkey` 加 CHECK / 事务守卫保护,任何
   DELETE / UPDATE 至 NULL / 不一致覆盖一律拒绝(用户裁 #4)。
5. **host 传输接线**:PC CLI(libp2p,复用 transport)先行打通真三方
   keygen+sign+reshare E2E;移动原生桥同接口(同一 SDK,两端同获益)。
6. **组记录** 同一事务校验全 n 方上报一致(§3.7)→ 与 address-derivation 的
   `groups.chaincode` 列(00004 迁移)对齐写入。

## 7. 风险与开放项(用户裁定状态)

**已裁定 2026-05-19(用户经 L1 入档,A 模型 + 身份模型):**
- ✅ §7.1 memberSet 权威源 = `coord.external.expected_members` **强制配置集**
  (运营方预声明)+ coord `group_members.identity_pubkey` 自证;未配置即
  不可参与(防自加入)。party 索引由 memberSet 确定性序定。
- ✅ §7.2 鲁棒性 = A(coord 中介)即是 robust 路径;B 自举不采纳,无"备选
  触发条件"项。
- ✅ §7.3 reshare 本轮纳入,同身份层(§3.bis);旧份额抹除强制;chaincode
  不变。

**已裁定 2026-05-19(追加 4 项设计精修 + 节奏):**
- ✅ §7.4 交付节奏 = **严格逐层**(用户按 L1 推荐裁,2026-05-19):每层
  finalize 后 L2 follow-up L1,L1 复核 + 用户确认方进下层。
- ✅ §3.ter 客户端状态上报与协商(用户裁 #1):attestation 协议,客户端为真。
- ✅ §3/§6 coord new-group 事件(用户裁 #2):事件化 keygen 触发。
- ✅ R3 coord 纯事件引导(用户裁 #3):coord 不在密码学路径,只发事件
  / 校验 identity / 记录元数据。
- ✅ R7 pubkey append-only(用户裁 #4):`groups.ecdsa_pubkey` 不可删/不可
  置空/不可不一致覆盖。

**验收硬判据(不可破)**:跨 n 个**独立进程/设备**(各自 keystore)跑
keygen,事后**每个 keystore 仅含 1 份** share;任意 t+1 进程可签、任意
≤t 不可;任一进程的磁盘/内存全程无他方分片;coord 库/日志无任何分片
或 PreParams;relay 仅见密文。任一条不满足即未达"安全的彻底三方"。

