# 安全模型(共享)

> 全系统威胁模型、信任边界、安全不变量、密钥管理、攻击/对策矩阵。
> 汇总 `architecture.md`、`server/server.md`、`mcp/sdk.md`、`contract/protocol.md`。性质:开发文档,不写代码。

## 1. 资产与目标

- **首要资产**:成员私钥分片 → 链上资金。安全目标:任何 < t 方(含所有服务端组件)**无法动用资金**。
- 次要资产:交易隐私(unsignedTx/businessInfo)、可用性。
- 安全优先级:**资金安全 > 隐私 > 可用性**(可用性可降级,资金安全不可妥协)。

## 2. 主体与信任级别

| 主体 | 信任 | 说明 |
|---|---|---|
| 外部业务服务 | **不可信** | 范围外;可提交任意信封/businessInfo |
| relay 角色 | **零信任** | 哑管道;Noise 端到端不在其终结 |
| coord 角色 | **信任最小化(资金)/ 隐私可信** | 见交易明文(经 API 本就接收)、可审查;但无分片、不参与 MPC |
| 运维管理员(单一全局) | **隐私可信 / 运维可信,资金不可信** | 可查交易+历史会话、控配额/封禁;不签发准入、看不到分片/MPC,偷不走钱(server/admin.md) |
| 成员设备 + 持有者 | **信任根** | 持分片 + 人审;需 ≥ t 方协同 |
| 攻击者(网络/relay 运营/单一恶意成员) | 敌手 | 见 §5 |

## 3. 安全不变量(不可妥协)

