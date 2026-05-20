# 安全审查记录(H-005 / PLAN-002 收尾)

> 范围:对照已合并代码逐条核对 `docs/design/security.md` 六条不可妥协红线 + 威胁模型 A + H-004 残留 + G-001 链感知信任边界 + DB 整库加密锁定。
> 权威依据:`docs/design/security.md`、`docs/design/PLAN.md` §3/§5、`docs/design/contract/protocol.md`、`internal/{mpc,txdecode,transport,server/*}`、`cmd/node/*`。
> 性质:审查记录,不改代码、不改 docs/design/。引证为本审查分支 HEAD(`3364126`)实测。

---

## 1. 六条红线逐条核对

### 红线 1 —— 禁止盲签 / WYSIWYS(进 MPC 前必经 tx-decode 重算摘要 ==digest32 + 人审)

**合规。**

- 设备签名流强制顺序在 `internal/mobileapi/sign.go:70-191`:信封验签(`:95`)→ 强制正 expiry 且未过期(`:106-113`)→ **tx-decode 安全闸**(`:118-122`)→ `OnDecoded` 人审(`:145`)→ 等待 host 决策(`:149-157`)→ 进 MPC 前再校验未过期(`:160-163`)→ MPC 签名(`:173`)→ 出结果前再校验(`:186-189`)。
- 任一安全类失败 `OnError` 硬拒、永不进 MPC,且 `OnDecoded` 永不触发(`internal/mobileapi/sign.go:22-25` 顺序契约 + `:119-122`)。
- 「重算摘要 == digest32」断言由框架而非解码器执行:`internal/txdecode/txdecode.go:64-101`,核心绑定为常量时间比较 `subtle.ConstantTimeCompare(recomputed[:], req.Digest32)`(`:95-97`),不等即 `ErrDigestMismatch` 硬拒。
- coord 不在 signers 内、不参与 MPC(`docs/design/contract/protocol.md:72`;见红线对应代码红线 3 项)。

结论:设备侧「验证后才签」不变量在代码层强制,无绕过路径。

### 红线 2 —— 解码-摘要双重绑定(tx-decode bug 退化为拒签而非误签)

**合规。**

- `internal/txdecode/txdecode.go:55-91`:`Recompute` 解析失败 / 无 facts 一律 `ErrDecodeRejected` 硬拒、不外泄任何 facts(`:84-91`);仅当摘要绑定成功才返回 `Result`(`:99-100`)。
- 可插拔覆盖仍受同一断言约束:`ChainDecoder` 接口契约(`internal/txdecode/txdecode.go:11-24`)+ `Register`(`:46-53`)说明覆盖实现的重算摘要仍由框架 `Decode` 断言 `==digest32`,覆盖无法绕过绑定。
- 未识别调用降级为「原始 + 谨慎审核」而非臆造标签:`internal/txdecode/abcheck.go:30-73`(A/B 仅声明式核对,B 不匹配是告警非拒签;仅摘要不匹配硬拒),与 `docs/design/PLAN.md §3` 信任边界一致。

结论:解码错误的最坏后果是拒签,A 区权威由密码学绑定兜底,不依赖解码器无 bug。

### 红线 3 —— 零信任 relay(Noise 端到端,不在 relay 终结)

**合规。**

- 传输栈 `internal/transport/transport.go:60-90`:**仅 Noise(无 TLS)**、pnet PSK、yamux、TCP(`NoTransports`+显式 TCP,QUIC 有意不启用);对端身份 = peerID = 公钥(`:28-29`)。
- relay 角色不读 coord、不 import coord:`internal/server/relay/relay.go:32`(“No coord field is read”)、`internal/server/relay/doc.go:6`(“本包绝不 import internal/server/coord”);实测 relay 包无 `internal/server/coord` / `internal/mpc` / `internal/txdecode` 生产引用。
- relay 仅 circuit-relay v2 哑管道转加密流:`internal/server/relay/relay.go:80-109`(pnet+Noise host + circuitv2 ACL/Resources),Noise 两 party 端到端不在 relay 终结。
- relay 之上额外成员认证:`senderAuth` 绑定 `(sessionId,round,payload)` 到成员身份(`internal/transport/session.go:154-161` 签、`:216-227` 验);跨 `sessionId` 消息无条件丢弃(`internal/contract/session.go:6-7`),防重放/串话。

结论:relay 仅得密文,无法还原 payload / 伪造 from / 持分片,最坏为丢弃/延迟/审查。

### 红线 4 —— 信任最小化 coord(无分片、不参与 MPC)

**合规。**

- `internal/server/coord/coord.go:16-19`:“It never holds a share and never runs MPC”;实测 coord 包不 import `internal/mpc`(`internal/server/coord/doc.go:10` 明确明文信封路径与 relay 隔离;`callback.go` 仅 import `btcec` 做验签,非 MPC 参与)。
- 结果免信任校验:`internal/server/coord/callback.go:95-120` `verifyRSV` 用组公钥 `ecdsa.RecoverCompact` 还原并要求 `== groups.ecdsa_pubkey`,失败则 FAILED、不外泄结果;`reportTerminal` 回传前再断言一次该闸(`:148-152`,纵深防御)。
- coord 不解 `unsignedTx`:`internal/server/coord/envelope.go:20-21`(“coord never parses unsignedTx — device-side tx-decode does”),仅校验 `metaHash`/`proposerSig`/`expiry`/`groupId`(`:74-84`、`:38-45`),与 `docs/design/contract/protocol.md:27` 一致。

结论:coord 偷不走钱、伪造不了审批、令不了各方签不同内容、改不动 businessInfo;符合「信任最小化(资金不可信)」定位。

