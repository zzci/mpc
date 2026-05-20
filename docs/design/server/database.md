# 数据库设计(server / coord)

> 仅 **coord 角色**有持久化;**relay 角色无状态、无数据库**;**mcp 端无数据库**(端上为加密 keystore,见 mcp/sdk.md)。
> 决策来源 `PLAN.md`、`server/server.md`(C 部分状态机/信封/TTL)。性质:开发文档,不写代码。

## 1. 选型(已定)

- **持久库:SQLite(单文件)**。承载待签列表/状态机/组与成员/审计/推送 token。
  - **架构影响**:SQLite 为嵌入式单文件,**非网络库** → coord 实质 **单节点**,原「HA 主备/多副本 + 共享持久化」不适用。改为 **单节点 + 文件备份**;如需容灾用 **Litestream/LiteFS** 做流式复制/只读副本。**与信任模型一致**:coord 宕机仅可用性降级,不影响资金安全(security.md §1 优先级)。
  - 并发:单写者;SQLite **WAL 模式** + `busy_timeout`;写事务用 `BEGIN IMMEDIATE` 串行化(无 `SELECT … FOR UPDATE`,见 §5)。
- **在线状态:内存 SQLite(`:memory:`)**。心跳在线集**无状态、不持久**,进程内 SQLite 库承载;重启即空(成员重连后心跳重建)。SQLite 无原生 TTL → 以 `expires_at` 列 + 周期清理实现过期语义。
- 迁移工具:版本化迁移(支持 sqlite 的 golang-migrate / goose),禁止手改 schema。

## 2. ER 概览

```
groups 1───* group_members
   │1
   │*
signing_requests 1───* request_approvals
   │1
   │*
request_events(审计/状态变更流水)

groups 1───* group_derived_addresses(AD-6,(group_id, child_index) 非硬化 HD 子地址)
admin_audit(独立,管理操作审计;追加写不可改)
presence(内存 SQLite `:memory:`,非持久:(group_id,member_id)→{relayPeerID,ts,expires_at};周期清理过期)
```

> **历史变更**:`push_tokens` 表于 00003 迁移删除(CFG-001 ruling 2026-05-19,
> coord 改单一固定 webhook,不再持推送 token 凭证;见 §7.3)。`chaincode`
> 列于 00004 迁移加入 `groups`(AD-3 commit-reveal HD 派生)。
> `group_derived_addresses` 于 00005 迁移加入(AD-6 lazy 持久化)。00006 加入
> `groups.ecdsa_pubkey` append-only 二层 trigger 守卫(DM-4 R7,§7.4)。

## 3. 表结构

### groups
| 列 | 类型 | 说明 |
|---|---|---|
| group_id | uuid PK | 钱包组 |
| ecdsa_pubkey | bytea | 主公钥(公开;用于回传前验签)。**append-only**:DM-4 R7,00006 trigger 守卫(§7.4) |
| threshold_t, parties_n | int | t/n |
| group_pubkey | bytea | 自主式信任锚:能力令牌验签公钥 |
| evm_address | text | 用户裁定 2026-05-18:记录派生地址(非单纯公钥)。ETH/BSC 共用(同 secp256k1+keccak256+EIP-55);开通时由 `internal/addr.ETHAddress(ecdsa_pubkey)` 派生持久化 |
| tron_address | text | TRON 地址(Base58Check, 0x41 前缀);开通时由 `internal/addr.TronAddress(ecdsa_pubkey)` 派生持久化 |
| epoch | int | 默认 0;reshare 单调递增(S-002 §4.1) |
| chaincode | BLOB(32) NULL | HD 派生 chaincode(AD-3 commit-reveal,00004);NULL=legacy 单地址组(F5) |
| created_at, updated_at | timestamptz | |

> **地址记录(用户裁定 2026-05-18)**:group 持久化派生出的实际链地址(evm/tron),不只存主公钥——"地址 ≠ 单纯公钥",地址=公钥经各链编码/哈希派生的结果。开通(S-002 ProvisionGroup)单事务内由 `internal/addr` 从 `ecdsa_pubkey` 派生并写入,确定性、幂等。**信任边界注**:此使 coord 轻度链感知(派生/存各链地址),但仅公开数据、不持密钥、不破资金安全/禁盲签/信任最小化;系以可用性换严格链无关的有意小放宽,经用户裁定。多地址仍由多 group 实现(无 BIP44/HD,见 PLAN/记忆)。

