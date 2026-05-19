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

## 来源与优先级(用户裁定 2026-05-19 —— Traefik 式三源)

`内置默认值  <  配置文件  <  环境变量  <  CLI 参数`(后者覆盖前者,CLI 最高,
与 Traefik 一致,便于运维临时覆盖)。三源**统一键空间**,任一键可由三种方式提供:

- **配置文件**:默认 `./server.yaml`,可由 `SERVER_CONFIG` 或 CLI `--config <path>` 指定。
- **环境变量(用户裁定 2026-05-19)**:前缀 `MPC_`,**单下划线**连接(无双下划线),
  全大写。即 env 名 = `MPC_` + 点分键(`.` 与键内 `_` 一律为 `_`)大写。
  例:`MPC_RELAY_ENABLE=true`、`MPC_COORD_HTTP_LISTEN=:8080`、
  `MPC_COORD_TTL_SKEW_TOLERANCE=30s`、`MPC_LOG_LEVEL=info`。
  > 消歧:嵌套分隔与键内下划线同为 `_`,故**不解析 env 名**,改由配置 schema
  > 为每个已知叶子键**生成**其 `MPC_<UPPER>` 名再精确匹配(schema-driven
  > generate-and-match,键集静态,无歧义)。
- **CLI 参数**:`--<点分键>=<值>`,与配置键一一对应。
  例:`--coord.http.listen=:8080`、`--relay.enable=true`、`--log.level=debug`。
- 启动校验:`relay.enable` 与 `coord.enable` 同为 false → 报错退出;已启用角色的
  必填项缺失 → fail-fast 退出。

## 取值:字面量或引用(用户裁定 2026-05-19)

任一配置值既可写**字面量**,也可写 `env:VAR` / `file:/path` **引用**(运维自择;
不再强制 secret 必须为引用)。引用在加载时解析,字面量原样使用。

> 安全说明:允许字面量是运维灵活性的有意取舍;真实 secret 以明文写入**提交到
> 仓库**的配置文件仍是运维须自行规避的风险(推荐 secret 用 `env:`/`file:` 或
> CLI 注入)。**DB 解锁口令仍是绝对例外**:不得进配置文件/env/CLI/KMS,仅经
> `admin-api` 解锁交互提供、内存驻留、重锁即清零(C9b、database.md §7)。生产
> 整库加密 fail-closed 护栏不受本变更影响(database.md §7.1)。

## 配置文件示例(YAML)

```yaml
log: { level: info, format: json }
metrics: { listen: ":9090" }          # 健康检查 / 指标;不记录载荷

relay:
  enable: true
  listen: ["/ip4/0.0.0.0/tcp/4001"]
  pnet_psk: env:MPC_RELAY_PNET_PSK   # 字面量或 env:/file: 引用
  token_verify:
    group_pubkeys: []                  # 自主式信任锚:组公钥集(能力令牌验签锚)
  rendezvous: { enable: true }
  limits:
    reservation_per_token: 4
    reservation_per_group: 8
    bandwidth_per_conn: "1MiB/s"
    circuit_max_duration: "10m"        # keygen-aware(security.md #10)

coord:
  enable: true
  http: { listen: ":8080" }            # 外部服务 + 成员 API
  db: { dsn: env:MPC_COORD_DB_DSN }   # 字面量或引用
  external:
    api_key: env:MPC_COORD_EXTERNAL_API_KEY        # 入站鉴权固定 api_key(外部→coord)
    result:                                        # 结果回传固定地址(coord→外部),避免与 notify 重名歧义
      url: https://biz.example.com/mcp/result      # demo;字面量或引用
      secret: env:MPC_COORD_EXTERNAL_RESULT_SECRET # 出站签名密钥(HMAC-SHA256,首选,防伪造)
      api_key: env:MPC_COORD_EXTERNAL_RESULT_API_KEY  # 备选 Bearer token(未配 secret 时用)
  notify:                                          # 单一固定通知地址(coord→外部),固定 webhook 不分层
    url: https://biz.example.com/mcp/notify        # demo;字面量或引用
    secret: env:MPC_COORD_NOTIFY_SECRET            # 出站签名密钥(HMAC-SHA256,首选,防伪造)
    api_key: env:MPC_COORD_NOTIFY_API_KEY          # 备选 Bearer token(未配 secret 时用)
  ttl: { skew_tolerance: "30s" }
  quorum: { signer_select: liveness }  # stable | liveness
  dispatch: { timeout: "120s" }
```

