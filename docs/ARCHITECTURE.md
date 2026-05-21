# 架构总览(顶级入口)

> 本文给非实现者(架构师 / 安全审查 / 集成者 / 运维)一个一站式系统理解,
> **不**重复 `docs/design/` 的实现细节。设计细节请进 `docs/design/`(每一节
> 末尾给出对应文件指针)。基线 HEAD `4028d54`(2026-05-21)。

## 1. 项目定位

**TSS MPC 自托管共管钱包的签名内核**。基于 tss-lib v3 的纯端上门限签名,
ETH / BSC / TRON 三链同用 secp256k1(一次 DKG 产单一主公钥三链通用)。

**核心不变量**:
1. **门限分布式持有** — 任一单机生命周期内只持自己那份 share,n 方独立设备
   各持一份;coord 永不见任何分片、PreParams、tss-lib party 内部状态。
2. **禁止盲签** — 设备侧重算链摘要并断言 `==digest32`,WYSIWYS 人审后才进 MPC。
3. **签名内核链无关** — 输出 `{R, S, V}` 即终,交易构造/广播由外部业务服务负责。
4. **关键密钥永不入库** — 分片、PreParams、Paillier 私钥不进任何持久库。

详见 `docs/design/security.md`(六条红线)、`docs/design/mcp/distributed-mpc.md`
(R1-R7 分布式 MPC 红线)。

## 2. 系统上下文

```
┌─ 外部业务服务(范围外)──┐  · 构造交易 / 持链逻辑 / 负责广播
│  · 提交签名信封           │  · 通过 A 面消费 `{R,S,V}` 结果
└───────────┬───────────────┘
            │ HTTPS + JSON(api.md A 面)
            ▼
┌─ node 服务端(单二进制双角色)──────────────┐
│  · coord 角色:待签编排 / API 对接 / D-001 │
│  · relay 角色:libp2p circuit-relay v2 中转 │   admin-api + admin-ui htmx
│  · admin-api:运维只读 + 受控写 + 强鉴权    │←──(同进程 //go:embed)
└──────────────────┬────────────────────────┘
                   │ libp2p Noise 端到端(MPC 数据面)+ HTTPS(成员 B 面控制面)
       ┌───────────┴───────────┐
       ▼                       ▼
┌─ 成员设备 0..n-1 ──┐   ┌─ PC wallet-cli ─┐
│ mobile SDK (gomobile)│   │ cli serve + UI  │ (调试 / 运维 / E2E 载体)
│ keystore / tx-decode │   │ /v1 + /ui htmx  │
│ WYSIWYS 人审 / Sign   │   └─────────────────┘
└──────────────────────┘
```

## 3. 组件与归属

| 组件 | 归属 | 一句话职责 | 详见 |
|---|---|---|---|
| `internal/mpc` | mcp | tss-lib v3 keygen/sign/reshare 封装(单方 + 全 n 模拟) | `mcp/sdk.md` §1,`distributed-mpc.md` |
| `internal/mpcnet` | mcp | 生产网络引擎(tss-over-libp2p pump 泛化,DM-2) | `distributed-mpc-impl.md` §B |
| `internal/transport` | mcp | libp2p Noise + circuit-relay v2 + PSK + rendezvous 客户端 | `contract/protocol.md` |
| `sdk` + `internal/mobileapi` | mcp | gomobile 友好扁平面(string/[]byte/callback);DM-3 hard-cut WireCallbacks | `mcp/sdk.md` §2 |
| `internal/keystore` | mcp | 分片设备 keychain + 口令派生密钥加密落盘 | `mcp/sdk.md` §6 |
| `internal/txdecode` | mcp | ETH/BSC/TRON 解码 + 重算摘要断言 + A 区事实产出 | `mcp/sdk.md` §4 |
| `internal/hd` + `internal/addr` | mcp | HD 派生(commit-reveal chaincode + KDD)/ 链地址纯派生 | `mcp/address-derivation.md` |
| `internal/cli` | mcp | E2E 多进程载体(`cli member` 子命令)+ DM-5 HostTransport | `distributed-mpc-impl.md` §B DM-5 |
| `internal/walletcli` | mcp | PC wallet 党(`cli serve` JSON + htmx UI) | `mcp/walletcli-ui.md` |
| `internal/server/coord` | server | coord 角色:待签编排 + B1..B12 API + 事件 dispatchHub | `server/server.md`,`contract/api.md` |
| `internal/server/coorddb` | server | SQLite + sqlcipher 全库加密;6 迁移版本 | `server/database.md` |
| `internal/server/relay` | server | libp2p relay v2 + 鉴权 + 配额 | `server/server.md` R 部分 |
| `internal/server/admin` | server | admin-api 只读查询 + 控制 + audit + admin-ui htmx | `server/admin.md` |
| `internal/contract` | contract | 信封规范化 + 验签 + canonical preimage | `spec/envelope-canonical.md`,`spec/group-provisioning.md` |
| `internal/coordclient` | mcp | SDK 侧 B 面 HTTP 客户端(签名鉴权) | `mcp/sdk.md` §2.1 |
| `tests/e2e/` | 测试 | Bun/TS 端到端三套(`e2e` / `e2e-docker` / `e2e-distributed-mpc`) | `testing.md` |

