# 交付状态记录

> 单一来源:实际 in HEAD 的可验证交付物 + 关键里程碑提交。基线 `origin/main`
> HEAD `4028d54`(2026-05-21)。每行可经 `git cat-file -e HEAD:<file>` 验证。
>
> 关联:`docs/ARCHITECTURE.md`(整合架构)、`docs/USAGE.md`(使用指南)。

## 1. 总览

| 阶段 / 范围 | 状态 | 关键提交 |
|---|---|---|
| **P0** 端上 MPC 打包验证(go1.25 基线 + gomobile 脚本) | ✅ GO(限定 + 真机后继门待移动环境) | `docs/design/P0-report.md` |
| **P1** MPC 核心(单进程仿真) | ✅ in main | `internal/mpc/` |
| **P2** 传输层 libp2p + relay 零信任 | ✅ in main | `internal/transport/`, `internal/server/relay/` |
| **P3** 协调 + tx-decode + 契约(coord + B1..B12) | ✅ in main | `internal/server/coord/` 等 |
| **P3.5** 管理面(admin-api + admin-ui htmx) | ✅ in main `dbd8884` | `internal/server/admin/` |
| **AD-1..AD-6 + H-005** 地址派生与 HD | ✅ 全部 FINALIZED 2026-05-19/20 | 下表 §3 |
| **DM-1..DM-6** 真分布式 MPC Phase 2 | ✅ 全部 FINALIZED 2026-05-20 | 下表 §4 |
| **cli-ui** wallet-cli htmx 面板(WYSIWYS + import + fetch/xpub/address) | ✅ FINALIZED 2026-05-20 | 下表 §5 |
| **§G 真测** 真 n 进程 keygen/sign/reshare 实证 | ✅ FINALIZED 2026-05-21 `4028d54` | 下表 §6 |
| **P4** 移动封装(gomobile bind + RN bridge + 真机) | 🟡 部分(脚本就位,GM-001 .aar 实证;真机 / .xcframework / RN bridge 待移动环境) | `docs/gomobile-build-report.md` |
| **P5** sample-app + 韧性场景 | 🔵 空缺(reshare 单测已通,sample-app + 完整韧性剧本未做) | — |
| **P6** 持续加固(WHA / RT / CFG / SDKCF / 安全审查持续) | 🟢 大部分 in main(逐项 finalized) | 下表 §7 |

## 2. 当前 `origin/main` 提交链(2026-05-21,从 HEAD 倒序)

```
4028d54 test(e2e-distributed-mpc): implement §G real n-process keygen/sign/reshare
6758102 docs: rebuild design docs to match HEAD (Phase 2 + AD + cli-ui finalized)
b6531cc feat(walletcli): add read-only fetch / xpub / address UI to wallet-cli panel
3bbe502 feat(walletcli): add import UI to wallet-cli htmx panel
b36ae4b feat(walletcli): add htmx wallet-cli inspection panel with WYSIWYS sign approval
e70bd75 feat(coord,coorddb,e2e): DM-6 same-tx n-party attestation commit + §G acceptance scaffold
941fa87 feat(cli,walletcli): DM-5 PC CLI host transport + walletcli placeholder removal
9d2fe86 feat(mobileapi,walletcli,sdk): DM-3 SDK single-party + outbound wire callback (hard-cut)
74ca04c feat(coord,coorddb): DM-4 event-orchestration + R7 append-only guard
a968ed8 feat(mpcnet): ADDITIVE production network engine extraction (DM-2)
327c715 feat(mpc): ADDITIVE single-party keygen/sign/reshare entry (DM-1)
addfbdd docs(api): mark B2 push deprecated + clarify A3 dual-auth gap
22d1445 docs(security-review): AD-5 H-005 closure gate (§9 + §7.bis verified GREEN)
c08555c feat(coord,coordclient,mobileapi,walletcli,sdk,coorddb): B8 xpub + B12 group_derived_addresses (AD-4 + AD-6)
cff15b6 feat(coorddb,coord): group_derived_addresses + B12 register/list (AD-6)
cf8adc8 feat(mpc,cli,hd): KDD signing wiring + offline IL helper (AD-1)
380b320 feat(coorddb): persist HD chaincode in groups (00004 + ProvisionGroup, AD-3)
bb6bfee feat(mpc,cli): post-DKG chaincode commit-reveal + E2E whitelist sweep (AD-2)
```

