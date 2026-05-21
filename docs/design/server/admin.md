# 管理面设计(server / admin)

> 单一全局**运维管理员**:查看交易记录与历史签名会话、日志/指标、防滥用控制。
> 关联 `server/server.md`、`server/database.md`、`contract/api.md`、`security.md`。性质:开发文档,不写代码。
>
> **不改信任锚**:管理员**不签发成员准入**;谁能连仍由钱包组组密钥签发的能力令牌决定(自主式,开放点 4 不变)。管理员仅运维级闸刀。

## 1. 范围

**可见(只读)**
- **交易记录**:每请求解码后详情(链/收款方/金额/合约/方法/`businessInfo`),按组/时间/状态/proposer 检索。
- **历史签名会话**:请求全生命周期 —— 状态时间线(`request_events`)、参与方/signers、审批记录、结果 `{R,S,V}`、耗时。
- 日志/指标/健康:coord `request_events`、relay 连接/预约/转发计数与拒绝原因。

**可控(防滥用,与威胁模型 A 一致)**
- 配额/限流参数(relay 预约/带宽、coord 速率)。
- 封禁 peerID / 撤销 circuit-relay 预约(运维闸刀)。
- 轮换 pnet PSK;查看/热载非 secret 配置。
- **DB 解锁 / 重锁**(口令);查看锁定状态(见 §8)。

**不可见 / 不可做(硬边界)**
- 看不到分片、MPC 报文载荷(relay 零信任,密码学保证,无人能读)。
- **不管理成员资格 / 不签发准入**(自主式不变);不能放行未持组令牌者绕过访问控制。
- 偷不走钱、伪造不了审批(门限 + 设备 WYSIWYS 兜底)。
- 解码仅**展示用**:管理面服务端用 `tx-decode` 库渲染可读详情,**非安全控制**;资金安全唯一权威仍是设备侧 WYSIWYS(security.md)。

## 2. 隐私定位

coord 进程经 API 本就接收信封明文(必须,才能入待签列表/编排)→ 管理员可见**不增加信任增量**。静态加密(`unsigned_tx`/`business_info`,database.md §7)降级为**仅防数据库文件被盗**的纵深防御,不防运营方。`security.md` 残余风险已记:**coord 运营方/管理员为交易隐私可信方**;资金安全不受影响。

## 3. 组件

| 模块 | 职责 | 语言 |
|---|---|---|
| `admin-api` | coord 内的管理接口:只读查询(交易/会话/审计/计数)+ 控制(配额/封禁/PSK 轮换);所有操作写 `admin_audit` | Go |
| `admin-ui` | 运维 Web 控制台,消费 `admin-api`;非公网暴露(见 §5) | **htmx + tailwindcss**(用户裁定 2026-05-18) |

> **admin-ui 技术栈与约束(用户裁定 2026-05-18)**:**htmx + tailwindcss**,**服务端渲染**,由 `node`(admin 角色)直接 serve HTML(无独立前端构建/无 SPA;htmx 单文件 vendored,tailwind 用 standalone CLI 产静态 css 或 CDN——不引入 Node 构建链入 Go server)。供运维**检查与核查**:§1 可见(只读)面——待签/请求全生命周期(`request_events` 时间线/signers/审批/`{R,S,V}`/耗时)、组/成员/连通性、relay 计数、`admin_audit`、LOCKED 态。**安全约束(不可破)**:必须挂在 §4 强鉴权(mTLS/OIDC+2FA、管理员身份独立)之后 + §5 非公网/内网-VPN/端口与外部·成员 API 隔离 + IP 允许列表;**不得暴露超出 admin 既有授权的数据**(与 A1↔§5.1 路由分离同一信任最小化纪律);所有经 UI 触发的控制操作(若启用)同样写 `admin_audit`、带 CSRF 防护。**默认只读核查面**;是否经 UI 暴露既有 admin 控制操作(unlock/relock/封禁/配额)= 用户决策项(见下)。

## 4. 鉴权与操作审计

- 管理员身份**独立**于成员/外部服务;强鉴权(mTLS 或 OIDC + 2FA)。
- 即便单一管理员,**读权限与控制权限分离令牌**(最小权限;误操作面收敛)。
- **全部管理操作**(含只读敏感查询可选)写 `admin_audit`(谁/何时/什么/参数/来源 IP);审计不可由管理员删除。
- 管理面 = 新攻击面:RBAC 即便单人也按角色建模,便于将来扩展。