## 4. 部署拓扑

`node` 是**单一可执行**,经配置开关启用 relay / coord 角色,**可单开或双开**。

```
┌── 部署 1:合并节点(默认,起步最简)──┐
│  node: relay=true, coord=true        │
│  · 三套监听:relay(0.0.0.0)、coord  │
│    external/member(分别端口)、admin │
│  · 同一 sqlcipher 库(coord 持久化)  │
│  · admin-ui 进程内嵌入                │
└──────────────────────────────────────┘

┌── 部署 2:角色拆分(运维隔离)──┐
│  node-relay: relay=true (多副本)│←── 第三方运营皆可,无状态、零信任
│  node-coord: coord=true (1 节点)│←── SQLite + 文件备份 / Litestream
│  admin-api 仅在 coord 节点内    │
└──────────────────────────────────┘
```

**信任域**:relay 是**零信任哑管道**(Noise 端到端);coord 是**信任最小化**
(明文信封必须可见以编排,但永不见 MPC 内部);admin 是**特权运维**(强鉴权后
可读所有非 share 数据)。合并部署不破坏密码学零信任 —— Noise 仍端到端,
coord 不在 MPC 路径上。

## 5. 关键时序

### 5.1 DKG / keygen(P1 单方仿真 + DM-1..DM-5 真分布式)

```
组初始化 →  各成员设备本地生成 PreParams(后台,严禁后端预生成下发)
        →  各设备启动 SDK.KeyGen(configJSON, WireCallbacks, cb)
        →  WireCallbacks 经 host(gomobile bridge / cli HostTransport)走 libp2p
           Noise 端到端,经 relay 中转 + rendezvous 发现对方
        →  tss-lib v3 keygen 完成,每方持 share_i;主公钥公开
        →  AD-2 post-DKG commit-reveal 协议产 chaincode(strict abort 模式)
        →  AD-3 同事务持久化 ecdsa_pubkey + chaincode + evm_address + tron_address
```

### 5.2 签名(sign)

```
①  外部服务 POST /v1/requests(信封 + proposerSig + metaHash)→ coord 入待签
②  成员上线连 relay,签名心跳上报(coord 维护内存在线集)
③  成员审批 → coord 评估:在线 ∩ 审批 ∩ 未过期 ≥ t → 选 signers → DISPATCHED
④  各 signer 设备:tx-decode 重算 ==digest32 → A/B 分区展示 → 人审 → MPC 前再校验未过期
⑤  signers 经 relay 跑 tss 签名(coord 不参与)→ {R,S,V}
⑥  指定一方 POST B7 → coord 用组主公钥验签 → A4 webhook 回传外部业务服务
```

详见 `docs/design/architecture.md` §4.2、`contract/api.md` B 面、`server/server.md` C3-C7。

### 5.3 Resharing(P5,丢失成员)

剩余 ≥ t 方发起 reshare → 经 relay 重建缺失分片 / 纳新成员 → **主公钥不变,
地址不变**,旧分片作废。窗口期冗余下降。coord B10 端点 + 严格
`expected_members` 集守门(EXPECTED_MEMBER_MISMATCH 拒绝陌生身份)。

## 6. 数据平面 / 控制平面分离

| 平面 | 协议 | 谁说话 | 谁可见 |
|---|---|---|---|
| MPC 数据 | libp2p Noise + circuit-relay v2 | 成员设备 ↔ 成员设备 | 仅设备本身;relay/coord/运营方均不可读 |
| 控制 / 编排 | HTTPS + JSON `/v1/*` | 外部业务 ↔ coord;成员 ↔ coord | coord 进程明文(信任最小化设计取舍) |
| 通知 | 单一固定 webhook | coord → 外部通知渠道 | 外部渠道翻译至 FCM/APNs;coord 不持推送凭证 |
| 管理 | HTTPS + JSON `/admin/*` + htmx UI | 运维 ↔ admin-api | strongAuth(mTLS/OIDC seam)+ Bearer + IP allowlist + 全审计 |

