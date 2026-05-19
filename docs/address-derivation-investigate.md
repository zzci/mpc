# 地址派生调研(PMA investigate · 只读 · 待用户 Q1–Q3 裁定)

> 阶段:PMA **investigate** 纯调研产物。**不含 proposal/implement**;不得据此改代码或拆派实现 L3。
> 实现阻断,直至用户 Q1–Q3 裁定经 L1(erzqfnje)下达。
> 零回归红线:核心 P0–P6 + 双 E2E 门 + 全 finalized 件不动;本文档与现终态零回归并存。
>
> 背景:2026-05-18 用户曾裁定「不做密钥派生 / 多地址=多 group / 刻意排除 BIP44/HD」,
> 据此交付了 G-001(coorddb `00002` groups.evm_address/tron_address,每 group 一对地址)、
> H-005 在该 NO-HD 前提下结论合规。**2026-05-19 用户发起反转**,新增「地址派生」范围 —— 本调研服务于该反转。

---

## I1 · tss-lib `external/tss-lib/crypto/ckd` 实际能力与限制

源:`external/tss-lib/crypto/ckd/child_key_derivation.go`(全文已读)。

**能力:**

- `ExtendedKey{ecdsa.PublicKey, Depth, ChildIndex, ChainCode[32], ParentFP, Version}` —— BIP32 扩展**公**钥结构。
- `DeriveChildKey(index, *ExtendedKey, curve) → (ilNum *big.Int, childPk *ExtendedKey, err)`:
  - `I = HMAC-SHA512(key=ChainCode, data=compressed_parent_pubkey(33) ‖ index_be32(4))`;`IL=I[:32]`、`childChainCode=I[32:]`。
  - 校验 `IL ∈ (0, N)`,否则 `invalid derived key`。
  - `childPub = parentPub + IL·G`(纯**点加**,公开可算);`childPriv = parentPriv + IL (mod N)`(**加性标量偏移**)。
  - 返回的 `ilNum` 即「把父私钥偏移到子私钥」的标量。
- `DeriveChildKeyFromHierarchy(indices[], *ExtendedKey, mod, curve) → (Σ_i IL_i mod N, 末级 childPk, err)`:
  多层路径**坍缩为单个加性标量** `Σ IL_i mod N` + 链式推进的 childPk。
- `String()` / `NewExtendedKeyFromString()`:BIP32 base58 扩展公钥(de)序列化(version‖depth‖parentFP‖childindex‖chaincode‖pubkey33‖checksum4)。

**硬限制:**

1. **仅非加固**(代码注释 L35–36 明示,`index >= HardenedKeyStart(2^31)` 直接 `errors.New("the index must be non-hardened")`)。
2. 加固派生**结构上不可能**:BIP32 加固级 HMAC 输入为 `0x00 ‖ 父私钥 ‖ index`;TSS 下父私钥分片、永不重组 → 无任一方持有该输入。加性 delta 技巧仅对非加固成立(IL 仅依赖父**公**钥)。
3. `Depth ≤ 255`;`IL` 偶发落在 `[N,∞)∪{0}` 时该 index 不可用(需跳过,BIP32 既有语义)。
4. `ExtendedKey` 需要 `ChainCode[32]`;**tss-lib v3 keygen `LocalPartySaveData` 不含 BIP32 chaincode**(`internal/mpc/keygen.go` 仅 `ECDSAPub` 等)→ 主 chaincode 来源是必须新增的实现决策点(见 I3、Q1)。

---

## I2 · 与本项目 TSS 分片私钥模型的接线点

**结论:tss-lib v3 原生支持非加固 HD 接线,本项目当前未接线(确证)。**

- tss-lib 既有件:
  - `external/tss-lib/ecdsa/signing/local_party.go:114 NewLocalPartyWithKDD(... keyDerivationDelta *big.Int ...)` —— 注释「returns a party with key derivation delta for HD support」,`p.temp.keyDerivationDelta = keyDerivationDelta`。
  - `external/tss-lib/ecdsa/signing/key_derivation_util.go:18 UpdatePublicKeyAndAdjustBigXj(keyDerivationDelta, keys[], extendedChildPk, ec)` —— 用 delta 调整各方保存的公开份额点。
  - `local_party_test.go:247–249` 给出范式:`keyDerivationDelta := il; UpdatePublicKeyAndAdjustBigXj(il, keys, &extendedChildPk.PublicKey, S256())`。
- 接线机制(各方各自标量偏移,不重组私钥):
  1. 由 `ckd` 从 `(父扩展公钥=群 ECDSAPub+主 chaincode, 路径 indices)` 算出 `IL`(及子公钥/子链地址)。
  2. 签名时以 `IL` 作 `keyDerivationDelta` 传 `signing.NewLocalPartyWithKDD`,并在签名前 `UpdatePublicKeyAndAdjustBigXj` 调公开份额;群对外等效用 `childPriv = groupPriv + IL` 签名,**全程私钥保持分片、子私钥从不显式重组**。
