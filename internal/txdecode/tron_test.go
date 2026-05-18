package txdecode

import (
	"crypto/sha256"
	"math/big"
	"testing"

	"google.golang.org/protobuf/encoding/protowire"
)

// --- independent protobuf encoder (Append* side; the decoder uses Consume*) ---

func pbBytes(num protowire.Number, v []byte) []byte {
	return protowire.AppendBytes(protowire.AppendTag(nil, num, protowire.BytesType), v)
}
func pbVarint(num protowire.Number, v uint64) []byte {
	return protowire.AppendVarint(protowire.AppendTag(nil, num, protowire.VarintType), v)
}

func tronAny(typeURL string, msg []byte) []byte {
	b := pbBytes(1, []byte(typeURL))
	b = append(b, pbBytes(2, msg)...)
	return b
}

// rawData wraps one contract message into Transaction.raw (field 11 Contract:
// field1 type, field2 Any{field2 value=msg}).
func rawData(t *testing.T, ctype uint64, typeURL string, msg []byte) []byte {
	t.Helper()
	contract := pbVarint(1, ctype)
	contract = append(contract, pbBytes(2, tronAny(typeURL, msg))...)
	return pbBytes(11, contract)
}

func addr21(b byte) []byte {
	a := make([]byte, 21)
	a[0] = tronAddrPrefix
	for i := 1; i < 21; i++ {
		a[i] = b
	}
	return a
}

func decodeTron(t *testing.T, raw []byte) *Result {
	t.Helper()
	d := sha256.Sum256(raw)
	res, err := New().Decode(req("tron", raw, d[:]))
	if err != nil {
		t.Fatalf("Decode tron: %v", err)
	}
	return res
}

func TestTronNativeTransfer(t *testing.T) {
	owner, to := addr21(0x11), addr21(0x22)
	msg := pbBytes(1, owner)
	msg = append(msg, pbBytes(2, to)...)
	msg = append(msg, pbVarint(3, 1_000_000)...) // 1 TRX = 1e6 sun
	raw := rawData(t, tronTransferContract, "type.googleapis.com/protocol.TransferContract", msg)

	res := decodeTron(t, raw)
	f := res.Facts
	if f.TxType != TxTronTransfer {
		t.Fatalf("TxType=%s", f.TxType)
	}
	if f.From == "" || f.From[0] != 'T' {
		t.Errorf("From=%s", f.From)
	}
	if f.To == "" || f.To[0] != 'T' {
		t.Errorf("To=%s", f.To)
	}
	if f.Value.Int64() != 1_000_000 {
		t.Errorf("Value=%v", f.Value)
	}
}

func TestTronTRC20Transfer(t *testing.T) {
	owner, token := addr21(0x11), addr21(0xaa)
	recip := make([]byte, 20)
	for i := range recip {
		recip[i] = 0x33
	}
	data := append([]byte(nil), selTransfer...)
	data = append(data, make([]byte, 12)...)
	data = append(data, recip...)
	amt := make([]byte, 32)
	big.NewInt(500).FillBytes(amt)
	data = append(data, amt...)

	msg := pbBytes(1, owner)
	msg = append(msg, pbBytes(2, token)...)
	msg = append(msg, pbBytes(4, data)...)
	raw := rawData(t, tronTriggerSmartContract, "type.googleapis.com/protocol.TriggerSmartContract", msg)

	res := decodeTron(t, raw)
	f := res.Facts
	if f.TxType != TxTronTrigger {
		t.Fatalf("TxType=%s", f.TxType)
	}
	if f.Call == nil || f.Call.Kind != CallERC20Transfer {
		t.Fatalf("call=%+v", f.Call)
	}
	if f.Call.Recipient == "" || f.Call.Recipient[0] != 'T' {
		t.Errorf("recipient=%s", f.Call.Recipient)
	}
	if f.Call.Amount.Int64() != 500 {
		t.Errorf("amount=%v", f.Call.Amount)
	}
}

func TestTronFreezeV2Stake(t *testing.T) {
	owner := addr21(0x11)
	msg := pbBytes(1, owner)
	msg = append(msg, pbVarint(2, 5_000_000)...) // frozen_balance
	msg = append(msg, pbVarint(3, 1)...)         // resource = ENERGY
	raw := rawData(t, tronFreezeBalanceV2, "type.googleapis.com/protocol.FreezeBalanceV2Contract", msg)

	res := decodeTron(t, raw)
	f := res.Facts
	if f.TxType != TxTronStake || f.TronContractName != "FreezeBalanceV2Contract" {
		t.Fatalf("type=%s name=%s", f.TxType, f.TronContractName)
	}
	if f.Value.Int64() != 5_000_000 {
		t.Errorf("Value=%v", f.Value)
	}
	if !hasWarn(f, "resource=ENERGY") {
		t.Errorf("expected stake info line, got %v", f.Warnings)
	}
}