物理路径分离 → relay 零信任与 coord 信任最小化互不污染。

## 7. 信任模型摘要

| 主体 | 信任级别 | 能做 | 不能做(因为...) |
|---|---|---|---|
| 外部业务服务 | 不可信 | 提交任意信封 / businessInfo | 让成员误签 — 设备 tx-decode + WYSIWYS 拦截 |
| relay 角色 | 零信任 | 丢弃 / 延迟 / 审查流量 | 读 / 改 MPC 内容 — Noise 端到端 + peerID=公钥 |
| coord 角色 | 信任最小化(资金)/ 隐私可信 | DoS / 审查 / 知交易隐私 | 偷钱 / 伪造审批 / 令各方签不同内容 — TSS 门限 + WYSIWYS + 同摘要绑定 |
| 运维管理员 | 隐私可信 + 运维特权 | 查交易 + 历史 + 控配额 / 封禁 / DB 解锁 | 偷钱 / 伪造审批 / 签发准入 / 看分片 — 门限 + 自主式不变 + 不可篡改审计 |
| 成员设备 + 持有者 | **信任根** | 持分片、独立审批 | 单方动用资金 — 需 ≥ t 方协作 |

详见 `docs/design/security.md`。

## 8. 横切不变量(全链生效)

1. **禁止盲签 / WYSIWYS**:`tx-decode` 重算 `==digest32` + A/B 展示 + 人审。
2. **PreParams 端上生成**:严禁后端预生成下发(含 Paillier 私钥)。
3. **TTL 一等公民**:coord + 设备两侧均校验未过期;requestId 不复用。
4. **同摘要绑定**:TSS 要求所有 signer 签同一 digest,抗分裂攻击。
5. **结果免信任校验**:coord 回传前用组主公钥验 `{R,S,V}`。
6. **链无关内核**:库仅产 `{R,S,V}`,构造/广播均外部服务负责。
7. **R7 公钥 append-only**:`groups.ecdsa_pubkey` 一旦写入永不可改(应用层 + SQLite trigger 二层守卫)。

## 9. 安全边界(纵深防御层)

| 层 | 防护 |
|---|---|
| 网络 | libp2p Noise 端到端 + PSK pnet + circuit-relay v2;relay 无明文 |
| 身份 | 成员身份私钥(独立于 share),A 面 mTLS/api_key,B 面成员签名鉴权 |
| API | 信封 proposerSig 全字段覆盖 + metaHash 双重绑定;coord 仅 PEM 公钥可见 |
| 数据 | coord SQLite + sqlcipher **全库页级加密**,默认 LOCKED;口令仅内存(zeroize);分片永不入库 |
| 派生 | HD 非硬化 only;chaincode commit-reveal 抗篡改;owning-member-only xpub |
| 审计 | `admin_audit` 追加写不可改;`request_events` 长期保留 |
| 运维 | dev/test 加密禁用需 `ALLOW_INSECURE_DB=1` + 非生产标记;生产 fail-closed 拒启动 |

详见 `docs/design/server/database.md` §7、`docs/security-review.md`。

## 10. 进一步阅读(按角色)

- **架构师**:`PLAN.md` 范围与决策 → `architecture.md` 时序与平面 →
  `security.md` 红线 → `distributed-mpc.md` R1-R7。
- **集成者**:`mcp/sdk.md` 扁平 API → `contract/api.md` A/B 面 →
  `mcp/walletcli-ui.md` 调试面板。
- **运维**:`server/server.md` 配置(完整键矩阵)→ `server/admin.md` 管理面 →
  `server/database.md` 备份与加密。
- **安全审查**:`security.md` + `security-review.md`(逐红线核对)+
  `release-readiness-audit.md`(独立审计基线;主干已大幅前进,以代码为准)。
- **测试者**:`testing.md` 阶段门 + §3.1/§3.3/§3.4 三套 E2E + `P0-report.md`。
- **使用者(运维 + 集成)**:`docs/USAGE.md`(快速开始 + 部署 + API 调用)。
- **交付状态**:`docs/DELIVERY.md`(P0-P6 + DM-* + AD-* + cli-ui 实际进度)。
