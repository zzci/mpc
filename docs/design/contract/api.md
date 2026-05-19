# 接口契约:外部服务 ↔ coord、成员 SDK ↔ coord

> coord 定义、对外暴露;外部业务服务消费提交/回传接口,mcp SDK 消费成员接口。
> 关联 `server/server.md`(C7)、`server/database.md`、`contract/protocol.md`。性质:开发文档,不写代码。
> 风格:HTTPS + JSON,REST 语义;字节字段 base64;时间 RFC3339 UTC。版本前缀 `/v1`。

## A. 外部业务服务 ↔ coord

### A1 鉴权
- `mtls` 或 `api_key`(见 server.md 配置 `coord.external.auth`)。
- 业务层另需信封 `proposerSig`(覆盖全字段含 `metaHash`),coord 与各设备均可验。

### A1 申请组地址/公钥(外部服务)
`GET /v1/groups/{groupId}/public` → `{ groupId, ecdsa_pubkey(b64), evm_address, tron_address, threshold_t, parties_n }`
- 外部服务据此「申请地址」:返回该组 G-001 持久化的派生链地址(evm=ETH/BSC 共用 EIP-55、tron=Base58Check)+ 主公钥。
- 仅公开数据,经 `coord.external.auth`(mtls/api_key)鉴权;**不泄成员/分片/groupPubkey/epoch/degraded 等私有或特权组态**。groupId 不存在 → `404`。
- **路由分离(L1 裁定 2026-05-18,解 A1↔§5.1 同路由冲突)**:本 A1 为**新增独立路径** `…/public`(外部 mtls/api_key 鉴权、精简、零成员泄露)。既有 `GET /v1/groups/{groupId}`(成员 B1 memberGate 鉴权 + 全量组视图含 groupPubkey/epoch/activeMembers/degraded)归 `docs/spec/group-provisioning.md §5.1` / X-001 / A-001,**保持不变、零回归**。两路由 method+path 不同,net/http 无冲突;鉴权与响应面物理隔离(防鉴权分支泄露,信任最小化)。

### A2 提交签名请求
`POST /v1/requests`
```
Req  { groupId, chain, unsignedTx(b64), digest32(b64), proposer,
       expiry(RFC3339), businessInfo?{...}, metaHash(b64), proposerSig(b64) }
Resp 202 { requestId, status:"PENDING" }
```
- **幂等**:同 `requestId` 重复提交返回原状态,不新建(`requestId` 全局唯一禁复用)。
- 校验失败 → `400`(`proposerSig` 无效 / `metaHash≠H(businessInfo)` / `expiry` 已过 / groupId 未知)。

### A3 查询状态
`GET /v1/requests/{requestId}` → `{ requestId, status, fail_reason?, result?{R,S,V}(b64) }`

### A4 结果回传(coord → 外部服务,固定地址)
结果回传**固定地址**(用户裁定 2026-05-19,删 longpoll)。
- `POST {coord.external.result.url}` body `{ requestId, status, RSV?(b64), reason? }`;失败按退避重试至确认或终态超时。
- **回调鉴权(防伪造,用户裁定 2026-05-19,必备;两种模式,`secret`/`api_key` 至少配一,皆配时签名优先)**:
  - **签名模式(首选)**:配 `coord.external.result.secret` 时,coord 用该 secret 对 `timestamp + "." + 原始 body` 做 HMAC-SHA256,置头:
    - `X-MCP-Timestamp: <unix 秒>`
    - `X-MCP-Signature: t=<unix 秒>,v1=<hex(HMAC-SHA256)>`
    外部服务**必须**用同一 secret 重算、常时比较、并按时钟 skew 容差拒过期/重放;验签不过即丢弃(防攻击者向回调端点伪造 `{requestId,status,RSV}`)。
  - **token 模式(备选)**:未配 `secret` 但配 `coord.external.result.api_key` 时,coord 置头 `Authorization: Bearer <api_key>`;外部服务**必须**常时比较该 token,不匹配即丢弃。(兼容只支持 Bearer 的接收端;无 body 绑定、不抗重放,故弱于签名。)
  通知地址(server.md `coord.notify`,扁平 `{url, secret, api_key}`)用 `coord.notify.secret`/`coord.notify.api_key` 同法两模式鉴权。出站凭据与入站 `coord.external.api_key` 物理隔离、互不复用。
