# `node` 服务端开发文档(relay + coord 双角色)

> 单一 Go 二进制 `node`,经配置 `relay.enable` / `coord.enable` 两个开关控制启用哪些角色(可单开任一,或双开)。对应 `PLAN.md` §3/§4。
> relay 角色交付于 P2,coord 角色交付于 P3。性质:开发文档,不写代码。
>
> **单程序双角色**:默认 `relay.enable=true` + `coord.enable=true` 同进程合并部署(最简);可拆为仅 `coord.enable` 实例 + 多个仅 `relay.enable` 实例。
>
> ```
> relay:
>   enable: true        # 启用 relay 角色
> coord:
>   enable: true        # 启用 coord 角色
> ```
> 两者均 false 视为无效配置,启动时报错退出。
> **合并不削弱 relay 密码学零信任**:Noise 两 party 端到端、不在 relay 终结;coord 的明文信封经「外部服务 → coord API」另一路径进入,**不经 relay 转发路径**;两条数据路径进程内逻辑隔离。合并仅是两角色交同一运营方(信任域合并),角色内部保持解耦以支持随时拆分。

---

# 配置(配置文件 + 环境变量)

## 来源与优先级

`内置默认值  <  配置文件  <  环境变量`(环境变量覆盖文件;便于容器/CI/密钥注入)。

- 配置文件路径:默认 `./node.yaml`,可由 `NODE_CONFIG` 指定。
- 环境变量约定:前缀 `TSSNODE_`,嵌套键用 `__`(双下划线)连接,大写。
  例:`TSSNODE_RELAY__ENABLE=true`、`TSSNODE_COORD__HTTP__LISTEN=:8080`、`TSSNODE_LOG__LEVEL=info`。
- 启动时校验:`relay.enable` 与 `coord.enable` 同为 false → 报错退出;声明为 secret 的必填项缺失 → fail-fast 退出。

## 配置文件示例(YAML)

```yaml
log:   { level: info, format: json }
metrics: { listen: ":9090" }          # 健康检查 / 指标;不记录载荷

relay:
  enable: true
  listen: ["/ip4/0.0.0.0/tcp/4001"]
  pnet_psk_ref: env:TSSNODE_RELAY__PNET_PSK   # secret,见下
  token_verify:
    source: config                     # config | coord-sync
    group_pubkeys: []                  # 自主式信任锚:组公钥集
  rendezvous: { enable: true }
  limits:
    reservation_per_token: 4
    reservation_per_group: 8
    bandwidth_per_conn: "1MiB/s"

coord:
  enable: true
  http: { listen: ":8080" }            # 外部服务 + 成员 API
  db:   { dsn_ref: env:TSSNODE_COORD__DB_DSN }   # secret;待签列表/状态/组公钥/推送token
  external:
    auth: mtls                         # mtls | api_key
    api_key_ref: env:TSSNODE_COORD__EXTERNAL__API_KEY   # secret(auth=api_key 时)
    result_callback: webhook           # webhook | longpoll
  push:
    fcm_cred_ref: env:TSSNODE_COORD__PUSH__FCM   # secret
    apns_cred_ref: env:TSSNODE_COORD__PUSH__APNS # secret
  ttl: { skew_tolerance: "30s" }
  quorum: { signer_select: liveness }  # stable | liveness
  dispatch: { timeout: "120s" }
```

## 参数表

| 键 | 说明 | secret |
|---|---|:--:|
| `log.level` / `log.format` | 日志级别 / 格式 | |
| `metrics.listen` | 健康检查与指标监听地址(不含载荷) | |
| `relay.enable` | 启用 relay 角色 | |
| `relay.listen` | libp2p 监听 multiaddrs | |
| `relay.pnet_psk` | private network 32B swarm key | ✅ |
| `relay.token_verify.source` | 能力令牌验签公钥来源:`config` 或向 coord 同步 | |
| `relay.token_verify.group_pubkeys` | 自主式信任锚:钱包组公钥集 | |
| `relay.rendezvous.enable` | 启用 rendezvous 发现 | |
| `relay.limits.*` | 预约/连接/带宽配额(防 DoS) | |
| `coord.enable` | 启用 coord 角色 | |
| `coord.http.listen` | 外部服务 + 成员 API 监听地址 | |
| `coord.db.dsn` | 持久化连接串(待签列表/状态机/组公钥/推送 token) | ✅ |
| `coord.external.auth` | 外部服务鉴权方式:`mtls` / `api_key` | |
| `coord.external.api_key` | 外部服务 API Key(auth=api_key 时) | ✅ |
| `coord.external.result_callback` | 结果回传方式:`webhook` / `longpoll` | |
| `coord.push.fcm` / `coord.push.apns` | 推送凭证 | ✅ |
| `coord.ttl.skew_tolerance` | 时钟偏移容差(超出保守判过期) | |
| `coord.quorum.signer_select` | 签名子集选取策略:`stable` / `liveness` | |
| `coord.dispatch.timeout` | 派发后等待签名完成超时(须 < 剩余 TTL) | |

