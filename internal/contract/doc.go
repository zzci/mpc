// Package contract 定义 protocol.md 权威类型（信封 SigningRequest / MpcMessage /
// CapToken / START StartSigning）与 S-001 唯一规范化序列化：proposerSig 预映像
// 与 metaHash 均从逻辑字段值确定性构造，绝不取 JSON/protobuf 线格式字节，故同一
// 逻辑信封经 JSON 提交与经 protobuf 下发产出逐字节一致的待签/待哈希输入。另含
// sessionId 隔离、senderAuth、version 协商。
// 权威基线（只读）：docs/design/contract/protocol.md、docs/design/contract/api.md、
// docs/spec/envelope-canonical.md。
package contract
