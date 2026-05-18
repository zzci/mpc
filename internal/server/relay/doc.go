// Package relay 实现 node 的 relay 角色：circuit-relay v2(HOP) + rendezvous + 访问控制
// （pnet PSK + 能力令牌 + 配额），无状态、只中转密文、不依赖 coord。
//
// 零信任为构造性保证：成员间 Noise 会话端到端、不在 relay 终结，relay 只转发密文，
// 既读不到 MPC 内容也无法伪造发件人（server.md R2）。relay 无状态、不持分片、无 DB，
// 且不依赖 coord（server.md R5）：本包绝不 import internal/server/coord，relay 启动
// 不需要任何 coord 配置。
//
// 访问控制分层（server.md R4）：pnet PSK 由 libp2p 自身强制（无 swarm key 无法说协议）；
// CapToken 在 (pnet+Noise) 安全连接建立后经 CapProtocolID 出示，再于 circuit-relay
// ACL（relay-reserve）与 rendezvous 处理器（rendezvous-register）强制——ConnectionGater
// 在连接建立期看不到应用层令牌，故令牌强制点为 ACL/handler 而非 gater，以 libp2p API
// 落地 R4 意图；配额为每令牌/每组预约上限 + circuitv2 单路 Data/Duration 上限。
//
// 权威基线（只读）：docs/design/server/server.md「第一部分:relay 角色」、
// docs/design/contract/protocol.md §6。由 N-002 实现。
package relay
