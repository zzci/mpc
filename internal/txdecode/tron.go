package txdecode

import (
	"crypto/sha256"
	"fmt"
	"math/big"

	"google.golang.org/protobuf/encoding/protowire"
)

// TRON Contract.type enum values (TRON core protocol Transaction.Contract.
// ContractType). Only the in-scope set is named; any other value is shown by
// number as TxTronOther (never a guessed label).
const (
	tronTransferContract       = 1
	tronTriggerSmartContract   = 31
	tronFreezeBalanceV2        = 54
	tronUnfreezeBalanceV2      = 55
	tronWithdrawExpireUnfreeze = 56
	tronDelegateResource       = 57
	tronUnDelegateResource     = 58
	tronCancelAllUnfreezeV2    = 59
)

var tronStakeNames = map[int32]string{
	tronFreezeBalanceV2:        "FreezeBalanceV2Contract",
	tronUnfreezeBalanceV2:      "UnfreezeBalanceV2Contract",
	tronWithdrawExpireUnfreeze: "WithdrawExpireUnfreezeContract",
	tronDelegateResource:       "DelegateResourceContract",
	tronUnDelegateResource:     "UnDelegateResourceContract",
	tronCancelAllUnfreezeV2:    "CancelAllUnfreezeV2Contract",
}

var tronResourceNames = map[uint64]string{0: "BANDWIDTH", 1: "ENERGY", 2: "TRON_POWER"}

// tronDecoder is the built-in TRON decoder. TRON's signing hash is
// sha256(raw_data); raw_data == unsignedTx. The protobuf walk is for A-zone
// display only — its correctness is backed by corpus + fuzz and the "never
// fabricate" degradation, not by the digest (the digest binds the bytes, not
// the parse; docs/design/PLAN.md §5 risk 9, the chain-inherent asymmetry).
type tronDecoder struct{}

// Recompute hashes the raw_data bytes (the chain digest) and best-effort
// parses contracts for display. It never hard-rejects on an unknown/garbled
// contract: the bytes stay digest-bound and the human reviewer is the
// backstop (WYSIWYS). Only an unsupported chain or digest mismatch (enforced
// by the framework) rejects.
func (d *tronDecoder) Recompute(unsignedTx []byte) (*Facts, [32]byte, error) {
	digest := sha256.Sum256(unsignedTx)
	f := &Facts{Chain: ChainTRON}

	contracts, err := tronContracts(unsignedTx)
	if err != nil || len(contracts) == 0 {
		f.TxType = TxTronUnparsed
		f.warn("CAUTION: TRON raw_data protobuf could not be decoded — bytes are digest-bound but NO fields are asserted; do not approve unless independently verified")
		return f, digest, nil
	}
	if len(contracts) > 1 {
		f.warn(fmt.Sprintf("CAUTION: %d contracts in one transaction — only the first is decoded below", len(contracts)))
	}
	d.fillFromContract(f, contracts[0])
	return f, digest, nil
}

type tronContract struct {
	typ   int32
	value []byte // the google.protobuf.Any.value (serialized contract message)
}

// tronContracts walks Transaction.raw (field 11 = repeated Contract) and, for
// each, Contract.type (field 1) + Contract.parameter (field 2, a
// google.protobuf.Any whose field 2 is the contract message bytes).
func tronContracts(raw []byte) ([]tronContract, error) {
	var out []tronContract
	err := eachField(raw, func(num protowire.Number, typ protowire.Type, b []byte, _ uint64) error {
		if num != 11 || typ != protowire.BytesType {
			return nil
		}
		var c tronContract
		if e := eachField(b, func(n protowire.Number, t protowire.Type, fb []byte, fv uint64) error {
			switch {
			case n == 1 && t == protowire.VarintType:
				c.typ = int32(fv)
			case n == 2 && t == protowire.BytesType: // google.protobuf.Any
				return eachField(fb, func(an protowire.Number, at protowire.Type, ab []byte, _ uint64) error {
					if an == 2 && at == protowire.BytesType { // Any.value
						c.value = ab
					}
					return nil
				})
			}
			return nil
		}); e != nil {
			return e
		}
		out = append(out, c)
		return nil
	})
	return out, err
}

