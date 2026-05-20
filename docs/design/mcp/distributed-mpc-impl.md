# 分布式 MPC 引擎实施件(Phase 1 — L2 proposal L1 ratified)

> 性质:L1 权威实施件(对照设计件 `distributed-mpc.md` @ `5c8a90b`,Phase 1)。中文。
> Phase 1 = 实施细节定稿,纯设计层零代码;**Phase 2 implement 严格队列**:地址派生 AD-1→AD-4/AD-6→AD-5 H-005 全 finalize 之后再启。
> 用户裁定 2026-05-20:批准 L1 推荐 bundle("按推荐处理")。L1 2 项澄清已 adopt:DM-1 措辞 **ADDITIVE** 非 refactor;DM-3 configJSON 切换 = **hard-cut**(旧缺新字段拒)。

## §A 锚定 & 范围

- 权威设计源 = `docs/design/mcp/distributed-mpc.md` @ commit `5c8a90b`(R1–R7 + §2.1 身份模型 + §3 keygen 仪式 + §3.bis reshare + §3.ter attestation + §7.4 严格逐层 + §7 末验收硬判据)。
- 用户已锁:A 模型(coord 事件中介) / §7.4 strict-layered / §3.ter / coord 纯事件(R3 收窄) / R7 公钥 append-only。
- **api.md §F 3 端点契约已由 L1 落入 `docs/design/contract/api.md` B9/B10/B11**(本批同次实施件 commit 一并)。

## §B 6 层 DAG 与文件归属

每层 useWorktree=true 隔离,文件不相交于本批;与地址派生跨批文件归属冲突见 §D。

| 层 | 范围 | 文件归属(枚举) |
|---|---|---|
| **DM-1** mpc 单方入口(**ADDITIVE** 非 refactor) | 既有 `internal/mpc/{keygen,signing,resharing}.go` 全 n 模拟签名/行为**零变更**(测试沿用);**新增** `internal/mpc/singleparty.go` 单方 API,返单方 share_i | `internal/mpc/singleparty.go`(NEW);相邻文件**禁改** |
| **DM-2** 生产网络引擎抽取 | 从 `internal/cli/mpcnet.go` 抽取 tss-over-libp2p pump 泛化;**复制+泛化非搬迁**,`internal/cli/mpcnet.go` 保留(E2E 载体零回归) | NEW `internal/mpcnet/{engine.go, session.go, transport_adapter.go, *_test.go}` |
| **DM-3** SDK 单方化 + wire 回调(**hard-cut**) | `sdk.KeyGen/Sign/Reshare` 入参 configJSON 加 `{groupId,sessionID,partyIndex,n,t,memberSet,relay{peerID,addrs[]},role}`,只产 share_i;**旧 configJSON 缺新字段 → 拒**(`CodeBadConfig`);新增出站 wire 回调 Go→host;`OnWireMessage` 接活 R5 gate;`docs/design/mcp/sdk.md` 同步修订 L1 落 | `sdk/sdk.go`、`internal/mobileapi/wirecallbacks.go`(NEW)、`internal/mobileapi/{keygen,sign,reshare}.go`;`docs/design/mcp/sdk.md` L1 改 |
| **DM-4** coord 事件契约 + R7 守卫 | 配置 `Coord.External.ExpectedMembers`、identity 注册扩展、keygen/reshare/attestation 端点(api.md B9/B10/B11 已落)、dispatchHub 扩 keygen-START/reshare-START/attestation-ACK、**R7 双层守卫**(§E) | `internal/server/config.go`、`internal/server/coord/{identity.go(NEW),members.go,keygen.go(NEW),reshare.go(NEW),attestation.go(NEW),dispatch.go,api.go}`、`coorddb/migrations/00006_groups_pubkey_append_only.sql`(NEW;**注意**:AD-6 占 00005,本迁移取 **00006**)、`coorddb/repo.go` R7 app 守卫 |
| **DM-5** host 传输接线 | PC CLI 复用 `internal/transport`,实现 wire 回调 host 侧;移动桥同接口零改;**先 PC CLI 打通真 3 进程 keygen+sign+reshare** | `internal/cli/host_transport.go`(NEW);移动桥侧零改 |
| **DM-6 收尾** 组记录同事务一致性 + 验收 | 跨 n 方 attestation 一致性(全等才写 groups)+ §G E2E 真多进程 n 方验收硬判据全过 | `internal/server/coord/provisioning.go`、`internal/server/coorddb/repo.go`、`tests/e2e/test/e2e-distributed-mpc/` NEW |

