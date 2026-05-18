// Package addr 提供 secp256k1 公钥到 ETH/BSC/TRON 地址的纯派生（无链上交互）。
// BSC 与 ETH 共用同一 secp256k1 + keccak256 + EIP-55 方案，BSCAddress 委托 ETHAddress；
// TRON 为 Base58Check(0x41 || keccak256[12:])。
package addr
