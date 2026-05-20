# 规约 SPEC-ENVELOPE-CANONICAL — 信封唯一规范化序列化

- **任务**：S-001（PLAN-002 批1，simple；DREV-001 D1 设计补全前置）
- **性质**：实施规约（C-001 据此编码）。**不改 docs/design/**（权威基线，只读引用）；docs/design/ 与本规约冲突项显式标注「实施时按本规约闭环 D1」。
- **基线引用提交**：与 docs/design/ 全树基线一致（DESIGN-REVIEW-001.md:4）。
- **日期**：2026-05-18
- **产出体**：L3/bkd，隶属 L2(57o81fv9)
- **状态**：✅ 已实施(2026-05)。规范化逻辑见 `internal/contract/envelope.go` 与 `internal/server/coord/provision_canonical.go`。

---

## 0. 问题与目标

DREV-001 D1（DESIGN-REVIEW-001.md:29-30,50）：信封 `SigningRequest` 的 `proposerSig` 覆盖全字段（docs/design/contract/protocol.md:22）、`metaHash=H(businessInfo)`（docs/design/contract/protocol.md:21），但**跨 JSON（api 提交，docs/design/contract/api.md:5,15-19）与 protobuf（线上 START，docs/design/contract/protocol.md:65-72）的规范化字节从未定义**，三方（外部业务服务 / coord / 设备）对同一逻辑信封无法得到逐字节一致的待签 / 待哈希输入 → 设备无法一致验签。

附加缺口：`businessInfo` 类型不一致（docs/design/contract/protocol.md:20 `bytes?` vs docs/design/contract/api.md:17 与 docs/design/server/server.md:188-190 结构化对象，DESIGN-REVIEW-001.md:30）；api.md A2 提交体缺 `version`/`createdAt` 且 `expiry` 用 RFC3339（docs/design/contract/api.md:17）与权威 int64 unix ms（docs/design/contract/protocol.md:18-19）冲突；database.md 时间存储两可（docs/design/server/database.md:98）。

**目标**：定义唯一规范化字节编码，使外部服务（提交 JSON）、coord（持久化/转发）、设备（接收 protobuf）三方对同一逻辑信封构造出**逐字节一致**的：
1. `proposerSig` 待签预映像（preimage）；
2. `metaHash` 的被哈希字节。

**核心方法**：签名与哈希**不基于任何线格式字节**（不签 protobuf 序列化字节、不签 JSON 文本），而是各方先把各自线格式解码为同一组**逻辑类型值**，再按本规约的确定性规则从逻辑值构造规范化字节。这同时规避 protobuf 序列化非确定性（字段序/varint/未知字段）与 JSON 文本非确定性（空白/键序/转义）。

不引入新密码学原语：哈希沿用 design 既有 SHA-256（docs/design/mcp/sdk.md:54 TRON `sha256(raw_data)` 已在设计原语集内）；`proposerSig` 沿用 design 既有 proposer 签名体系（docs/design/contract/protocol.md:22、docs/design/contract/api.md:11）。本规约仅定义"被签 / 被哈希的字节如何确定性构造"。

---

## 1. 逻辑字段集（权威：docs/design/contract/protocol.md:10-23）

| # | 字段 | 逻辑类型 | 权威出处 | 说明 |
|---|---|---|---|---|
| 1 | `version` | uint | protocol.md:11 | 信封版本，见 protocol.md:83-87 §7 |
| 2 | `requestId` | uuid | protocol.md:12 | 全局唯一，禁复用 |
| 3 | `groupId` | string | protocol.md:13 | |
| 4 | `chain` | string | protocol.md:14 | 不透明标签 |
| 5 | `unsignedTx` | bytes | protocol.md:15 | 不透明；设备 tx-decode 解析 |
| 6 | `digest32` | bytes(32) | protocol.md:16 | 固定 32 字节 |
| 7 | `proposer` | string | protocol.md:17 | |
| 8 | `createdAt` | int64（unix ms） | protocol.md:18 | 权威：int64 unix ms |
| 9 | `expiry` | int64（unix ms） | protocol.md:19 | 权威：绝对过期，int64 unix ms |
| 10 | `businessInfo` | 可选；结构化对象 | protocol.md:20 / api.md:17 / server.md:188-190 | 详见 §4，类型口径见 §4.1 |
| 11 | `metaHash` | bytes(32) | protocol.md:21 | `H(businessInfo)`；缺省 `H("")`，见 §4 |
| 12 | `proposerSig` | bytes | protocol.md:22 | 覆盖范围见 §3，**不入自身预映像** |

---

## 2. 规范化字节编码（canonical bytes）

### 2.1 预映像结构

规范化预映像 `P` 为以下顺序的字节串连接：

```
P = DOMAIN ‖ F(version) ‖ F(requestId) ‖ F(groupId) ‖ F(chain)
      ‖ F(unsignedTx) ‖ F(digest32) ‖ F(proposer)
      ‖ F(createdAt) ‖ F(expiry) ‖ F(metaHash)
```

- 字段顺序固定，与 docs/design/contract/protocol.md:10-23 声明顺序一致。
- `businessInfo`（#10）**不直接进入** `P`：其完整性经 `metaHash`（#11，已含于 `P`）传递绑定（依据见 §3）。
- `proposerSig`（#12）**不进入** `P`：它是对 `P` 的签名输出。

### 2.2 域分隔（DOMAIN）

```
DOMAIN = ASCII("TSS-ENVELOPE-CANONICAL-v1") ‖ 0x00
```

固定常量前缀，防止跨用途预映像碰撞；版本号 `v1` 随本规约升级而升（与信封 `version` 字段正交，后者标识业务信封版本，见 protocol.md:83-87）。

### 2.3 逐字段编码 `F(·)`

| 字段 | 类型类别 | 编码 `F` | 长度前缀 |
|---|---|---|---|
| `version` | 整数 | uint64 大端，固定 8 字节 | 无（结构定长） |
| `requestId` | UUID | RFC 4122 二进制 16 字节（由字符串解析；十六进制小写无关，取二进制） | 无（定长 16） |
| `groupId` | 字符串 | UTF-8 **NFC** 规范化字节 | `uint32` 大端长度前缀 |
| `chain` | 字符串 | UTF-8 **NFC** 规范化字节 | `uint32` 大端长度前缀 |
| `unsignedTx` | 字节 | 原始字节（不透明，不做任何变换） | `uint32` 大端长度前缀 |
| `digest32` | 字节 | 原始字节；**必须恰为 32 字节**（否则拒绝） | 无（定长 32） |
| `proposer` | 字符串 | UTF-8 **NFC** 规范化字节 | `uint32` 大端长度前缀 |
| `createdAt` | 整数（时间） | int64 大端，固定 8 字节；值为 unix 毫秒 | 无（定长 8） |
| `expiry` | 整数（时间） | int64 大端，固定 8 字节；值为 unix 毫秒 | 无（定长 8） |
| `metaHash` | 字节 | 原始字节；**必须恰为 32 字节**（SHA-256 输出，见 §4） | 无（定长 32） |

规则要点：

1. **整数**：一律大端定宽（`version`=uint64/8B，`createdAt`/`expiry`=int64/8B）。时间统一 unix **毫秒**（与权威 protocol.md:18-19 一致），不使用任何字符串时间表示。
2. **字符串**：先 Unicode **NFC** 归一化，再取 UTF-8 字节；变长字段加 `uint32` 大端长度前缀，防止变长字段串连歧义 / 拼接混淆攻击。
3. **字节**：原样字节。`digest32`/`metaHash` 结构定长 32，无需长度前缀但**必须校验长度**；`unsignedTx` 变长，加长度前缀。
4. **定长字段不加长度前缀**（`version`/`requestId`/`digest32`/`createdAt`/`expiry`/`metaHash`），其长度由本规约结构固定且强制校验，无歧义。
5. 编码失败（如 `digest32`≠32B、`version` 超 uint64、时间非整数）→ 构造失败 → 验签方按 docs/design/contract/protocol.md:25「任一不过即拒签」拒绝。

### 2.4 待签摘要与签名

```
proposerSig = ProposerSign( SHA-256(P) )
```

- 先对预映像 `P` 取 SHA-256 得 32 字节摘要，再由 proposer 私钥签名。
- 签名算法**不新增**，沿用 design 既有 proposer 签名体系（protocol.md:22、api.md:11）。docs/design/contract 未显式标注 proposer 签名曲线；本规约按项目既有 secp256k1 体系约定（与项目 ECDSA/secp256k1 主链一致），并作为 **D1 实施闭环项**标注（docs/design/ 未写明，实施 C-001 时按本规约固定为 secp256k1 ECDSA over SHA-256 摘要）。
- 验签方：以同一规则重建 `P`、取 SHA-256、用 proposer 公钥验签（coord 持组内既知 proposer 公钥；设备依 protocol.md:25 验证）。

---

## 3. `proposerSig` 覆盖范围

`proposerSig` 通过预映像 `P`（§2.1）覆盖：

`version`、`requestId`、`groupId`、`chain`、`unsignedTx`、`digest32`、`proposer`、`createdAt`、`expiry`、`metaHash`。

- 与 docs/design/contract/protocol.md:22「覆盖以上全部字段(含 version/metaHash)」一致：`version` 与 `metaHash` 均显式在 `P` 内。
- `businessInfo` **不直接列入** `P`，其完整性由 `metaHash=H(businessInfo)`（§4）**传递绑定**——`metaHash` 在 `P` 内且受 `proposerSig` 保护，篡改 `businessInfo` 必使 `H(businessInfo)≠metaHash`，被 docs/design/contract/protocol.md:25 的设备前置校验拒签。
- **设计取舍说明（D1 实施闭环标注）**：docs/design/contract/protocol.md:22 措辞「全部字段」未明确 `businessInfo` 是否另需进预映像。本规约采"经 `metaHash` 间接绑定、不入预映像"——标准的「对变长可选部分签其哈希」模式，使预映像不依赖 `businessInfo` 规范化、降低三方实现负担，且不弱化完整性（与 DESIGN-REVIEW-001.md:50 结论一致：businessInfo 完整性由 metaHash∈proposerSig 保证）。此为对 design 措辞的实施细化，非矛盾；标注为「实施时按本规约闭环 D1」。

---

## 4. `metaHash` 与 `businessInfo` 规范化

### 4.1 `businessInfo` 类型口径统一（D1 实施闭环标注）

冲突：docs/design/contract/protocol.md:20 `businessInfo: bytes?`；docs/design/contract/api.md:17 与 docs/design/server/server.md:188-190 为结构化对象 `{ title, summary, items[], refs{...}, requester, memo, displayHints }`；docs/design/server/database.md:59 `business_info jsonb`。

**本规约统一**：`businessInfo` 逻辑上为**结构化对象**；其**规范化字节形态**唯一定义为对该对象施加 **RFC 8785 JSON Canonicalization Scheme（JCS）** 得到的 UTF-8 字节，记为 `BI_bytes`。

- 协议层（protocol.md:20 的 `bytes?`）所传 / 持久化的 `businessInfo` 字节**就是** `BI_bytes`（JCS 规范化字节），消解 `bytes?` 与「结构化对象」口径分裂。
- 外部业务服务以 JSON 对象提交（api.md:17）；coord 在 A2 受理时一次性 JCS 规范化为 `BI_bytes` 后持久化与转发；线上 protobuf START（protocol.md:65-72）的 `businessInfo` 字段承载 `BI_bytes`。三方 `H` 输入因此逐字节一致。
- 标注「实施时按本规约闭环 D1」（docs/design/ 不改；C-001 与 api 层实施按本规约）。

### 4.2 `metaHash` 算法

```
businessInfo 存在：metaHash = SHA-256( BI_bytes )         // BI_bytes = JCS(businessInfo 对象)
businessInfo 缺省：metaHash = SHA-256( <空字节串> )
```

- **哈希函数**：SHA-256（明确）。不新增原语——SHA-256 已在 design 原语集内（docs/design/mcp/sdk.md:54）。`metaHash` 固定 32 字节。
- **"缺省"精确定义**：`businessInfo` 字段在提交体中**缺省（未出现）或显式 null**，二者等价处理为缺省；`metaHash = SHA-256("")`，即对长度为 0 的字节串取 SHA-256，定值：

  ```
  e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855
  ```

  与 docs/design/contract/protocol.md:21「businessInfo 缺省时 H("")」一致；本规约明确 `H=SHA-256` 且空输入为零长度字节串（非空 JSON 对象 `{}`，亦非字符串字面量 `""`）。
- 验签方按 docs/design/contract/protocol.md:25「`metaHash==H(businessInfo)`」校验：以收到的 `businessInfo`（缺省或 `BI_bytes`）按上式重算并比对。

### 4.3 JCS 规范化要点（RFC 8785）

`BI_bytes` 由对 `businessInfo` 对象施加 RFC 8785 得到，确定性来源：对象成员按 UTF-16 码元序排序、最短数字表示、固定转义、UTF-8 输出、无多余空白。

- 仅作用于 `businessInfo`（唯一结构化嵌套字段）；信封其余字段不经 JCS，按 §2.3 编码。
- `displayHints`/`refs`/`items[]` 等嵌套结构随对象整体 JCS 化，无需逐字段特例。
- RFC 8785 为编码规范，非密码学原语，不违反「不引入新原语」。

---

## 5. api.md A2 提交体补全项（写入本规约；不改 docs/design/）

针对 docs/design/contract/api.md:13-21 A2 `POST /v1/requests`，DREV-001 D1（DESIGN-REVIEW-001.md:29）要求补全。实施时 api 层按本规约，标注「DREV-001 D1 闭环」：

| 项 | 现状（docs/design/contract/api.md） | 本规约要求 |
|---|---|---|
| `version` | 缺（api.md:16-17 无） | **必填**，整数；对应逻辑 `version`（§1 #1） |
| `createdAt` | 缺（api.md:16-17 无） | **必填**，整数 unix **毫秒**（与 protocol.md:18 一致） |
| `expiry` 编码 | `expiry(RFC3339)`（api.md:17） | 改为整数 unix **毫秒**（JSON 中为数字；与权威 protocol.md:19 一致，**以本规约为准**，RFC3339 弃用） |
| 全局时间风格 | api.md:5「时间 RFC3339 UTC」 | 信封时间字段（`createdAt`/`expiry`）一律 int64 unix ms 数字；api.md:5 的 RFC3339 通则**不适用于信封时间字段**（以本规约 / protocol.md:18-19 权威为准） |
| 字节字段 | api.md:5 base64、api.md:16-17 `(b64)` | 保持 base64 传输；**规范化预映像用解码后原始字节**（§2.3），故 base64 仅传输层表示，不影响一致性 |
| `businessInfo` | `businessInfo?{...}`（api.md:17） | 结构化对象；coord 受理时按 §4.1 JCS 规范化为 `BI_bytes`，再算 `metaHash`、校验 `metaHash==SHA-256(BI_bytes)` |

补全后 A2 `Req` 逻辑字段（实施口径，docs/design/ 不改）：

```
Req { version, requestId?, groupId, chain, unsignedTx(b64), digest32(b64),
      proposer, createdAt(int64 ms), expiry(int64 ms),
      businessInfo?{...}, metaHash(b64), proposerSig(b64) }
```

> 说明：`requestId` 由谁生成不属 D1 范围（api.md:18-20 幂等语义按 requestId 全局唯一禁复用），本规约不变更其语义，仅纳入预映像（§2.1）。`proposerSig`/`metaHash` 的校验失败仍归 `400 INVALID_ENVELOPE`（api.md:21,69），语义不变。

---

## 6. 与 docs/design/contract/protocol.md §1 字段集对齐核对表

| protocol.md:10-23 字段 | 本规约处理 | 一致性 |
|---|---|---|
| `version`（:11） | §2.3 uint64/8B；入 `P` | ✅ 一致（protocol.md:22 要求含 version） |
| `requestId`（:12） | §2.3 UUID 16B；入 `P` | ✅ 一致 |
| `groupId`（:13） | §2.3 NFC+len 前缀；入 `P` | ✅ 一致 |
| `chain`（:14） | §2.3 NFC+len 前缀；入 `P` | ✅ 一致 |
| `unsignedTx`（:15） | §2.3 原始字节+len 前缀；入 `P` | ✅ 一致（不透明，不变换） |
| `digest32`（:16） | §2.3 定长 32B；入 `P` | ✅ 一致 |
| `proposer`（:17） | §2.3 NFC+len 前缀；入 `P` | ✅ 一致 |
| `createdAt`（:18 int64 unix ms） | §2.3 int64/8B unix ms；入 `P` | ✅ 一致（权威口径） |
| `expiry`（:19 int64 unix ms） | §2.3 int64/8B unix ms；入 `P` | ✅ 一致（权威口径） |
| `businessInfo`（:20 `bytes?`） | §4.1 统一为 JCS `BI_bytes`；**不入 `P`**，经 metaHash 绑定 | ⚠ 实施闭环 D1：口径细化（bytes?=JCS 字节），非矛盾（§4.1） |
| `metaHash`（:21 `H(businessInfo)`，缺省 `H("")`） | §4.2 `SHA-256`；缺省=SHA-256(空字节串)；入 `P` | ⚠ 实施闭环 D1：明确 H=SHA-256、空输入定义（protocol.md 未指定哈希函数） |
| `proposerSig`（:22 覆盖全字段含 version/metaHash） | §3 覆盖 `P`（含 version/metaHash）；businessInfo 经 metaHash 传递绑定 | ⚠ 实施闭环 D1：businessInfo 经 metaHash 间接绑定（§3 取舍说明），与「覆盖全字段」意图一致 |
| 设备前置校验（:25） | 验 proposerSig（重建 `P`）、metaHash==SHA-256(businessInfo)、now<expiry、tx-decode==digest32 | ✅ 不改校验语义，仅明确字节构造 |
| START 信封（:65-72） | protobuf 承载逻辑字段；`businessInfo`=`BI_bytes`；验签按本规约重建 `P` | ✅ 一致（解码为逻辑值后构造 `P`） |
| 版本协商（:83-87） | DOMAIN 的 `v1` 与信封 `version` 正交（§2.2） | ✅ 不冲突 |

无与 design 权威矛盾项；⚠ 三项为 docs/design/ 未写明处的实施细化，已显式标注「实施时按本规约闭环 D1」（DESIGN-REVIEW-001.md:19,72 允许在实施前置规约阶段闭环）。

---

## 7. database.md 时间存储建议

针对 docs/design/server/database.md:98（`timestamptz→TEXT(RFC3339) 或 INTEGER(unix ms)` 两可）与 docs/design/server/database.md:63-64（`created_at`/`expiry timestamptz`）：

- **建议（实施口径，docs/design/ 不改）**：`signing_requests.created_at` / `signing_requests.expiry` 在 SQLite 落库为 **`INTEGER`（unix 毫秒）**，与本规约逻辑类型（§1 #8/#9）和权威 protocol.md:18-19 一致。
- 理由：信封时间已统一 int64 unix ms；存为 INTEGER 使「持久值 ↔ 预映像构造值」零转换，消除 RFC3339↔ms 往返带来的 `proposerSig` 重验不一致风险；并直接服务 docs/design/server/database.md:103 `(status, expiry)` 过期局部索引（整数比较）。
- 其余 `timestamptz` 列（`groups.created_at` 等纯审计/管理元数据，不入信封预映像）不受本规约约束，按 database.md:98 既有两可由实施选择；本规约仅约束**进入 `proposerSig`/`metaHash` 的信封时间字段**。
- 标注「实施时按本规约闭环 D1」——database.md:98 两可表述对信封时间字段收敛为 INTEGER(unix ms)。

---

## 8. JSON ↔ protobuf 一致性论证

设外部服务 JSON 提交为 `J`，线上 protobuf START 信封为 `R`（protocol.md:65-72），三方为：外部服务（产 `J`）、coord（受 `J`、转 `R`）、设备（受 `R`）。

1. **解码到统一逻辑值**：`J` 与 `R` 各自按其线格式解码为 §1 的同一组逻辑值——base64 字符串与 protobuf bytes 解码为**同一原始字节**；JSON 数字与 protobuf int64/uint 解码为**同一整数**；UUID 字符串与其二进制为**同一 16 字节**；`businessInfo` JSON 对象由 coord 一次性 JCS 化为 `BI_bytes`，protobuf 承载同一 `BI_bytes`（§4.1）。
2. **从逻辑值确定性构造**：三方均按 §2.3 规则从逻辑值构造 `P`，不读取任何线格式原始字节作为签名输入：
   - 不签 protobuf 序列化字节 → 规避字段序/varint/未知字段/默认值省略等 protobuf 非确定性；
   - 不签 JSON 文本 → 规避空白/键序/转义/数字格式等 JSON 文本非确定性；
   - 唯一的结构化嵌套（`businessInfo`）由 RFC 8785 JCS 收敛为唯一字节（§4.3）。
3. **结论**：相同逻辑信封 → 相同逻辑值 → 相同 `P` → 相同 `SHA-256(P)` → `proposerSig` 三方可一致验；相同 `businessInfo` → 相同 `BI_bytes` → 相同 `metaHash`。设备据 docs/design/contract/protocol.md:25 的 `proposerSig`/`metaHash` 校验对 JSON 源与 protobuf 源得到一致结果，D1 闭环。

---

## 9. 验收对照（任务验收项）

| 验收项 | 本规约位置 |
|---|---|
| 字段顺序表 | §1（逻辑集）、§2.1（预映像顺序） |
| 逐字段确定性编码 | §2.3 |
| `proposerSig` 覆盖范围 | §3 |
| `metaHash` 定义（含 businessInfo 缺省） | §4.2（含 SHA-256("") 定值）、§4.1（类型口径）、§4.3（JCS） |
| JSON↔protobuf 一致性论证 | §8 |
| api.md A2 补全项 | §5 |
| 与 protocol.md/database.md 对齐核对表 | §6（protocol.md）、§7（database.md） |
| design 引用真实可核、不矛盾、冲突显式标注 | 全文 file:行引用；⚠ 项标注「实施时按本规约闭环 D1」 |
| 不引入新密码学原语 | §0、§2.4、§4.2（SHA-256 沿用 docs/design/mcp/sdk.md:54；proposer 签名沿用 protocol.md:22） |
