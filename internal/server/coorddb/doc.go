// Package coorddb 提供 coord 持久化：SQLite 单文件 + 版本化迁移（禁手改 schema）+
// 整库加密 + LOCKED 生命周期（Argon2id 口令仅内存，fail-closed）+ 内存在线集。
// 具体实现由 D-001 完成（权威：docs/design/server/database.md）。
package coorddb