### 红线 5 —— PreParams 端上生成(含 Paillier 私钥,严禁后端预生成/下发)

**合规。**

- `internal/mpc/keygen.go:36-52`:`KeygenConfig.PreParams` 显式标注 RED LINE —— 生产必须各设备本地生成,后端/协调者预生成下发即破坏自托管模型;该参数仅供调用方传入「自己生成」的值,不是服务端下发钩子。
- `resolvePreParams`(`internal/mpc/keygen.go:164-185`):`PreParams` 为 nil 时各方本地 `keygen.GeneratePreParamsWithContext` 生成,无任何 env/config/API 注入路径。
- reshare 同红线:`internal/mpc/recover.go:34-39`。
- 移动层亦标注红线并后台生成不阻塞 UI:`internal/mobileapi/keygen.go:49-50`、`:70`(`OnProgress("preparams")`)。

结论:无服务端预生成/下发 PreParams 的代码路径,Paillier 私钥不离设备。

### 红线 6 —— TTL 一等公民(coord 与设备双侧校验 expiry;requestId 禁复用;ts+nonce 防重放)

**合规。**

- 设备侧三重校验:`internal/mobileapi/sign.go:106-113`(拒绝无正 expiry 信封 —— 堵住 `NotExpired` 把非正值当「永不过期」的绕过)、`:160-163`(进 MPC 前)、`:186-189`(出结果前),并以 `decisionDeadline` 约束人审等待(`:193-221`)。
- coord 侧:`internal/server/coord/ttl.go:9-17` `isExpired` 带 skew 保守判定,`:32-37` `dispatchDeadline` 钳到剩余 TTL;`internal/server/coord/envelope.go:38-39` `expiry>createdAt` 强制。
- requestId 全局唯一禁复用:schema `internal/server/coorddb/migrations/00001_init.sql:30`(`request_id TEXT PRIMARY KEY`,注释「禁复用」)。
- ts+nonce 防重放:`internal/server/coord/auth.go:79`(`verifyMemberAuth`)+ `nonceCache.use`(带 `now`/`expiry` 一次性)。

> **FIX-001(2026-05-18,nonceCache 注入时钟一致性)**:原 `nonceCache.use` 内部直读 `time.Now()` 进行过期淘汰与重放窗口判定,而过期时间戳 `expiry` 由注入时钟 `c.clock` 派生,二者时钟源不一致。修复:`verifyMemberAuth` 取一次 `tnow := c.clock.Now()`,同时传 `now=tnow` 与 `expiry=tnow.Add(memberAuthWindow)` 进 `use`,淘汰与重放判定改用注入 `now`,遵循 `clock.go` 「所有取时经 Clock」纪律。**生产语义零变更**:线上 `c.clock == systemClock{}`(`time.Now().UTC()`),修复前 `expiry` 与淘汰 `now` 已同源于系统时钟,改动仅消除测试确定性时钟下的 store/evict 时钟错配;重放/nonce 安全语义(单次性、5min 窗口、域分隔)未改。消除 `TestMemberAuthReplay` 在真实墙钟 > 2026-05-18T12:05:00Z 后的确定性误判(测试时钟固定 12:00:00Z,expiry=12:05:00Z,墙钟越过后新写 nonce 被真实钟提前淘汰致重放漏检)。全树 `-race` 绿,coord 安全用例全绿。

结论:expiry/requestId/nonce 在 coord 与设备双侧均强制,符合一等公民定位。

**六红线小结:全部合规,无降级、无绕过路径。**

---

## 2. 威胁模型 A 复议(relay 访问控制:pnet + CapToken + 配额;H-003 加固)

威胁模型 A 定义(`docs/design/security.md` §5「未授权第三方」行、`docs/design/contract/protocol.md:74-83`):仅阻止未授权接入,不对 relay 隐藏分组元数据。

- **三层访问控制**(`internal/server/relay/relay.go:34-44`):
  - L1 pnet PSK —— libp2p 自身强制,无 swarm key 无法说协议(`relay.go:80-89` `libp2p.PrivateNetwork(psk)`)。
  - L2 CapToken —— 安全连接建立后经 `CapProtocolID` 出示,在 circuit-relay ACL / rendezvous handler 处校验(`relay.go:91-105`);`ConnectionGater` 看不到应用层 token,故 token 强制点有意置于 ACL/handler 而非 gater,在 libp2p API 内实现 R4 意图。
  - L3 配额 —— per-token/per-group 预约上限(`internal/server/relay/authz.go:14-37`)+ circuitv2 per-circuit Data/Duration 限制。
- **CapToken 校验**:`internal/server/relay/captoken.go:92-153` —— `capTokenDigest` 域分隔前像 + `verifyCapToken` 校验 scope、有效窗口(带 skew 容差)、`groupSig` 对受信组公钥锚验签;groupSig 不验任何受信锚即 `ErrUntrustedToken`。短 TTL 自然失效即撤销(`protocol.md:80`)。
- **H-003 加固复核**(`docs/task/H-003.md` GREEN):rendezvous anti-DoS 上限(maxRegisterAddrs=16 / maxNamespacesPerPeer=8)封堵单恶意成员内存耗尽;circuit 连接数/带宽维度由 circuitv2 `MaxCircuits/MaxReservations` 资源界定,去推测化(不加泄漏型计数器),`authz.go:19-37` 注释明确该取舍;零信任不变量(不 import coord、只转密文)保持;N-002 测试全绿。

结论:威胁模型 A 对策完整且与设计一致;H-003 加固未引入信任增量或泄漏面,不破零信任不变量。**威胁模型 A 合规。**