## 5. 部署与暴露

- `admin-api` 与 `admin-ui` 同进程:`internal/server/admin/ui.go` 通过
  `//go:embed uiassets/{htmx.min.js, tw.css, templates/*.tmpl}` 嵌入 Go 二进制,
  由 `admin-api` 同一 `http.ServeMux` 直接 serve(commit `b36ae4b` 之前已落地)。
  **无独立前端部署步骤、无 Node 构建链、无 npm install**;升级 admin-api 即升级 UI。
- **不对公网暴露**:仅内网 / VPN / mTLS / IP 允许列表(`s.netGate` 已实施
  IP allowlist);与外部业务服务、成员 API 端口隔离(`coord.admin.listen` ≠
  `coord.external.listen` ≠ `coord.member.listen`)。
- 单一全局管理员(非每组);组级别可见性通过查询过滤,不引入组管理员角色。
- 路由表(实测,`internal/server/admin/`):
  - JSON API(`server.go:103-118`):`POST /api/{unlock,relock}`、
    `GET /api/lock-status`、`GET /api/transactions{,/<requestId>}`、
    `GET /api/audit`、`GET /api/relay/metrics`、
    `POST /api/controls/{ban-peer,revoke-reservation,rotate-psk,quota}`
  - **设备配对**(可选,需 `WithPairing` 装配,见 `server/pairing.md`):
    `POST /api/pairing`、`GET /api/pairing`、`DELETE /api/pairing/{token}`、
    `GET /api/pairing/{token}/qr.png`(image/png QR)
  - htmx UI(`ui.go:158-174`):
    - 静态资产:`GET /assets/{htmx.min.js,tw.css}`
    - 鉴权:`GET /login`、`POST /session`、`POST /logout`
    - 仪表盘(LOCKED 可达):`GET /`(exact match `/{$}`)
    - 数据页(LOCKED fail-closed):`GET /transactions{,/<requestId>}` /
      `/audit` / `/relay`
    - 配对页(可选):`GET /pairing`、`POST /pairing`、`POST /pairing/{token}/delete`
  - 健康检查:`GET /healthz`

## 6. 数据来源

- 交易/会话/审计:coord SQLite(`signing_requests`、`request_events`、`request_approvals`、`groups`/`group_members` 只读;见 database.md)。**保留策略改长期/归档**以支撑历史(database.md §6)。
- relay 计数:relay 角色可观测指标(server.md R6),只读聚合,不含载荷。
- 在线状态:内存 SQLite(瞬时,展示当前在线;不入历史)。

## 8. DB 锁定管理(整库加密,防文件泄露)

- coord 持久库默认 **LOCKED**(整库加密,server/database.md §7、server.md C9b)。**所有 §1 可见数据在 LOCKED 下均不可得**(API `503 LOCKED`)—— 管理面本身在解锁前**只**暴露解锁入口与锁定状态。
- **解锁**:`admin-api` 解锁端点,管理员强鉴权 + 输入口令 → Argon2id 派生密钥(仅内存)→ 挂载库 → UNLOCKED。
- **重锁**:管理员主动 `relock`,或可选**空闲超时自动重锁**(配置项)→ 清零内存密钥 → LOCKED。
- **口令处理**:仅经解锁交互传入,**不入配置/env/KMS/日志**;内存驻留,重锁/退出即清零(zeroize)。
- **尝试审计**:LOCKED 下无法写加密库 → 解锁尝试(成功/失败/来源)记进程日志/指标 + 限速退避防爆破;解锁成功后补记 `admin_audit`。
- **口令丢失不可恢复**(资金不受影响,见 server.md C9b);安全离线备份口令为运维职责。

## 7bis. 验收(P3/P6)

- 管理员可检索任一历史请求的解码详情 + 完整状态时间线 + 审批 + 结果。
- 控制操作(封 peerID/调配额/轮换 PSK/解锁/重锁)生效且全部进 `admin_audit`,管理员无法篡改审计。
- 管理员**无法**:看分片/MPC 明文、绕过自主式访问控制放行陌生 peer、伪造审批或动用资金。
- `admin-ui` 非公网可达;读/控权限分离生效。
- LOCKED 默认成立:重启后非解锁不提供任何数据;错误口令限速;泄露 `.db` 无口令不可读;重锁后再次拒服务。
