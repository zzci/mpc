package txdecode

import (
	"math/big"
	"testing"

	"github.com/umbracle/fastrlp"

	"github.com/royqta/mcp-wallet/internal/contract"
)

// buildLegacy builds a legacy signing preimage (6 = pre-EIP-155, 9 = EIP-155).
func buildLegacy(t *testing.T, eip155 bool, chainID int64, to, data []byte, value *big.Int) ([]byte, []byte) {
	t.Helper()
	a := &fastrlp.Arena{}
	arr := a.NewArray()
	arr.Set(a.NewUint(3))
	arr.Set(a.NewBigInt(big.NewInt(20_000_000_000)))
	arr.Set(a.NewUint(21000))
	arr.Set(a.NewCopyBytes(to))
	arr.Set(a.NewBigInt(value))
	arr.Set(a.NewCopyBytes(data))
	if eip155 {
		arr.Set(a.NewBigInt(big.NewInt(chainID)))
		arr.Set(a.NewBigInt(big.NewInt(0)))
		arr.Set(a.NewBigInt(big.NewInt(0)))
	}
	raw := arr.MarshalTo(nil)
	d := keccak256(raw)
	return raw, d[:]
}

func TestLegacyPreEIP155(t *testing.T) {
	to := mustHex(t, "00112233445566778899aabbccddeeff00112233")
	tx, dg := buildLegacy(t, false, 0, to, nil, big.NewInt(7))
	res, err := New().Decode(req("eth", tx, dg))
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}
	if res.Facts.ChainID != nil {
		t.Errorf("pre-155 must have nil ChainID")
	}
	if !hasWarn(res.Facts, "pre-EIP-155") {
		t.Errorf("expected pre-155 warning, got %v", res.Facts.Warnings)
	}
}

func TestLegacyContractCreation(t *testing.T) {
	tx, dg := buildLegacy(t, true, 1, nil, mustHex(t, "60016002"), big.NewInt(0))
	res, err := New().Decode(req("eth", tx, dg))
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}
	if res.Facts.To != "" {
		t.Errorf("creation must have empty To, got %s", res.Facts.To)
	}
	if !hasWarn(res.Facts, "contract-creation") {
		t.Errorf("expected creation warning, got %v", res.Facts.Warnings)
	}
}

func TestLegacyBadToLengthRejected(t *testing.T) {
	a := &fastrlp.Arena{}
	arr := a.NewArray()
	for i := 0; i < 3; i++ {
		arr.Set(a.NewUint(1))
	}
	arr.Set(a.NewCopyBytes(make([]byte, 19))) // bad 'to' length
	arr.Set(a.NewUint(1))
	arr.Set(a.NewBytes(nil))
	tx := arr.MarshalTo(nil)
	if _, err := New().Decode(req("eth", tx, make([]byte, 32))); err == nil {
		t.Fatal("19-byte 'to' must be rejected")
	}
}

func TestERC20ApproveAndTransferFrom(t *testing.T) {
	token := mustHex(t, "dac17f958d2ee523a2206206994597c13d831ec7")
	a1 := mustHex(t, "1111111111111111111111111111111111111111")
	a2 := mustHex(t, "2222222222222222222222222222222222222222")

	approve := append(append([]byte(nil), selApprove...), make([]byte, 12)...)
	approve = append(approve, a1...)
	amt := make([]byte, 32)
	big.NewInt(99).FillBytes(amt)
	approve = append(approve, amt...)
	tx, dg := buildLegacy(t, true, 1, token, approve, big.NewInt(0))
	res, _ := New().Decode(req("eth", tx, dg))
	if res.Facts.Call.Kind != CallERC20Approve {
		t.Errorf("approve kind=%s", res.Facts.Call.Kind)
	}

	tf := append(append([]byte(nil), selTransferFrom...), make([]byte, 12)...)
	tf = append(tf, a1...)
	tf = append(tf, make([]byte, 12)...)
	tf = append(tf, a2...)
	tf = append(tf, amt...)
	tx, dg = buildLegacy(t, true, 1, token, tf, big.NewInt(0))
	res, _ = New().Decode(req("eth", tx, dg))
	c := res.Facts.Call
	if c.Kind != CallERC20TransferFrom || !addrEqual(c.From, "0x"+"11"+"11111111111111111111111111111111111111") {
		t.Errorf("transferFrom=%+v", c)
	}
}

