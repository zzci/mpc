package coorddb

import "errors"

// ErrLocked 表示持久库处于 LOCKED（未挂载、内存无密钥）。任何数据访问在 LOCKED
// 下一律返回此错误（fail-closed）；上层（X-001/admin）据此映射为 503 LOCKED
// （docs/design/server/server.md C9b、server/database.md §7、docs/spec/group-provisioning.md §9）。
var ErrLocked = errors.New("coorddb: store is LOCKED")

// ErrUnlocked 表示在已 UNLOCKED 状态下重复解锁。
var ErrUnlocked = errors.New("coorddb: store already UNLOCKED")

// ErrEmptyPassphrase 表示解锁口令为空。口令仅运行期经接口传入，本模块不持久化、
// 不读配置/env（database.md §7：入了即丧失防文件泄露意义）。
var ErrEmptyPassphrase = errors.New("coorddb: empty passphrase")

// ErrPlaintextMode 表示对 dev/test 整库加密禁用模式（NewPlaintextStore，
// database.md §7.1）的 Store 调用 Unlock——禁用态不派生密钥、不经口令解锁。
var ErrPlaintextMode = errors.New("coorddb: store is in plaintext (encryption-disabled) mode; Unlock not applicable")

// ErrNotPlaintext 表示对加密 Store（NewStore）调用 OpenInsecure——明文挂载
// 仅限 NewPlaintextStore 构造的禁用模式 Store。
var ErrNotPlaintext = errors.New("coorddb: OpenInsecure requires a plaintext (encryption-disabled) store")