func TestTronUnknownContractTypeNotFabricated(t *testing.T) {
	owner := addr21(0x11)
	raw := rawData(t, 6 /* AssetIssueContract, unsupported */, "type.googleapis.com/protocol.AssetIssueContract", pbBytes(1, owner))
	res := decodeTron(t, raw)
	if res.Facts.TxType != TxTronOther {
		t.Fatalf("TxType=%s", res.Facts.TxType)
	}
	if res.Facts.TronContractType != 6 {
		t.Errorf("raw type=%d", res.Facts.TronContractType)
	}
	if res.Facts.TronContractName != "" {
		t.Errorf("must not fabricate name, got %q", res.Facts.TronContractName)
	}
	if !hasWarn(res.Facts, "not in the supported set") {
		t.Errorf("expected caution, got %v", res.Facts.Warnings)
	}
}

func TestTronGarbledDegradesNotMisSigns(t *testing.T) {
	raw := []byte{0x08, 0xff} // truncated varint -> protowire parse error
	d := sha256.Sum256(raw)
	res, err := New().Decode(req("tron", raw, d[:]))
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}
	if res.Facts.TxType != TxTronUnparsed {
		t.Fatalf("TxType=%s", res.Facts.TxType)
	}
	if !hasWarn(res.Facts, "could not be decoded") {
		t.Errorf("expected strong caution, got %v", res.Facts.Warnings)
	}
	// bytes still digest-bound: a wrong digest still hard-rejects.
	if _, err := New().Decode(req("tron", raw, make([]byte, 32))); err == nil {
		t.Fatal("wrong digest must reject even for unparsed TRON")
	}
}

func TestTronSkipsUnknownWireTypesAndBytesError(t *testing.T) {
	// Unknown fixed64 field (wire type 1) before the contract: must be
	// skipped via ConsumeFieldValue, not mis-parsed.
	owner := addr21(0x11)
	msg := pbBytes(1, owner)
	msg = append(msg, pbBytes(2, addr21(0x22))...)
	msg = append(msg, pbVarint(3, 1)...)
	raw := protowire.AppendFixed64(protowire.AppendTag(nil, 14, protowire.Fixed64Type), 1234)
	raw = append(raw, rawData(t, tronTransferContract, "type.googleapis.com/protocol.TransferContract", msg)...)
	res := decodeTron(t, raw)
	if res.Facts.TxType != TxTronTransfer {
		t.Fatalf("unknown fixed64 not skipped: %s", res.Facts.TxType)
	}

	// Truncated length-delimited field -> bytes parse error -> degrade.
	bad := protowire.AppendTag(nil, 11, protowire.BytesType)
	bad = append(bad, 0x7f) // claims 127 bytes, none follow
	d := sha256.Sum256(bad)
	r, err := New().Decode(req("tron", bad, d[:]))
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}
	if r.Facts.TxType != TxTronUnparsed {
		t.Fatalf("truncated bytes must degrade, got %s", r.Facts.TxType)
	}
}

func TestTronIncompleteInnerWarnsNotClean(t *testing.T) {
	// TransferContract with only owner_address: to_address + amount absent.
	// Must NOT present as a clean recognized transfer — caution required.
	msg := pbBytes(1, addr21(0x11))
	raw := rawData(t, tronTransferContract, "type.googleapis.com/protocol.TransferContract", msg)
	res := decodeTron(t, raw)
	if res.Facts.TxType != TxTronTransfer {
		t.Fatalf("TxType=%s", res.Facts.TxType)
	}
	if res.Facts.To != "" {
		t.Errorf("to must stay empty, not fabricated: %s", res.Facts.To)
	}
	if !hasWarn(res.Facts, "missing to_address") {
		t.Errorf("expected missing-field caution, got %v", res.Facts.Warnings)
	}

	// Malformed inner protobuf (truncated varint inside Any.value).
	raw = rawData(t, tronTransferContract, "type.googleapis.com/protocol.TransferContract", []byte{0x08, 0xff})
	res = decodeTron(t, raw)
	if !hasWarn(res.Facts, "parameter protobuf is malformed") {
		t.Errorf("expected malformed-inner caution, got %v", res.Facts.Warnings)
	}
}

func TestTronInvalidAddressPrefixWarns(t *testing.T) {
	bad := make([]byte, 21) // wrong version byte (0x00, not 0x41)
	msg := pbBytes(1, bad)
	msg = append(msg, pbBytes(2, addr21(0x22))...)
	msg = append(msg, pbVarint(3, 1)...)
	raw := rawData(t, tronTransferContract, "type.googleapis.com/protocol.TransferContract", msg)
	res := decodeTron(t, raw)
	if !hasWarn(res.Facts, "not a valid TRON address") {
		t.Errorf("expected address caution, got %v", res.Facts.Warnings)
	}
}
