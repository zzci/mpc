// Package addr provides pure derivation of ETH/BSC/TRON addresses from
// a secp256k1 public key (no on-chain interaction). BSC shares ETH's
// secp256k1 + keccak256 + EIP-55 scheme (BSCAddress delegates to
// ETHAddress); TRON is Base58Check(0x41 || keccak256[12:]).
package addr
