# 使用指南(运维 + 集成)

> 顶级使用文档,面向(a)运维部署 `node` / 解锁 admin、(b)外部业务服务集成 A 面、
> (c)成员客户端集成 B 面 / SDK、(d)PC wallet-cli 操作员。关联
> `docs/ARCHITECTURE.md`、`docs/DELIVERY.md`。基线 HEAD `4028d54`。

## 1. 快速开始

### 1.1 构建二进制

```bash
git clone <repo> && cd mcp
gofmt -l . && go vet ./... && go build ./...
# 产物:cmd/server(node)、cmd/cli(member harness + wallet-cli)
```

依赖基线:`go 1.25`、`protobuf v1.36.6`、`libp2p v0.48.0`、`x/crypto ≥ v0.39.0`。
不需要 npm / Node 构建链(htmx + tailwind 全部 vendored 进 Go 二进制)。

### 1.2 启动 node(合并部署,默认)

```bash
cp server.example.yaml server.yaml   # 编辑 listen / token / db.path 等
./server --config ./server.yaml
```

或经环境变量覆盖任意字段(`MPC_` + 大写点分键,嵌套与键内 `_` 一律单 `_`):

```bash
MPC_COORD_HTTP_LISTEN=":8080" \
MPC_COORD_DB_PATH="/var/lib/mcp/coord.db" \
./server
```

或 CLI 参数(优先级最高):`--coord.http.listen=:8080`。

### 1.3 解锁 coord 库(生产首启)

默认 LOCKED:整库 sqlcipher 加密,启动即 fail-closed,任何 API 返 `503 LOCKED`
除 `/healthz`。解锁:

```bash
curl -X POST -u "admin:<bearer>" \
  -d '{"passphrase":"<operator passphrase>"}' \
  https://<admin-host>/api/unlock
```

口令仅经此交互入,**不入配置 / env / KMS / 日志**;内存驻留;`relock` 或 idle
超时即 zeroize。**生产丢失口令 = coord 库不可恢复**(分片在成员设备,资金不受影响,
但编排状态丢)。详见 `docs/design/server/database.md` §7。

dev/test 经 `coord.db.encryption.enable=false` + `ALLOW_INSECURE_DB=1` 双重显
式确认禁用加密;生产护栏拒绝该组合(无 `ALLOW_INSECURE_DB` 即 fail-closed)。

## 2. 部署拓扑

### 2.1 合并节点(默认)

```yaml
# server.yaml(简化)
relay:
  enable: true
  listen: ["/ip4/0.0.0.0/tcp/4001"]
coord:
  enable: true
  http:
    listen: ":8080"          # 外部业务 A 面 + 成员 B 面 同端口(经路由区分)
    admin_listen: ":8090"    # admin-api + admin-ui;**不对公网**
  external:
    api_key: env:MPC_EXT_API_KEY
  notify:                    # CFG-001 单一固定 webhook
    url: https://notify.example.com/mcp
    secret: env:MPC_NOTIFY_SECRET
```

### 2.2 角色拆分

```bash
# node-relay 多副本(无状态)
./server --relay.enable=true --coord.enable=false

# node-coord 单节点(SQLite + Litestream / 文件备份)
./server --relay.enable=false --coord.enable=true
```

### 2.3 admin-ui 访问

合并部署后 admin-ui 自动随 admin-api 进程对外:

- `GET https://<admin-host>:8090/login` → 提交 read/control token
- 主页 `/`:LOCKED 态可达(仅显锁定);UNLOCKED 后显示 Transactions / Audit / Relay 卡片。
- 受 `s.netGate`(IP allowlist)+ StrongAuth seam(mTLS/OIDC 可注入)保护。

详见 `docs/design/server/admin.md` §3-§5。

## 3. 外部业务服务集成(A 面)

A 面 = 外部业务服务 ↔ coord。**HTTPS + JSON**;鉴权 mTLS 或 `api_key`(配置
`coord.external.auth`)。

### 3.1 申请组地址(A1)

```http
GET /v1/groups/{groupId}/public
Authorization: Bearer <coord.external.api_key>

→ 200 { groupId, ecdsa_pubkey(b64), evm_address, tron_address, threshold_t, parties_n }
→ 404 groupId 未知
```

`evm_address` 同 ETH/BSC 共用(同 secp256k1+keccak256+EIP-55);`tron_address`
为 Base58Check(0x41 前缀)。

### 3.2 提交签名信封(A2)

