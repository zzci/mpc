# 端上签名 SDK 设计(mcp)

> 移动端 MPC 签名 SDK:Go(tss-lib)经 gomobile(路径 A)编译为 .aar/.xcframework,RN 集成。
> 关联 `architecture.md`、`contract/protocol.md`、`contract/api.md`、`PLAN.md`。性质:开发文档,不写代码。
> **mcp 无数据库**:端上为设备 keychain + 加密 keystore。

## 1. 模块

| 模块 | 职责 |
|---|---|
| `mpc-core` | tss-lib v3 封装:keygen/sign/reshare 编排、消息泵、会话隔离 |
| `mobile-api` | gomobile 友好**扁平接口**(string/[]byte/callback,无泛型/复杂结构体) |
| `tx-decode` | 内置只读三链解码器 + 重算摘要断言 + A/B 核对(见 §4) |
| `keystore` | 分片落盘加密(设备 keychain + 口令),备份/恢复 |
| `transport` | libp2p 客户端:Noise/yamux/pnet/rendezvous/circuit-relay(见 contract/protocol.md §2) |
| `coord-client` | 调 coord 成员 API:拉取/审批/心跳/START/上报(contract/api.md B) |
| `rn-bridge` | RN 原生模块(Kotlin/Swift/TS)桥接 gomobile lib ↔ JS |
| `addr`(可选) | 公钥→ETH/TRON 地址纯派生 |

## 2. mobile-api 扁平接口(示意)

gomobile 约束 → 仅扁平类型,复杂类型 JSON/bytes 进出,异步用 callback 接口:

```
KeyGen(configJSON string, wire WireCallbacks, cb KeyGenCallback)
Sign(configJSON string, wire WireCallbacks, cb SignCallback) *SignSession
Reshare(configJSON string, wire WireCallbacks, cb ReshareCallback)
OnWireMessage(b []byte)                               // host→Go MPC 消息回灌
ExportShare(passphrase string) []byte / ImportShare(...)
FetchTransactions(reqJSON string) (string, error)    // 经 coord 获取交易信息(见 §2.1)
FetchXpub(reqJSON string) (string, error)            // owning-member-only xpub 拉取(B8)

interface WireCallbacks {                              // Go→host 出站桥(DM-3)
  OnWireMessage(b []byte)                              // host 负责按 tag 路由到 peer
}

interface SignCallback {
  OnDecoded(aFactsJSON string, bInfoJSON string, mismatchJSON string)  // 待人审
  OnResult(rsv []byte) / OnError(code string, msg string)
}
// Sign 返回 *SignSession,UI 通过 ss.Approve() / ss.Reject() 决策回传。
```

**DM-3 hard-cut(commit `9d2fe86`)**:`configJSON` 是必填的密封信封,字段集
`{groupId, sessionID, partyIndex, n, t (Sign/KeyGen) | oldT/newT (Reshare),
memberSet[], relay{peerID, addrs[]}, role, passphrase}`;旧版缺字段直接拒。
`WireCallbacks` 是 gomobile 桥必填第三参数(host 端实现 libp2p 出站),无桥即
无 MPC——SDK 不再自带 transport。详见 `distributed-mpc-impl.md §B DM-3`。

- 复杂 tss-lib 类型全封装 Go 侧;RN 仅见 string/[]byte/回调。
- 实际签名:`sdk/sdk.go`(对外门面)+ `internal/mobileapi/`(扁平实现)。

### 2.1 经 coord 获取交易信息(用户裁定 2026-05-19)

`FetchTransactions(reqJSON string) (string, error)` —— 设备主动经 **coord 成员
API**(api.md B,签名鉴权)查询交易信息,供 App 列出/详情展示,不进入 MPC。

- `reqJSON`:`{ coordBaseURL, groupId, memberId, since? , requestId? }`。给
  `requestId` 则查单条状态(api.md A3 `GET /v1/requests/{id}`);否则拉取该组
  待签列表(api.md B3 `GET /v1/groups/{groupId}/pending?since=`)。
- 返回:JSON `{ serverTime, items:[ { requestId, status, remainingTTL,
  envelope, aFacts(本地 tx-decode 重算的 A 区已校验事实), abMismatch } ] }`。
  A 区由设备侧 `tx-decode` 重算(==digest32 双重绑定,§4),**绝不**采信 coord
  下发的展示字段——「获取交易信息」仍受同一防盲签不变量约束。