---

## 3. H-004 残留核对(cmd/node 未接 TSSNODE_ADMIN__ 强化 env)

**事实**:
- `cmd/node/admin.go:24-67` 仅从 env 读 `TSSNODE_ADMIN__LISTEN/READ_TOKEN/CONTROL_TOKEN`,构造的 `admin.Config` **未填** `AllowedCIDRs`,且未经 `WithTLS` / StrongAuth seam 注线。
- admin 模块本身加固完整:`internal/server/admin/config.go:24-39`(Config 含 `AllowedCIDRs`)、`internal/server/admin/server.go:34-36`(`strongAuth==nil`→bearer-only 软校验、`tlsCfg==nil`→明文、`allowNets` 空→无 IP allowlist)。
- 软校验非静默:`internal/server/admin/server.go:190-208` `logHardeningPosture` 对每个留给部署的闭环发响亮 Warn(无强鉴权 / 无 IP allowlist / 非 loopback 且无 allowlist 无 TLS = 公网暴露风险)。
- token 未配置时 admin-api 不启动、无不安全默认:`cmd/node/admin.go:39-43`(coord 仍跑,store 保持 LOCKED,A/B API fail-close 503,直到运维配置 token 并解锁)。

**安全影响评估**:
- admin-api 默认 `127.0.0.1:9091`(`cmd/node/admin.go:28`,非公网),且无 token 即不启动 —— **fail-closed,不存在默认放行的攻击面**。
- 残留风险:运维若把 admin listen 改到非 loopback 且未经外部网络层(VPN/防火墙/反代 mTLS)收口,则 IP allowlist / 强鉴权 / TLS 三个 seam 因 cmd/node 未接线而处于「软校验 + 响亮告警」态,强鉴权(mTLS/OIDC+2FA)纯靠部署在前端终结。
- 缓解已在位:读/控 token 分离(`admin/config.go:29-34`,≥16B,`config.go:52-55`)、LOCKED fail-closed、`admin_audit` 结构性不可篡改 + principal 归属(H-004 GREEN 记录)、`logHardeningPosture` 使残留责任在进程日志可审计。
- 性质判定:**部署收口项(P6 集成 follow-up),非资金安全风险**。admin 不签发准入、看不到分片/MPC、偷不走钱(`docs/design/security.md` §5「恶意/被控管理员」行);env 接线缺失只影响管理面自身的网络边界强度,不触及六红线与门限不变量。

结论:与 H-004 GREEN 自评一致 —— admin 模块加固完整,`cmd/node` 的 `TSSNODE_ADMIN__ALLOWED_CIDRS` / strong-auth / TLS env 接线缺失为 **P6 部署收口 follow-up**,**非 go/no-go 阻塞项**(默认 loopback + 无 token 不启动 + 响亮告警使其 fail-closed 且可审计)。建议:单列 `cmd/node` admin-env 接线小任务于 P6,或在部署文档强制要求外部网络层收口 + 配置 `TSSNODE_ADMIN__LISTEN` 保持 loopback。

---

## 4. G-001 coord 链感知信任边界复议(持久化派生 evm/tron 地址)

**设计裁定**(`docs/task/G-001.md`、`docs/design/PLAN.md §1` 用户裁定):group 须持久化派生实际链地址(evm_address/tron_address),用户有意的可用性取舍。

**实现/合并状态(事实核对)**:
- 本审查分支 HEAD(`3364126`)`docs/design/server/database.md` groups 段(§32-38)与迁移 `internal/server/coorddb/migrations/00001_init.sql:9-18` **均无** evm_address/tron_address 列;`internal/server/coorddb/repo.go:19-31` `GroupRecord` 亦未含。
- L1 G-001 数据库设计基线提交存在于其他分支线(`git log --all`:`2b29a63` L1 authoritative design revision groups evm/tron_address、`293975e` L1 G-001 database design baseline committed);G-001 任务 `in_progress`(worktree `bkd/11l3axil`),尚未合并入本 H-005 审查分支 HEAD。
- 地址派生原语已就位且为纯公钥派生:`internal/addr/addr.go:25-60`(`ETHAddress`/`BSCAddress`/`TronAddress`,secp256k1 公钥 → keccak256/Base58Check,无任何 secret 输入)。

**安全分析(对设计裁定本身,与实现进度无关)**:
- 链地址 = 组主公钥的确定性纯函数派生;组主公钥 `groups.ecdsa_pubkey` 本就是公开列(`migrations/00001_init.sql:11` 注释「主公钥(公开)」)。派生地址不含、不泄露任何分片或私钥材料(`internal/addr/addr.go:25-60` 仅接收公钥)。
- coord 持久化派生地址 **不引入信任增量**:coord 经 API 本就持信封明文(`docs/design/security.md` §8 设计性接受的隐私取舍),多存一个由已公开公钥确定性派生的公开地址,既不增加 coord 可窃取的资产,也不削弱门限/WYSIWYS 兜底。
- **不破六红线**:地址派生不参与 MPC、不影响 digest 绑定、不改 PreParams 路径、不绕过设备侧 WYSIWYS、不弱化 relay 零信任。coord 仅「轻度链感知」(存储/返回公开地址),仍不解 `unsignedTx`、不参与签名。

结论:G-001 设计裁定 **不破任何红线、不增信任增量**(地址=公开公钥确定性派生,公开信息),安全维度可接受。**残留事实**:其 schema/迁移/repo 实现尚未合并入本审查分支 HEAD(G-001 `in_progress`,独立 worktree);本审查就「设计裁定」给出合规结论,实现合并后建议复核迁移版本化与 api.md 边界返回(归 G-001 自身验收 + L2 合并门,非 H-005 阻塞)。