```http
POST /v1/requests
Authorization: Bearer <api_key>
Content-Type: application/json

{
  "groupId": "...",
  "chain": "eip155:1",          // 不透明标签
  "unsignedTx": "<base64>",     // 链原始字节
  "digest32": "<base64>",       // 32 字节摘要(coord 不重算,设备会重算并断言==digest32)
  "proposer": "biz-svc-1",
  "expiry": "2026-05-21T12:00:00Z",
  "businessInfo": { ... },      // 带外说明,设备 A/B 核对依据
  "metaHash": "<base64 H(businessInfo)>",
  "proposerSig": "<base64 ECDSA(coord.external.proposer_pubkey, full envelope)>"
}

→ 202 { requestId, status:"PENDING" }
→ 400 校验失败(proposerSig 无效 / metaHash 不匹配 / expiry 过期 / groupId 未知)
```

`requestId` **全局唯一禁复用**;同 `requestId` 重复提交返原状态(幂等)。

### 3.3 查询状态(A3)

```http
GET /v1/requests/{requestId}
Authorization: Bearer <api_key>

→ 200 { requestId, status, fail_reason?, result?{ R(b64), S(b64), V } }
```

终态:`RETURNED`(含 RSV)/`EXPIRED` / `REJECTED` / `FAILED`(含 reason)。

### 3.4 结果回传(A4 webhook,固定地址)

`coord.external.result.url` 配置后,coord 终态时 POST:

```http
POST {coord.external.result.url}
X-MCP-Timestamp: <unix sec>
X-MCP-Signature: t=<unix sec>,v1=<hex HMAC-SHA256(secret, ts + "." + body)>
Content-Type: application/json

{ "requestId":"…", "status":"RETURNED", "RSV":"<base64>", "reason":null }
```

外部服务**必须**:
- 用同一 secret 重算 HMAC,**常时比较**;
- 拒绝 `|now − X-MCP-Timestamp| > 300s`(规范 ±300s skew);
- 窗口内去重 timestamp/已见签名(防重放)。

或 `Authorization: Bearer <api_key>`(无 body 绑定,弱模式)。详见
`docs/design/contract/api.md` A4 / WHA-001。

## 4. 成员客户端集成(B 面)

B 面 = 成员设备 ↔ coord。每请求由成员身份私钥签名(`memberId + 方法 +
关键参数 + ts + nonce`)。

### 4.1 心跳(B5,A2 连通性)

```http
POST /v1/members/self/heartbeat
X-Member-Id: m0
X-Member-Ts: <unix ms>
X-Member-Nonce: <base64 16-byte random>
X-Member-Sig: <base64 ECDSA(member.identity_priv, memberAuthDigest)>

{ "groupId":"…", "memberId":"m0", "relayPeerID":"12D3Koo…", "ts":..., "sig":"..." }
→ 204
```

### 4.2 拉取待签(B3)

```http
GET /v1/groups/{groupId}/pending?since=<RFC3339>
… signed headers …

→ 200 { items:[ { ...信封..., status, remainingTTL } ], serverTime }
```

### 4.3 审批 / 拒绝(B4)

```http
POST /v1/requests/{requestId}/decision
{ "memberId":"m0", "decision":"approved", "sig":"<base64>" }
→ 200 { status }
```

### 4.4 接收 START(B6)

主路径 **长轮询** `GET /v1/groups/{groupId}/dispatch?wait=…`(同一通道 fan-out
keygen-START / sign-START / reshare-START / attestation-ACK,DM-4 dispatchHub)。
旁路:coord 经 `coord.notify` webhook 向外部通知渠道投递(FCM/APNs 由外部翻译,coord 不持推送凭证)。

### 4.5 上报结果(B7)

```http
POST /v1/requests/{requestId}/result
{ "memberId":"m0", "RSV":"<base64 65 bytes>", "sig":"<base64>" }
→ 200(coord 用组主公钥验签后 → RETURNED 并 A4 回传)
```

### 4.6 拉取 xpub(B8,owning-member-only)

```http
GET /v1/groups/{groupId}/xpub
… signed headers …

→ 200 { ecdsaPubkeyHex, chaincodeHex }
```

成员从此可**离线**派生子地址(`internal/hd.DeriveChildAddress`)。

### 4.7 keygen / reshare / attestation(B9 / B10 / B11,DM-4)

详见 `docs/design/mcp/distributed-mpc-impl.md` §F + `docs/design/contract/api.md` B9-B11。

