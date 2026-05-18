package node

// DB 解锁口令处理的接口位（docs/design/server/server.md C9b、server/database.md §7）。
//
// coord 持久库整库加密；派生密钥的运营方口令**绝不**进入配置文件 / 环境变量 / KMS
// （入了即丧失防文件泄露意义）。因此本配置包不提供任何口令字段，Config 也不引用本
// 接口；口令仅在 coord 运行期经 admin-api 解锁交互在内存提供、重锁即清零。
//
// 本任务（N-001）仅声明接口位，不实现解锁；解锁/重锁/限速退避属 X-001 / admin
// territory（server/admin.md）。relay 角色无 DB，不涉及解锁。

// UnlockProvider 提供 coord 整库加密的内存驻留解锁口令。
// 实现由 admin-api 在解锁交互时注入；调用方用毕应及时清零返回的字节。
type UnlockProvider interface {
	// Passphrase 返回当前内存中的解锁口令；LOCKED 期间应返回错误。
	Passphrase() ([]byte, error)
}
