package txdecode

import (
	"fmt"
	"math/big"

	"github.com/umbracle/fastrlp"
)

// EIP-1559 typed-transaction envelope prefix (docs/design/mcp/sdk.md §4
// "Keccak256(RLP/typed)"). A legacy unsigned tx is a bare RLP list whose
// first byte is >= 0xC0, so 0x02 unambiguously selects the 1559 form.
const eip1559TxType = 0x02

// known EVM mainnet chain ids used only for an advisory label cross-check;
// the embedded signed chainId is authoritative.
const (
	chainIDEth = 1
	chainIDBsc = 56
)

// ERC20 / TRC20 selectors positively recognized. Anything else degrades to
// CallUnrecognized with raw calldata (never a fabricated label).
var (
	selTransfer     = []byte{0xa9, 0x05, 0x9c, 0xbb} // transfer(address,uint256)
	selApprove      = []byte{0x09, 0x5e, 0xa7, 0xb3} // approve(address,uint256)
	selTransferFrom = []byte{0x23, 0xb8, 0x72, 0xdd} // transferFrom(address,address,uint256)
)

// evmDecoder is the built-in ETH/BSC decoder. expectChainID is the chain id
// the advisory envelope label implies (0 = no expectation).
type evmDecoder struct {
	chain         Chain
	expectChainID int64
}

// Recompute parses the EVM unsigned tx, then *recomputes* the signing digest
// from the parsed structured fields (not by hashing the input): a decode bug
// changes the re-encoded preimage so the digest no longer equals digest32 and
// the framework hard-rejects — "reject rather than mis-sign" (docs/design/mcp/sdk.md §4). A
// malformed/unsupported encoding returns ErrDecodeRejected (cannot bind).
func (d *evmDecoder) Recompute(unsignedTx []byte) (*Facts, [32]byte, error) {
	var zero [32]byte
	if len(unsignedTx) == 0 {
		return nil, zero, fmt.Errorf("%w: empty unsignedTx", ErrDecodeRejected)
	}

	typed := unsignedTx[0] == eip1559TxType
	body := unsignedTx
	if typed {
		body = unsignedTx[1:]
	}

	p := &fastrlp.Parser{}
	root, err := p.Parse(body)
	if err != nil {
		return nil, zero, fmt.Errorf("%w: rlp: %w", ErrDecodeRejected, err)
	}
	elems, err := root.GetElems()
	if err != nil {
		return nil, zero, fmt.Errorf("%w: tx is not an RLP list", ErrDecodeRejected)
	}

	if typed {
		return d.recompute1559(elems)
	}
	return d.recomputeLegacy(elems)
}

// bigAt reads list element i as a big.Int (RLP integers are big-endian byte
// strings; empty == 0).
func bigAt(elems []*fastrlp.Value, i int) (*big.Int, error) {
	b := new(big.Int)
	if err := elems[i].GetBigInt(b); err != nil {
		return nil, fmt.Errorf("%w: element %d not a scalar: %w", ErrDecodeRejected, i, err)
	}
	return b, nil
}

// addrAt reads list element i as an EVM address: 20 bytes, or empty for
// contract creation. Any other length is malformed -> rejection.
func addrAt(elems []*fastrlp.Value, i int) (raw []byte, creation bool, err error) {
	b, e := elems[i].Bytes()
	if e != nil {
		return nil, false, fmt.Errorf("%w: element %d not bytes: %w", ErrDecodeRejected, i, e)
	}
	switch len(b) {
	case 0:
		return nil, true, nil
	case 20:
		return b, false, nil
	default:
		return nil, false, fmt.Errorf("%w: 'to' is %d bytes, want 20 or 0", ErrDecodeRejected, len(b))
	}
}

func u64ptr(b *big.Int) *uint64 {
	if !b.IsUint64() {
		return nil
	}
	v := b.Uint64()
	return &v
}