## 3. 地址派生(AD-1..AD-6 + H-005)

| 任务 | 范围 | 提交 |
|---|---|---|
| AD-1 | KDD signing 接线 + `internal/hd` 离线 IL helper(BIP32 非硬化) | `cf8adc8` |
| AD-2 | post-DKG commit-reveal 产 chaincode(strict abort) | `bb6bfee` |
| AD-3 | coorddb chaincode 持久化(00004 迁移 + ProvisionGroup 同事务) | `380b320` |
| AD-4 | walletcli 离线 `wallet address <i>` + B-side xpub 客户端(B8) | `c08555c` / `5184701` |
| AD-6 | `group_derived_addresses` 表 + B12 register/list 端点(00005 迁移) | `cff15b6` |
| AD-5(H-005) | 安全审查覆盖(§9 全项 + §7.bis 链接性二度披露 GREEN) | `22d1445`(`docs/security-review.md`) |

设计参考:`docs/design/mcp/address-derivation.md`(已标 ✅ FINALIZED 全项)。

## 4. 分布式 MPC Phase 2(DM-1..DM-6)

| 层 | 范围 | 提交 |
|---|---|---|
| DM-1 | `internal/mpc/singleparty.go`(ADDITIVE 单方 keygen/sign/reshare API) | `327c715` |
| DM-2 | `internal/mpcnet/{engine,session,transport_adapter}.go`(生产网络引擎抽取) | `a968ed8` |
| DM-3 | SDK 单方化 + WireCallbacks 出站桥(hard-cut configJSON 字段集) | `9d2fe86` |
| DM-4 | coord 事件契约 B9/B10/B11 + R7 双层守卫(00006 trigger) | `74ca04c` |
| DM-5 | `internal/cli/host_transport.go`(PC CLI libp2p 实接 wire)+ walletcli env 驱动 | `941fa87` |
| DM-6 | `coorddb.CommitAttestationQuorum` 同事务 + §G E2E scaffold | `e70bd75` |

设计参考:`docs/design/mcp/distributed-mpc.md`(R1-R7)、
`docs/design/mcp/distributed-mpc-impl.md`(已标 ✅ DELIVERED 全 6 层)。

## 5. wallet-cli htmx 面板(`cli serve` UI)

| 批次 | 范围 | 提交 |
|---|---|---|
| 主轮廓 | htmx 面板 + WYSIWYS sign approval(`/ui` / `/ui/sign/{id}` / Approve / Reject) | `b36ae4b` |
| import | 备份恢复(passphrase env-only;UI 禁止经 HTTP 输入 passphrase) | `3bbe502` |
| 只读补全 | `/ui/fetch` / `/ui/xpub` / `/ui/address`(coord 查询 + 离线派生) | `b6531cc` |

设计参考:`docs/design/mcp/walletcli-ui.md`(新增,与 admin-ui 关系区分)。

## 6. §G 真分布式 MPC 实证(2026-05-21,`4028d54`)

| 测试 | 验证项 | 结果 |
|---|---|---|
| `keygen-3of3.test.ts` | 3 OS 进程经真 libp2p Noise+circuit-relay v2 跑 tss-lib v3 keygen;主公钥跨方一致;allViaRelay 全 true | ✅ 1 pass |
| `sign-2of3.test.ts` | (a) t+1=2 协作产 RSV + ecrecover 至主公钥 + low-S(b) 单签者 tss-lib 拒签(keycount<t+1) | ✅ 2 pass |
| `reshare.test.ts` | (a) 3-to-3 rotate 主公钥 invariant(b) 3-to-4 B10 admission 陌生身份 → 409 EXPECTED_MEMBER_MISMATCH | ✅ 2 pass |
| `attestation-quorum.test.ts` | REGISTERED 幂等 commit / R7 violation 409 / INCONSISTENT no-commit | ✅ 3 pass |