### group_members
| 列 | 类型 | 说明 |
|---|---|---|
| group_id | uuid FK | |
| member_id | text | 组内成员标识 |
| identity_pubkey | bytea | 成员身份公钥(验心跳/审批/上报签名) |
| status | text | active / removed(resharing 后) |
| PK | (group_id, member_id) | |

### signing_requests
| 列 | 类型 | 说明 |
|---|---|---|
| request_id | uuid PK | 全局唯一,**禁复用** |
| group_id | uuid FK | |
| chain | text | 不透明标签 |
| unsigned_tx | bytea | 不透明;**静态加密**(隐私) |
| digest32 | bytea(32) | 待签摘要 |
| proposer | text | |
| business_info | jsonb | 带外说明;**静态加密**(隐私) |
| meta_hash | bytea | H(business_info) |
| proposer_sig | bytea | 覆盖全字段(含 meta_hash) |
| status | text | PENDING/DISPATCHED/SIGNING/SIGNED/RETURNED/EXPIRED/REJECTED/FAILED |
| created_at | timestamptz | |
| expiry | timestamptz | 绝对过期时刻 |
| dispatched_at | timestamptz null | |
| signers | text[] null | 选定签名子集 |
| result_rsv | bytea null | 验签通过后的 {R,S,V} |
| fail_reason | text null | EXPIRED/FAILED 原因 |

### request_approvals
| 列 | 类型 | 说明 |
|---|---|---|
| request_id | uuid FK | |
| member_id | text | |
| decision | text | approved / rejected |
| sig | bytea | 成员身份密钥对 (request_id+decision) 的签名 |
| decided_at | timestamptz | |
| PK | (request_id, member_id) | |

### request_events(审计)
| 列 | 类型 | 说明 |
|---|---|---|
| id | bigserial PK | |
| request_id | uuid FK | |
| from_status, to_status | text | 状态迁移 |
| actor | text | external / member:<id> / coord |
| detail | jsonb | |
| at | timestamptz | 仅元数据;**不记** unsigned_tx/分片 |

### group_derived_addresses(AD-6,00005 迁移)
| 列 | 类型 | 说明 |
|---|---|---|
| group_id | text FK | 父 group(distributed-mpc R7 保证不可删) |
| child_index | int | 非硬化 HD 子索引,CHECK `0 ≤ child_index < 2^31` |
| evm_address | text | `internal/hd.DeriveChildAddress` 派生(ETH/BSC) |
| tron_address | text | TRON 同主公钥 + chaincode 派生 |
| child_pubkey | BLOB | 子公钥(可选;便于核对) |
| created_at | timestamptz | |
| PK | (group_id, child_index) | 幂等 upsert |

> **AD-6 lazy 写入**:仅当 B12 `RegisterDerivedAddress` 在 owning-member 上下文
> 触发时落盘;读路径(`/v1/groups/{groupId}/derived/{i}`)成员鉴权,外侧 A 面
> 不暴露(H-005 §7)。删除受 R7 间接保护(group 不可删)+ 应用层校验。

> **SQLite 类型映射**:上表 `bytea→BLOB`、`uuid/text→TEXT`、`jsonb→TEXT`(JSON 字符串,可选 JSON1 扩展)、`timestamptz→TEXT(RFC3339)或 INTEGER(unix ms)`、`bigserial→INTEGER PRIMARY KEY AUTOINCREMENT`、`text[]→TEXT(JSON 数组)`。SQLite 动态类型,约束以应用层 + CHECK 兜底。

## 4. 索引

- `signing_requests (group_id, status)`、`(status, expiry)` —— 法定人数评估与过期扫描热点。
- `signing_requests (expiry) WHERE status NOT IN (终态)` —— 过期定时器局部索引。
- `request_approvals (request_id)`;`request_events (request_id, at)`。
- `group_derived_addresses (group_id)`(00005)—— 按 group 列举子地址。