## §C 每层硬门套件

通用每层(全部硬性):useWorktree=true / base = 当时 origin/main / 显式 pathspec / build + vet + gofmt + golangci-lint(项目包域 0) + 校准 `-race -p1 -timeout=1200s` 全包 ok + E2E-001 + E2E-002 + cat-file IN_HEAD / EN 注释 / 0 CJK 非 `docs/design` / commit 英文无 AI 协作者 / 零回归(核心 P0–P6 + 双 E2E + 全 finalized 件不动)/ charter-10。

层特定:
- DM-1 单元 + 既有全 n 模拟保留绿(零回归自证)。
- DM-2 生产引擎 smoke(单机内 3 方模拟,真 libp2p stack)。
- DM-3 gomobile binding regen ok + sdk_test 单方路径覆盖 + 旧 configJSON 拒被验证(hard-cut)。
- DM-4 R7 守卫 negative-test(尝试 UPDATE/DELETE/NULL 必失败);attestation 重放 reject;dispatchHub 新事件 dispatch 正/负路径。
- DM-5 PC CLI 真 3 进程 keygen+sign+reshare E2E(本层引入,挂 §G)。
- **DM-6 收尾门** = §G 验收硬判据全过方算本批 finalize。

## §D 与地址派生序列依赖矩阵(关键)

| Phase 2 层 | 文件域重叠地址派生 | 序列要求 |
|---|---|---|
| DM-1 | `internal/mpc/*`(AD-2 commit-reveal + AD-1 KDD helper 在此)| DM-1 必待 **AD-2 + AD-1** finalize |
| DM-2 | `internal/cli/mpcnet.go`(AD-2 keygen + AD-1 signing 段)| DM-2 必待 **AD-2 + AD-1** finalize |
| DM-3 | `sdk/*` + `internal/mobileapi/*`(AD-4 触此)| DM-3 必待 **AD-4** finalize |
| DM-4 | `internal/server/coord/*` + `coorddb/repo.go`(AD-6 触此)+ api.md(L1 已落 B8-B12)| DM-4 必待 **AD-3 + AD-4 + AD-6** finalize |
| DM-5 | `internal/cli/*` | DM-5 必待 **AD-4** finalize |
| DM-6 | `internal/server/coord/provisioning.go` + `coorddb/repo.go` | DM-6 必待 **AD-3 + AD-4 + AD-6 + AD-5 H-005** finalize |

**结论**:Phase 2 整体 implement **必排在地址派生 AD-1→AD-4/AD-6→AD-5 全 finalize 之后**。Phase 1 本件 = 设计层零代码,与地址派生 AD-* 并行无冲突。

## §E R7 pubkey append-only 双层守卫

### 应用层(主防护,事务级)

`internal/server/coorddb/repo.go` `Store.WithTx` 内,groups.ecdsa_pubkey 写路径加 pre-check:`SELECT ecdsa_pubkey FROM groups WHERE group_id=?`;若返非空且与拟写值不等 OR 拟写值为 NULL/empty → 事务 ROLLBACK + `ErrR7Violation`。所有 groups 写入口(ProvisionGroup / 任何潜在 update 路径)必经此守卫。

### DB 层(深防护,SQLite trigger,sqlcipher 兼容)

新版本化 goose 迁移 `00006_groups_pubkey_append_only.sql`(charter-10,AD-6 占 00005):