1. **门限**:伪造有效签名需 ≥ t 个真实分片方参与 MPC;无任何捷径。
2. **禁止盲签 / WYSIWYS**:进 MPC 前必经 `tx-decode` 重算链摘要 `==digest32` + A 区展示 + 人审。
3. **解码-摘要双重绑定**:`tx-decode` bug 退化为**拒签**而非误签(A 区权威由密码学绑定兜底,不依赖解码器无 bug)。
4. **PreParams 端上生成**:含 Paillier 私钥,**严禁后端预生成/下发**。
5. **TTL 一等公民**:coord 与设备双侧校验 `expiry`;`requestId` 禁复用;`ts+nonce` 防重放;链上 nonce 兜底。
6. **同摘要绑定**:TSS 要求各方对同一 digest 签名,天然抵御「向不同成员发不同信封」分裂攻击。
7. **结果免信任校验**:coord 回传前用组公钥验 `ECDSA(pub,digest32,R,S)`,无效不外泄。
8. **会话隔离**:`sessionId=requestId` 强隔离,跨会话消息丢弃。
9. **coord 库整库加密 + 默认锁定 fail-closed**:库全页加密,密钥由口令 Argon2id 派生仅内存;启动即 LOCKED,未解锁拒绝一切(API `503 LOCKED`、不收交易、不泄数据);泄露 `.db` 文件无口令不可读(server/database.md §7、server.md C9b)。
10. **联网 keygen/reshare 必带 Paillier 证明(L1 裁定 2026-05-18,RA-001 P1-1)**:任何**经网络**的 keygen/resharing,生产路径**必须启用** Paillier modulus(`SetNoProofMod` 不调用)与 factor(`SetNoProofFac` 不调用)ZK 证明 —— 此为 GG18/GG20 对**恶意成员**(投递构造性恶意 Paillier 公钥)的核心防御,不可省。tss-lib no-proof 测试模式**仅限 dev/test**,须**显式门控 + 生产 fail-closed 护栏**(与 #9 加密护栏、FIX-003 同纪律):默认证明开;关闭须显式非生产标记(`env=dev|test`/构建 tag/`ALLOW_INSECURE_MPC=1` 类);生产/release/CI 联网 keygen 证明必开,无显式非生产标记而证明被关 → **致命退出拒启动**。N-002 relay circuit-v2 时长上限须**对 keygen 会话放宽/可配/支持预约续期**,使带证明 keygen(分钟级)能在连接周期内完成(server/server.md §relay)。`internal/cli` 当前无条件 `SetNoProofMod/Fac`(mpcnet.go:171-172/259-260)为发布阻断 P1,经专项 fix-L3 修复;testing.md §6 据此修订。

## 4. 信任边界详解

- **relay 零信任(密码学保证)**:Noise 两 party 端到端、对端身份=peerID=公钥;relay 仅转加密流。即便与 coord 同进程合并,coord 明文经 API 另一路径,不经 relay 转发(server/server.md 首部)。relay 最坏:丢弃/延迟/审查。
- **coord 信任最小化**:见交易隐私、可 DoS/审查;**偷不走钱、伪造不了审批、令不了各方签不同内容、改不动 businessInfo**(门限 + WYSIWYS + 同摘要 + proposerSig/metaHash)。
- **businessInfo 仅辅助**:proposerSig+metaHash 防 coord 篡改,但**不绑定链上效果**;仅 A 区保护资金,B 区被钓鱼由「A 权威 + A/B 告警 + 人审」兜底。
- **外部业务服务不可信**:可投恶意 unsignedTx/误导 businessInfo → 被设备侧 WYSIWYS 拦截。

## 5. 攻击 / 对策矩阵

| 攻击者 | 企图 | 对策 |
|---|---|---|
| 网络监听 / 恶意 relay 运营 | 读/改 MPC、伪造发件人 | Noise 端到端 + senderAuth(protocol.md §3);仅得密文 |
| 恶意 coord | 偷钱 / 伪造审批 | 无分片、不参与 MPC、门限 |
| 恶意 coord | 分裂攻击(不同成员不同信封) | 同摘要绑定,tss 失败;回传前组公钥验签 |
| 恶意 coord / 外部服务 | 钓鱼(businessInfo 与实际不符) | A 区(unsignedTx 解码 + 摘要绑定)权威;A/B 告警;人审 |
| 恶意 coord | 重放旧请求 | requestId 一次性 + expiry + ts/nonce + 链上 nonce |
| 恶意 coord | 谎报成员在线 | 缺真实分片方 tss 自然失败 |
| 未授权第三方 | 蹭 relay / 枚举成员 | pnet PSK + 能力令牌(自主式) + rendezvous 命名空间 HMAC(组密钥);威胁模型 A |
| 单一恶意/被控成员 | 单方动资金 | 门限 ≥ t;其余成员人审可拒 |
| 丢失/被盗设备 | 提取分片 | 设备安全区 + 口令加密 keystore;丢失即 resharing 重置 |
| 拒绝服务 | 压垮 relay/coord | 配额/限流;relay 多副本失败转移;coord 单节点宕机=可用性降级(资金安全不受损) |
| 恶意/被控管理员 | 偷钱/伪造审批/放行陌生 peer | 无分片、不参与 MPC、门限 + WYSIWYS;**不签发准入**(自主式不变),陌生 peer 无组令牌仍连不上、无分片仍签不出;管理操作全入 `admin_audit` 不可篡改;读/控权限分离;管理面非公网(server/admin.md) |
| 恶意管理员/运营方 | 窥探交易隐私 | **设计性接受**:运营方/管理员为隐私可信方(见 §8);资金安全不受影响 |
| 窃取 coord `.db` 文件(离线) | 读出交易/历史 | **整库加密 + 默认 LOCKED**:无口令文件即一堆密文;密钥不落盘(server/database.md §7) |
| 进程内存取密钥 / LOCKED 态探测 | 提取库密钥或数据 | LOCKED 下内存无密钥、库未挂载、API `503` 不泄;解锁后密钥仅内存,重锁/退出清零;解锁尝试限速 |

## 6. 密钥与凭证管理

| 凭证 | 存放 | 规则 |
|---|---|---|
| 私钥分片 | 成员设备安全区 + 口令加密 | 绝不离开设备明文;丢失走 resharing |
| 组主公钥 | coord 库(公开) | 仅用于回传前验签 |
| 组密钥(令牌签发,自主式信任锚) | 组持有 | 签发短 TTL 能力令牌;P6 复议是否引入服务签发 |
| pnet PSK | server 配置 secret(env/密钥文件) | 不入库、不入提交配置;泄露需轮换全网 |
| coord DB 加密密钥 / 推送凭证 / API Key | server 配置 secret | KMS/env 注入;启动 fail-fast 校验 |

## 7. 审计与合规

- coord `request_events` 记状态迁移流水(仅元数据,不记分片);**保留改长期/归档**以支撑管理面历史(server/database.md §6)。
- 管理面 `admin_audit` 记全部管理操作(谁/何时/什么/来源),**管理员不可篡改/删除**;读/控权限分离。
- relay 仅计数/拒绝原因,**不记 peer 间载荷**。
- **整库静态加密 + 默认锁定**:coord 库全页加密,口令 Argon2id 派生密钥仅内存;启动 LOCKED、fail-closed(server/database.md §7、server.md C9b)。**有效防离线文件泄露**;**不防在线运营方/管理员**(解锁运行态本就持明文,设计性接受)。
- 第三方安全审计列入 P6;tss-lib v3 已含 Kudelski 2019 + v2/v3 修复历史。

## 8. 残余风险

- **交易隐私对 coord 运营方/管理员开放(设计性接受)**:coord 经 API 本就接收信封明文,管理面可见不增加信任增量;资金安全不依赖其诚实,门限 + WYSIWYS 兜底。隐私敏感部署可加强管理面访问控制与审计,但无法对运营方在密码学上隐藏(本设计未采用对运营方匿名化方案)。
- 管理面为新增攻击面:缓解见 §5「恶意/被控管理员」行 + server/admin.md(独立强鉴权、权限分离、审计不可篡改、非公网、不签发准入)。
- coord 可审查(可用性);资金安全不依赖其诚实。
- `tx-decode` 未覆盖的新链特性 → 降级「原始 + 告警」,不臆造(可用性折中,非资金风险)。
- 社工:诱导用户在 A 区明显异常时仍批准 —— 由 UI 强警示 + 流程缓解,非纯技术可消除。
- **DB 口令丢失 → coord 库不可恢复**(设计如此,密钥无托管):仅损失编排/历史数据,**资金安全不受影响**(分片在成员设备)。缓解:运维须安全离线备份口令;可选口令分片/多托管留待 P6。
