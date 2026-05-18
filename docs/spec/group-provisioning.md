# 规约补全:keygen 后组/成员开通契约(DREV-001 D2-1)

> 性质:**设计补全规约**,补 `docs/design/` 权威留白,**不改 docs/design/**、不写实现。实施 **X-001**(coord 角色:组成员开通)、**D-001**(coord 数据库)据此。
> 关联权威:`docs/design/contract/api.md`、`docs/design/contract/protocol.md`、`docs/design/server/database.md`、`docs/design/server/server.md`、`docs/design/server/admin.md`、`docs/design/mcp/sdk.md`。
> 风格沿用 `docs/design/contract/api.md:5`:HTTPS + JSON,REST 语义;字节字段 base64;时间 RFC3339 UTC;版本前缀 `/v1`;错误码沿用 `api.md` C 表(`docs/design/contract/api.md:65-79`)。

## 0. 缺口与定位(DREV-001 D2-1 闭环)

`docs/design/contract/api.md:7-64`(A/B 全节)无任何组/成员注册端点;`docs/design/server/database.md:32-48` 定义 `groups`(`ecdsa_pubkey`/`group_pubkey`/`threshold_t`/`parties_n`)与 `group_members`(`identity_pubkey`/`status`),但:

- B1 成员鉴权依赖 `group_members.identity_pubkey`(`docs/design/contract/api.md:36`);
- A4 回传验签依赖 `groups.ecdsa_pubkey`(`docs/design/contract/api.md:30`);
- 二者**无填充路径** —— keygen / reshare 产物如何进入 coord 库未定义。

本规约定义该填充路径,**不引入管理员签发**(`docs/design/server/admin.md:6,23`:管理员不签发准入,自主式信任锚不变),信任锚仍为**钱包组组密钥**(`docs/design/server/server.md:130,133`)。

## 1. 角色与提交方

| 项 | 取值 |
|---|---|
| 谁开通 | **组的 keygen 参与成员集合**(联合行为)。**非**外部业务服务、**非** proposer、**非**管理员。 |
| 经哪个 API | 本规约新增 `/v1/groups*` 端点(独立于 `api.md` A/B 两区)。 |
| 提交载体 | 任一成员设备经其 `coord-client`(`docs/design/mcp/sdk.md:16`)代投;传输身份不参与正确性判定(负载自证),仅做限流。 |
| 鉴权前提 | 开通**先于** B1(`api.md:36` 的 `identity_pubkey` 此刻尚未入库)→ 不能用 B1 成员签名鉴权,改用**自证负载**(§6)。 |

> `identity_pubkey` 是**成员身份密钥**(设备生成,用于 B1 验签/心跳/审批,见 `api.md:36,53,49`),**与 TSS 分片无关**;开通是其首次进入 coord 的唯一路径。

## 2. 规范化负载(签名锚)

开通/更新负载须有**唯一规范化字节**,使设备签名与 coord 验签逐字节一致。沿用 **S-001**(`docs/spec/envelope-canonical.md`,批1 并行产出)对 `SigningRequest` 同款规范化纪律:确定字段序、base64 字节编码、整型/时间编码一致;签名覆盖范围显式标注。本规约负载(`GroupProvisioning` / `MembershipUpdate`)按同一规范化产生 `H(payload)`(SHA-256);S-001 未闭环前,实施方以 S-001 定稿为准,不得自创第二套编码。

## 3. 组开通契约(keygen 后)

### 3.1 `POST /v1/groups`

```
Req  GroupProvisioning {
       version       : uint,                 // 负载版本,见 §10 版本协商
       groupId       : string,               // 组自选;约束见 3.3
       ecdsaPubkey   : bytes(b64),           // keygen 主公钥(回传验签锚,api.md:30)
       groupPubkey   : bytes(b64),           // 能力令牌验签锚(protocol.md:73-82)
       thresholdT    : int, partiesN : int,  // t / n
       members       : [ { memberId, identityPubkey(b64) } ],   // |members| == partiesN
       createdAt     : RFC3339,
       groupSig      : bytes(b64),           // §6.1 组密钥(groupPubkey 对应私钥)对 H(payload) 的签名
       memberCoSigs  : [ { memberId, sig(b64) } ]   // §6.2 ≥ thresholdT 个,覆盖同一 H(payload)
     }
Resp 201 { groupId, status:"PROVISIONED" }
```

- coord 校验(§6)通过 → 单事务写 `groups` 一行 + `group_members` 每成员一行(`status="active"`)+ 一条开通审计事件(`request_events` 风格,actor=`coord`,见 `docs/design/server/database.md:80-88`)。
- 校验失败 → 见 §8。

### 3.2 字段对齐 `database.md`

| 负载字段 | 落 `groups`(`database.md:32-39`) | 落 `group_members`(`database.md:41-48`) |
|---|---|---|
| ecdsaPubkey | `ecdsa_pubkey` | |
| groupPubkey | `group_pubkey` | |
| thresholdT / partiesN | `threshold_t` / `parties_n` | |
| createdAt | `created_at`(=`updated_at`) | |
| members[].memberId | | `member_id`(PK 之一) |
| members[].identityPubkey | | `identity_pubkey` |
| (开通即) | | `status="active"` |

写读经 `group_members` 既有 PK `(group_id, member_id)` 与 `groups` PK `group_id`(`database.md:33,48`);无需新增索引。

### 3.3 `groupId` 防抢注

- coord 以**首个通过 §6 自证校验的注册**占用该 `groupId`,并锁定 `groupId ↔ ecdsaPubkey` 绑定。
- **建议(SHOULD)**:`groupId = base64url(SHA-256(ecdsaPubkey))` 截断 → 与主公钥密码学绑定;抢注者无对应组密钥无法产出有效 `groupSig`(§6.1),抢注不可行。形态仍为 `TEXT`,不与 `database.md:33` `uuid PK` 矛盾。
- 同 `groupId` 重复提交且字段一致 → 幂等返回原状态(§7);同 `groupId` 不同 `ecdsaPubkey` → `409 STATE_CONFLICT`。

## 4. 成员集更新契约(reshare 后)

### 4.1 `POST /v1/groups/{groupId}/membership`

```
Req  MembershipUpdate {
       version        : uint,
       groupId        : string,
       epoch          : int,                  // 单调递增 reshare 计数;coord 拒 epoch ≤ 当前
       ecdsaPubkeyAssert : bytes(b64),        // 必须 == 库中 ecdsa_pubkey(主公钥不变断言)
       groupPubkey    : bytes(b64),           // 允许 reshare 轮换;不变则同值
       thresholdT     : int, partiesN : int,  // reshare 可改 t/n
       addedMembers   : [ { memberId, identityPubkey(b64) } ],
       removedMemberIds : [ string ],
       updatedAt      : RFC3339,
       groupSig       : bytes(b64),           // §6.1 用**库中现存 groupPubkey 对应组密钥**签 H(payload)(授权本次轮换)
       memberCoSigs   : [ { memberId, sig(b64) } ]   // §6.2 ≥ thresholdT 个,且签名者须为库中 status="active" 成员
     }
Resp 200 { groupId, epoch, status:"UPDATED" }
```

- **主公钥不变断言**:`ecdsaPubkeyAssert != groups.ecdsa_pubkey` → `409 STATE_CONFLICT`(对齐 `docs/design/mcp/sdk.md:74`「主公钥不变,地址不变」)。
- 应用语义(单事务):`addedMembers` upsert 为 `status="active"`;`removedMemberIds` 置 `status="removed"`(对齐 `database.md:47` active/removed);更新 `groups.threshold_t/parties_n/group_pubkey/updated_at`;写一条更新审计事件。**不物理删除**成员行(保留审计与历史,呼应 `database.md:111-116` 长期保留取向)。
- `epoch` 单调:`epoch ≤ groups` 当前 epoch → `409 STATE_CONFLICT`(防回滚旧成员集重放)。实施方在 `groups` 增列承载 epoch(D-001 经迁移工具产出,**禁手改 schema**,`database.md:12`);本规约不改 `docs/design/server/database.md`,仅声明该列需求。

### 4.2 更新鉴权(剩余 ≥ t 成员授权)

`memberCoSigs` 的签名者 `memberId` 必须在**库中现存且 `status="active"`** 的成员集合内,且数量 ≥ `thresholdT`(取**更新前**的 t)。即:成员集变更须由**现有信任集合的剩余 ≥ t 成员**授权,新增/伪造成员不能自我授权入组(防恶意改组)。叠加 §6.1 **库中现存组密钥**对负载的签名(授权 groupPubkey 轮换本身),双重绑定。

## 5. 公开信息读回(relay 同步 / 幂等校验,支撑性)

### 5.1 `GET /v1/groups/{groupId}`

```
Resp 200 { groupId, ecdsaPubkey(b64), groupPubkey(b64), thresholdT, partiesN,
           epoch, members:[ { memberId, identityPubkey(b64), status } ] }
```

- 仅返回**公开信息**(`groups`/`group_members` 均为公开列,`database.md:32-48`、`admin.md:53` 标注只读);不含任何分片/私钥。
- 用途:① 提交方幂等预检;② coord 作为权威源向 relay 同步**信任组公钥集**(`docs/design/server/server.md:130`「relay 配置信任的组公钥集(或经 coord 同步)」)。
- 鉴权:本组成员 B1 签名(`api.md:36`)或运维只读;**非公网开放**,与 `admin.md:48` 暴露约束一致(具体暴露策略归 X-001/A-001,本规约不展开)。

## 6. 鉴权与防伪造模型(自主式,无管理员签发)

开通是**信任锚建立点**,必须防恶意注册劫持组。模型 = **组密钥签名 ⊕ ≥t 成员身份联署**,二者皆为自主式(`docs/design/server/server.md:133`),不引入服务/管理员签发(`docs/design/server/admin.md:6,23`)。

### 6.1 组密钥签名(自主式信任锚,非资金主私钥)

- 签名锚选定为**组密钥**(`groupPubkey` 对应私钥)—— 即 `docs/design/contract/protocol.md:79` 中签发 `CapToken` 的「钱包组组密钥」,设计既有的自主式信任锚角色。`groupSig = sign(组私钥, H(payload))`;coord 验 `verify(groupPubkey, H(payload), groupSig)`(开通时 `groupPubkey` 取自负载,见 §6.2 兜底;reshare 时取**库中现存** `group_pubkey`,授权本次轮换)。
- **不用资金主私钥(TSS 主私钥门限)对 `H(payload)` 裸签**:`docs/design/contract/protocol.md:25` 与 `docs/design/mcp/sdk.md:54` 规定进 MPC 前必须 `tx-decode` 重算链摘要断言 `==digest32`,否则拒签(WYSIWYS)。`H(payload)` 非链上交易、无链解码 → 对主私钥发起裸非交易门限签名会绕过/抵触该不变量。组密钥本就是设计中用于签发信任/能力工件的密钥,以其担纲既保持自主式、又不破坏 WYSIWYS。
- 若实施方另需「主公钥分片持有」强证明绑定 `ecdsaPubkey ↔ 真实 keygen 产物`,**必须**经 S-001(`docs/spec/envelope-canonical.md`)定义的专用 attestation 信封类型产生(纳入 `tx-decode`/digest 纪律),不得对资金主私钥发起裸 `H(payload)` 门限签名。本规约不强制该强证明:coord 无分片、本无法独立核验 keygen 内部,自主式模型下信任根即组密钥;`ecdsaPubkey` 真实性由组密钥签名(覆盖该字段)+ §6.2 ≥t 成员联署共同自证。

### 6.2 ≥t 成员身份联署

- 每个 `memberCoSigs[i].sig = sign(member_identity_priv, H(payload))`;coord 用**同一负载内声明的** `members[*].identityPubkey`(开通)或**库中 active 成员的** `identity_pubkey`(reshare)验签。
- 数量须 ≥ `thresholdT` 且签名者两两不同。把成员身份密钥与本次开通行为绑定 —— 单方不能事后注入任意 `identity_pubkey`(联署 + §6.1 双锁)。
- 开通(§3)首注册时 `groupPubkey` 与各 `identityPubkey` 均来自负载自描述 —— 此为自主式信任根的**首次确立**(TOFU 性质,与 `docs/design/server/server.md:133` 自主式锚一致):coord 无先验、无分片,本不可能独立核验 keygen 内部;`groupSig`(组密钥对全负载签名)+ ≥t 成员联署共同构成「该组自证其信任锚与成员集」。后续 reshare(§4)则改用**库中已确立**的组密钥 + active 成员授权,不再自描述。

### 6.3 边界

- 传输层鉴权(mTLS/api_key)**非必需**:负载自证,coord 只信密码学证明;但限流(`429`,§8)防滥用注册风暴。
- coord **不**解释 `groupPubkey` 用途、**不**校验链上;仅作公开锚存储与 relay 同步源。
- 与 `admin.md:6,23` 一致:管理员**不**参与开通/更新鉴权,无放行旁路。

## 7. 幂等与并发

- **幂等键**:开通 = `groupId`;成员集更新 = `(groupId, epoch)`。重复提交且负载等价 → 返回原状态(`201`/`200` 同体),不重复写、不报错;负载冲突 → `409`(§3.3 / §4.1)。沿用 `docs/design/contract/api.md:88` 写操作幂等约定。
- **并发**:coord 单节点 SQLite,无 `SELECT … FOR UPDATE`;开通/更新整个「校验 → 占用/比对 → 落库 + 审计」在单个 `BEGIN IMMEDIATE` 事务内,靠 SQLite 写锁串行化,与 `docs/design/server/database.md:106-109` §5 并发纪律一致。`epoch` 单调 + 事务内「读现 epoch → 校验严格大于 → 写」防并发双更新与回滚重放。
- **重放**:`H(payload)` 覆盖 `createdAt/updatedAt` 与 `epoch`;幂等键 + `epoch` 单调即抗重放,无需额外 nonce。

## 8. 错误码(复用 `api.md` C 表 `docs/design/contract/api.md:65-79`)

| HTTP | code | 本契约语义 |
|---|---|---|
| 400 | INVALID_ENVELOPE | 负载字段/规范化/`H(payload)` 校验失败;`|members| != partiesN`;`memberCoSigs` 不足 `thresholdT` |
| 401 | UNAUTHENTICATED | `groupSig` 验不过(非该组密钥所签);成员联署签名无效 |
| 403 | FORBIDDEN | reshare 联署者非库中 active 成员(§4.2) |
| 404 | NOT_FOUND | 成员集更新时 `groupId` 未开通 |
| 409 | STATE_CONFLICT | `groupId ↔ ecdsaPubkey` 绑定冲突;主公钥不变断言失败;`epoch ≤ 当前` |
| 429 | RATE_LIMITED | 开通/更新限流 |
| 503 | LOCKED | coord 库锁定(§9) |
| 5xx | INTERNAL | 服务端错误(可重试) |

错误体沿用 `api.md:79`:`{ error:{ code, message } }`,message 不泄敏感信息。

## 9. LOCKED 行为(对齐 `api.md` 锁定态)

`/v1/groups*` 为 coord 持久库写/读端点 → 适用 `docs/design/contract/api.md:81-84` 与 `docs/design/server/database.md:130-143` §7:库 LOCKED 时一律 `503 {code:LOCKED}`,fail-closed,不写不读不泄;客户端退避重试,不视为终态失败。开通/更新只能在 UNLOCKED 态受理。

## 10. 与 `protocol.md` 能力令牌关系

- 本规约登记的 `groupPubkey` **即** `docs/design/contract/protocol.md:73-82` §6 `CapToken.groupSig` 的验签锚(钱包组组密钥,自主式信任锚)。
- 开通是该锚进入 coord 的唯一路径;coord 据 §5.1 作为权威源向 relay 同步信任组公钥集(`docs/design/server/server.md:130`),relay 经 `ConnectionGater` 据此校验能力令牌(`docs/design/server/server.md:125-132` R4)。
- 版本协商:负载 `version` 与 `protocol.md:83-87` §7 / `api.md:91` D 同纪律 —— 不识别即拒(不降级猜测);不兼容变更升 `/v2`。
- coord 不签发 `CapToken`(由组密钥签发,`protocol.md:79`);本契约仅建立其验签锚,不越界。

## 11. 与权威文档对齐核对表

| 关注点 | 权威 file:line | 本规约处置 | 是否矛盾 |
|---|---|---|---|
| 主公钥回传验签锚来源 | `docs/design/contract/api.md:30` | §3.1 `ecdsaPubkey` → `groups.ecdsa_pubkey`(§3.2) | 否(补填充路径) |
| B1 成员验签锚来源 | `docs/design/contract/api.md:36` | §3.1 `members[].identityPubkey` → `group_members.identity_pubkey` | 否(补填充路径) |
| 幂等写约定 | `docs/design/contract/api.md:88` | §7 沿用,幂等键 `groupId`/`(groupId,epoch)` | 否 |
| 错误码 / LOCKED | `docs/design/contract/api.md:65-84` | §8/§9 复用 C 表 + LOCKED fail-closed | 否 |
| `groups`/`group_members` 列与 PK | `docs/design/server/database.md:32-48` | §3.2 字段映射,复用既有 PK,无新增索引 | 否 |
| `status` active/removed | `docs/design/server/database.md:47` | §4.1 reshare 置位,不物理删 | 否 |
| 禁手改 schema(epoch 增列) | `docs/design/server/database.md:12` | §4.1 声明列需求,迁移工具产(D-001) | 否(仅声明,不改 design) |
| SQLite 单写者并发纪律 | `docs/design/server/database.md:106-109` | §7 `BEGIN IMMEDIATE` 串行化 | 否 |
| 长期保留 / 不删 | `docs/design/server/database.md:111-116` | §4.1 移除=置 removed,保留行 | 否 |
| 能力令牌验签锚 = 组密钥 | `docs/design/contract/protocol.md:73-82` | §10 `groupPubkey` 即该锚,coord 不签发 | 否 |
| 版本协商纪律 | `docs/design/contract/protocol.md:83-87` | §10 不识别即拒,升 `/v2` | 否 |
| 自主式信任锚,不引服务签发 | `docs/design/server/server.md:130,133` | §6 组密钥签名 + ≥t 成员联署,无 admin | 否 |
| WYSIWYS:进 MPC 前必 tx-decode 断言 digest32 | `docs/design/contract/protocol.md:25`、`docs/design/mcp/sdk.md:54` | §6.1 用组密钥(非资金主私钥)签名,不对主私钥发起裸非交易门限签;强证明须经 S-001 attestation 信封 | 否(显式规避抵触) |
| relay 经 coord 同步信任组公钥集 | `docs/design/server/server.md:130` | §5.1 coord 作权威源读回 | 否(补支撑) |
| 管理员不签发准入/不管成员资格 | `docs/design/server/admin.md:6,23` | §1/§6.3 开通不涉管理员,无旁路 | 否 |
| reshare 主公钥/地址不变 | `docs/design/mcp/sdk.md:74` | §4.1 主公钥不变断言 → 失败 409 | 否 |
| 规范化字节(签名一致) | `docs/spec/envelope-canonical.md`(S-001) | §2 同款规范化纪律,不另立编码 | 否(依赖 S-001 定稿) |

## 12. 验收

- 含:keygen 后开通端点(§3)、reshare 成员集更新端点(§4)、公开信息读回(§5)、鉴权/防伪造模型(§6 自主式:组密钥签名 + ≥t 成员联署,不对资金主私钥裸签以保 WYSIWYS)、幂等与并发(§7)、与 `api.md`/`database.md`/`protocol.md` 对齐核对表(§11)。
- 引用 file:line 均经核对真实;不与 `docs/design/` 权威矛盾(§11 全列「否」)。
- 补充性质,显式标注:**DREV-001 D2-1 闭环,实施 X-001 / D-001 据此**。
- 仅新增本文件,未改 `docs/design/` 及其它文档。