## 密钥处理

- 标 secret 的项**禁止**写入提交到仓库的配置文件;一律经环境变量或挂载的密钥文件注入(配置中以 `env:VAR` / `file:/path` 引用)。
- 启动时校验所有已启用角色的必填 secret 存在,缺失即 fail-fast,不带降级默认值。
- 持久化层**绝不存**分片/私钥/PSK 明文(见 C9)。

---

# 第一部分:relay 角色(零信任传输,P2)

## R1. 职责与非职责

**职责**
- libp2p **circuit-relay v2(HOP)**:成员手机多在 NAT/蜂窝网后,直连不通时中转加密流。
- **rendezvous**:同一钱包组成员互相发现(以组命名空间)。
- 访问控制(威胁模型 A:仅阻止未授权使用)、配额限流。

**非职责(硬边界)**
- 不参与 MPC、不持任何分片。
- 不解析/不存储签名请求信封(那是 coord 角色,经另一路径)。
- 读不到 MPC 内容、无法伪造发件人。

## R2. 零信任为何成立(密码学,非约定)

- 两成员之间的 **Noise 安全会话端到端建立、不在 relay 终结**;relay 转发的只是已加密的 libp2p 流。
- libp2p **对端身份 = 公钥**(peer ID),relay 无法伪造发件人。
- 即使与 coord 角色同进程合并仍成立(见首部数据路径隔离说明)。
- 结论:relay 最坏只能**丢弃/延迟/审查**流量(可用性影响),**无法读取或篡改 MPC 内容**。

## R3. 成员发现(rendezvous)

- 命名空间 = `HMAC(groupSecret, "tss-group")`,Base32 截断。外人无 `groupSecret` → **枚举不到成员**(隐蔽,非访问控制本身)。
- 成员注册自身 `{peerID, multiaddrs}` 到 rendezvous;其余成员按命名空间查询并建 Noise 连接。
- 注册需通过 R4 访问控制(防止未授权占用 rendezvous)。

## R4. 访问控制(威胁模型 A — 仅阻止未授权,不隐藏成员元数据)

三层叠加,均在 relay 侧由 libp2p `ConnectionGater` 强制:

1. **private network PSK(pnet)** 打底:无 32 字节 swarm key 者**无法说该协议**,relay 对公网不可见。PSK 按部署域分发。
2. **能力令牌(capability token)**:成员申请 circuit-relay 预约 / rendezvous 注册时出示令牌;令牌由**钱包组组密钥**(自主式信任锚,与去中心化/零信任一致)签发,短 TTL、按组隔离、可撤销。relay 配置信任的组公钥集(或经 coord 同步)。
3. **配额与限流**:每令牌/每组预约数、连接数、转发带宽上限,防 DoS。与授权正交。

> 信任锚 = **自主式**(组自带组密钥);P6 加固时复议是否引入服务签发选项。不采用匿名凭证(威胁模型 A 不要求对 relay 隐藏分组元数据)。

## R5. 拓扑、复制与故障转移

- relay 角色**无状态**,可水平复制;**任何第三方可自建 relay 接入**。
- 客户端持**可配置 relay 列表**,自动故障转移(连接失败/预约失败切换下一节点)。
- 推荐部署:默认 `relay.enable=true`+`coord.enable=true` 合并起步;按规模/容灾增设仅 `relay.enable` 独立实例。
- coord 角色是单一逻辑权威(HA),relay 角色多副本 —— 二者解耦,**独立 relay 不依赖 coord 可用性**(A2:连通性由成员上报 coord,relay 不上报、不耦合)。

## R6. 配置 / 接口 / 运维

- 配置:见上方「配置」章节 `relay.*`(配置文件 + `TSSNODE_RELAY__*` 环境变量覆盖)。
- 协议:libp2p circuit-relay v2(HOP/STOP)、rendezvous、Noise、(broadcast 经 GossipSub 由客户端 `transport` 模块使用,relay 仅承载底层连接)。
- 可观测:连接/预约/转发字节计数、拒绝原因(未授权/超配额)、健康检查端点;**不记录** peer 间载荷。
- 安全:仅 libp2p 标准栈,无自实现加密;升级随 go-libp2p 跟进 CVE。

