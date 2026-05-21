# wallet-cli htmx 检查面板设计

> PC 端钱包成员工具 `cli serve` 的 htmx + tailwindcss 服务端渲染检查面板。
> 关联 `mcp/sdk.md`、`server/admin.md`(对照同款 UI 模式)、`security.md`。
> 性质:开发文档(已交付,见 §10)。

## 1. 定位与边界

`internal/walletcli/`(commit `b36ae4b` / `3bbe502` / `b6531cc`)是 **PC 钱包
成员端**(非移动 SDK,非生产 admin 面),为开发与运维提供:
- 交互式 shell(`cli` 入口,见 `walletcli.go`)。
- HTTP JSON API(`cli serve`,`/api/v1/*`,见 `httpapi.go`)。
- **htmx 服务端渲染面板**(本文件)`/*`,作为 JSON API 的可视化前端。

**不是 admin-ui**:admin-ui(`internal/server/admin/ui.go`)运行在 **coord**
进程,看的是 coord 持久库(交易/审计/relay 指标)。wallet-cli UI 运行在
**成员端 wallet party 进程**,看的是本机 SDK 状态(待签待审/已持有的分片
入口/离线派生地址)。两者文件不相交、信任边界不同。

**不是生产管理面**:wallet-cli UI 默认仅 loopback 监听(`cli serve --listen
127.0.0.1:8787`),供单一操作员在本机访问;非 loopback 须显式设
`MPC_WALLET_HTTP_TOKEN`,fail-closed。

## 2. 路由(`internal/walletcli/ui.go:131-148`)

**JSON API**(`internal/walletcli/httpapi.go`):路径前缀 `/api/v1/*` —— 集成
脚本、自动化、原 `cli` 子进程外的所有机器调用都走这条。

**htmx UI**(`internal/walletcli/ui.go`):路径根 `/*` —— 浏览器导航默认即 UI。
无 `/ui` 前缀;一个反向代理可按 `/api` vs 其他做路径切分。

| 方法 | 路径 | 作用 |
|---|---|---|
| GET | `/assets/htmx.min.js` | vendored htmx |
| GET | `/assets/tw.css` | tailwind 子集(与 admin-ui 同基线,见 §4) |
| GET | `/login` | 登录表单(仅当 `MPC_WALLET_HTTP_TOKEN` 配置时) |
| POST | `/session` | 验 token、设 cookie、跳 `/` |
| POST | `/logout` | 清 cookie、跳回 |
| GET | `/{$}` | 概览(version、auth 模式、pending 计数;exact-match 根) |
| GET | `/sign` | 待签列表(id、age、mismatch 标记) |
| GET | `/sign/{id}` | 待签详情:WYSIWYS A-facts / B-info / mismatch JSON + Approve/Reject |
| POST | `/sign/{id}/approve` | 通过(htmx 内联交换 → `sign_result`) |
| POST | `/sign/{id}/reject` | 拒绝 |
| GET | `/import` | 备份恢复表单(multipart) |
| POST | `/import` | 处理 ExportShare 备份,$MPC_WALLET_PASSPHRASE 必备 |
| GET | `/fetch` | 经 coord 查询交易信息(只读) |
| POST | `/fetch` | 同上 |
| GET | `/xpub` | 拉 owning-member xpub(只读,memberGate) |
| POST | `/xpub` | 同上 |
| GET | `/address` | 离线 BIP32 非硬化派生 m/i |
| POST | `/address` | 同上 |
| GET | `/api/v1/{health,version,…}` | 机器调用面(同操作的 JSON 等价端点),详见 `httpapi.go` |

## 3. 鉴权与会话

- **Token 模式**(`MPC_WALLET_HTTP_TOKEN` 设):
  - 浏览器从 `/login` 提交 token → POST `/session` 常时比较 → 派生
    256-bit 随机 sid → `Set-Cookie: wallet_ui_sid=<sid>; Path=/;
    HttpOnly; SameSite=Strict; MaxAge=30m`。
  - 后续请求:cookie 或 `Authorization: Bearer <token>` 二选一即放行
    (代理/自动化用 Bearer,人用 cookie)。token 永不回显给浏览器。
  - Bearer 但 token 不匹配 → `401`(自动化反馈错误);浏览器缺凭据 → `303 → /login`。
- **无 token 模式**(默认 loopback):JSON guard 已允许无鉴权;UI 同步跳过
  session 流(`/login` 重定向到 `/`),全部路由开放。
- **CSRF**:依赖 `SameSite=Strict` cookie + `HttpOnly`;同源策略阻止跨站
  form submission。不引入额外 CSRF token(单租户工具,SameSite=Strict
  已足够;参考 admin-ui 同款决策)。

## 4. 资产与模板

- `internal/walletcli/uiassets/htmx.min.js`:从
  `internal/server/admin/uiassets/htmx.min.js` 复制(同 vendored 版本)。
- `internal/walletcli/uiassets/tw.css`:tailwind utility subset,与
  admin-ui 基线相同(同字节)以便两面板样式同步维护。
- 模板(`internal/walletcli/uiassets/templates/*.tmpl`):
  - `partials.tmpl`:`head` / `nav` / `foot`
  - `login.tmpl` / `index.tmpl`
  - `sign_list.tmpl` / `sign_detail.tmpl` / `sign_result.tmpl`(htmx 片段)
  - `import.tmpl` / `import_result.tmpl`(htmx 片段)
  - `fetch.tmpl` / `xpub.tmpl` / `address.tmpl` / `query_result.tmpl`
  - `error.tmpl`