func (d *evmDecoder) recomputeLegacy(elems []*fastrlp.Value) (*Facts, [32]byte, error) {
	var zero [32]byte
	var chainID *big.Int
	switch len(elems) {
	case 6: // pre-EIP-155 (no chainId, no replay protection)
	case 9: // EIP-155: [...6, chainId, 0, 0]
	default:
		return nil, zero, fmt.Errorf("%w: legacy tx has %d elements, want 6 or 9", ErrDecodeRejected, len(elems))
	}

	nonce, err := bigAt(elems, 0)
	if err != nil {
		return nil, zero, err
	}
	gasPrice, err := bigAt(elems, 1)
	if err != nil {
		return nil, zero, err
	}
	gas, err := bigAt(elems, 2)
	if err != nil {
		return nil, zero, err
	}
	toRaw, creation, err := addrAt(elems, 3)
	if err != nil {
		return nil, zero, err
	}
	value, err := bigAt(elems, 4)
	if err != nil {
		return nil, zero, err
	}
	data, err := elems[5].Bytes()
	if err != nil {
		return nil, zero, fmt.Errorf("%w: data not bytes: %w", ErrDecodeRejected, err)
	}

	a := &fastrlp.Arena{}
	arr := a.NewArray()
	arr.Set(a.NewBigInt(nonce))
	arr.Set(a.NewBigInt(gasPrice))
	arr.Set(a.NewBigInt(gas))
	arr.Set(toValue(a, toRaw))
	arr.Set(a.NewBigInt(value))
	arr.Set(a.NewCopyBytes(data))
	if len(elems) == 9 {
		chainID, err = bigAt(elems, 6)
		if err != nil {
			return nil, zero, err
		}
		// EIP-155 trailing (chainId, 0, 0); the two zeros must be empty.
		for _, i := range []int{7, 8} {
			z, e := bigAt(elems, i)
			if e != nil {
				return nil, zero, e
			}
			if z.Sign() != 0 {
				return nil, zero, fmt.Errorf("%w: EIP-155 trailing element %d non-zero", ErrDecodeRejected, i)
			}
		}
		arr.Set(a.NewBigInt(chainID))
		arr.Set(a.NewBigInt(big.NewInt(0)))
		arr.Set(a.NewBigInt(big.NewInt(0)))
	}
	digest := keccak256(arr.MarshalTo(nil))

	f := d.baseFacts(TxEVMLegacy, chainID, toRaw, creation, value, data)
	f.Nonce = u64ptr(nonce)
	f.GasLimit = u64ptr(gas)
	f.GasPrice = gasPrice
	if len(elems) == 6 {
		f.warn("CAUTION: pre-EIP-155 legacy tx — no embedded chainId, no replay protection")
	}
	d.crossCheckChainID(f, chainID)
	decodeCall(f, d.chain, toRaw, data)
	return f, digest, nil
}

func (d *evmDecoder) recompute1559(elems []*fastrlp.Value) (*Facts, [32]byte, error) {
	var zero [32]byte
	if len(elems) != 9 {
		return nil, zero, fmt.Errorf("%w: EIP-1559 tx has %d elements, want 9", ErrDecodeRejected, len(elems))
	}
	chainID, err := bigAt(elems, 0)
	if err != nil {
		return nil, zero, err
	}
	nonce, err := bigAt(elems, 1)
	if err != nil {
		return nil, zero, err
	}
	maxPrio, err := bigAt(elems, 2)
	if err != nil {
		return nil, zero, err
	}
	maxFee, err := bigAt(elems, 3)
	if err != nil {
		return nil, zero, err
	}
	gas, err := bigAt(elems, 4)
	if err != nil {
		return nil, zero, err
	}
	toRaw, creation, err := addrAt(elems, 5)
	if err != nil {
		return nil, zero, err
	}
	value, err := bigAt(elems, 6)
	if err != nil {
		return nil, zero, err
	}
	data, err := elems[7].Bytes()
	if err != nil {
		return nil, zero, fmt.Errorf("%w: data not bytes: %w", ErrDecodeRejected, err)
	}
	if elems[8].Type() != fastrlp.TypeArray {
		return nil, zero, fmt.Errorf("%w: accessList is not a list", ErrDecodeRejected)
	}

	a := &fastrlp.Arena{}
	arr := a.NewArray()
	arr.Set(a.NewBigInt(chainID))
	arr.Set(a.NewBigInt(nonce))
	arr.Set(a.NewBigInt(maxPrio))
	arr.Set(a.NewBigInt(maxFee))
	arr.Set(a.NewBigInt(gas))
	arr.Set(toValue(a, toRaw))
	arr.Set(a.NewBigInt(value))
	arr.Set(a.NewCopyBytes(data))
	arr.Set(rebuildValue(a, elems[8])) // accessList: structural passthrough
	raw := arr.MarshalTo(nil)
	digest := keccak256(append([]byte{eip1559TxType}, raw...))

	f := d.baseFacts(TxEVM1559, chainID, toRaw, creation, value, data)
	f.Nonce = u64ptr(nonce)
	f.GasLimit = u64ptr(gas)
	f.MaxFeePerGas = maxFee
	f.MaxPriorityFeePerGas = maxPrio
	d.crossCheckChainID(f, chainID)
	decodeCall(f, d.chain, toRaw, data)
	return f, digest, nil
}

