# 设备配对设计(server / pairing)

> 新设备首次接入的引导通道:运维在 admin-ui 生成一次性配对 token + 二维码,
> 设备扫码即可学习 coord/relay 引导信息并把自身身份公钥提交给 coord。
>
> 关联:`server/admin.md`(创建/列出/撤销 + UI 页面)、`contract/api.md`
> (公开端点 A5/A6)、`security.md`(信任边界 + 公开面纪律)。性质:开发文档。

## 1. 动机

旧引导路径要求运维**离线**把 coord URL / relay 信息 / 群成员身份等 hand-pass
给新设备,易错且不可审计。新机制让运维一键生成"机器可读"二维码,设备扫码即
可自我配置 + 把自己的公钥安全地告诉 coord。

## 2. 不变量

1. **token-as-auth**:公开 coord 端点(`/v1/pairing/{token}/{config,enroll}`)
   不验 API key,**仅**校验 token 存在 + 未用 + 未过期。token 长度 32 字节
   (64 hex)足以抗暴力枚举(2^256 状态空间)。
2. **单次使用**:配对成功立刻 mark `UsedAt` + `UsedBy`;后续重放 →
   `409 STATE_CONFLICT (consumed)`。已用 token **保留在内存**作审计,直到
   `GC()` 显式回收或 `Delete`(后者写 audit)。
3. **TTL 强制**:默认 10min,上限 24h(`maxPairingTTL` const 守护);过期
   token Consume → `409 (expired)`。GC 周期清理未使用且已过期。
4. **关键密钥永不进 QR / 公开响应**:relay PSK、admin 凭据、设备私钥、coord
   API key 等敏感凭据**均不**出现在 QR 二维码内容或 `/v1/pairing/*` 响应中。
   QR 内容仅是一个**公开 URL**(`/v1/pairing/{token}/config`),设备 GET 该
   URL 后看到的是 `{coordBaseUrl, relayPeerID, relayAddrs, groupId, label,
   expiresAtMs}` —— 无私钥、无 PSK、无管理员凭据。
5. **不变更现有信任模型**:配对**不**给设备开通组成员资格;它只是引导设备
   学到 coord/relay 引导信息 + 把身份公钥告知 coord。运维仍按既有 B 面流程
   (`POST /v1/groups/{groupId}/membership`)把设备加入具体的 group_members。
6. **公开但限流**:`/v1/pairing/*` 受 `externalRL` 每-IP 限流挡 brute-force;
   不依赖 IP allowlist(配对是 onboarding 场景,设备 IP 不可预知)。
7. **可审计**:所有 create / delete / consume 写入不可篡改的 `admin_audit`。

## 3. 数据流

```
┌── admin /pairing ───┐                              ┌── new device ───┐
│ POST /pairing       │                              │ scan QR (PNG)   │
│ (groupId?,label,    │                              │ → URL          │
│  ttlSeconds)        │                              │ → GET config   │
│ → token (64 hex)    │   (token 进 PairingStore)    │   (coord URL+   │
│ → /api/pairing/.../  │ ◄────────────────────────── │    relay info)  │
│   qr.png            │                              │ → generate id   │
│ (PNG → <img src>)   │                              │ → POST enroll   │
└─────────────────────┘                              │   (identityPub) │
                                                     │ → write pair.json│
                                                     └─────────────────┘
                              ▲              ▼
                    admin_audit (create/delete)  +  admin_audit (consume-via-coord-handler not yet — TODO future)
```

## 4. 组件

| 文件 | 职责 |
|---|---|
| `internal/server/pairing.go` | 共享 `PairingStore`(in-memory + 并发安全;CRUD + Consume + GC) |
| `internal/server/coord/enroll.go` | 公开 coord 端点:`GET/POST /v1/pairing/{token}/{config,enroll}`;`PairingPublicInfo` 配置注入 |
| `internal/server/admin/pairing.go` | admin JSON API(`/api/pairing` CRUD + qr.png)+ htmx UI 页(`/pairing`) |
| `internal/server/admin/uiassets/templates/pairing.tmpl` | UI 模板:生成表单 + 列表 + QR `<img>` |
| `internal/walletcli/pair.go` | wallet-cli `pair <url>` 命令:fetch config + POST identity + 落地 `pair.json` |

## 5. 端点表

### 5.1 公开 coord(`/v1/pairing/{token}/*`)