```sql
-- +goose Up
CREATE TRIGGER trg_groups_ecdsa_pubkey_append_only
BEFORE UPDATE OF ecdsa_pubkey ON groups
WHEN OLD.ecdsa_pubkey IS NOT NULL AND (NEW.ecdsa_pubkey IS NULL OR NEW.ecdsa_pubkey != OLD.ecdsa_pubkey)
BEGIN SELECT RAISE(ABORT, 'R7: groups.ecdsa_pubkey is append-only'); END;
CREATE TRIGGER trg_groups_ecdsa_pubkey_no_delete
BEFORE DELETE ON groups
WHEN OLD.ecdsa_pubkey IS NOT NULL
BEGIN SELECT RAISE(ABORT, 'R7: groups row with ecdsa_pubkey is non-deletable'); END;
-- +goose Down
DROP TRIGGER trg_groups_ecdsa_pubkey_append_only;
DROP TRIGGER trg_groups_ecdsa_pubkey_no_delete;
```

negative-test 覆盖:UPDATE→NULL / UPDATE→不同值 / DELETE 既有 row → 均 ABORT。两层叠加:应用守卫拒(主)+ DB 兜底拒(深),互为冗余。

## §F api.md 契约(L1 已落入 `docs/design/contract/api.md` B9–B11)

3 端点已正式入 api.md:

- **B9** `POST /v1/groups/{groupId}/keygen` — 发起 keygen,coord dispatch keygen-START
- **B10** `POST /v1/groups/{groupId}/reshare` — 旧+新委员会双签发起 reshare
- **B11** `PUT /v1/groups/{groupId}/attestation` — 客户端状态对账,coord 聚合定 `REGISTERED/NEEDS_KEYGEN/NEEDS_RESHARE/INCONSISTENT`

新增错误码:`LEGACY_NO_HD`(409,F5)、`EXPECTED_MEMBER_MISMATCH`(409,强制集守门)。

## §G E2E 真多进程 n 方验收(§7 末硬判据,DM-5 引入,DM-6 收尾门)

新增 `tests/e2e/test/e2e-distributed-mpc/`(平级 e2e-docker),Bun+Docker 真 n 进程:

1. `keygen-3of3`:3 独立容器/进程,各自 keystore 卷不共享 → 每进程 keystore **仅含 1 份** share(枚举字段);coord 容器日志/库无任何 share/PreParams;relay 容器流量仅密文(无明文 share 模式)。
2. `sign-2of3`:任 t+1=2 进程签名成功;任 ≤t=1 进程超时/失败(无降级)。
3. `reshare-3to3-rotate`(必)/ `reshare-3to4`(可选第一阶段):旧份额抹除(读 keystore 自证);chaincode 不变(读 coord groups.chaincode 自证);新方进入强制集后方接受。
4. attestation 一致性:全 3 报 `holdsShare=true` 一致 → REGISTERED;1 报 false → NEEDS_RESHARE。

**DM-6 收尾门**:本套件全过方算本批 finalize 入 review-park rule4。

## §H 节奏(§7.4 严格逐层)

按 §B 顺序:**DM-1 → DM-2 → DM-3 → DM-4 → DM-5 → DM-6 收尾**。每层 finalize 后 L2 follow-up L1 报硬门证据,**L1 复核 + 用户确认方进下层**。任一层硬门红 → revert + 打回 ≤2;耗尽或新分叉 → YELLOW 升 L1,L2/L3 不自决。Phase 2 implement 严格排在地址派生 AD-1→AD-4/AD-6→AD-5 全 finalize 之后(§D 矩阵)。

## §I 与地址派生 §7.bis 协调

`group_derived_addresses` 占用 `00005_group_derived_addresses.sql`(AD-6 本批)→ Phase 2 R7 守卫迁移取 **`00006_groups_pubkey_append_only.sql`**(charter-10 顺序号续);两者独立,无 schema 冲突。