模板与 ui.go 通过 `//go:embed uiassets/htmx.min.js uiassets/tw.css
uiassets/templates/*.tmpl` 嵌入 Go 二进制,**无独立前端构建**。

## 5. WYSIWYS 流程(`/sign`)

1. 操作员或自动化通过 JSON `POST /api/v1/sign` 提交 START envelope → coord
   逻辑跑 prepareSign → 设备侧解码 → 进入 pending 状态(decode 字段持
   入 `pendingSign.decoded` 供 UI 渲染)。
2. 浏览器 GET `/sign/{id}` 显示 A-facts / B-info / mismatch JSON
   pretty-printed;mismatch=`[]` 时显示 `clean` 徽章,非空显 `flagged`。
3. 操作员 `Approve`:`POST /sign/{id}/approve` → 后端调用
   `pendingSign.ss.Approve()` → MPC 跑 → terminal callback 触发 →
   htmx 片段 `sign_result.tmpl` 内联交换显示 RSV(hex)或错误码。
4. `Reject` 同路径不进 MPC。

**WYSIWYS 不变量(security.md §1)**:UI 路径与 JSON 路径**共享同一
`pendingSign` map + 同一 `signSession` 接口**;UI 不是另一条审批渠道,
仅 JSON 的可视化封装。terminal callback 后从 map 中 pop,host 关闭,
context 取消(`internal/walletcli/ui.go:344-360`)。

## 6. 默认安全值(UI Q1)

UI 仅暴露**只读 + 已存在的批准动作**(同 admin-ui Q1 默认):
- 待签列表/详情(只读)、approve/reject(已存在 JSON 路径,UI 是视图)
- 备份导入(`/import`,passphrase env-only,unset 即 disabled fail-fast)
- 只读查询:fetch、xpub、address(无密钥/无副作用)

**不暴露**:
- `keygen` / `reshare` / `export`:销毁性或备份导出敏感,JSON-only。
- `wire`:内部 MPC 消息管道,人工不应触发。
- passphrase 不接受经 HTTP 入(env-only,与 JSON API 同纪律)。

## 7. 安全约束

- **Loopback 默认**:非 loopback `--listen` 须 `MPC_WALLET_HTTP_TOKEN`,
  fail-closed(`httpapi.go:48`)。
- **token 常时比较**:`subtle.ConstantTimeCompare`(`ui.go:209`)。
- **CSP 严格**:`default-src 'none'; script-src 'self'; style-src 'self';
  connect-src 'self'; form-action 'self'; base-uri 'none';
  frame-ancestors 'none'`(`ui.go:386-389`)。
- **import 上限**:1 MiB(`importMaxBytes`),防 DoS。
- **query 上限**:fetch/xpub 256 KiB(`queryMaxBytes`)。
- **address index 边界**:`[0, 2^31)`(BIP32 非硬化),内置守护
  `strconvParseUint32`。
- **不读 keystore 明文**:UI 仅引用 SDK + pending map;keystore 经
  passphrase env 解封一次,UI 无第二路径接触。

## 8. 测试(`internal/walletcli/ui_test.go`)

- auth-loopback / token-gate / session 流 + cookie 严格性
- 资产 content-type 与非空
- 待签列表 / 详情 / mismatch 标记 / 404
- htmx approve+reject 片段 + pending pop
- import 表单 passphrase-未设警告 / 已设可用 / no-passphrase POST
  failure / 无文件 / 坏 blob 全覆盖
- fetch/xpub/address 表单字段渲染 + nav 高亮
- `strconvParseUint32` 单元(0、1、max、溢出、负数、空、非十进制、hex 前缀)

全套 `-race` 通过(commit `b6531cc` 验证 `golangci-lint v2 0 issues`)。

## 9. 与 admin-ui 的关系

| 维度 | admin-ui | wallet-cli UI |
|---|---|---|
| 宿主 | coord 进程(`internal/server/admin/`) | wallet-cli 进程(`internal/walletcli/`) |
| 数据源 | coord SQLite(`signing_requests` / `admin_audit` / relay 指标) | SDK 状态 + 内存 pending map + 离线派生 |
| 鉴权 | StrongAuth(mTLS/OIDC seam)+ Bearer 双 token(读/控分离) | 单 Bearer token(`MPC_WALLET_HTTP_TOKEN`)+ cookie session |
| LOCKED 行为 | 锁定下仅显示锁定态(admin.md §8) | 不涉及(wallet-cli 无 DB) |
| 控制操作 | 默认只读(unlock/封禁/PSK 轮换为可选) | 仅 import + sign approve/reject(无 keygen/reshare/export) |
| 网络暴露 | 内网/VPN/mTLS + IP allowlist | 默认 loopback;非 loopback 强 token |
| 部署 | `//go:embed` 嵌入 admin-api 进程 | `//go:embed` 嵌入 wallet-cli 进程 |
| CSS | 同 tw.css 基线 | 同 tw.css 基线(刻意保持一致) |

## 10. 交付历史

- `b36ae4b` 2026-05-20 — htmx 面板 + WYSIWYS sign approval(主轮廓)
- `3bbe502` 2026-05-20 — `/import` 备份恢复(passphrase env-only)
- `b6531cc` 2026-05-20 — `/fetch` / `/xpub` / `/address` 只读补全

所有 hard gates GREEN:gofmt / build / vet / race `-timeout=1200s` 19/19
packages / golangci-lint walletcli 0 issues。