- coord **回传前用组公钥验** `ECDSA(pub, digest32, R,S)`,无效 → `FAILED` 不回传伪结果。
- 终态:`RETURNED`(含 RSV)/`EXPIRED`/`REJECTED`/`FAILED`(含 reason)。

## B. 成员 SDK ↔ coord

### B1 鉴权
- 每请求由成员身份私钥签名(`memberId + 方法 + 关键参数 + ts + nonce`);coord 用 `group_members.identity_pubkey` 验签;按 `groupId` 隔离与授权。

### B2 注册推送 token
`PUT /v1/members/self/push` `{ groupId, memberId, platform:"fcm|apns", token, sig }` → `204`

### B3 拉取待签(上线拉取,A4)
`GET /v1/groups/{groupId}/pending?since=…`(签名于 header)
```
Resp { items:[ { ...信封字段..., status, remainingTTL(秒) } ], serverTime }
```
- 仅返回未过期、本组、对该成员可见项;每项带**剩余 TTL**。

### B4 审批 / 拒绝
`POST /v1/requests/{requestId}/decision` `{ memberId, decision:"approved|rejected", sig }` → `200 {status}`
- 仅 `PENDING` 且未过期可受理;拒绝致不可达 t → `REJECTED`。

### B5 心跳(连通性,A2)
`POST /v1/members/self/heartbeat` `{ groupId, memberId, relayPeerID, ts, sig }` → `204`
- coord 验签后写在线集(内存 SQLite,`expires_at` + 周期清理);**不依赖 relay 上报**。

### B6 接收 START
- 首选**推送**(FCM/APNs)唤起;补充**长轮询** `GET /v1/groups/{groupId}/dispatch?wait=…`
- START 载荷:`{ requestId, 完整信封, signers[], deadline }`;设备据 `contract/protocol.md` 进入 MPC 前流程(tx-decode + 人审 + 未过期校验)。

### B7 上报签名结果
`POST /v1/requests/{requestId}/result` `{ memberId, RSV(b64), sig }` → `200`
- 由 signers 中**指定一方**上报;coord 用组公钥验签,有效 → `SIGNED→RETURNED` 并回传外部服务,无效 → `FAILED`。
- 多方重复上报幂等取首个有效。

## C. 错误码

| HTTP | code | 含义 |
|---|---|---|
| 400 | INVALID_ENVELOPE | 签名/metaHash/字段校验失败 |
| 401 | UNAUTHENTICATED | 鉴权/签名无效 |
| 403 | FORBIDDEN | 跨组/无权 |
| 404 | NOT_FOUND | requestId/group 不存在 |
| 409 | STATE_CONFLICT | 状态不允许该操作(已终态/已派发) |
| 410 | EXPIRED | 请求已过期 |
| 429 | RATE_LIMITED | 限流 |
| 503 | LOCKED | coord 库锁定,服务不可用(见下) |
| 5xx | INTERNAL | 服务端错误(可重试) |

错误体:`{ error:{ code, message, requestId? } }`(message 不泄敏感信息)。

**锁定态(LOCKED)**:coord 持久库默认整库加密 + 锁定(server/database.md §7、server.md C9b)。未解锁时:
- A/B **所有**数据端点(提交/查询/拉取/审批/心跳/上报/回传)一律 `503 {code:LOCKED}`,**不接受任何交易、不返回任何信息**(fail-closed,防数据泄露)。
- 仅解锁端点(管理员侧,见 server/admin.md)与最小健康检查可用;解锁端点**不在**本契约(非外部服务/成员接口)。
- 客户端对 `503 LOCKED` 应退避重试,不视为请求失败终态。

## D. 通用约定

- 所有写操作幂等键:`requestId`(A 侧)/`(requestId, memberId)`(B 侧决策)。
- 重放防护:成员请求 `ts+nonce`,coord 拒绝过期/重复 nonce。
- 分页:`pending` 用 `since` 游标。
- 版本:不兼容变更升 `/v2`;`contract/protocol.md` 定义信封/消息版本协商。

## E. 验收(P3)

- A2/A4 幂等与回传前组公钥验签;伪 RSV → FAILED 不外泄。
- B 全程成员签名校验;过期项不出现在 B3,过期后 B4/B7 返回 410。
- 心跳驱动在线集,法定人数发起端到端联通(对应 architecture.md §4.2)。
