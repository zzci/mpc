# 线上协议契约(mcp ↔ server 双方共守)

> 信封、libp2p 协议、Noise/rendezvous/circuit-relay、心跳、START、能力令牌、版本协商。
> mcp 与 server 必须严格一致以防漂移。关联 `contract/api.md`、`server/server.md`、`mcp/sdk.md`。性质:开发文档,不写代码。
> 编码:protobuf(线上消息)/JSON(API,见 api.md);所有签名覆盖范围显式标注。
> **例外(用户 2026-05-18 裁定)**:`MpcMessage`(§3)线上编码用 **JSON**(4 字节长度前缀 + `contract.MpcMessage` JSON)。依据:该消息 mcp↔mcp 端到端,relay 仅转发密文不解析、coord 不参与 MPC,从不跨 mcp↔server 边界且两端共用同一 Go 类型,protobuf 所防的跨实现线格式漂移在此不成立。其余线上消息仍 protobuf。

## 1. 签名请求信封(权威定义)

```
SigningRequest {
  version     : uint   // 信封版本,见 §7
  requestId   : uuid   // 全局唯一,禁复用
  groupId     : string
  chain       : string // 不透明标签(coord/relay 不解释)
  unsignedTx  : bytes  // 不透明;设备 tx-decode 解析
  digest32    : bytes(32)
  proposer    : string
  createdAt   : int64  // unix ms
  expiry      : int64  // 绝对过期,unix ms
  businessInfo: bytes? // 结构化(见 mcp/sdk.md tx-decode A/B),可选
  metaHash    : bytes  // H(businessInfo);businessInfo 缺省时 H("")
  proposerSig : bytes  // 覆盖以上全部字段(含 version/metaHash)
}
```
- 设备**进 MPC 前**必须:验 `proposerSig`、`metaHash==H(businessInfo)`、`now<expiry`、`tx-decode` 重算链摘要 `==digest32`(任一不过即拒签)。
- coord 仅校验 `proposerSig`/`metaHash`/`expiry`/groupId,不解 `unsignedTx`。

## 2. libp2p 协议栈(数据平面)

| 层 | 选择 |
|---|---|
| 传输 | TCP / QUIC(libp2p 默认协商) |
| 安全 | **Noise**(端到端,对端身份=peerID=公钥) |
| 多路复用 | yamux |
| 私网 | **pnet PSK**(无 key 无法说协议;按部署域分发) |
| 发现 | **rendezvous**,命名空间 `base32(HMAC(groupSecret,"tss-group"))` |
| 中转 | **circuit-relay v2**(直连不通时,relay 仅转加密流) |
| 广播 | **GossipSub**(tss broadcast 消息);定向消息走 P2P stream |

协议 ID(示例,版本化):
- `/tss/mpc/1.0.0` —— MPC 消息流(keygen/sign/reshare)。
- `/tss/heartbeat/1.0.0` —— 不用于 relay;心跳走 coord API(见 api.md B5)。
- circuit-relay/rendezvous 用 libp2p 标准协议 ID。

## 3. MPC 消息封装

- 源自 tss-lib `WireBytes()`,封装:
```
MpcMessage { version, sessionId(=requestId 或 keygen/reshare 会话ID),
             from(partyID), to[](空=broadcast), isBroadcast,
             round, payload(tss WireBytes), senderAuth }
```
- `sessionId` 强隔离:跨会话消息一律丢弃(防重放/串话)。
- `senderAuth`:发送方对 `(sessionId,round,payload)` 的成员身份签名 —— **tss-lib 层之上的额外认证**,即便 Noise 已认证 peer,也绑定到成员身份与会话。
- 定向消息经 P2P stream 发 `to`;broadcast 经 GossipSub topic = `sessionId`。

## 4. 心跳(连通性,A2)

- 走 **coord API**(`POST /v1/members/self/heartbeat`,见 api.md B5),非 libp2p。
- 载荷 `{groupId, memberId, relayPeerID, ts, sig}`;`sig`=成员身份私钥签名;coord 验签写在线集(TTL)。
- relay **不参与、不上报**,保持与 coord 解耦。

## 5. START(coord → 设备)

```
StartSigning { requestId, envelope(完整 SigningRequest), signers[](memberId),
               selfRole(此设备是否 signer), relayHints[](multiaddr), deadline }
```
- 经推送唤起 + 长轮询补偿(api.md B6)。
- 设备收到后:校验 envelope(§1)→ `tx-decode` + A/B + 人审(WYSIWYS)→ 经 §2 栈与其余 signers 跑 `/tss/mpc`。
- coord 不在 signers 内,不收发 `/tss/mpc`。

## 6. 能力令牌(relay 访问控制,威胁模型 A)

```
CapToken { groupId, memberId, scope(relay-reserve|rendezvous-register),
           notBefore, notAfter, nonce, groupSig }
```
- 由**钱包组组密钥**签发(自主式信任锚);短 TTL、按组隔离、可撤销(短 TTL 自然失效)。
- relay 经 `ConnectionGater` 在 circuit-relay 预约 / rendezvous 注册处校验 `groupSig`(信任组公钥集,见 server.md R4)。
- 不含成员匿名化(威胁模型 A 不要求对 relay 隐藏分组)。

## 7. 版本与兼容

- `version` 字段在 `SigningRequest` / `MpcMessage` / 协议 ID 三处并行。
- 协商:libp2p 多协议协商选最高共有 `/tss/mpc/x.y.z`;信封 `version` 不识别即拒签(不降级猜测)。
- 不兼容变更升主版本;API 侧对应升 `/v2`(见 api.md D)。

## 8. 验收(P2/P3)

- relay 抓包:仅密文,无法还原 `MpcMessage.payload` 或伪造 `from`(Noise + senderAuth)。
- 跨 `sessionId` 注入消息被丢弃。
- 无有效 `CapToken`/PSK 无法预约 relay 或注册 rendezvous。
- 版本不匹配时拒签而非误解析。