func TestShortCalldataUnrecognized(t *testing.T) {
	token := mustHex(t, "dac17f958d2ee523a2206206994597c13d831ec7")
	tx, dg := buildLegacy(t, true, 1, token, []byte{0x01, 0x02}, big.NewInt(0))
	res, _ := New().Decode(req("eth", tx, dg))
	if res.Facts.Call == nil || res.Facts.Call.Kind != CallUnrecognized {
		t.Fatalf("call=%+v", res.Facts.Call)
	}
	if !hasWarn(res.Facts, "<4-byte calldata") {
		t.Errorf("warn=%v", res.Facts.Warnings)
	}
}

func TestEIP1559WithAccessList(t *testing.T) {
	to := mustHex(t, "00112233445566778899aabbccddeeff00112233")
	a := &fastrlp.Arena{}
	arr := a.NewArray()
	arr.Set(a.NewBigInt(big.NewInt(1)))
	arr.Set(a.NewUint(1))
	arr.Set(a.NewUint(1))
	arr.Set(a.NewUint(2))
	arr.Set(a.NewUint(21000))
	arr.Set(a.NewCopyBytes(to))
	arr.Set(a.NewUint(5))
	arr.Set(a.NewBytes(nil))
	al := a.NewArray()
	entry := a.NewArray()
	entry.Set(a.NewCopyBytes(to))
	keys := a.NewArray()
	keys.Set(a.NewCopyBytes(make([]byte, 32)))
	entry.Set(keys)
	al.Set(entry)
	arr.Set(al)
	raw := append([]byte{eip1559TxType}, arr.MarshalTo(nil)...)
	d := keccak256(raw)
	res, err := New().Decode(req("eth", raw, d[:]))
	if err != nil {
		t.Fatalf("accessList round-trip failed: %v", err)
	}
	if res.Facts.TxType != TxEVM1559 {
		t.Errorf("type=%s", res.Facts.TxType)
	}
}

func TestABAmountTokenMethodFromAndAbsent(t *testing.T) {
	token := mustHex(t, "dac17f958d2ee523a2206206994597c13d831ec7")
	recip := mustHex(t, "00112233445566778899aabbccddeeff00112233")
	data := append(append([]byte(nil), selTransfer...), make([]byte, 12)...)
	data = append(data, recip...)
	amt := make([]byte, 32)
	big.NewInt(777).FillBytes(amt)
	data = append(data, amt...)
	tx, dg := buildLegacy(t, true, 1, token, data, big.NewInt(0))

	r := req("eth", tx, dg)
	r.BusinessInfo = &contract.BusinessInfo{DisplayHints: map[string]string{
		hintAmount: "777",
		hintToken:  "0xdac17f958d2ee523a2206206994597c13d831ec7",
		hintMethod: "erc20-transfer",
	}}
	res, err := New().Decode(r)
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}
	if len(res.Mismatches) != 0 {
		t.Fatalf("expected match, got %+v", res.Mismatches)
	}

	r2 := req("eth", tx, dg)
	r2.BusinessInfo = &contract.BusinessInfo{DisplayHints: map[string]string{
		hintAmount: "1",
		hintFrom:   "0xsomebody",    // A-zone has no From for EVM -> absent
		hintMethod: "erc20-approve", // wrong
	}}
	res, _ = New().Decode(r2)
	if len(res.Mismatches) != 3 {
		t.Fatalf("expected 3 mismatches, got %+v", res.Mismatches)
	}
}

func TestTronDelegateAndWithdraw(t *testing.T) {
	owner, recv := addr21(0x11), addr21(0x44)
	msg := pbBytes(1, owner)
	msg = append(msg, pbVarint(2, 1)...)         // resource ENERGY
	msg = append(msg, pbVarint(3, 9_000_000)...) // balance
	msg = append(msg, pbBytes(4, recv)...)
	raw := rawData(t, tronDelegateResource, "type.googleapis.com/protocol.DelegateResourceContract", msg)
	res := decodeTron(t, raw)
	if res.Facts.TxType != TxTronStake || res.Facts.Value.Int64() != 9_000_000 {
		t.Fatalf("delegate facts=%+v", res.Facts)
	}
	if res.Facts.To == "" || res.Facts.To[0] != 'T' {
		t.Errorf("receiver=%s", res.Facts.To)
	}

	raw = rawData(t, tronWithdrawExpireUnfreeze, "type.googleapis.com/protocol.WithdrawExpireUnfreezeContract", pbBytes(1, owner))
	res = decodeTron(t, raw)
	if !hasWarn(res.Facts, "WithdrawExpireUnfreezeContract") {
		t.Errorf("warn=%v", res.Facts.Warnings)
	}
}