- 实现**复用既有 `coord-client`**(`coordclient.Client` 的 B 侧签名请求 +
  `Pending`/状态查询)与设备 keystore 成员身份私钥;**纯增量**:不改 coord 服务
  端、不改 MPC/契约/加密。gomobile 扁平:仅 string + error。
- 安全:LOCKED 时 coord 端 `503 LOCKED` 原样透传为 error;无成员密钥/密钥未导入
  时拒绝;不缓存明文敏感数据。

## 3. 签名设备侧流程(WYSIWYS)

```
收 START(coord_client/推送) → 校验信封(proposerSig/metaHash/expiry,protocol.md §1)
→ tx-decode 解析 unsignedTx + 重算链摘要断言 ==digest32(失败→拒签)
→ 产出 A 区事实 + B 区 businessInfo + A/B 核对结果 → OnDecoded → UI 分区展示
→ 用户 Approve(进 MPC 前再校验 now<expiry)
→ transport 经 relay 与 signers 跑 /tss/mpc → 出 {R,S,V}
→ 出结果前再校验未过期 → coord_client 上报(api.md B7)
```
任一校验失败 → `OnError` 拒签,不进/中止 MPC。

## 4. tx-decode(内置三链,安全攸关)

- 覆盖:ETH/BSC(legacy + EIP-1559 + ERC20/合约调用)、TRON(原生 + TRC20 + Stake2.0 系统合约)。
- **核心不变量**:解析 unsignedTx → 按链规则重算摘要(EVM:Keccak256(RLP/typed);TRON:sha256(raw_data))→ 断言 `==digest32`。**解码 bug 退化为拒签而非误签**(双重绑定)。
- 产出 **A 区**:to/value/chain/合约/方法等已校验事实(资金安全唯一权威)。
- **A/B 声明式核对**:按 `businessInfo.displayHints` 比对金额/收款方等,不一致显著告警。
- **未识别调用不臆造**:展示原始 selector/calldata + 「谨慎审核」告警。
- **可插拔覆盖**:允许替换解码器,但覆盖实现须满足同一「重算摘要==digest32」断言。

## 5. 线程 / 错误 / 状态模型

- **线程**:PreParams(安全素数,10–30s)与 MPC 计算在后台线程 + 进度回调;**严禁 UI 线程**;**严禁后端预生成下发**(含 Paillier 私钥)。
- **错误**:统一 `{code,msg}`;安全类错误(信封无效/摘要不符/过期/未授权)一律**硬拒签**,不重试不降级。
- **状态**:每会话 `sessionId=requestId` 强隔离;并发会话独立;掉线可在 TTL 内由 coord 重新发起(architecture.md §4.2)。

## 6. keystore

- 分片经设备安全区(iOS Keychain/Secure Enclave、Android Keystore)+ 用户口令派生密钥加密落盘。
- 备份/恢复:加密导出(`ExportShare(passphrase)`);**丢失成员**走 resharing 重建(§7),不依赖明文备份。
- 绝不明文持久化分片;进程内最小驻留。

## 7. 丢失成员 resharing(P5)

- 设备/分片丢失 → 剩余 ≥ t 方经 `Reshare` 重建缺失分片或纳新成员;**主公钥不变,地址不变**。
- 窗口期冗余下降需提示尽快完成;coord 协调发起(信封类型=reshare,流程类比签名)。

## 8. 打包(路径 A)

- gomobile bind → Android `.aar`、iOS `.xcframework`;纯 Go 无 cgo(P0 已验编译风险低)。
- `rn-bridge` 自动链接;扁平 API 跨 JS 桥不丢类型(P0 RN 冒烟提前验证,见 P0-tasks.md T5)。

## 9. 验收(P1/P3/P4)

- P1:进程内多 party keygen/sign/reshare 端到端(参考 `tss-lib/ecdsa/*/local_party_test.go`)。
- P3:tx-decode 三链真实语料 + 模糊测试;摘要断言双重绑定;未识别降级正确。
- P4:.aar/.xcframework 经 RN 桥跑通 sign;PreParams 后台不卡 UI(P0 阈值)。