## 参数表

| 键 | 说明 | 含 secret |
|---|---|:--:|
| `log.level` / `log.format` | 日志级别 / 格式 | |
| `metrics.listen` | 健康检查与指标监听地址(不含载荷) | |
| `relay.enable` | 启用 relay 角色 | |
| `relay.listen` | libp2p 监听 multiaddrs | |
| `relay.pnet_psk` | private network 32B swarm key | ✅ |
| `relay.token_verify.group_pubkeys` | 自主式信任锚:钱包组公钥集(能力令牌验签锚;relay 本地验签,**不依赖 coord**) | |
| `relay.rendezvous.enable` | 启用 rendezvous 发现 | |
| `relay.limits.*` | 预约/连接/带宽/circuit 时长配额(防 DoS) | |
| `coord.enable` | 启用 coord 角色 | |
| `coord.http.listen` | 外部服务 + 成员 API 监听地址 | |
| `coord.db.dsn` | 持久化连接串 | ✅ |
| `coord.external.api_key` | 入站(外部→coord)API Key,鉴权**固定** api_key,恒必填 | ✅ |
| `coord.external.result.url` | 结果回传地址(coord→外部,恒必填) | |
| `coord.external.result.secret` | 结果回传**出站签名密钥**:coord 对每次回调 HMAC-SHA256 签名(首选模式),外部据此验真、拒伪造 | ✅ |
| `coord.external.result.api_key` | 结果回传**备选 Bearer token**:未配 `secret` 时改用 `Authorization: Bearer`;`secret`/`api_key` 须至少配一 | ✅ |
| `coord.notify.url` | 单一固定通知地址(coord 仅 POST 通知事件;FCM/APNS 等由外部通知渠道处理,coord 不持推送凭证) | |
| `coord.notify.secret` | 通知**出站签名密钥**(HMAC-SHA256,首选,防伪造) | ✅ |
| `coord.notify.api_key` | 通知**备选 Bearer token**(未配 `secret` 时启用);`secret`/`api_key` 须至少配一 | ✅ |
| `coord.ttl.skew_tolerance` | 时钟偏移容差(超出保守判过期) | |
| `coord.quorum.signer_select` | 签名子集选取策略:`stable` / `liveness` | |
| `coord.dispatch.timeout` | 派发后等待签名完成超时(须 < 剩余 TTL) | |

## 变更摘要(用户裁定 2026-05-19)

1. **通知 = 单一固定 webhook**:删除 `coord.push.{fcm,apns}` 与一切推送凭证;
   coord 仅向 `coord.notify`(单一固定地址,不分层)POST 通知事件,
   FCM/APNS/其它由**外部通知渠道**翻译投递。coord 不再区分 fcm/apns、
   不再持推送凭证。
2. **external 鉴权固定 `api_key`**(删除 `auth` 枚举与 `mtls` 选项,`api_key`
   恒必填);**结果回传固定地址**(删除 `result_callback` 枚举与
   `longpoll` 路径,`coord.external.result.url` 恒必填)。
3. **配置框架 = Traefik 式三源**:文件 + 环境变量 + CLI 参数,统一键空间、
   `默认<文件<env<CLI` 优先级;任一值可为**字面量或 `env:`/`file:` 引用**
   (解除「secret 必须为引用、明文禁止」硬约束)。DB 解锁口令例外与生产加密
   fail-closed 护栏**不变**。