func (d *tronDecoder) fillFromContract(f *Facts, c tronContract) {
	f.TronContractType = c.typ
	switch c.typ {
	case tronTransferContract:
		f.TxType = TxTronTransfer
		f.TronContractName = "TransferContract"
		warnIfInnerMalformed(f, "TransferContract", c.value)
		owner, to, amount := tronTransferFields(c.value)
		f.From = tronAddr(f, "owner_address", owner)
		f.To = tronAddr(f, "to_address", to)
		f.Value = amount
		requireTronAddr(f, "TransferContract", "owner_address", owner)
		requireTronAddr(f, "TransferContract", "to_address", to)
	case tronTriggerSmartContract:
		f.TxType = TxTronTrigger
		f.TronContractName = "TriggerSmartContract"
		warnIfInnerMalformed(f, "TriggerSmartContract", c.value)
		owner, contract, callValue, data := tronTriggerFields(c.value)
		f.From = tronAddr(f, "owner_address", owner)
		f.To = tronAddr(f, "contract_address", contract)
		f.Value = callValue
		requireTronAddr(f, "TriggerSmartContract", "owner_address", owner)
		requireTronAddr(f, "TriggerSmartContract", "contract_address", contract)
		if len(data) > 0 {
			f.Data = data
			decodeCall(f, ChainTRON, contract20(contract), data)
		}
	case tronFreezeBalanceV2, tronUnfreezeBalanceV2, tronDelegateResource,
		tronUnDelegateResource, tronWithdrawExpireUnfreeze, tronCancelAllUnfreezeV2:
		f.TxType = TxTronStake
		f.TronContractName = tronStakeNames[c.typ]
		warnIfInnerMalformed(f, f.TronContractName, c.value)
		d.fillStake(f, c)
	default:
		f.TxType = TxTronOther
		f.warn(fmt.Sprintf("CAUTION: TRON contract type %d is not in the supported set — no fields decoded, review raw transaction", c.typ))
	}
}

// warnIfInnerMalformed raises a prominent caution when the contract's inner
// protobuf message is structurally invalid: the field readers tolerate a
// partial parse, so without this a malformed message would display as a
// "clean" recognized tx (docs/design/mcp/sdk.md §4: do not fabricate the unrecognized — incomplete
// decode must warn, not silently present zeroed fields).
func warnIfInnerMalformed(f *Facts, name string, b []byte) {
	if eachField(b, func(protowire.Number, protowire.Type, []byte, uint64) error { return nil }) != nil {
		f.warn(fmt.Sprintf("CAUTION: TRON %s parameter protobuf is malformed — decoded fields may be incomplete, review raw transaction", name))
	}
}

// requireTronAddr warns when an essential address is absent, so a missing
// owner/recipient never displays as an empty-but-recognized transfer.
func requireTronAddr(f *Facts, name, field string, raw []byte) {
	if len(raw) == 0 {
		f.warn(fmt.Sprintf("CAUTION: TRON %s is missing %s — incomplete decode, review raw transaction", name, field))
	}
}

func (d *tronDecoder) fillStake(f *Facts, c tronContract) {
	owner := tronBytesField(c.value, 1)
	f.From = tronAddr(f, "owner_address", owner)
	if len(owner) == 0 {
		f.warn(fmt.Sprintf("CAUTION: TRON %s is missing owner_address — incomplete decode, review raw transaction", f.TronContractName))
	}
	switch c.typ {
	case tronWithdrawExpireUnfreeze, tronCancelAllUnfreezeV2:
		f.warn(fmt.Sprintf("Stake2.0: %s by %s", f.TronContractName, f.From))
	case tronFreezeBalanceV2, tronUnfreezeBalanceV2:
		amount := big.NewInt(int64(tronVarintField(c.value, 2)))
		res := tronResource(tronVarintField(c.value, 3))
		f.Value = amount
		f.warn(fmt.Sprintf("Stake2.0: %s %s sun, resource=%s", f.TronContractName, amount, res))
	case tronDelegateResource, tronUnDelegateResource:
		res := tronResource(tronVarintField(c.value, 2))
		amount := big.NewInt(int64(tronVarintField(c.value, 3)))
		recv := tronBytesField(c.value, 4)
		f.To = tronAddr(f, "receiver_address", recv)
		f.Value = amount
		f.warn(fmt.Sprintf("Stake2.0: %s %s sun resource=%s to %s", f.TronContractName, amount, res, f.To))
	}
}