## 5. 状态机落库

- 状态迁移见 `server/server.md` C3。每次迁移:更新 `signing_requests.status`(+ `dispatched_at/signers/result_rsv/fail_reason`)并写一条 `request_events`,**同事务**;终态触发对外回报(api.md)。
- 并发(SQLite):无 `SELECT … FOR UPDATE`。法定人数发起在 `BEGIN IMMEDIATE` 事务内「读状态→校验仍为 PENDING→改 DISPATCHED」,靠 SQLite 写锁串行化防双发 START;WAL + `busy_timeout` 降低争用;coord 单节点天然无跨节点竞态。

## 6. 保留与清理(长期,支撑管理面历史)

- 管理面需查历史交易与会话(server/admin.md §1/§6)→ 终态请求与 `request_events` **长期保留**;保留期/归档策略**可配**(默认长期保留,超期转**归档库**而非删除)。
- 存储增长:`unsigned_tx`/`business_info` 体积随历史累积 → 归档分卷 + 可选冷存储;归档库结构同主库,管理面可跨主库+归档检索。
- 过期扫描:周期任务按局部索引取 `now ≥ expiry 且非终态` → 置 EXPIRED + 回报(与保留正交:过期是状态迁移,不等于删除)。
- 管理查询索引:为 admin 检索补 `signing_requests (group_id, created_at)`、`(proposer, created_at)`、`(status, created_at)`;`request_events (request_id, at)` 已有。

### admin_audit(管理操作审计)
| 列 | 类型 | 说明 |
|---|---|---|
| id | INTEGER PK AUTOINC | |
| admin_id | TEXT | 管理员标识 |
| action | TEXT | 查询/封禁/调配额/轮换PSK… |
| params | TEXT(JSON) | 操作参数(不含 secret 明文) |
| src_ip | TEXT | 来源 |
| at | TEXT | 时间 |

- **追加写、管理员不可改/删**(应用层强制 + 仅 append;敏感只读查询可选记录)。

## 7. 整库静态加密 + 默认锁定(防 DB 文件泄露)

- **绝不存**:分片、任何私钥、pnet PSK、能力令牌私钥 —— 明文或密文均不入库。
- **整库加密**:coord 持久库**全库页级加密**(SQLCipher 或等价的加密 SQLite;服务端 Go,允许 cgo)。不再只加密单列 —— **整个 `.db` 文件离机即一堆密文**。
- **密钥来源**:运营方**口令** → `Argon2id`(高内存参数)派生数据库密钥;**密钥仅驻内存**,**绝不落盘、不入配置/env/KMS**(入了即失去「防文件泄露」意义)。
- **默认锁定(LOCKED),fail-closed**:
  - 进程启动即 **LOCKED**:库未挂载、内存无密钥。
  - LOCKED 下 coord **拒绝一切**:不接受外部信封、不接受成员请求、不返回任何数据;API 一律 `503 LOCKED`(api.md);除「解锁」与最小健康检查外无任何端点可读到信息。
  - 仅 `admin-api` 解锁(口令)成功 → 派生密钥、挂载库 → **UNLOCKED**,正常服务。
  - 支持 **重锁(relock)**:管理员主动或可选空闲超时 → 清零内存密钥、卸载库 → 回 LOCKED。
- **口令尝试**:LOCKED 时无法写加密库 → 解锁尝试记进程日志/指标(非加密库);成功后补记 `admin_audit`;失败**限速 + 退避**防爆破。
- **运维警示(必读)**:口令仅内存、无托管 → **口令丢失则 coord 库不可恢复**。**但资金不受影响**(分片在成员设备;coord 库仅编排/历史)。须安全离线备份口令;可选(默认关)口令分片/多托管方案留待 P6。
- 仍仅存公开信息(组公钥)+ 编排元数据 + 历史;**加密不改变**「运营方/管理员在 UNLOCKED 运行态可见交易」这一既有取舍(security.md §8)——本机制专防**离线文件泄露**,非防在线运营方。
- 在线状态用内存 SQLite(无敏感数据,不加密、不持久)。