## 5. SDK 集成(mobile / PC)

### 5.1 扁平 API(`sdk.SDK`,gomobile 友好)

```go
import "github.com/zzci/mpc/sdk"

s, _ := sdk.NewSDK("/path/to/keystore-dir")

// keygen — DM-3 hard-cut configJSON
s.KeyGen(`{"groupId":"...","sessionID":"...","partyIndex":0,"n":3,"t":1,
            "memberSet":["...","..."],"relay":{"peerID":"...","addrs":["..."]},
            "role":"member","passphrase":"…"}`,
         myWireCallbacks, myKeyGenCallback)

// sign — 返 *SignSession,UI 经 ss.Approve()/Reject() 决策
ss := s.Sign(startJSON, myWireCallbacks, mySignCallback)
// 收到 OnDecoded 后人审 → ss.Approve() or ss.Reject() → OnResult / OnError

// reshare
s.Reshare(configJSON, myWireCallbacks, myReshareCallback)

// 离线派生 xpub 子地址
addrJSON, _ := s.DeriveAddress(xpubJSON, /* childIndex */ 0)

// host 收到 MPC 入站消息时回灌
s.OnWireMessage(rawBytes)
```

`WireCallbacks`:host 实现一个 `OnWireMessage([]byte)` —— Go 经此调用 host 出
站(libp2p 路由到 peer)。host 端的入站则调 `sdk.OnWireMessage` 回灌。

参考实现:`internal/cli/host_transport.go`(PC 端,基于 internal/transport
libp2p);`mobile/`(RN bridge,gomobile 桥接)。

### 5.2 RN bridge(P4 范围)

`internal/mobileapi/` 扁平 API + gomobile bind 产 .aar / .xcframework;
RN 原生模块(Kotlin/Swift/TS)桥接 JS。详见 `docs/design/mcp/sdk.md` §8 + P0 报告。

## 6. PC wallet-cli(运维 + 调试)

### 6.1 交互式 shell

```bash
export MPC_WALLET_PASSPHRASE="<keystore passphrase>"
./cli --keystore /path/to/keystore

wallet> keygen 2 3                # 启 2-of-3 keygen
wallet> import backup-file.bin    # 恢复备份
wallet> export m0 out-backup.bin  # 导出某分片
wallet> sign start-coord.json     # 经 coord START 文件签名(WYSIWYS approve/reject)
wallet> fetch req.json            # 查 coord 交易信息
wallet> xpub req.json             # 拉本组 xpub(B8)
wallet> address 0 xpub.json       # 离线派生 m/0 地址
wallet> reshare 1 1 3             # 重分片
wallet> quit
```

### 6.2 HTTP 服务(`cli serve`)

```bash
# 默认 loopback;非 loopback 必须设 token,否则 fail-closed
export MPC_WALLET_PASSPHRASE="..."
export MPC_WALLET_HTTP_TOKEN="<bearer>"   # 非 loopback 必需
./cli serve --listen 127.0.0.1:8787 --keystore /path/to/keystore
```

JSON API(等价 shell 命令):
- `GET /api/v1/health` / `GET /api/v1/version`
- `POST /api/v1/keygen` `{Threshold, Parties}`
- `POST /api/v1/reshare` `{OldThreshold, NewThreshold, NewParties}`
- `POST /api/v1/import` `{blob: base64}` / `POST /api/v1/export` `{moniker}`
- `POST /api/v1/sign` `{start: <coord START JSON>}` → `{id, aFacts, bInfo, mismatch}`
- `POST /api/api/v1/sign/{id}/approve` 或 `/reject` → `{rsv}` 或 `{error}`
- `POST /api/v1/fetch` (body = req JSON) → coord 交易信息
- `POST /api/v1/wire` `{msg: base64}` → 手动灌 MPC 消息(测试通道)

### 6.3 htmx 检查面板(根路径 `/*`,API 在 `/api/v1/*`)

浏览器访问 `http://127.0.0.1:8787/ui`(或带 token):
- `/ui` 概览(version、auth、pending count)
- `/sign` 待签列表 → `/sign/{id}` WYSIWYS 详情 + Approve/Reject
- `/import` 备份恢复表单(passphrase 经 env;UI 禁经 HTTP 输入)
- `/fetch` / `/xpub` / `/address` 只读查询

**WYSIWYS 不变量**:UI 与 JSON 共用同一 pendingSign + signSession;UI 是 JSON
的可视化封装,**不是另一条审批渠道**。详见 `docs/design/mcp/walletcli-ui.md`。

