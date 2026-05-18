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