- 本项目现状(未接线,确证):
  - `internal/cli/mpcnet.go:210 signing.NewLocalParty(...)` —— 平凡构造,无 KDD。
  - `internal/mpc/*.go`、`internal/cli/{mpcnet,device}.go` 全树 grep 无 `ckd / KeyDerivation / DeriveChild / UpdatePublicKeyAndAdjustBigXj` 引用。
  - `crypto/ckd` 构件保持未接线,与 2026-05-18 NO-HD 裁定一致。

---

## I3 · 影响面与改动边界(coorddb / internal/addr / coord api / cli mpcnet)

| 面 | 现状 | 派生接入后的影响 |
|---|---|---|
| `internal/addr/addr.go` | 纯 `pubkey → ETH/BSC/TRON` 地址(EIP-55 / Base58Check) | **零改动**:对子公钥调用同一函数即可;地址派生不触碰本包。 |
| `internal/server/coorddb`(`00002_group_chain_addresses.go`) | groups 表 `evm_address/tron_address`,**开通时由 ecdsa_pubkey 派生一次**;一 group = 一对地址 | 一 group → **多**子地址(按 index/路径)→ schema 影响:新增 派生地址表 或 (group,path)→地址 维度。**charter-10**:必须新版本化 goose 迁移(`00004…`,沿 `00002` Go-migration 范式),禁手改既有迁移。是否持久化 = **Q2**。 |
| coord api(`internal/server/coord`) | 端点按 groupId 隔离,存主公钥不存地址(轻度链感知:已存派生地址) | 若服务端管理子地址簿:新增「派生/列举子地址」端点 + api.md 契约面;若纯客户端派生:coord 面零改。= **Q2**。 |
| `internal/cli/mpcnet.go` / `device.go` | `keygen.NewLocalParty` / `signing.NewLocalParty`(无 KDD);keygen 不产/不存 chaincode | 签名路径改走 `NewLocalPartyWithKDD` + `UpdatePublicKeyAndAdjustBigXj`,delta=ckd IL;**主 chaincode 来源**需新增(keygen 时随机产并随 SaveData 持久化 / 或由群公钥确定性派生 —— 实现分叉,见 Q1)。device 出账/展示需带路径或子地址簿。 |
| `internal/mpc`(keygen/signing/recover/resharing) | `LocalPartySaveData` 序列化(`MarshalSaveData`) | 若 chaincode 随 keygen 持久化 → SaveData 包络需扩展(向后兼容/迁移既有分片);若确定性派生 → 无需改 SaveData。= Q1 实现分叉。 |

**改动边界(零回归):** 核心 P0–P6 协议、双 E2E 门、全 finalized 件(WHA-001/RT-001/CFGDOC-001/CFG-001/SDKCF-001/D-001/G-001…)不动;派生为**新增能力**,签名主路径改造需以双 E2E + 校准 -race 守恒。

---

## I4 · 信任边界:非加固「父扩展公钥 + 任一子私钥 → 推全部兄弟私钥」对 H-005 的含义

非加固 BIP32 既有弱点:`childPriv = parentPriv + IL`,而 `IL` 由**公开**的 `(父扩展公钥, chaincode, index)` 可算 → 任得一个**重组出的**子私钥即 `parentPriv = childPriv − IL` → 进而推**全部兄弟私钥**。

对本项目 H-005(`docs/security-review.md`)的差异分析:

- H-005 现结论针对的是 **NO-HD 多 group 模型**:每地址=独立 group 主公钥,无共享 chaincode、**无兄弟泄露面**,结论「地址=公开公钥确定性派生,公开信息,不破红线、不增信任增量」。
- 引入非加固 HD 后,信任边界**实质变化**,两项残留:
  1. **链接性/隐私**:持父扩展公钥(xpub,含 chaincode)者可推算**全部**子公钥/子地址 → 所有派生地址对外**公开可关联**到同一父。NO-HD 多 group 模型无此关联。
  2. **灾难性单点**:**任一**子标量私钥一旦被重组/导出/旁路泄露(如未来导出功能、签名实现缺陷泄露有效标量)→ 父 + 全部兄弟沦陷。TSS 设计上**从不重组子私钥**(份额化签名),此弱点在「无任何一方持有子私钥」前提下不被触发;但前提依赖签名实现严谨 + 永不提供子私钥导出 + xpub 暴露面管控。
- → 非加固 HD 是相对当前 finalized NO-HD 模型的**安全边界增量**,**必须**纳 H-005 复核;`xpub/chaincode` 应否按「秘密」而非「公开」管控 = 复核关键(= Q3)。本调研只枚举,不裁定。

---

## I5 · 完整 BIP44(加固路径)在 TSS 下的可行性与代价