## 7. 测试与门禁

### 7.1 Go 全量 race recgate

```bash
go test -race -timeout=1200s ./...
# 或使用 rtk(可输出友好):
rtk test go test -race -timeout=1200s ./...
```
基线:**19 packages all ok**。

### 7.2 E2E-001 完整环(`tests/e2e/test/e2e/full-ring.test.ts`)

```bash
cd tests/e2e && bun run test:e2e
# 启 node + mock-extsvc + 3 cli member 子进程 → keygen+sign+reshare → A4 验签
```

### 7.3 E2E-002 Docker 隔离拓扑

```bash
cd tests/e2e && bun run test:e2e-docker
# 容器化各角色,真 Docker 网络(无 localhost 直连)
```

### 7.4 §G 真分布式 MPC 实证

```bash
cd tests/e2e && bun run test:e2e-dmpc
# 序列化 4 文件:
# - attestation-quorum(3 pass)
# - keygen-3of3(1 pass)
# - sign-2of3(2 pass:positive + lone signer 拒)
# - reshare(2 pass:rotate invariant + 3-to-4 EXPECTED_MEMBER_MISMATCH)
# 总:8 pass / 4 skip / 0 fail / ~5min
```

### 7.5 静态检查

```bash
gofmt -l . && go vet ./... && golangci-lint run --timeout=180s
```

## 8. 备份与恢复

### 8.1 设备分片备份(操作员侧)

```bash
# Export(单分片,口令封装)
wallet> export m0 backup-m0.bin

# Import(恢复到另一设备 / 另一进程)
MPC_WALLET_PASSPHRASE="..." wallet> import backup-m0.bin
# 或经 UI:/import 上传 backup-m0.bin
```

ExportShare 用口令派生密钥(Argon2id 强度,见 keystore.go)对称封装,**绝不**明
文持有分片。备份介质丢失 → 走 reshare 重建(`docs/design/mcp/sdk.md` §7)。

### 8.2 coord 库备份

- **文件备份**:LOCKED 状态(整库密文)直接 `cp` 即可,落盘文件无明文。
- **Litestream**:配置流式复制至只读副本(`docs/design/server/database.md` §1)。
- **关键**:口令离线安全保管;无口令则数据库不可恢复(资金不受影响,
  分片在成员设备)。

## 9. 监控

- `GET /healthz` — 单字段 `{ "status":"ok" }`,LOCKED 下唯一可达端点。
- `GET /metrics`(`metrics.listen`)— Prometheus 格式;无 payload 数据。
- `admin-ui`:Audit 页 + Relay 页;`request_events` 长期保留(`docs/design/server/database.md` §6)。

## 10. 常见错误与处置

| 错误码 | 含义 | 处置 |
|---|---|---|
| `503 LOCKED` | coord 库未解锁 | admin POST `/api/unlock` 输入口令 |
| `400 INVALID_ENVELOPE` | proposerSig 无效 / metaHash 不匹配 / expiry 过期 | 重新构造信封;检查时钟同步 |
| `404 NOT_FOUND` | groupId / requestId 未知 | 先 A1 申请地址 / 检查 requestId |
| `409 STATE_CONFLICT` | 状态机不允许的迁移(R7 violation / 重复 commit) | 不应重试;检查业务逻辑 |
| `409 EXPECTED_MEMBER_MISMATCH` | 身份不在 `coord.external.expected_members` 集 | 在配置中添加该身份 hex pubkey 后重启 coord |
| `412 PRECONDITION_FAILED`(cli) | `$MPC_WALLET_PASSPHRASE` 未设 | 设置环境变量后重试 |
| `401 UNAUTHORIZED`(A 面) | `coord.external.api_key` 不匹配 | 检查 token;不在 process tree / shell history 暴露 |
| `401 UNAUTHORIZED`(B 面) | 成员签名头无效 / nonce 重放 / ts 偏差 > skew | 检查 identity_priv / 时钟 / 重发 |

详尽错误码表:`docs/design/contract/api.md` C 节。

## 11. 进一步阅读

- 整合架构:`docs/ARCHITECTURE.md`
- 交付状态:`docs/DELIVERY.md`
- 设计深度(实现者):`docs/design/`(17 篇,各组件 source-of-truth)
- 安全审查:`docs/security-review.md`
- P0 移动打包:`docs/design/P0-report.md`