---

## 5. DB 整库加密 + LOCKED / 单二进制角色隔离 / businessInfo 不削弱禁盲签

### 5.1 coord 整库加密 + 默认锁定 fail-closed

**合规。**
- `internal/server/coorddb/store.go:14-29`:整库加密 + LOCKED 生命周期;启动 LOCKED(db 未挂载、内存无密钥)。
- `Unlock`(`store.go:40-90`):口令 → `loadOrInitKDF` + `deriveKey`(Argon2id)→ raw key 经 `_pragma_key x'<hex>'` 直传 SQLCipher(`dsn` `:161-168`,跳过其内置 KDF);坏口令 PingContext/读 sqlite_master 即失败(`:70-79`,等价「泄露 .db 无口令不可读」)。
- `Relock`(`store.go:92-107`):`zeroize(s.key)` 清零内存密钥并卸载库;`Close` 等价 Relock(`:110`)。
- fail-closed:`conn()`(`store.go:119-127`)LOCKED 时返回 `ErrLocked`,`WithTx`(`:131-156`)随之 fail-closed,上层映射 503 LOCKED。
- 口令仅内存、绝不落盘/配置/env/KMS:`internal/server/coord/coord.go:175-229`(`UnlockProvider` 由 admin-api 交互注入,coord 仅驱动解锁循环;每次尝试后 `zeroize(pass)` `:207`/`:231`,失败指数退避限速 `:199-227`)。

### 5.2 单二进制 relay/coord 信任边界隔离

**合规。**
- 角色解耦:relay 不 import coord(`internal/server/relay/doc.go:6`、`relay.go:32`);coord 明文信封经「外部服务→coord API」另一路径进入,不经 relay 转发(`internal/server/coord/doc.go:10`)。
- `cmd/node` 按 `Relay.Enable`/`Coord.Enable` 各自开关分派(`cmd/node/main.go:31`、`:37`);合并部署仅是两角色交同一运营方(信任域合并),不削弱 relay 密码学零信任(Noise 仍两 party 端到端,见红线 3)。
- 注:`cmd/node/relay.go:21-23` 注明 relay.enable 与 coord.enable 同真时当前为顺序分派(同进程双角色编排归 R-001/X-001),属编排实现细节,**非安全边界问题**(角色内部解耦已保证可随时拆分)。

### 5.3 businessInfo 不削弱禁盲签

**合规。**
- `internal/txdecode/abcheck.go:11-16`、`:30-73`:B 区(businessInfo.displayHints)仅声明式核对,B 不匹配是响亮人审告警(`f.warn` `:39`),**永不硬拒**;仅 digest 不匹配硬拒(红线 2)。B 不视觉冒充 A,A 区(unsignedTx 解码 + 摘要绑定)为资金安全唯一权威,与 `docs/design/PLAN.md §3` / `docs/design/security.md` §4 一致。
- businessInfo 经 `proposerSig`+`metaHash` 防 coord 篡改(`internal/server/coord/envelope.go:74-84`),但不绑定链上效果;被钓鱼由「A 权威 + A/B 显著告警 + 人审」兜底,不破禁盲签不变量。

---

## 6. 结论

### 6.1 红线 / 威胁模型合规性总评

| 项 | 结论 | 关键引证 |
|---|---|---|
| 红线 1 禁盲签/WYSIWYS | 合规 | `internal/mobileapi/sign.go:70-191` |
| 红线 2 解码-摘要双重绑定 | 合规 | `internal/txdecode/txdecode.go:55-101` |
| 红线 3 零信任 relay | 合规 | `internal/transport/transport.go:60-90`、`internal/server/relay/relay.go:32` |
| 红线 4 信任最小化 coord | 合规 | `internal/server/coord/coord.go:16-19`、`callback.go:95-120` |
| 红线 5 PreParams 端上生成 | 合规 | `internal/mpc/keygen.go:36-52`、`recover.go:34-39` |
| 红线 6 TTL 一等公民 | 合规 | `internal/mobileapi/sign.go:106-189`、`coord/ttl.go:9-37` |
| 威胁模型 A(relay 访问控制 + H-003) | 合规 | `internal/server/relay/{relay.go:34-44,captoken.go:92-153,authz.go:14-37}` |
| coord 整库加密 + LOCKED | 合规 | `internal/server/coorddb/store.go:14-168` |
| 单二进制角色隔离 | 合规 | `internal/server/{relay/doc.go:6,coord/doc.go:10}` |
| businessInfo 不削弱禁盲签 | 合规 | `internal/txdecode/abcheck.go:11-73` |
| G-001 链感知信任边界(设计裁定) | 合规(不破红线/不增信任增量) | `internal/addr/addr.go:25-60`、`docs/design/security.md §8` |

**总评:已合并代码在六条红线、威胁模型 A、DB 加密锁定、角色隔离、businessInfo 定位上全部与 `docs/design/security.md` 一致,无降级、无绕过路径、无信任增量。**

### 6.2 残留项清单(均非资金安全风险、非阻塞)