BIP44 路径 `m / 44' / coin' / account' / change / index`:前三级**加固**(`'`)。

- ckd **不足**:显式拒 `index≥2^31`;加固 HMAC 需父私钥,TSS 下不可得;加性 delta 技巧仅非加固成立。**完整 BIP44 用 ckd 不可实现。**
- 备选(均需另案评估,代价递增):
  - (a) **MPC 内计算加固 HMAC-SHA512**(对分片秘密做 SHA512 电路/MPC)—— tss-lib 无此件,工程代价极高,基本不现实。
  - (b) **每 BIP44 account = 一次独立 keygen/group**:不做真加固派生,以多 group 结构模拟 account 级隔离 —— **即现有 NO-HD 多 group 模型**。
  - (c) **仅支持非加固尾段**(account 级用独立 group,其下 `change/index` 走非加固 ckd 派生):BIP44 前缀非密码学加固、仅结构模拟。
- → 「完整 BIP44 加固」与 ckd 互斥;若硬需,落 (b)/(c),(b)/(c) 都回到「多 group」决策面 —— 与 Q1 强耦合。

---

## 决策分叉:Q1–Q3(待用户裁定,经 L1 下达;实现阻断)

> 沿用 `design-decision-multigroup-no-hd` 备忘已记录的三问;下表给出**每一裁定对实现方案的分叉**。

### Q1 · 派生模型

| 选项 | 实现分叉 | 关键代价 |
|---|---|---|
| Q1-a 维持 NO-HD 多 group(等于不接入 ckd) | 零代码;现终态即终态 | 与「新增地址派生范围」字面冲突,需用户确认是否撤销反转 |
| Q1-b 非加固「父扩展公钥 + 非加固 index 子地址簿」(ckd 可达上限) | 接 `NewLocalPartyWithKDD`+`UpdatePublicKeyAndAdjustBigXj`;新增主 chaincode 来源(**Q1-b-i** keygen 随机产+持久化扩 SaveData+既有分片迁移 / **Q1-b-ii** 由群公钥确定性派生,SaveData 不变);签名路径改造 + 双 E2E 守恒 | 中等;触签名主路径(高敏);chaincode 来源二选一 |
| Q1-c 完整 BIP44 加固 | ckd 不足 → 走 I5(b)/(c),回到多 group 结构;或 (a) MPC-HMAC(不现实) | 高 / 极高;实为另案 |

### Q2 · 派生地址持久化边界

| 选项 | 实现分叉 | 信任边界 |
|---|---|---|
| Q2-a 纯客户端派生(coord 不存子地址) | coorddb/coord-api **零改**;仅 cli/device 侧派生展示 | 维持 coord 信任最小化;延续「coord 仅轻度链感知」 |
| Q2-b coord 持久化子地址簿 | 新 goose 迁移(`00004…`,charter-10)+ GroupRecord/repo + api.md 端点 + 测试 | coord 链感知加深;纳 H-005(xpub/索引簿暴露面) |

### Q3 · 非加固兄弟泄露特性的 H-005 可接受性

| 选项 | 后续动作 |
|---|---|
| Q3-a 接受(TSS 永不重组子私钥为前提,记残留:xpub 链接性 + 永不提供子私钥导出) | H-005 复核记残留并放行;Q1-b 可推进 |
| Q3-b 不接受 / 要求加固 | 落 Q1-c(另案,大概率回多 group) |
| Q3-c 接受但 xpub/chaincode 按秘密管控(非公开) | 额外:xpub 存储/传输保密设计 + H-005 复核该管控 |

**强耦合:** Q3-b ⟹ Q1-c;Q1-a ⟹ Q2/Q3 不适用;Q1-b ⟺ 需 Q2 与 Q3 同裁。

---

## 调研结论(不含建议,仅事实)

1. ckd 仅非加固;`childPriv=parentPriv+IL`、`childPub=parentPub+IL·G`;多层坍缩为单加性标量;BIP32 xpub 序列化齐备。
2. tss-lib v3 原生支持非加固 HD 接线(`NewLocalPartyWithKDD`/`UpdatePublicKeyAndAdjustBigXj`);**本项目当前确未接线**,与 2026-05-18 NO-HD 裁定一致。
3. 接入面:`internal/addr` 零改;coorddb/coord-api 改动取决于 Q2;cli `mpcnet/device` + 主 chaincode 来源是核心改造点;charter-10 迁移纪律适用。
4. 非加固相对现 NO-HD finalized 模型有**真实信任边界增量**(链接性 + 灾难性单点),**必须**纳 H-005 复核(Q3)。
5. 完整 BIP44 加固与 ckd 互斥,属另案,且回退到多 group 决策面(Q1-c)。

**🛑 下一步受阻:** 不进入 proposal/implement、不拆/派实现 L3,直至用户 Q1–Q3 裁定经 L1 下达。