## R7. 验收要点(P2)

- 三机经 relay 跑通 keygen+sign 的 MPC 消息往返。
- **零信任验证**:在 relay 抓包仅见密文,无法还原任一 MPC 消息明文或发件人伪造。
- 未持 PSK/令牌者无法建立 relay 预约或 rendezvous 注册。
- 杀掉一个 relay 副本,客户端自动切换,会话不中断或可重试恢复。

---

# 第二部分:coord 角色(信任最小化编排者,P3)

定位:中心编排者 + 与外部业务服务的对接入口。

## C1. 职责与非职责

**职责**
- 接收外部业务服务提交的签名请求(信封)→ 持久化为「待签列表」。
- 追踪成员在线/连 relay 状态(成员签名心跳上报,A2)。
- 法定人数 ≥ t 且已审批且未过期 → 选定签名子集并发「开始」。
- TTL/过期一等公民处理。
- 外部业务服务对接:提交入口 + `{R,S,V}` 回传(A1,**结果必须回传**)。
- 成员侧:推送(FCM/APNs)+ 上线拉取(A4)。

**非职责(硬边界)**
- **不参与 MPC、不持分片**(A3)。
- 不做链/业务逻辑(交易构造、calldata、广播在外部服务)。
- 不决定「签什么」——由设备侧内置 `tx-decode` 解码器(重算摘要断言 `==digest32`)+ 人审(WYSIWYS)保证。

## C2. 签名请求信封 Schema

```
SigningRequest {
  requestId   : UUID(全局唯一,禁复用)
  groupId     : 钱包组标识
  chain       : 不透明标签(coord 不解释)
  unsignedTx  : 不透明 blob(原样下发各成员;设备侧 tx-decode 解析 + 重算摘要断言 + 展示)
  digest32    : 实际待签 32 字节摘要
  proposer    : 发起方标识
  createdAt, expiry : 时间戳 + 绝对过期时刻
  businessInfo? : 带外业务说明(结构化,可选):
                  { title, summary, items[], refs{invoiceId/orderId/...},
                    requester, memo, displayHints }
  metaHash    : H(businessInfo)
  proposerSig : 外部服务/发起方对以上全部字段(含 metaHash)的签名
}
```
coord 侧附加追踪字段:`status`、`approvals[]`、`presentSigners[]`、`dispatchedAt`、`result`。