1. **[P6 部署收口] cmd/node admin-env 接线缺失**:`cmd/node/admin.go` 未接 `TSSNODE_ADMIN__ALLOWED_CIDRS` / strong-auth / TLS。缓解已在位(默认 loopback `:28`、无 token 不启动 `:39-43`、`logHardeningPosture` 响亮告警 `server.go:190-208`)。建议 P6 单列接线小任务或部署文档强制外部网络层收口。
2. **[G-001 实现合并待复核] groups evm/tron 地址 schema/迁移**:设计裁定安全合规;实现尚未合并入本审查分支 HEAD(G-001 `in_progress`,worktree `bkd/11l3axil`)。合并后由 G-001 自身验收 + L2 合并门复核迁移版本化与 api.md 边界返回,非 H-005 阻塞。
3. **[设计性接受,非缺陷] 交易隐私对 coord 运营方/管理员开放**:`docs/design/security.md` §8 已记;资金安全由门限 + WYSIWYS 兜底,不依赖其诚实。
4. **[设计性接受] DB 口令丢失 → coord 库不可恢复**:`docs/design/security.md` §8;资金不受影响(分片在设备),缓解=运维安全离线备份口令。
5. **[已记 P6] 第三方安全审计、gomobile@go1.25 端上打包兼容性实测**:`docs/design/security.md` §7 / `docs/design/PLAN.md §1`,P0 范围外。

### 6.3 go / no-go(安全维度)

**GO(安全维度放行)。**

依据:六条红线 + 威胁模型 A + DB 整库加密锁定 + 单二进制角色隔离 + businessInfo 定位逐条核对均合规,且由代码层强制(常量时间摘要绑定、fail-closed LOCKED、relay 不 import coord、PreParams 无服务端下发路径);G-001 设计裁定不破红线、不增信任增量。残留项均为部署收口 / 实现合并待复核 / 设计性接受,**无一触及资金安全不变量或构成 go/no-go 阻塞**。建议残留项 1、2 在 P6 / G-001 合并门按上述跟踪。

> **FIX-003(2026-05-18,dev/test 整库加密禁用开关 + 生产铁律护栏)**:为解 E2E 完整流程中 coord 默认 LOCKED 503 阻断,新增 `coord.db.encryption.enable`(默认 **true** = 加密+LOCKED,安全默认不变)。仅 dev/test 可置 false;**生产铁律护栏**:`internal/node` 启动校验 `Config.Validate` —— coord 启用且加密禁用且 env `ALLOW_INSECURE_DB!=1` → `errInsecureDBNotConfirmed` **fail-closed 致命退出拒启动**。加固生产/release/CI 永不设该 env(H-004 加固面、本审查硬核对项),故「误在生产禁用加密致资金编排数据明文落盘」由护栏使其**不可能**而非仅不推荐。禁用仅影响离线文件加密这一防护,不改其它信任边界/红线;§7.2 独立专测覆盖密文落盘/Argon2id/错口令拒/relock zeroize/生产护栏 fail-closed。结论:dev 便利与生产安全经默认值+启动期 fail-closed 护栏双重保证,符六红线不削弱。

---

## 7. AD-5 H-005 收尾门复核(地址派生 / 非加固 HD,2026-05-20)