4. **出站回调鉴权(用户裁定 2026-05-19,防回调伪造)**:**根因**:coord→外部
   的结果/通知回调此前无鉴权(callback 仅 POST,无签名),攻击者可向外部业务
   服务的回调端点伪造 `{requestId,status,RSV}` → 业务侧据伪造签名结果误动作。
   **修订点(用户 2026-05-19 追加)**:
   - **改名解歧义**:`coord.external.result_webhook` → **`coord.external.result`**
     (原名与 `notify.webhook` 的 `webhook` 重复,易混淆)。
   - **notify 扁平化**:固定 webhook 不再分层,`coord.notify.webhook.{url,secret}`
     → **`coord.notify.{url, secret, api_key}`**。
   - **两种鉴权模式**(请求签名 / token):
     - **签名(首选)**:配 `secret` 时,coord 每次回调以 `secret` 对
       `时间戳 + "." + 原始 body` 做 **HMAC-SHA256**,置头
       `X-MCP-Signature: t=<unix>,v1=<hex>` + `X-MCP-Timestamp`;外部用同
       secret 重算、常时比较、按 skew 容差拒重放/伪造(body 绑定、抗重放,
       安全性最强)。
     - **token(备选)**:未配 `secret` 但配 `api_key` 时,coord 改置
       `Authorization: Bearer <api_key>`;外部常时比较该 token。
       (兼容只支持 Bearer 的接收端;无 body 绑定,弱于签名,故为备选。)
     - `secret` 与 `api_key` **至少配一**;两者皆配时 `secret` 优先(用签名)。
   - 凭据为出站方向,与入站 `external.api_key` **物理隔离**(方向不同、字段
     独立、互不复用);均可字面量或 `env:`/`file:` 引用。
   `mock-extsvc`(测试替身)须实现签名与 token 两种验证以 E2E 实证防伪造;
   api.md A4 同步定义两种回调鉴权头。
   - **规范 skew 窗口(L1 裁定 2026-05-19,解 WHA-001 YELLOW)**:回调签名
     验签的时钟 skew 容差 = **`±300s`**,为 **receiver 侧策略**(coord 仅签发
     准确 `X-MCP-Timestamp`,不持此参数;故无新增 coord 配置键)。与
     `coord.ttl.skew_tolerance`(coord 请求过期轴,语义不同)无关。300s 为
     `t=,v1=` 方案业界惯例(覆盖跨主机投递+退避重试);集成方可收紧不应放宽。
     `mock-extsvc` 现 `WEBHOOK_SKEW_S=300` 即规范值(无需改码),api.md A4 已落。
5. **relay 令牌验签固定 config-only(用户裁定 2026-05-19,解配置面误导)**:
   `relay.token_verify` 删除 `source` 字段与 `coord-sync` 取值,仅保留
   `group_pubkeys`。**根因**:`coord-sync` 既未实现(启动即拒)、又无对应
   coord 地址字段可用、且违反「独立 relay 不依赖 coord」架构不变量(R5 /
   §203),仅属 §196 P6 投机选项;暴露一个不可用且违背不变量的枚举值是误导
   配置面。relay 始终本地以 `group_pubkeys`(自主式信任锚)验能力令牌签名,
   零 coord 依赖;单值开关违反「不留无意义可配项」。`source` 同步从 config
   schema/校验/默认值/example/.env 移除(RT-001 实施,串行于 WHA-001 合并后,
   同改 config.go)。
6. **file/env/CLI 完整对应可文档化(用户裁定 2026-05-19)**:框架机制本已
   schema 驱动、对**每个**叶子键三源(文件 / `MPC_`+大写点分 env / `--点.分`
   CLI)1:1 等价(含 `[]string` 逗号分隔),无逐键注册、不会漂移。问题在
   **文档枚举不全**:`.env.example` 仅列运维子集,本参数表缺
   `coord.db.encryption.enable`、把 `relay.limits.*` 合并成一行、且无
   env/CLI 列。**对策(CFGDOC-001,串行于 RT-001 之后——最后一个改 schema 的
   任务,确保只文档化一次稳定 schema)**:本参数表升级为**完整 schema 派生
   键矩阵**(每叶子键:yaml 路径 · `MPC_…` env 名 · `--…` CLI · 默认值 ·
   是否 secret);`.env.example` 枚举**全部**键(每键一行 `MPC_…=` 占位 +
   CLI 等价注释);新增**覆盖测试**断言「文档键集 == config.go schema 叶子
   集」(防再漂移)。纯文档 + 1 测试,零 schema/行为变更;English-only。

