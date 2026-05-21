# 架构(指针)

> 整合架构文档已迁至 `docs/ARCHITECTURE.md`(顶级,面向架构师/安全审查/集成者/运维)。
> 实现深度细节仍在 `docs/design/` 全树(每个组件 source-of-truth)。

## 顶级入口(读者无需穿越 17 篇 design/*)

- **架构总览**:`docs/ARCHITECTURE.md` — 系统上下文、组件、部署、时序、信任、安全边界。
- **交付状态**:`docs/DELIVERY.md` — P0-P6 + DM-* + AD-* + cli-ui 实际进度 + 验证方式。
- **使用指南**:`docs/USAGE.md` — 快速开始 / 部署 / A 面/B 面集成 / SDK / wallet-cli / 测试 / 备份 / 监控。

## 深度索引(实现者)

- 总览/范围/信任模型:`docs/design/PLAN.md`、`docs/design/architecture.md`、`docs/design/security.md`
- 端上 SDK(mpc-core/mobile-api/tx-decode/keystore/transport/coord-client):`docs/design/mcp/sdk.md`
- 分布式 MPC(R1-R7)+ 实施件:`docs/design/mcp/distributed-mpc.md`、`distributed-mpc-impl.md`
- 地址派生(HD 非硬化 + chaincode commit-reveal):`docs/design/mcp/address-derivation.md`
- wallet-cli htmx 面板:`docs/design/mcp/walletcli-ui.md`
- node 单二进制双角色(relay/coord):`docs/design/server/server.md`
- coord 持久化(SQLite + 整库加密 + LOCKED):`docs/design/server/database.md`
- 管理面(admin-api/admin-ui):`docs/design/server/admin.md`
- 契约:`docs/design/contract/protocol.md`(线上权威)、`docs/design/contract/api.md`(HTTP/JSON)
- 测试策略 + §3.4 §G 真分布式 MPC E2E:`docs/design/testing.md`
- P0 移动打包:`docs/design/P0-tasks.md`、`docs/design/P0-report.md`

## 实施件(规约层)

- 信封规范化(S-001):`docs/spec/envelope-canonical.md` ✅ 已实施
- 组成员开通(S-002):`docs/spec/group-provisioning.md` ✅ 已实施

## 安全 / 审计

- `docs/security-review.md`(逐红线 + H-005 GREEN)
- `docs/release-readiness-audit.md`(RA-001 独立审计,基线远旧于 HEAD,
  作为方法论参考)
- `docs/gomobile-build-report.md`(.aar 真构实证)
