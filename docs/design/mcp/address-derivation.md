# 地址派生设计(非加固 HD,自托管钱包)

> 性质:L1 权威设计件(用户裁定锁定,实施按本件)。中文。
> 调研轨:`docs/address-derivation-investigate.md`(`f695aff`,临时件,留作审计)。
> 关联 `mcp/sdk.md`、`contract/protocol.md`、`contract/api.md`、`server/database.md`、`docs/security-review.md`(H-005)。

## 1. 决策锁定(用户权威)

- **Q1** 派生模型 = **非加固 HD**;子地址必须**单机离线**(零方在线、零交互)。
- **Q2** 扩展公钥 `(Q_master, chaincode)` 持久化于 **coorddb**。
- **Q3** chaincode 来源 = **方案 B**(init 一次性 commit-reveal 小 MPC 轮产随机 `c`);**接受**非加固「xpub + 任一子私钥 → 推全兄弟私钥」特性,纳 **H-005** 复核。
- **F1** xpub 暴露面 = **owning-member-only**(coord 持久 xpub,**仅**向**该组成员经身份签名鉴权后的自有设备**释放,**绝不**外露 A 侧业务服务)。
- **F2** 派生路径 = **扁平单层** `m/<index>`,`index ∈ [0, 2^31)`(非加固,uint32 上界即 2^31)。
- **F3** commit-reveal 协议 = 本件 §3 明定(L1 pin)。
- **F4** schema 形态 = `groups` 表**加列** `chaincode BLOB(32)`,新版本化 goose 迁移 **`00004`**(charter-10:禁手改 00001/00002/00003)。
- **F5** 后兼容 = HD **仅适用本设计落地后新建的 group**;`f695aff` 前既有 group **维持单地址、不可 HD**(注入 `c` 需 reshare,违 Q3 init-once / 无 reshare 红线)。

## 2. 密码学

xpub = `(Q_master ∈ secp256k1, c ∈ {0,1}^256)`。子 `index i ∈ [0, 2^31)`(非加固):

```
IL = HMAC-SHA512(key=c, data = compressed(Q_master)33 ‖ i_be32)[0:32]
若 IL == 0 或 IL ≥ N(secp256k1 阶) → 跳过该 i(BIP32 既定语义)
Q_child = Q_master + IL·G        // EC 点加,纯公钥
address  = internal/addr.{ETHAddress|TronAddress}(uncompressed(Q_child))
```

- `internal/addr` **零改**(子公钥喂同函数)。
- 持 `(Q_master, c)` 者**单机离线**算任意 `i` 的 `Q_child` 与三链地址,零方在线、零交互、无私钥。
- IL **公开**(任何持 xpub 者可算),非秘密。

## 3. chaincode 生成(commit-reveal,L1 pin,init 一次性)

DKG 完成同会话内追加一轮:

1. **承诺阶段**:每方 `P_j`(`j ∈ [1..n]`)本地取 `r_j ∈ {0,1}^256` 随机,广播 `C_j = SHA-256(DST_CR ‖ group_id ‖ j_be32 ‖ r_j)`。`DST_CR = "mcp/v1/chaincode-commit"`(域分隔常量,固定字节串)。
2. **揭示阶段**:全员收齐 `C_*` 后,各方广播自己的 `r_j`。
3. **校验**:每方独立校验 `SHA-256(DST_CR ‖ group_id ‖ j_be32 ‖ r_j) == C_j`(`∀ j`)。
4. **派生**:`c = HKDF-SHA256(salt = DST_CC ‖ group_id, ikm = r_1 ‖ r_2 ‖ … ‖ r_n, info = "", L = 32)`。`DST_CC = "mcp/v1/chaincode-derive"`。
5. **abort**:任一 `C_j` 校验失败、任一 `r_j` 缺失/超时(deadline 与 DKG 同步)、任一方对最终 `c` 计算不一致 → **abort 整个 init**,不产 group、不写 coorddb;以**新 `group_id`** 重试(无降级、无部分成功)。

**协议性质**:
- **不可单方偏置**:`c` 经 HKDF 混合所有 `r_j`,承诺先于揭示锁死贡献,任一恶意方无法在见到他人 `r` 后改自己的 `r`;最后揭示者亦无操纵窗口(承诺已固定其 `r_j`)。
- **绑定唯一**:`group_id` 进入承诺前像与 HKDF salt;跨组重放无效。
- **协调者**:复用 DKG 既有协调路径(无新协议层、无新组件)。
- **传输**:复用既有 libp2p Noise + circuit-relay v2 + rendezvous(`internal/transport`);**禁新增传输**。
- **持久化**:`c` 在同 init 事务内随 `Q_master`/`evm_address`/`tron_address` 一并写 coorddb(§4)。

## 4. 持久化(F4,charter-10)

`groups` 表加列(沿 `00002_group_chain_addresses` Go-migration 范式):

```sql
-- 新版本化迁移 00004(禁手改 00001/00002/00003)
ALTER TABLE groups ADD COLUMN chaincode BLOB(32) NULL;  -- F5:legacy NULL,新群必有
```

- `ProvisionGroup` 在 DKG + commit-reveal 同事务内写入 `chaincode`(连同既有 `ecdsa_pubkey`/`evm_address`/`tron_address`)。
- `chaincode IS NULL` 标识 legacy group(单地址、不可 HD,§8)。
- Down 迁移按 `00002` 全表重建注记范式镜像。

## 5. 派生路径(F2)