**完整真测 2026-05-21 GREEN 基线**:
```
cd tests/e2e && bun run test:e2e-dmpc
# 4 文件序列化;8 pass / 4 skip / 0 fail / ~5 分钟
```
参考:`docs/design/testing.md` §3.4。

## 7. P6 加固里程碑(选,逐项 finalized in main)

| 任务 | 范围 | 提交 / 文件 |
|---|---|---|
| WHA-001 | 外部回传 webhook 双模式鉴权(签名 + Bearer,±300s skew) | `20f7a26` 系 |
| RT-001 | relay token_verify 配置化 | `3c5123b` |
| CFGDOC-001 | env/cli 配置完整性 + schema doc test | `2999685` |
| CFG-001 + SDKCF-001 | 配置整改批 + SDK status wiring | `454bb03` 系 |
| FIX-002 | node 双角色启动缺陷 | `0fd6a98` |
| FIX-004 | MPC proof guardrail(`ALLOW_INSECURE_MPC` 测试门 + 生产 fail-closed) | `960ffe6` |
| H-004 / H-005 | 安全复核闭环 | `docs/security-review.md` |
| 整库加密 + LOCKED | sqlcipher + Argon2id + zeroize + 生产 fail-closed 护栏 | `internal/server/coorddb/` |
| admin 强鉴权 seam | StrongAuth interface + Bearer 双 token + IP allowlist | `internal/server/admin/` |

## 8. 未完成 / 待用户裁定

- **P4 移动**:gomobile `bind` 脚本就位,Linux/CI 已实证 .aar 真构(`docs/gomobile-build-report.md`);
  真机 / .xcframework / RN bridge / sample-app 在移动环境外(范围外硬约束)。
- **P5 sample-app**:RN 钱包示例 app 未起;reshare 集成测已通过 DM-* / §G 实证。
- **Phase 3 范围**:cli htmx 已闭合,Phase 2 + §G 已闭合;**Phase 3 词汇待用户定义**(GM-001 真 gomobile 流水线 / P5 sample-app / E2E 真硬件 / 安全审计 持续 等候选)。
- **harness §3.2 calibration**:E2E-001 历史上 3 次同款 fragility(non-causal,记录在案);
  scope decision parked。
- **blh4o7cx**:shared-tree contamination,L1 已隔离,scope decision parked。

## 9. 验证方式

任一行交付物可用以下方法独立验证:

```bash
# 1) 提交是否在 main
git log --oneline origin/main | grep -E "DM-|AD-|htmx|§G|attestation"

# 2) 文件是否在 HEAD
git cat-file -e HEAD:internal/cli/host_transport.go  # DM-5
git cat-file -e HEAD:internal/server/coord/dm6_test.go  # DM-6 unit
git cat-file -e HEAD:internal/walletcli/ui.go  # cli htmx
git cat-file -e HEAD:tests/e2e/test/e2e-distributed-mpc/keygen-3of3.test.ts  # §G real test

# 3) 硬门复跑(Go 全树 + race)
gofmt -l . && go vet ./... && go build ./...
rtk test go test -race -timeout=1200s ./...

# 4) E2E 真分布式 MPC(显式开关 + 序列化)
cd tests/e2e && bun run test:e2e-dmpc
```

## 10. 历史审计参考

- `docs/release-readiness-audit.md`(RA-001,2026-05-18 基线 `aa56c98`)
  独立审计 NO-GO / CONDITIONAL-GO 报告。**注**:该报告基线远旧于当前 HEAD;
  P1 清单中许多项已闭合(DM/AD/§G/cli-ui)。建议作为方法论参考而非当前状态。
- `docs/security-review.md`(逐红线核对 + H-005 GREEN)。
- `docs/design/P0-report.md`(P0 GO 裁定 + 真机后继门)。
