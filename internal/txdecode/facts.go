package txdecode

import "math/big"

// Chain is the normalized chain identifier the decoder dispatches on. The
// envelope's req.Chain is an opaque label (docs/design/contract/protocol.md:17);
// normalizeChain maps its known spellings to one of these.
type Chain string

const (
	// ChainETH is Ethereum mainnet/EVM (secp256k1, keccak256 signing hash).
	ChainETH Chain = "eth"
	// ChainBSC is BNB Smart Chain; EVM-identical signing scheme to ETH.
	ChainBSC Chain = "bsc"
	// ChainTRON is the TRON network (sha256(raw_data) signing hash).
	ChainTRON Chain = "tron"
)

// TxType labels the recognized transaction shape. Unrecognized shapes degrade
// to a *Raw* variant with a caution warning; the decoder never fabricates a
// label it did not positively recognize (docs/design/mcp/sdk.md §4).
type TxType string

const (
	// TxEVMLegacy is a legacy / EIP-155 EVM transaction.
	TxEVMLegacy TxType = "evm-legacy"
	// TxEVM1559 is an EIP-1559 (type 0x02) EVM transaction.
	TxEVM1559 TxType = "evm-1559"
	// TxTronTransfer is a native TRX TransferContract.
	TxTronTransfer TxType = "tron-transfer"
	// TxTronTrigger is a TriggerSmartContract (incl. TRC20 transfer).
	TxTronTrigger TxType = "tron-trigger"
	// TxTronStake is a Stake 2.0 system contract.
	TxTronStake TxType = "tron-stake"
	// TxTronOther is a structurally valid TRON contract whose type is not in
	// the supported set: raw type number is shown, never a guessed label.
	TxTronOther TxType = "tron-other"
	// TxTronUnparsed is TRON raw_data whose protobuf could not be walked at
	// all; bytes remain digest-bound but no facts are asserted (strong
	// caution; the human reviewer is the backstop, WYSIWYS).
	TxTronUnparsed TxType = "tron-unparsed"
)

// CallKind classifies a decoded contract call. Only positively recognized
// kinds are labeled; anything else is CallUnrecognized with raw bytes.
type CallKind string

const (
	// CallERC20Transfer is ERC20/TRC20 transfer(address,uint256).
	CallERC20Transfer CallKind = "erc20-transfer"
	// CallERC20Approve is ERC20/TRC20 approve(address,uint256).
	CallERC20Approve CallKind = "erc20-approve"
	// CallERC20TransferFrom is ERC20 transferFrom(address,address,uint256).
	CallERC20TransferFrom CallKind = "erc20-transferFrom"
	// CallUnrecognized means the selector was not positively recognized: the
	// raw selector and calldata are surfaced with a caution warning and NO
	// fabricated method name (docs/design/mcp/sdk.md §4: do not fabricate an unrecognized call).
	CallUnrecognized CallKind = "unrecognized"
)

// DecodedCall is the structured view of contract calldata. It is only
// populated when a call is present; recognized fields are set only for
// positively recognized selectors.
type DecodedCall struct {
	Kind CallKind
	// Selector is the 0x-prefixed 4-byte function selector (hex).
	Selector string
	// TokenContract is the called contract address (the token for ERC20/
	// TRC20), formatted per chain (EIP-55 hex / TRON base58).
	TokenContract string
	// Recipient/Amount are set only for recognized transfer/approve/
	// transferFrom; recipient formatted per chain.
	Recipient string
	Amount    *big.Int
	// From is the funds source for transferFrom (recognized only).
	From string
	// RawCallData is the full calldata, always retained so an unrecognized
	// call can still be inspected raw by the human reviewer.
	RawCallData []byte
}

// Facts is the A-zone: the to/value/chain/contract/method facts decoded from
// unsignedTx. It is the *single* authority for fund safety and is returned
// only after the recomputed chain digest has been asserted == digest32
// (docs/design/PLAN.md §3 display-contract A-zone).
type Facts struct {
	Chain  Chain
	TxType TxType
	// ChainID is the signed EVM chain id (authoritative; embedded in the
	// signing preimage). nil for TRON and pre-EIP-155 legacy.
	ChainID *big.Int
	// From is the funds source. EVM unsigned txs do not carry the sender
	// (recovered from the signature), so it is empty for EVM; for TRON it is
	// owner_address (base58).
	From string
	// To is the recipient. For a contract call it is the contract address;
	// the effective payee of a recognized token transfer is Call.Recipient.
	To string
	// Value is the native value moved (wei for EVM, sun for TRON).
	Value *big.Int
	// EVM gas/nonce facts (nil/zero for TRON).
	Nonce                *uint64
	GasLimit             *uint64
	GasPrice             *big.Int // legacy
	MaxFeePerGas         *big.Int // EIP-1559
	MaxPriorityFeePerGas *big.Int // EIP-1559
	// Data is the raw EVM calldata / TRON contract data (nil if none).
	Data []byte
	// Call is the decoded calldata view (nil if no call data).
	Call *DecodedCall
	// TronContractType is the raw TRON Contract.type enum value, retained so
	// an unsupported/other type is shown by number, never a guessed name.
	TronContractType int32
	// TronContractName is the recognized TRON contract name when in the
	// supported set; empty otherwise (never fabricated).
	TronContractName string
	// Warnings are prominent human-review cautions: unrecognized call,
	// chainId/label mismatch, unparsed TRON, A/B discrepancies. Non-fatal:
	// they are surfaced loudly for the human, distinct from a hard rejection.
	Warnings []string
}

// Mismatch is one A/B declarative discrepancy: a businessInfo.displayHints
// claim that does not match the digest-bound A-zone fact. A/B mismatch is a
// prominent warning for the human reviewer (B is out-of-band, not chain-
// binding), NOT a hard rejection — only digest mismatch hard-rejects
// (docs/design/PLAN.md §3 trust boundary, docs/design/mcp/sdk.md §4).
type Mismatch struct {
	Field    string
	Expected string
	Actual   string
}

// Result is the decode outcome. It is only returned when DigestVerified is
// true (recomputed chain digest == req.Digest32); a verification failure
// returns an error and no Result so unverified facts can never reach the UI
// as the authoritative A-zone.
type Result struct {
	Facts          *Facts
	Mismatches     []Mismatch
	DigestVerified bool
}

func (f *Facts) warn(msg string) { f.Warnings = append(f.Warnings, msg) }