| 方法 | 路径 | 鉴权 | 行为 |
|---|---|---|---|
| GET | `/v1/pairing/{token}/config` | token-as-auth | 返 `{token, groupId?, label?, expiresAtMs, coordBaseUrl, relayPeerID?, relayAddrs?}`,**不**消耗 token |
| POST | `/v1/pairing/{token}/enroll` | token-as-auth | body `{identityPubkey: hex, label?}`,Consume + return 同 config + ticket info |

错误码:`404 NOT_FOUND` (unknown token) / `409 STATE_CONFLICT` (expired or consumed) / `400 INVALID_ENVELOPE` (bad identity hex)。

### 5.2 admin(`/api/pairing/*`,scopeControl)

| 方法 | 路径 | 行为 |
|---|---|---|
| POST | `/api/pairing` | body `{groupId?, label?, ttlSeconds?}`,创建 token;201 + JSON view |
| GET | `/api/pairing` | 200 `{items:[…]}` 全部 ticket(含 used) |
| DELETE | `/api/pairing/{token}` | 204;Delete + audit |
| GET | `/api/pairing/{token}/qr.png` | image/png QR(256×256,Q 级纠错) |

### 5.3 admin UI(`/pairing`,session/bearer auth)

`GET /pairing` 表单生成 + 列表渲染 + 嵌入 `<img src=/api/pairing/{token}/qr.png>`。
`POST /pairing` 表单 create;`POST /pairing/{token}/delete` 表单 delete。

## 6. wallet-cli `pair` 流程

```
wallet> pair https://coord.example.com/v1/pairing/<token>/config
  1. GET <url> → pairConfig{token, coordBaseUrl, relay...}
  2. crypto/rand → secp256k1 priv → btcec.PubKey() → compressed pub hex
  3. POST <coordBaseUrl>/v1/pairing/<token>/enroll {identityPubkey: pubHex}
  4. 收到 200 + pairConfig(同上,token 现已 used)
  5. write <keystoreDir>/pair.json (priv + pub + coord URL + relay 引导)
{"paired":true, "groupId":"...", "coordBaseUrl":"...", "identityPubHex":"02..."}
```

`pair.json` 文件 mode 0600 + atomic rename。再次 `pair` 会覆盖。

## 7. 装配示例(`cmd/server`)

```go
pair := server.NewPairingStore(nil) // 共享 store
coordInst, _ := coord.New(cCfg, store, presence,
    coord.WithPairingStore(pair),
    coord.WithPairingInfo(coord.PairingPublicInfo{
        CoordBaseURL: cfg.PublicCoordURL,  // 必填:外部可见 URL
        RelayPeerID:  cfg.RelayPeerID,
        RelayAddrs:   cfg.RelayAddrs,
    }),
)
adminSrv, _ := admin.New(aCfg, store,
    admin.WithPairing(pair, cfg.PublicCoordURL),
)
```

未注入 `WithPairingStore` / `WithPairing` → 配对功能整体禁用,所有相关
路由 404,UI 页面 404,nav 隐藏。

## 8. 安全审查清单

- ✅ token 长度 ≥ 32 字节高熵随机(`crypto/rand`)。
- ✅ 公开端点受 `externalRL` 每-IP 限流。
- ✅ token 单次使用,replay 拒(`ErrPairingUsed` → 409)。
- ✅ TTL 上限 24h;过期拒(`ErrPairingExpired` → 409)。
- ✅ 关键密钥不进 QR / 公开响应(relay PSK / 设备私钥 / admin 凭据)。
- ✅ identityPubkey 长度 + hex 严校验;非 33B/65B → 400。
- ✅ 配对**不**直接改 group_members;membership 仍走既有 B 面流程,信任锚不变。
- ✅ 全部状态变更(create/delete/consume)进 `admin_audit`(append-only)。
- ✅ HTTP 客户端(wallet-cli)用 `context.WithTimeout` + `NewRequestWithContext`(noctx-clean)。
- ⚠ `pair.json` 落本地 keystore 目录,mode 0600;**包含**设备身份私钥
  (用于后续 B 面签名鉴权)。读取者需有 keystore 同等访问权,与现有 keystore
  纪律一致。

## 9. 验收

- 单元测试覆盖 PairingStore(create / get / consume / replay / expire /
  GC / delete)、coord enroll handler、admin CRUD + QR PNG render、UI 页
  渲染、wallet-cli `pair` happy path + 错误路径。
- Race recgate 20/20 GREEN 通过(`go test -race -timeout=1200s ./...`)。
- golangci-lint v2 0 issues。