**`businessInfo`(带外业务说明)**:供签名方理解业务意图(如「支付发票 #1234 给供应商 X」)。**安全定位 —— 仅辅助展示,不绑定链上效果**:
- coord 仅**携带/持久化/索引**(待签列表可按 `refs`/`title` 展示),**不可篡改**(纳入 `proposerSig`+`metaHash`)。
- 设备侧审批界面**强制分区**:A「已校验交易事实」(tx-decode 解码 + 摘要绑定,资金安全唯一权威)/ B「业务说明(带外,proposer 签名,非链上校验)」;B 不得冒充 A。
- 不替代、不弱化 C1 的禁止盲签不变量;A/B 声明式核对由设备侧内置 `tx-decode` 执行(不在 coord)。

## C3. 待签列表状态机

```
PENDING ──(≥t 在线且审批且未过期)──▶ DISPATCHED ──▶ SIGNING ──▶ SIGNED ──▶ RETURNED
   │                                     │            │
   ├──(到期)──▶ EXPIRED                  │            └──(签名验签失败/超时)──▶ FAILED
   ├──(成员拒绝致不可达 t)──▶ REJECTED   └──(签名中成员掉线,TTL 内)──▶ 回退 PENDING 重新调度
```
- 终态:`RETURNED`(成功回传外部服务)、`EXPIRED`、`REJECTED`、`FAILED`。
- 每次状态变更均回报外部业务服务(含 `EXPIRED`/`FAILED` 原因)。

## C4. 连通性追踪(A2)

- 成员 SDK 连上 relay 后,周期性发**签名心跳**:`{memberId, groupId, relayPeerID, ts, sig}`,有 TTL。
- coord 维护每组「在线成员集」(心跳过期即移除)。**不依赖 relay 上报**,relay 与 coord 解耦。
- 心跳签名用成员身份密钥,coord 验签防伪造在线状态。

## C5. 法定人数发起算法

```
对每个 PENDING 请求,事件驱动(收到审批/心跳/到期定时器)重评估:
  if now ≥ expiry        → EXPIRED(回报外部服务)
  approvedOnline = 在线 ∩ 已审批 ∩ 属于该组
  if |approvedOnline| ≥ t:
     signers = 稳定排序后取前 t(或按心跳新鲜度/RTT 择优)
     状态 → DISPATCHED;向 signers 下发 START(含完整信封)
     启动 dispatch 超时计时器(< 剩余 TTL)
  调度后若某 signer 掉线且仍 < 终态且未过期 → 回 PENDING 重新选 signer
```
- coord 只发 START,**不进入 tss 协议**;选定成员经 relay 自行跑 MPC。

## C6. TTL / 过期(一等公民)

- (a) coord 到期立即 `EXPIRED` 并回报外部服务「已过期」,从活动列表剔除。
- (b) 设备侧进入 MPC **前** 与 出 `{R,S,V}` **前** 各校验一次 `now < expiry`,过期立即拒签。
- (c) 推送/拉取响应均携带**剩余 TTL**,客户端据此决定是否还值得发起。
- (d) `requestId` 一次性,过期/终态后不得复用。
- (e) 时钟偏移:统一 UTC,允许小幅 skew 容差(配置项),容差外保守判过期。

## C7. 接口

**外部业务服务 ↔ coord**
- `POST /requests`:提交信封 → `requestId` ack。
- `结果回调`(Webhook 或长连接):`{requestId, status, R,S,V?}`;coord **在回传前用该组公钥验证 ECDSA(pub, digest32, R,S) 有效**(免信任的完整性闸门,验不过 → `FAILED` 不回传伪结果)。
- 鉴权:mTLS 或 API Key + 请求签名(`proposerSig`)。

**成员 SDK ↔ coord**
- 注册推送 token;拉取本组未过期待签项(带剩余 TTL);提交审批/拒绝;心跳;接收 START;上报签名完成 + 最终 `{R,S,V}`(由签名子集中指定一方上报,coord 验签)。
- 鉴权:成员身份密钥签名;按 `groupId` 隔离。

## C8. 信任边界与攻击面

| coord 能做 | 被什么挡住(不能做) |
|---|---|
| 拒绝/延迟发起(审查) | 偷资金 —— 无分片、不参与 MPC、伪造签名需 ≥ t 方 |
| 获知交易详情(隐私代价) | 让成员盲签 —— 设备侧 `tx-decode` 重算摘要断言 + 人审(WYSIWYS) |
| 重放旧请求尝试 | —— requestId 一次性 + expiry + 链上 nonce 兜底 |
| 谎报某成员在线 | 推进 MPC —— 缺真实分片方 tss 自然失败 |
| 向不同成员发不同信封 | 产出有效签名 —— TSS 要求各方同一 digest,且 coord 回传前验签 |
| 谎报结果给外部服务 | —— coord 回传前对 `{R,S,V}` 按组公钥验签,无效即 FAILED |
| 篡改 `businessInfo` 钓鱼 | —— proposerSig+metaHash 覆盖,coord 改即验签失败;且 B 区非权威,A 区(unsignedTx)兜底 |

恶意外部服务:可提交恶意 `unsignedTx` 或误导性 `businessInfo`,但被设备侧 WYSIWYS 拦截 —— **A 区(unsignedTx 解码 + 摘要绑定)是资金安全唯一权威**,`businessInfo` 仅参考;`digest` 与 `unsignedTx` 的绑定由设备重算保证。

## C9. 部署与可用性

- 单一逻辑权威,**HA 部署**(主备/多副本 + 共享持久化);宕机 = 不可发起新签名(可用性),**不影响资金安全**。
- 默认与 relay 角色同二进制(`relay.enable=true`+`coord.enable=true`);可拆为仅 `coord.enable` + 多个仅 `relay.enable` 实例。
- 持久化:待签列表 + 状态机 + 组公钥(公开信息)+ 推送 token;**绝不存**分片/私钥/PSK 明文。

## C10. 验收要点(P3)

- 信封提交 → 通知 → 心跳 → 法定人数 → START → 三机经 relay 签名 → `{R,S,V}` 验签 → 回传外部服务,端到端跑通。
- 过期请求在 (a)(b) 两处均被拒,外部服务收到 `EXPIRED`。
- 篡改信封 / 谎报在线 / 不同成员不同信封 → 均无法产出有效签名(对应 C8 各行)。
- coord 宕机重启后状态机从持久化恢复,未过期请求可续。