func (d *evmDecoder) baseFacts(t TxType, chainID *big.Int, toRaw []byte, creation bool, value *big.Int, data []byte) *Facts {
	f := &Facts{Chain: d.chain, TxType: t, ChainID: chainID, Value: value}
	if len(data) > 0 {
		f.Data = data
	}
	if creation {
		f.warn("CAUTION: contract-creation transaction — no recipient address")
	} else if len(toRaw) == 20 {
		f.To = toChecksumHex(toRaw)
	}
	return f
}

func (d *evmDecoder) crossCheckChainID(f *Facts, chainID *big.Int) {
	if d.expectChainID == 0 || chainID == nil {
		return
	}
	if chainID.Cmp(big.NewInt(d.expectChainID)) != 0 {
		f.warn(fmt.Sprintf(
			"CAUTION: signed chainId %s does not match envelope chain label %q (expected %d) — embedded chainId is authoritative",
			chainID, d.chain, d.expectChainID))
	}
}

// toValue encodes the 'to' field: a 20-byte address, or RLP empty string for
// contract creation.
func toValue(a *fastrlp.Arena, toRaw []byte) *fastrlp.Value {
	if len(toRaw) == 0 {
		return a.NewBytes(nil)
	}
	return a.NewCopyBytes(toRaw)
}

// rebuildValue structurally clones a parsed RLP value into the arena. Used for
// the EIP-1559 accessList, which is part of the signed preimage but whose
// semantics the decoder does not interpret: a faithful structural clone keeps
// the recomputed digest bound to exactly the parsed input.
func rebuildValue(a *fastrlp.Arena, v *fastrlp.Value) *fastrlp.Value {
	if v.Type() == fastrlp.TypeArray {
		out := a.NewArray()
		kids, _ := v.GetElems()
		for _, c := range kids {
			out.Set(rebuildValue(a, c))
		}
		return out
	}
	b, _ := v.Bytes()
	return a.NewCopyBytes(b)
}

// decodeCall fills f.Call from calldata. Recognized ERC20/TRC20 selectors get
// structured fields; everything else is CallUnrecognized with raw bytes and a
// caution warning — the decoder never invents a method label.
func decodeCall(f *Facts, chain Chain, contract20, data []byte) {
	if len(data) == 0 {
		return
	}
	call := &DecodedCall{RawCallData: data}
	if len(data) < 4 {
		call.Kind = CallUnrecognized
		call.Selector = selectorHex(data)
		f.Call = call
		f.warn("CAUTION: contract call with <4-byte calldata — unrecognized, review raw data")
		return
	}
	sel := data[:4]
	args := data[4:]
	call.Selector = selectorHex(sel)
	if len(contract20) == 20 {
		call.TokenContract = formatAddr(chain, contract20)
	}

	switch {
	case eq(sel, selTransfer) && len(args) == 64:
		call.Kind = CallERC20Transfer
		call.Recipient = formatAddr(chain, args[12:32])
		call.Amount = new(big.Int).SetBytes(args[32:64])
	case eq(sel, selApprove) && len(args) == 64:
		call.Kind = CallERC20Approve
		call.Recipient = formatAddr(chain, args[12:32])
		call.Amount = new(big.Int).SetBytes(args[32:64])
	case eq(sel, selTransferFrom) && len(args) == 96:
		call.Kind = CallERC20TransferFrom
		call.From = formatAddr(chain, args[12:32])
		call.Recipient = formatAddr(chain, args[44:64])
		call.Amount = new(big.Int).SetBytes(args[64:96])
	default:
		call.Kind = CallUnrecognized
		f.warn(fmt.Sprintf(
			"CAUTION: unrecognized contract call selector %s — raw calldata shown, no method inferred",
			call.Selector))
	}
	f.Call = call
}

// formatAddr renders a 20-byte address for the given chain family. EVM uses
// EIP-55; TRON uses Base58Check (TRC20 calldata carries the 20-byte hash).
func formatAddr(chain Chain, addr20 []byte) string {
	if chain == ChainTRON {
		s, _ := tronBase58(addr20)
		return s
	}
	return toChecksumHex(addr20)
}

func eq(a, b []byte) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