> 范围:对 AD 批 5 项(`AD-1` KDD 签名 + `AD-2` 后置 commit-reveal + `AD-3` chaincode 持久化 + `AD-4` walletcli 离线派生 + `AD-6` `group_derived_addresses`)合并入 `origin/main` 后,**逐条**核对 `docs/design/mcp/address-derivation.md §9 H-005 覆盖项 + §7.bis 链接性二度披露**。
> 权威依据:`docs/design/mcp/address-derivation.md`、`docs/design/contract/api.md` B8/B12/C 表、`internal/{mpc,hd,cli,server/coord,server/coorddb,coordclient}`。
> 引证为本审查分支 HEAD `c08555c` 实测。

### 7.1 §9.1 — commit-reveal 不可单方偏置(AD-2)

**合规。**

- **H/HKDF 选择 + DST 唯一性**:承诺 = `SHA-256`,派生 = `HKDF-SHA256`;DST 常量两个独立字节串 `mcp/v1/chaincode-commit` / `mcp/v1/chaincode-derive`(`internal/mpc/chaincode.go:37,41`),非格式化字符串、不可变,二者前像空间天然不相交(commit DST 不为 derive 接受,反之亦然)。
- **group_id 双绑定(防跨组重放)**:`group_id` 同时进入承诺前像(`internal/mpc/chaincode.go:71-72`)与 HKDF salt(`:115-117`),任一已成功 commit-reveal 转出的 transcript 在 `group_id′ ≠ group_id` 下既算不出相同 `C_j` 也算不出相同 `c`(§3 binding-uniqueness)。
- **承诺先于揭示(顺序锁死)**:`internal/cli/chaincode.go:82-114` 严格顺序 —— 先 broadcast 本方 `C_j`(`:82`)→ 收齐全员 `C_*`(`:100`)→ 才 broadcast 本方 `r_j`(`:105`)→ 收齐全员 `r_*`(`:112`)。任一恶意方见到他人 `r` 时其 `C_j` 已被全员持有并定型,**无后揭示偏置窗口**(最后揭示者亦如此)。
- **β2 phase-skew 修复不破不变量**:`chaincodeCollect` 缓冲 future-phase 消息至 `pending` 而非丢弃(`internal/cli/chaincode.go:99,242-258`,L1 ruling 2026-05-20 修 E2E-002),不影响承诺/揭示顺序锁定 —— 缓冲消息仍须本端 commit-broadcast 完成后才会被消费(`:104-114`),恶意方无法借此抢跑揭示。等价 buffer 行为复核:同 from 重复 reveal 必字节相等(`:250-256` `equivocation` 抛错),不同字节即 abort,Byzantine fork 不可。
- **r_j 32B 熵充足**:`internal/mpc/chaincode.go:55,130-136` 固定 32 字节、`crypto/rand.Reader` 读;`GenerateChaincodeRandomness` 是生产路径唯一来源(`internal/cli/chaincode.go:72`)。256-bit 熵贡献,与 #10(联网 keygen Paillier 证明)同纪律。
- **校验常量时间**:`VerifyChaincodeCommit` 使用 `hmac.Equal` 常量时间比较(`internal/mpc/chaincode.go:93`),不泄时序侧信道。
- **abort 严格(no partial success)**:任一 commitment 校验失败、长度不符、equivocation、reveal 缺失/超时 → 整个 `runChaincodeCommitReveal` 返回 error(`internal/cli/chaincode.go:124-131,225,253,261`);`internal/cli/device.go:329-334` 直接传播,**不进 ProvisionGroup、不写 coorddb、不释放 group_id**(§3 step 5)。重试须新 `group_id`,代码层无 in-session retry 路径。
- **senderAuth 锁定 round**:`contract.SignSenderAuth` 在前像中包含 `msg.Round`(`internal/contract/proposer.go:96-103`),`runChaincodeCommitReveal` 以 `Round=1` 发 commit、`Round=2` 发 reveal(`internal/cli/chaincode.go:32-33,82,105`),**commit 签名不可被回放为 reveal**(对应前像不同,签名失败)。结合 sessionId 隔离(`<groupId>:chaincode`)与 transport `AcceptInbound`(`internal/transport/session.go:220-228`),跨 session/跨 round/跨 from 重放均闸断。
- **transport 复用既有 libp2p 路径(无新协议层)**:`runChaincodeCommitReveal` 经 `transport.Session` + `contract.MpcMessage` 流出/入(`internal/cli/chaincode.go:158-167`),与 keygen 同一 senderAuth/sessionId 闸 — 满足 §3「禁新增传输」。

> **微观偏离记录(非偏置)**:`internal/cli/device.go:317-329` 用「DKG 完成后另开一个 sessionId = `<groupId>:chaincode` 的 transport.Session 立即跑 commit-reveal」实现 §3「DKG 完成同会话内追加一轮」。**密码学等价**:`§3` 的安全论证仅依赖 `group_id`(进入承诺前像 + HKDF salt),sessionId 不进任一,session 切换仅是 transport 复用层细节;且新 session 仍走同一 senderAuth/AcceptInbound 闸,不放大攻击面。本审查接受。

### 7.2 §9.2 — xpub 暴露面对手能力分析(AD-4)

**合规。**

- **owning-member-only by route + body**:B8 路由 `GET /v1/groups/{groupId}/xpub` 仅经 `lockGate` + `memberGate`(`internal/server/coord/api.go:56`),`hXpub` 第一步即 `memberGate(... "B8:xpub", []byte(r.URL.RawQuery))`(`internal/server/coord/xpub.go:36`);非成员、跨组、缺/坏 `X-Member-*` 头、过期 ts、复用 nonce、错误 EC 签名一律 fail-close,**未触 DB chaincode 列**(`memberGate` 在 `db.xpub` 之前,`api.go:467-481`)。
- **A 侧零暴露(F1 硬约束)**:外部业务路由 A1 `GET /v1/groups/{groupId}/public` 走完全独立的 `extGate→extAuth` 链与独立 DTO `groupPublicExtView`(`internal/server/coord/api.go:73,407-414`),响应字段仅 `{groupId, ecdsa_pubkey, evm_address, tron_address, threshold_t, parties_n}` —— **无 chaincode、无 xpub、无 derived 列表**;`hGroupPublicExt` 物理隔离的 DTO 使「auth-branch bug 串到 memberView 致 chaincode 泄外」**结构上不可能**。
- **对手能力分析(三模型)**:
  1. **恶意外部 A**:即便持合法 `external.api_key`(`extAuth`),也只能命中 A1 DTO(无 chaincode),且 `coord.external.*` 配置面无 chaincode 字段。**结论**:外部 A 取不到 xpub。
  2. **恶意 relay**:relay 不 import coord、仅转 Noise 密文(红线 3,§3 已复核);B8 由 coord HTTP 服务直发申请方,**不经 relay**(coord 明文路径独立于 MPC 信道,§4 已复核)。**结论**:relay 不在 xpub 流路径上,取不到 chaincode。
  3. **持 xpub 的设备失窃**:F1 释放给 owning-member 设备 keystore;失窃后攻击者持 `(Q_master, chaincode)` 可枚举全部子地址(§9.5 链接性,见 7.5)但**仅能算公钥**,**不能动资金**(分片仍在其它 t-1 设备 + 设备口令 keystore + 资金签名需 ≥ t 方);丢失即按既有 resharing 流程重置(security.md 攻击对策表)。**结论**:失窃后果限于隐私链接性,资金安全由门限兜底,与设计 §F1 一致。
- **memberGate ts+nonce 一致性**:复用 `verifyMemberAuth` 经 `nonceCache.use(now=tnow, expiry=tnow.Add(memberAuthWindow))` 注入时钟一致路径(`internal/server/coord/api.go:477` + FIX-001 已复核),防重放;空查询串 (GET 无 body) 仍签 `(method, groupId, params=URL.RawQuery)`(`xpub.go:36`),与 B-GET 既有契约一致。
- **LOCKED 闸**:`lockGate` 外层(`api.go:56,81-90`)在 LOCKED 时直接 503 不进 handler;且 `db.xpub` 经 `Store.WithTx`(`internal/server/coord/db.go:110-126`),双重 fail-closed。

### 7.3 §9.3 — 非加固"xpub + 任一重组子私钥 → 父+全兄弟"残留依赖(AD-1)

**合规(三残留依赖逐条核实)。**

非加固 HD 的已知特性:若攻击者同时持 xpub `(Q_master, c)` 与 **任一**子标量 `x_child = x_master + IL_i`,则可解出 `x_master`,进而推任意 `IL_j` 与全部兄弟 `x_j = x_master + IL_j`。在本设计「TSS 永不重组子私钥」前提下,该攻击不触发。三残留依赖:

- **(i) 签名实现严谨 —— 永不导出 `x_j` 或 `IL·x_j` 之外的标量**:`internal/mpc/signing.go:76-156` 经 `tss-lib v3` `signing.NewLocalPartyWithKDD` 跑门限 ECDSA;返回 `Signature{R,S,V}`(`:151-156`),无标量字段。`UpdatePublicKeyAndAdjustBigXj` 调用(`:122-126`)仅在 **本会话内的 `keys` 副本**上加 `IL` 偏置(`UnmarshalSaveData` 返出的新 slice,`:108-115`),**不写回持久化 share**(`Sign` 入参 `cfg.Shares` 自身的 `SaveData` 字节不被修改 —— 文档显式记录 `:117-121`)。子私钥的份额标量 `x_j + IL` **仅在 tss-lib 内部协议轮内存在并参与门限运算**,经 R/S 输出 + tss-lib 自带的零中间状态终结后不持久;签名 API 不返回单方份额。
- **(ii) 子私钥 API 永不暴露**:全模块树 grep `x_j|childPriv|ChildPriv|child priv|IL\.x|export.*share`(本审查实测,`internal/mpc/signing.go,internal/hd/derive.go`)无任何子私钥 export entry point。`sdk/sdk.go:100-107` `ExportShare/ImportShare` 仅经设备 keystore + passphrase 处理 **份额**(master 份额),且 backup 走 keystore 加密通道,**绝不**接收 `child index` 参数 / **绝不**导出子标量。
- **(iii) xpub 释放路径受 §7 限定**:见 7.2 — B8 owning-member-only + A 侧零暴露 + LOCKED fail-closed,**xpub 离开 coord 仅经 memberGate**,无其它 export sink。`coordclient.Xpub`(`internal/coordclient/xpub.go:39-64`)在客户端解析 hex → 直接缓存到本机 keystore(`wallet address <i>` 单机离线消费,`internal/walletcli/walletcli.go:322-358`),不再外传。
- **离线派生路径全 public**:`internal/hd/derive.go:46-77` `Derive` 经 `tss-lib v3 ckd.DeriveChildKey` 返 `(IL, Q_child)`,`IL` 为公开(任何持 xpub 者可算),`Q_child` 为 EC 点;参数 `chaincode` 在内部经 `make+copy` 隔离不外泄(`:60-64`);**无任何标量私钥触及**。
- **联网 keygen Paillier 证明(security.md 不变量 #10)同纪律护栏复核**:`internal/mpc/{keygen.go:137,recover.go:143}` + `internal/cli/insecure.go:40-50` —— 生产路径默认 ON,关闭须 `ALLOW_INSECURE_MPC=1` 显式标记并 stderr 响亮告警;`cmd/cli` 从不设该 env,故生产/release/CI 联网 keygen 始终 ON。子私钥不重组不触发此攻击,但 #10 是「恶意成员投递构造性 Paillier」防御,与本批 commit-reveal 安全(§9.1)正交,**未被本批削弱**。

### 7.4 §9.4 — legacy 边界 `409 LEGACY_NO_HD`

**合规。**

- 错误体:`internal/server/coord/errors.go:30,57-67` `codeLegacyNoHD = "LEGACY_NO_HD"` + `errLegacyNoHD()` 固定 `status=409 Conflict, message="group predates HD; multi-group remains the multi-address path"`(与 `docs/design/mcp/address-derivation.md §8 / api.md C 表 :136` 字面一致)。
- 触发路径:`hXpub` 读 `db.xpub` 返 `hasChaincode=false`(`internal/server/coord/db.go:121-122` chaincode IS NULL)即 `errLegacyNoHD()`(`xpub.go:51-55`)。memberGate 先于 DB 校验,故 legacy 群 + 非成员仍是 401/403(不泄露 legacy 事实给非成员)。
- 客户端契约:`coordclient.Xpub`(`internal/coordclient/xpub.go:36-38`)允许调用方按 `apiErr.Code == "LEGACY_NO_HD"` 分支到多 group 多地址路径(F5);`coordclient/errors.go` 暴露 `CodeLegacyNoHD` 类型常量(`grep "CodeLegacyNoHD"` 实测命中)。
- legacy → HD 不存在迁移路径:`migrations/00004_chaincode.sql:21-23` `chaincode BLOB` 列 `CHECK (chaincode IS NULL OR length(chaincode) = 32)` —— legacy 群以 NULL 保留;`coorddb/repo.go` `ProvisionGroup` 仅在新建群时写入 chaincode,**无 update path 注入到既有群**(Q3 init-once / 无 reshare 红线,代码层强制)。
- 测试覆盖:`internal/server/coord/xpub_test.go:99-111` 显式核实 legacy 群 → 409 + `code == "LEGACY_NO_HD"` + message 匹配。

### 7.5 §9.5 + §7.bis 链接性披露(AD-6 + 设计性接受)

**合规(设计性接受 + 实现边界与设计一致)。**

- **链接性事实**:持 xpub 者可纯公钥枚举全部子地址(`Q_child = Q_master + IL·G`,§2);故较「无 HD 多 group 各产单地址」放宽 —— 一个 group 内全部子地址在持 xpub 者视角下**链接到同一 group 主公钥**。已在 `address-derivation.md §1 Q3` + `§7.bis.3` + `§9.5` 显式记并由用户**确认接受**(2026-05-20 Q-A/B/C/D 裁定 bundle)。
- **泄露面收敛(实现核对)**:
  - **xpub 释放 = owning-member-only**(7.2 已复核):非成员不得 xpub → 不得算子地址。
  - **`group_derived_addresses` 表读 = owning-member-only**:B12 GET `/v1/groups/{groupId}/derived` 经 `memberGate("B12:derived:list")`(`internal/server/coord/derived.go:117-122`),A 侧无对应路由(`api.go` 仅 A1 minimal DTO);**避免「免鉴权列表便利化外暴露」(§7.bis.3)**。
  - **写 = 本组任一成员(Q-D)**:`hDerivedRegister` 仅经 `memberGate("B12:derived:register")`(`derived.go:64-72`);未要求门限(派生本身离线 + 公钥纯算,Q-D 显式接受)。
- **schema 与 §7.bis.1 一致**:`internal/server/coorddb/migrations/00005_group_derived_addresses.sql:24-34` —— `(group_id, child_index)` 主键、`child_index` CHECK `[0, 2^31)`(非加固边界、与 `internal/hd/derive.go:17 MaxIndex` 一致)、`child_pubkey` 可选 33B SEC1 compressed(handler 端 33B 严格校验 `derived.go:88-91`)、`created_at` unix sec。**FK 经应用层强制**(`PRAGMA foreign_keys` off,`derived.go:64-71` `ErrDerivedGroupMissing` 父行不存在即 404);设计 §R7 `groups` append-only 保证 ON DELETE 事件不可能发生(`migrations/00005:11-15` 注记)。
- **幂等 + 冲突(§7.bis.4 重放防护)**:`(group_id, child_index)` 命中且 EVM+TRON 一致 → 200 幂等(`coorddb/derived.go:82-90`);矛盾 → `ErrDerivedAddressConflict` → 409 `STATE_CONFLICT`(`coord/derived.go:104-105,159-170`)。重放沿 B-side `ts+nonce`(`memberGate` 经 `nonceCache.use` 已闸,见 FIX-001)。
- **LOCKED**:B12 两路由均经 `lockGate`(`api.go:61-62`),`coorddb/derived.go:111-115,59-63` 通过 `Store.conn`/`Store.WithTx` 双重 fail-closed。
- **不出现"反向"A 侧导出**:`group_derived_addresses` 在 A 侧无路由暴露 —— 即便外部业务服务持合法 `extAuth`,**地址→group 反查**也无可调端点(§7.bis.3 显式「另案」)。本批不预实现,**符 F1 严格** owning-member-only。
- **`child_pubkey` audit aid 边界**:可选 33B SEC1 compressed;33B 严格闸断(`coord/derived.go:88-91`)、coord 不重算/不强校验与 `(group_id, index)` 派生一致性(故 audit aid 性质,§7.bis.1)。**安全分析**:即便恶意成员上报与本组 xpub 不一致的 `child_pubkey`,因 coord 不基于该值做任何资金/签名决策(B12 lazy 注册仅是地址簿登记),最坏后果为本组成员看到一条不匹配自家本地派生的"audit aid"记录(可线下核对剔除)—— **不破红线 / 不增信任增量**;若未来引入「coord 基于 child_pubkey 重派生验证」属新设计变更,走 PMA。
- **写并发**:`RegisterDerivedAddress` 经 `Store.WithTx`(BEGIN IMMEDIATE 单写者,`coorddb/derived.go:63-103`)序列化;`(group_id, child_index)` 主键 + 应用层 conflict check 双重保证不产生重复或脏写。

### 7.6 AD-5 收尾门 GO

| §9 项 | 结论 | 关键引证 |
|---|---|---|
| §9.1 commit-reveal 不可偏置 | 合规 | `internal/mpc/chaincode.go:37-136`、`internal/cli/chaincode.go:54-269`、`internal/contract/proposer.go:96-114` |
| §9.2 xpub 暴露面 owning-member-only | 合规 | `internal/server/coord/{api.go:56,73,407-414;xpub.go:27-67;db.go:104-127}` |
| §9.3 KDD 永不重组子私钥 | 合规 | `internal/mpc/signing.go:76-156`、`internal/hd/derive.go:46-93`、`sdk/sdk.go:100-107` |
| §9.4 legacy 409 LEGACY_NO_HD | 合规 | `internal/server/coord/errors.go:30,57-67`、`xpub.go:51-55`、`migrations/00004_chaincode.sql:21-23` |
| §9.5 + §7.bis 链接性披露 | 合规(设计性接受) | `internal/server/coord/derived.go:64-170`、`coorddb/derived.go:59-140`、`migrations/00005_group_derived_addresses.sql:24-38` |

**AD-5 H-005 收尾门:GO(放行)。**

依据:`address-derivation.md §9` 5 项 + `§7.bis` 链接性二度披露逐条对照 `c08555c` 已合并代码均合规,**不破六红线、不增信任增量、不引新攻击面**:(a) chaincode 经 commit-reveal 集体产出且不可单方偏置,(b) xpub 严格 owning-member-only 且 A 侧零暴露,(c) TSS 永不重组子私钥(签名仅产 R/S/V,无标量出口),(d) legacy 群以 409 LEGACY_NO_HD 显式拒绝 HD 释放(无 silent fallback),(e) `group_derived_addresses` 读写两面均 owning-member-only,A 侧无端点(链接性放宽既受用户接受,但实现层不便利化外暴露)。**残留风险**与 §6.2 列表叠加但**均为既有性质**(设备失窃由门限/keystore 兜底、链接性设计性接受、coord 隐私可信对运营方开放),无 AD 批新增的 go/no-go 阻塞项。

