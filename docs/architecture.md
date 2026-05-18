# 架构（指针）

> 权威架构 = 定稿 `docs/design/` 全树（main commit 17f0709），勿在此另维护并行副本以防漂移。

- 总览/范围/信任模型：`docs/design/PLAN.md`、`docs/design/architecture.md`、`docs/design/security.md`
- 端上 SDK（mpc-core/mobile-api/tx-decode/keystore/transport/coord-client）：`docs/design/mcp/sdk.md`
- node 单二进制双角色（relay/coord，配置 `relay.enable`/`coord.enable`）：`docs/design/server/server.md`
- coord 持久化（SQLite + 整库加密 + LOCKED）：`docs/design/server/database.md`
- 管理面（admin-api/admin-ui）：`docs/design/server/admin.md`
- 契约：`docs/design/contract/protocol.md`（线上权威）、`docs/design/contract/api.md`（HTTP/JSON）
- 测试策略/阶段门禁：`docs/design/testing.md`；P0：`docs/design/P0-tasks.md`

实施分解（模块/DAG/批次/复用评估）：`docs/plan/PLAN-002.md`（取代 PLAN-001）。
设计审核结论：`docs/review/DESIGN-REVIEW-001.md`（GREEN）。