// --- TRON contract message field readers (protowire) ---

func tronTransferFields(b []byte) (owner, to []byte, amount *big.Int) {
	amount = new(big.Int)
	_ = eachField(b, func(n protowire.Number, t protowire.Type, fb []byte, fv uint64) error {
		switch {
		case n == 1 && t == protowire.BytesType:
			owner = fb
		case n == 2 && t == protowire.BytesType:
			to = fb
		case n == 3 && t == protowire.VarintType:
			amount.SetInt64(int64(fv))
		}
		return nil
	})
	return owner, to, amount
}

func tronTriggerFields(b []byte) (owner, contract []byte, callValue *big.Int, data []byte) {
	callValue = new(big.Int)
	_ = eachField(b, func(n protowire.Number, t protowire.Type, fb []byte, fv uint64) error {
		switch {
		case n == 1 && t == protowire.BytesType:
			owner = fb
		case n == 2 && t == protowire.BytesType:
			contract = fb
		case n == 3 && t == protowire.VarintType:
			callValue.SetInt64(int64(fv))
		case n == 4 && t == protowire.BytesType:
			data = fb
		}
		return nil
	})
	return owner, contract, callValue, data
}

func tronBytesField(b []byte, want protowire.Number) []byte {
	var out []byte
	_ = eachField(b, func(n protowire.Number, t protowire.Type, fb []byte, _ uint64) error {
		if n == want && t == protowire.BytesType {
			out = fb
		}
		return nil
	})
	return out
}

func tronVarintField(b []byte, want protowire.Number) uint64 {
	var out uint64
	_ = eachField(b, func(n protowire.Number, t protowire.Type, _ []byte, fv uint64) error {
		if n == want && t == protowire.VarintType {
			out = fv
		}
		return nil
	})
	return out
}

func tronResource(v uint64) string {
	if name, ok := tronResourceNames[v]; ok {
		return name
	}
	return fmt.Sprintf("resource#%d", v)
}

// tronAddr formats a protobuf TRON address (21 bytes, 0x41‖20). A malformed
// prefix/length is surfaced as a prominent caution rather than mis-displayed.
func tronAddr(f *Facts, field string, raw []byte) string {
	if len(raw) == 0 {
		return ""
	}
	s, ok := tronBase58(raw)
	if !ok {
		f.warn(fmt.Sprintf("CAUTION: %s is not a valid TRON address (raw shown)", field))
	}
	return s
}

// contract20 reduces a 21-byte (0x41‖20) TRON address to its 20-byte body so
// recognized TRC20 calldata can format the token contract consistently.
func contract20(raw []byte) []byte {
	if len(raw) == 21 && raw[0] == tronAddrPrefix {
		return raw[1:]
	}
	return raw
}

// eachField iterates protobuf wire fields of one message. Unknown fields are
// skipped. A wire-level parse error stops iteration and is returned, letting
// the caller degrade to TxTronUnparsed (bytes still digest-bound).
func eachField(b []byte, visit func(num protowire.Number, typ protowire.Type, raw []byte, varint uint64) error) error {
	for len(b) > 0 {
		num, typ, n := protowire.ConsumeTag(b)
		if n < 0 {
			return fmt.Errorf("protowire tag: %w", protowire.ParseError(n))
		}
		b = b[n:]
		switch typ {
		case protowire.VarintType:
			v, m := protowire.ConsumeVarint(b)
			if m < 0 {
				return fmt.Errorf("protowire varint: %w", protowire.ParseError(m))
			}
			if err := visit(num, typ, nil, v); err != nil {
				return err
			}
			b = b[m:]
		case protowire.BytesType:
			v, m := protowire.ConsumeBytes(b)
			if m < 0 {
				return fmt.Errorf("protowire bytes: %w", protowire.ParseError(m))
			}
			if err := visit(num, typ, v, 0); err != nil {
				return err
			}
			b = b[m:]
		default:
			m := protowire.ConsumeFieldValue(num, typ, b)
			if m < 0 {
				return fmt.Errorf("protowire skip: %w", protowire.ParseError(m))
			}
			b = b[m:]
		}
	}
	return nil
}
