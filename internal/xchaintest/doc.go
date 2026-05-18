// Package xchaintest 是三链真实摘要 {R,S,V} 交叉验证的集成测试包（仅测试，
// 不导出任何实现）。端到端串联 internal/txdecode（真实 unsignedTx 解码 +
// 重算链摘要断言 ==digest32）→ internal/mpc（进程内门限签名出 {R,S,V}）→
// secp256k1 ecrecover / stdlib 验签 → internal/addr 地址派生交叉一致。
//
// 覆盖 ETH(legacy/EIP-155)、BSC(EIP-1559)、TRON(原生 TransferContract);
// 断言 low-S 规范化(S<=N/2)与 recovery id V 正确(精确 V 还原组主公钥、
// 翻转 V 不还原)。权威基线(只读):docs/design/testing.md §3、docs/design/PLAN.md §2、
// docs/design/mcp/sdk.md §4。本包不改动 mpc/txdecode/addr/contract 任何实现。
package xchaintest