## 密钥处理(随上款修订)

- 已启用角色的必填项缺失 → 启动 fail-fast,不带降级默认值。
- 持久化层**绝不存**分片/私钥/PSK 明文(见 C9)。
- **DB 解锁口令绝对例外**:不得进配置文件/env/CLI/KMS;仅 `admin-api` 解锁交互
  提供、内存驻留、重锁清零(C9b、server/database.md §7)。

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
- coord 角色是单一逻辑权威(SQLite 单节点 + 文件备份/Litestream 容灾),relay 角色多副本 —— 二者解耦,**独立 relay 不依赖 coord 可用性**(A2:连通性由成员上报 coord,relay 不上报、不耦合)。

## R6. 配置 / 接口 / 运维

- 配置:见上方「配置」章节 `relay.*`(配置文件 + `MPC_RELAY_*` 环境变量覆盖)。
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
- 成员侧:通知(单一固定 webhook,见配置章 §变更摘要)+ 上线拉取(A4)。

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
- 拉取本组未过期待签项(带剩余 TTL);提交审批/拒绝;心跳;接收 START;上报签名完成 + 最终 `{R,S,V}`(由签名子集中指定一方上报,coord 验签)。
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

- 单一逻辑权威,**SQLite 单节点持久化 + 文件备份**(容灾可选 Litestream/LiteFS 流式复制/只读副本);非多写 HA。宕机 = 不可发起新签名(可用性降级),**不影响资金安全**(security.md §1)。在线状态用内存 SQLite(不持久,重启由心跳重建)。
- 默认与 relay 角色同二进制(`relay.enable=true`+`coord.enable=true`);可拆为仅 `coord.enable` + 多个仅 `relay.enable` 实例。
- 持久化:待签列表 + 状态机 + 组公钥(公开信息)+ 审计;**绝不存**分片/私钥/PSK 明文。历史长期保留以支撑管理面(server/database.md §6)。
- **管理面**:coord 内置 `admin-api` + 独立 `admin-ui`(单一运维管理员:查交易/历史会话、防滥用、审计;不签发准入)。详见 `server/admin.md`。

## C9b. 锁定生命周期(整库加密,防 DB 文件泄露)

- coord 持久库**全库加密**;密钥由运营方**口令**经 Argon2id 派生,**仅内存**(不落盘/不入配置/env/KMS)。详见 `server/database.md` §7。
- 状态机:`启动 → LOCKED`(库未挂载、无密钥)`──解锁(口令)──▶ UNLOCKED`(正常服务)`──重锁/空闲超时──▶ LOCKED`。
- **LOCKED 下 fail-closed**:coord 拒绝外部信封、成员请求、所有数据查询 → API 一律 `503 LOCKED`(contract/api.md);仅解锁端点 + 最小健康检查可用;磁盘无任何明文。
- **解锁/重锁经 `admin-api`**(管理员鉴权 + 口令);解锁尝试限速退避,记进程日志/指标(LOCKED 时无法写加密库),成功后补记 `admin_audit`。详见 `server/admin.md`。
- **relay 角色不受影响**:relay 无 DB、无状态 → coord LOCKED 期间,`relay.enable` 实例仍可正常承载已建会话的 MPC 中转(但无 coord 编排,不会有新签名发起)。
- **口令丢失 = coord 库不可恢复**(设计如此;**资金不受影响**,分片在成员设备)。须安全离线备份口令。

## C10. 验收要点(P3)

- 信封提交 → 通知 → 心跳 → 法定人数 → START → 三机经 relay 签名 → `{R,S,V}` 验签 → 回传外部服务,端到端跑通。
- 过期请求在 (a)(b) 两处均被拒,外部服务收到 `EXPIRED`。
- 篡改信封 / 谎报在线 / 不同成员不同信封 → 均无法产出有效签名(对应 C8 各行)。
- coord 宕机重启后**默认 LOCKED**;解锁(正确口令)后状态机从持久化恢复,未过期请求可续;错误口令拒绝且限速。
- 泄露 `.db` 文件无口令不可读;LOCKED 期间任何 API 均 `503 LOCKED` 不泄数据;relay 实例不受 coord 锁定影响。
