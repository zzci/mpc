// Package txdecode 提供 ETH/BSC/TRON 三链「只读」交易解码、按链规则重算签名
// 摘要并断言 ==digest32、A 区已校验事实产出、A/B 声明式核对与未识别降级。
//
// 安全攸关：A 区是资金安全的唯一权威依据；解码 bug 不得退化为误签。
// 双重绑定不变量（docs/design/mcp/sdk.md §4、docs/design/PLAN.md §2/§3）——
//
//   - EVM：解析 unsignedTx → 从「解析出的结构化字段」重算
//     Keccak256(RLP / 0x02‖typed-RLP) → 断言 ==digest32。解析错误使重算
//     摘要与 digest32 不符，从而「拒签而非误签」（强双重绑定）。
//   - TRON：链签名为 sha256(raw_data) 本身，无法从字段无歧义重建逐字节
//     protobuf，故重算 = sha256(unsignedTx) 并断言 ==digest32：绑定的是
//     字节↔digest32↔信封（proposerSig 覆盖）。protobuf 解析仅供 A 区展示，
//     其正确性由真实语料 + 模糊测试与「未识别不臆造」兜底（此不对称是链固有，
//     见 docs/design/PLAN.md §5 风险 9 与 §4「TRON:sha256(raw_data)」）。
//
// 任一安全类失败（信封非法 / 摘要不符 / 解析无法绑定）一律硬拒签：返回 error
// 且不返回可展示的 A 区，调用方必须据此拒签、不得进入 MPC（docs/design/mcp/sdk.md §5）。
// 可插拔覆盖（ChainDecoder）允许替换解码器，但 ==digest32 断言由本包框架强制，
// 覆盖实现自动受同一绑定约束。
//
// 范围边界：解码「在内」；交易构造 / calldata 编码 / 广播「在外」（外部业务服务）。
// 权威基线（只读）：docs/design/mcp/sdk.md §4、docs/design/PLAN.md §2/§3、
// docs/design/contract/protocol.md（digest32 / 信封语义）、internal/contract（C-001）。
package txdecode