### 7.1 dev/test 加密禁用开关(用户裁定 2026-05-18) + 生产铁律护栏

- **动机**:E2E/集成测试中 coord 默认 LOCKED + 整库加密导致 `/healthz` 在解锁前不就绪、harness 死锁。允许 **dev/test 经参数禁用整库加密**(如 `coord.db.encryption.enable=false` / `--db-encryption=off`):禁用时**不派生密钥、不 LOCKED**,coord 启动即 UNLOCKED-等价、`/healthz` 立即就绪 —— E2E 据此跑通完整 MPC/coord/relay 流程,**不再用 harness 解锁时序 hack**。
- **生产铁律护栏(不可破、非用户可选、H-005 安全审查硬核对项)**:禁用加密**仅限非生产**。`node` 启动时:若加密被禁用 **且** 未显式置非生产标记(如 `env=dev|test` / 构建 tag / `ALLOW_INSECURE_DB=1` 显式确认)→ **fail-closed 致命退出**(loud fatal,拒启动)。生产/release 配置**必须** `encryption.enable=true`;P6 加固(H-004)与安全审查(H-005)**必须**核验:任何生产路径无法启用该禁用开关、默认即加密 LOCKED、禁用开关在生产配置/CI 发布门被拒。**误在生产禁用 = 资金编排数据明文落盘的安全红线**,护栏须使其不可能而非仅不推荐。
- 禁用仅影响**离线文件加密**这一防护;不改变其它信任边界/红线。dev 用临时库,禁用态产物不得用于任何真实部署。

### 7.2 push_tokens 撤销(CFG-001 ruling 2026-05-19)

原 `push_tokens` 表(平台 fcm/apns + token)在 CFG-001 用户裁定下退役:coord
不再持任何推送凭证,通知改为**单一固定 webhook**(`coord.notify`,见
`server/server.md`),由外部通知渠道翻译/投递 FCM/APNs。00003 迁移 DROP 此表,
B2 register-push-token 端点同步移除。Down 路径保留以支持 schema 回滚至 00002。

### 7.3 R7 append-only 二层守卫(DM-4,00006 迁移)

`groups.ecdsa_pubkey` 一旦写入即不可改写,实现 distributed-mpc.md R7
("group 公钥追加而非覆盖")。两层守卫:
1. **应用层**:`coorddb/repo.go:guardR7AppendOnly`(主拒绝点)。
2. **存储层**:00006 SQLite trigger
   `trg_groups_ecdsa_pubkey_append_only`(BEFORE UPDATE ... RAISE ABORT)+
   配套 DELETE 守卫,捕获任何绕过应用层的原始 SQL/写入。

两层同时拒,任一层故障另一层兜底。R7 violation 在 API 边返回 409
`STATE_CONFLICT`(参见 coord/errors.go)。

### 7.4 独立加密专测(用户裁定 2026-05-18,与 E2E-001 解耦)

加密正确性**不并入 E2E-001**(避免与终态门缠绕),由**独立专测**验证:(a) 启用加密时 `.db` 文件落盘**实为密文**(读原始字节断言非明文/无可识别表名)、(b) Argon2id 口令→解锁→UNLOCKED 正常读写、(c) **错误口令拒绝**、(d) relock 清零(zeroize)后回 LOCKED 不可读、(e) **生产护栏**:禁用开关在模拟生产标记下 fail-closed 拒启动。该专测属交付物与最终验收项。

## 8. 验收(P3)

- 状态机迁移与 `request_events` 同事务,异常回滚一致。
- coord 重启后由库恢复未过期请求并续(对应 server/server.md C10)。
- 过期局部索引扫描在大表下命中索引(EXPLAIN 验证)。
- **泄露 `.db` 文件无口令无法读出任何信息**(全库密文)。
- 启动默认 LOCKED;LOCKED 下任何 API 均 `503 LOCKED` 且不泄数据;错误口令限速;解锁后正常,重锁后再次拒服务。
- 错误口令不可恢复性提示明确;口令正确方可挂载。