- 唯一约定:`m/<index>`,`index ∈ [0, 2^31)`,uint32 大端编码进 HMAC 输入。
- 不支持多账户层级(`m/<account>/<index>`)与 BIP44 尾段;未来若需扩展属新设计变更,走 PMA(constraint 6)。

## 6. 签名集成(tss-lib v3 KDD,无 reshare)

花费子 `i` 时:

1. 任一方(或协调者)离线算 `IL`(§2)。
2. 签名各方本地:
   ```go
   signing.UpdatePublicKeyAndAdjustBigXj(IL, keys, &Q_child, S256())
   p := signing.NewLocalPartyWithKDD(msg, params, sd, IL, outCh, endCh)
   ```
3. 群对 `Q_child` 出有效签名;**子私钥从不重组**,份额 `x_j` 不变,**无 reshare**。
4. `IL` 公开传递(xpub 持有者可独立重算),非秘密。

`internal/cli/mpcnet.go` 现 `signing.NewLocalParty(...)` 平凡构造改为 KDD-aware(传 `IL`)。

## 7. API 暴露面(F1)

xpub 严格 owning-member-only:

- **B 侧(成员↔coord)**:新增 `GET /v1/groups/{groupId}/xpub` —— `memberGate` 鉴权(`group_members.identity_pubkey` 签名,同既有 B-side),**仅向本组成员**返回 `{Q_master, chaincode}`。
- **A 侧(外部业务↔coord)**:**禁止**经 `coord.external.*` 暴露 `chaincode`(也不在任何 A 侧响应面)。A 侧获子地址走"coord 代派生"的只读地址查询(若需,另案新增,**本设计不预实现**)—— A 侧客户端不持 `c`。
- **CLI(walletcli/sdk)**:成员设备经 B-side 取 xpub 后本地缓存,后续 `wallet address <i>` 单机离线派生。
- 凭据/参数命名沿配置框架 v2 规范(`MPC_*` env / `--coord.*` CLI / `server.yaml`)。

## 8. 后兼容(F5)

- `f695aff` 之前建的 group:`chaincode IS NULL`,**单地址、不可 HD**;`GET /v1/groups/{groupId}/xpub` 对其返 **`409 STATE_CONFLICT`**(`api.md` C 表既有 code),错误体 `error.code = "LEGACY_NO_HD"`,`message = "group predates HD; multi-group remains the multi-address path"`。
- 本设计落地后新建的 group:必走 commit-reveal,`chaincode` 非空。
- 不提供 legacy→HD 迁移路径(违 Q3 init-once / 无 reshare)。

## 9. H-005 安全复核覆盖项(AD-5 阻 implement 收尾门)

须显式审视并文档化:

1. **commit-reveal 不可偏置**:H/HKDF 选择、DST 唯一性、group_id 绑定、承诺先于揭示、`r_j` 32B 熵充足、abort 严格、不可重放跨组。
2. **xpub 暴露面取舍**:owning-member-only 在 H-005 模型下的对手能力分析(假定恶意外部 A、假定恶意 relay、假定持 xpub 的设备失窃)。
3. **非加固"xpub + 任一重组子私钥 → 父+全兄弟"**:在 TSS **永不重组子私钥**前提下不被触发;残留依赖列举:① 签名实现严谨(永不导出 `x_j` 或 `IL·x_j` 之外的标量)② 子私钥 API 永不暴露 ③ xpub 释放路径受 §7 限定。
4. **legacy 边界**:`409 LEGACY_NO_HD` 行为校验。
5. **链接性披露**:持 xpub 可枚举全部子地址 → 隐私模型边界相对现 NO-HD 多 group 放宽,显式记录用户已接受。

## 10. 实现约束(全部硬性,违任一即 YELLOW 停)

- 全部 `AD-*` L3 **必须 useWorktree=true 隔离**(`o436xwlk`/`blh4o7cx` 共享树污染前车)。
- 基线 `origin/main = f695aff`(禁引 `ecff6b5`/pre-squash hash)。
- 零回归红线:核心 P0–P6 + 双 E2E 门 + 全 finalized 件(`FIX-004`/`DEP-001` fail-closed 已 file:line 核实真实)一律不动。
- charter-10:仅新增 `00004` 迁移,禁手改既有迁移。
- `api.md` 属 `docs/design/contract/` **L1 权威**,本设计的 A1/B-side xpub 端点由 **L1 改**(非 L3)。
- 任何 §1–§9 外**新设计分叉** L2 升 L1,L2/L3 不得自决(constraint 6)。
- H-005(AD-5)阻 implement 收尾门:未通过则不入 review-park。

## 11. 任务 DAG(L2 执行)

```
F1–F5 锁定(本件)
   ├── AD-2 init commit-reveal 产 c          (internal/mpc + cli/mpcnet keygen 段 + transport 复用)
   ├── AD-3 coorddb chaincode 持久化         (00004 迁移 + GroupRecord/ProvisionGroup + 测试)
   └─→ AD-1 签名 KDD 接线                    (internal/mpc + cli/mpcnet signing 段,新 internal/hd 离线 IL helper)
       ├─→ AD-4 coord api + walletcli 离线派生命令  (B-side xpub 端点 + walletcli `wallet address <i>`;api.md 由 L1 改)
       └─→ AD-5 H-005 复核(docs/security-review.md;§9 全项)→ 收尾门
```

AD-2 ∥ AD-3 文件不相交可并;AD-1 依赖 AD-2(c 可得)+ AD-3(sd 载体已含 chaincode 列);AD-4 依 AD-1。每 AD 隔离 worktree、显式 pathspec、校准 `-race` + 双 E2E + cat-file。
