# 多 group(一个设备多个钱包)

> 用户裁定 2026-05-18:多地址 = 多 group(**不**使用 BIP44/HD)。一个设备可
> 同时是多个钱包组的成员。本文记录 2026-05-21 完整 wired 后的实施。性质:
> 开发文档。关联:`mcp/sdk.md`、`server/admin.md`、`mcp/walletcli-ui.md`、
> `server/pairing.md`、`server/database.md`。

## 1. 模型

| 资源 | 多 group | 备注 |
|---|---|---|
| keystore `*.ks` 文件 | ✅ 每 moniker 一份 | `internal/keystore.Store` 原生支持 |
| `mobileapi.SDK.shares` | ✅ moniker → Share union map | 不再被 setOwnShare 清零 |
| `mobileapi.SDK.groups` | ✅ groupId → groupMeta map | 新增,替换原 singular `group *groupMeta` |
| `pairings.json` | ✅ 列表,append/replace by groupId | 替换原 `pair.json`(legacy 自动迁移读入) |
| `coord.group_members` | ✅ 同 identity_pubkey 可入多 group | PK=(group_id, member_id),identity_pubkey 无 UNIQUE 约束 |
| `coord.signing_requests` | ✅ 按 group_id 路由 | dispatchHub per-(groupId,type) channel |

## 2. API 契约

### 2.1 mobileapi 内部接口(`internal/mobileapi/sdk.go`)

```go
// 装入一个 group 的 share + meta;不清除其他 group。
func (s *SDK) setOwnShare(groupID string, share mpc.Share,
    threshold, parties, partyIndex int, pubHex string)

// 按 groupID 路由查找 share(Sign/Reshare 的取路径)。
func (s *SDK) snapshotShareForGroup(groupID string) (
    share mpc.Share, threshold, parties, partyIndex int, ok bool)

// 只持 1 个 group 时返;>=2 group 时 ok=false(caller 必须显式 groupID)。
func (s *SDK) snapshotOwnShare() (...)
```

### 2.2 SDK 公开接口(`sdk/sdk.go`)

```go
// 返 JSON {"items":[{groupId,threshold,parties,partyIndex,ecdsaPubHex,moniker},…]}
// 仅元数据,share material 不在响应内。flat 类型保持(string out,error out)。
func (s *SDK) ListGroupsJSON() (string, error)
```

### 2.3 路由不变量

`KeyGen / Sign / Reshare` 的 `configJSON` 必含 `groupId`(DM-3 hard-cut 已强制);
SDK 据此路由到正确的 share 条目。**调用方不需要"切换激活 group"调用**。

## 3. 持久化

### 3.1 walletcli `pairings.json`

```json
[
  {
    "coordBaseUrl": "https://coord.example.com",
    "groupId": "g-1",
    "label": "Alice's iPhone",
    "relayPeerID": "12D3Koo...",
    "relayAddrs": ["/ip4/.../tcp/4001"],
    "identityPubHex": "02...",
    "identityPrivHex": "...",
    "pairedAtMs": 1716291600000
  },
  { "...另一 group..." }
]
```

**写入语义**:`persistPair(rec)` 按 `groupID` 匹配替换旧记录;无匹配则 append。
原子重写(写 tmp + rename),mode 0600。

**legacy 迁移**:存在 `pair.json`(旧单记录格式)但无 `pairings.json` 时,
`loadPairings()` 读 `pair.json` 包装为单元素 list 返回。下次 `persistPair` 写
`pairings.json` 时覆盖 — 但 `pair.json` 不被删除(forward-compatible:旧二进制
仍可读到这一条)。

### 3.2 keystore 多 share

`internal/keystore.Store` 原生按 moniker 索引文件;`Store.Load(moniker, pass)`
按 moniker 解封。每 group keygen / reshare 产出一份 share,以 moniker 命名
(由 mpc 层选择,通常带 groupID 前缀)落盘。

## 4. UI

### 4.1 wallet-cli `wallet groups`(shell)

```
wallet> groups
{
  "items": [
    {
      "groupId": "g-1",
      "source": "sdk+pair",
      "threshold": 2, "parties": 3, "partyIndex": 0,
      "ecdsaPubHex": "02...",
      "moniker": "g-1-share",
      "coordBaseUrl": "https://...",
      "label": "Alice's iPhone",
      "identityPubHex": "02..."
    },
    { "groupId": "g-2", "source": "pair" },
    { "groupId": "g-3", "source": "sdk", "threshold": 1, … }
  ]
}
```

`source` 三态:
- `sdk` — 当前进程通过 keygen/reshare 装入,但 `pairings.json` 无记录
  (例如 CLI member harness 直接跑 keygen,无 pair 步骤)
- `pair` — `pairings.json` 有,但 SDK 内存还未装入(pair 后,keygen 前)
- `sdk+pair` — 两侧都有,正常稳态

### 4.2 wallet-cli `/groups` UI 页

`GET /groups` 渲染同 union 视图为 HTML 表;nav 链接在 partials.tmpl 的 nav 中。
列:Group ID、Source 徽章、t/n/i、master pubkey 短、coord URL、label、
identity pubkey 短。

### 4.3 admin `/devices` 跨组视图

`GET /devices?id=<hex>` 输入框 + 表单;输入 33B/65B secp256k1 hex,后端
查 `group_members.identity_pubkey = ?` 返该 identity 所在的全部 (groupId,
memberId, status)。同 identity 在多个 group 内会列出多行。

JSON API:`GET /api/devices/{identityHex}/groups` → `{identityPubkeyHex, items, count}`。

## 5. 安全 / 信任不变量(未变)

- 多 group 不弱化 R1(单 share 持有):每 group 独立一份 share,设备进程
  内可能同时持有 N 份 *不同 group 的* share,但每 group 仍是 1 份 share_i。
- 多 group 不弱化 R7(append-only pubkey):每 group 各自的
  `groups.ecdsa_pubkey` 仍受 00006 trigger + 应用层守护。
- 多 group 不引入 BIP44/HD:仍是 non-hardened HD 仅在 group 内做 child 地址
  派生(`address-derivation.md`),group 之间用**独立 keygen 仪式**生成。
- 多 group 不改 trust anchor:配对 / pairings.json **不**直接写 group_members;
  membership 仍走 `POST /v1/groups/{groupId}/membership` 既有 B 面路径。

## 6. 装配 / 兼容

- 既有单 group 测试调用 `snapshotOwnShare()`:仍工作(返单 group 的 share),
  仅当多 group 时返 ok=false 强制 caller 显式 groupID。
- 既有调用 `setOwnShare`:已 hard-cut 改签名,加 groupID 形参;调用点只有
  `keygen.go` 和 `reshare.go`,均传 `*cfg.GroupID`。
- `mobileapi.SDK.SetOwnShareForTest`:测试 seam,gomobile 反射扫描时按
  `ForTest` 后缀跳过(`gomobile_test.go:62`)。

## 7. 验收

- `internal/mobileapi/groups_test.go`(4 cases):append / unknown route /
  ListGroupsJSON / replace same groupID。
- `internal/walletcli/groups_test.go`(5 cases):pairings list / migrate
  legacy / persist appends / replace by groupId / cmdGroups SDK-only /
  cmdGroups SDK+pair union。
- `internal/server/admin/devices_test.go`(4 cases):cross-group lookup /
  no-match / bad hex / LOCKED.
- Hard gates: race-recgate 20/20 GREEN、golangci-lint 0 issues、
  gomobile-flat surface 守护通过(`TestExportedSurfaceIsFlat`)。
