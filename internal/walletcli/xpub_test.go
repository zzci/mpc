package walletcli

import (
	"bufio"
	"bytes"
	"crypto/ecdsa"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math/big"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	tssCrypto "github.com/bnb-chain/tss-lib/v3/crypto"
	"github.com/bnb-chain/tss-lib/v3/tss"

	"github.com/zzci/mpc/internal/addr"
	"github.com/zzci/mpc/internal/hd"
)

// AD-4 walletcli xpub + address commands. The xpub command is a thin SDK
// delegate; the address command is the offline derive (no MPC, no network,
// no keystore access) — it consumes a previously-cached xpub JSON. The two
// together implement the user-facing flow of docs/design/mcp/address-
// derivation.md §7.

const validMemberKey = "0101010101010101010101010101010101010101010101010101010101010101"

// scalarPub builds a deterministic secp256k1 public key from a small scalar
// (the same pattern as internal/hd/derive_test.go). The point is uncompressed
// 65B so it round-trips through addressOp's btcec.ParsePubKey + addr.*.
func scalarPub(t *testing.T, n int64) *ecdsa.PublicKey {
	t.Helper()
	p := tssCrypto.ScalarBaseMult(tss.S256(), new(big.Int).SetInt64(n))
	return p.ToECDSAPubKey()
}

func uncompressed(pub *ecdsa.PublicKey) []byte {
	out := make([]byte, 65)
	out[0] = 0x04
	pub.X.FillBytes(out[1:33])
	pub.Y.FillBytes(out[33:65])
	return out
}

// xpubJSONFor produces a verbatim B8 wire body for the given (masterPub, cc),
// in the form `addressOp` consumes.
func xpubJSONFor(masterPub *ecdsa.PublicKey, chaincode []byte) string {
	return `{"ecdsaPubkeyHex":"` + hex.EncodeToString(uncompressed(masterPub)) +
		`","chaincodeHex":"` + hex.EncodeToString(chaincode) + `"}`
}

// TestAddressOp_OfflineDeriveMatchesHDPlusAddr cross-validates addressOp's
// output against an independent recomputation through hd.Derive + internal/addr
// (the same components production uses). A mismatch here means addressOp does
// something subtle the offline contract doesn't allow.
func TestAddressOp_OfflineDeriveMatchesHDPlusAddr(t *testing.T) {
	master := scalarPub(t, 23)
	chaincode := bytes.Repeat([]byte{0x73}, 32)
	const idx uint32 = 17

	out, err := addressOp(xpubJSONFor(master, chaincode), idx)
	if err != nil {
		t.Fatalf("addressOp: %v", err)
	}
	var got addrView
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatalf("decode addrView: %v (%s)", err, out)
	}
	if got.Index != idx {
		t.Fatalf("index = %d, want %d", got.Index, idx)
	}

	// Independent recomputation through the same primitives.
	il, child, err := hd.Derive(master, chaincode, idx)
	if err != nil {
		t.Fatalf("hd.Derive: %v", err)
	}
	wantChild := hd.ChildPubBytes(child)
	wantEVM, _ := addr.ETHAddress(wantChild)
	wantBSC, _ := addr.BSCAddress(wantChild)
	wantTron, _ := addr.TronAddress(wantChild)

	if got.ChildPubkeyHex != hex.EncodeToString(wantChild) {
		t.Fatalf("childPubkeyHex mismatch")
	}
	if got.ILHex != hex.EncodeToString(il.Bytes()) {
		t.Fatalf("ilHex mismatch: got %s want %s", got.ILHex, hex.EncodeToString(il.Bytes()))
	}
	if got.EVMAddress != wantEVM {
		t.Fatalf("EVMAddress %q want %q", got.EVMAddress, wantEVM)
	}
	if got.BSCAddress != wantBSC {
		t.Fatalf("BSCAddress %q want %q", got.BSCAddress, wantBSC)
	}
	if got.TronAddress != wantTron {
		t.Fatalf("TronAddress %q want %q", got.TronAddress, wantTron)
	}
}

func TestAddressOp_RejectsBadJSON(t *testing.T) {
	if _, err := addressOp("not json", 0); err == nil {
		t.Fatal("expected JSON rejection")
	}
}

func TestAddressOp_RejectsBadHex(t *testing.T) {
	cc := bytes.Repeat([]byte{0x01}, 32)
	cases := []string{
		`{"ecdsaPubkeyHex":"zz","chaincodeHex":"` + hex.EncodeToString(cc) + `"}`,
		`{"ecdsaPubkeyHex":"` + hex.EncodeToString(uncompressed(scalarPub(t, 3))) + `","chaincodeHex":"zz"}`,
	}
	for i, body := range cases {
		if _, err := addressOp(body, 0); err == nil {
			t.Fatalf("case %d: expected bad-hex rejection", i)
		}
	}
}

func TestAddressOp_RejectsBadPubkey(t *testing.T) {
	cc := bytes.Repeat([]byte{0x01}, 32)
	// 65 bytes of zeros: hex decodes but is not a secp256k1 point.
	body := `{"ecdsaPubkeyHex":"` + strings.Repeat("00", 65) +
		`","chaincodeHex":"` + hex.EncodeToString(cc) + `"}`
	if _, err := addressOp(body, 0); err == nil {
		t.Fatal("expected bad-pubkey rejection")
	}
}

func TestAddressOp_RejectsOutOfRangeIndex(t *testing.T) {
	master := scalarPub(t, 7)
	cc := bytes.Repeat([]byte{0x5a}, 32)
	if _, err := addressOp(xpubJSONFor(master, cc), hd.MaxIndex); err == nil {
		t.Fatal("expected ErrIndexOutOfRange surfaced")
	}
}

// TestAddressOp_DistinctIndicesProduceDistinctAddresses exercises the typical
// wallet-CLI loop: cache xpub once, derive m/0..m/N. Each i must yield a
// distinct EVM and TRON address, otherwise the offline addressOp would be
// silently broken (a wallet that mints the same address for every receive
// path is a fund-safety bug).
func TestAddressOp_DistinctIndicesProduceDistinctAddresses(t *testing.T) {
	master := scalarPub(t, 11)
	cc := bytes.Repeat([]byte{0xab}, 32)
	seenEVM := map[string]struct{}{}
	seenTron := map[string]struct{}{}
	for i := uint32(0); i < 8; i++ {
		out, err := addressOp(xpubJSONFor(master, cc), i)
		if err != nil {
			t.Fatalf("addressOp(%d): %v", i, err)
		}
		var v addrView
		if err := json.Unmarshal([]byte(out), &v); err != nil {
			t.Fatalf("decode %d: %v", i, err)
		}
		if _, dup := seenEVM[v.EVMAddress]; dup {
			t.Fatalf("evm address collision at i=%d", i)
		}
		seenEVM[v.EVMAddress] = struct{}{}
		if _, dup := seenTron[v.TronAddress]; dup {
			t.Fatalf("tron address collision at i=%d", i)
		}
		seenTron[v.TronAddress] = struct{}{}
	}
}

func TestSessionAddressCommand_Wires(t *testing.T) {
	master := scalarPub(t, 31)
	cc := bytes.Repeat([]byte{0x42}, 32)
	dir := t.TempDir()
	xpubFile := filepath.Join(dir, "xpub.json")
	if err := os.WriteFile(xpubFile, []byte(xpubJSONFor(master, cc)), 0o600); err != nil {
		t.Fatalf("write xpub: %v", err)
	}

	se, out, errw := newTestSession(t,
		"address 0 "+xpubFile+"\naddress 1 "+xpubFile+"\nquit\n")
	se.loop()
	if errw.Len() > 0 && strings.Contains(errw.String(), "error:") {
		t.Fatalf("unexpected error in errw: %s", errw.String())
	}
	// Two address lines on out.
	rdr := bufio.NewScanner(out)
	rdr.Buffer(make([]byte, 0, 4096), 1<<20)
	addrs := []addrView{}
	for rdr.Scan() {
		var v addrView
		if err := json.Unmarshal([]byte(rdr.Text()), &v); err != nil {
			t.Fatalf("decode out line %q: %v", rdr.Text(), err)
		}
		addrs = append(addrs, v)
	}
	if len(addrs) != 2 {
		t.Fatalf("expected 2 address lines, got %d", len(addrs))
	}
	if addrs[0].Index != 0 || addrs[1].Index != 1 {
		t.Fatalf("indices: %d %d", addrs[0].Index, addrs[1].Index)
	}
	if addrs[0].EVMAddress == addrs[1].EVMAddress {
		t.Fatal("m/0 and m/1 must produce different EVM addresses")
	}
}

func TestSessionAddressCommand_UsageError(t *testing.T) {
	se, _, errw := newTestSession(t, "address\naddress 0\nquit\n")
	se.loop()
	if !strings.Contains(errw.String(), "usage: address <i> <xpub-file>") {
		t.Fatalf("missing usage message: %s", errw.String())
	}
}

func TestSessionAddressCommand_BadIndex(t *testing.T) {
	dir := t.TempDir()
	xpubFile := filepath.Join(dir, "xpub.json")
	master := scalarPub(t, 5)
	if err := os.WriteFile(xpubFile,
		[]byte(xpubJSONFor(master, bytes.Repeat([]byte{0x01}, 32))), 0o600); err != nil {
		t.Fatalf("write xpub: %v", err)
	}
	se, _, errw := newTestSession(t, "address notanint "+xpubFile+"\nquit\n")
	se.loop()
	if !strings.Contains(errw.String(), "index must be a non-negative integer") {
		t.Fatalf("bad index not reported: %s", errw.String())
	}
}

func TestSessionXpubCommand_FetchesFromCoord(t *testing.T) {
	// Stand up a minimal coord stub serving B8 over httptest, then drive the
	// session's `xpub <req-file>` command end-to-end through the SDK. The
	// member-auth bytes are verified by coordclient's own suite; here the
	// stub just answers and the test asserts the JSON flows back to stdout.
	wantPub := bytes.Repeat([]byte{0x03}, 33)
	wantCC := bytes.Repeat([]byte{0x99}, 32)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]string{
			"ecdsaPubkeyHex": hex.EncodeToString(wantPub),
			"chaincodeHex":   hex.EncodeToString(wantCC),
		})
	}))
	t.Cleanup(srv.Close)

	dir := t.TempDir()
	reqFile := filepath.Join(dir, "xpub-req.json")
	if err := os.WriteFile(reqFile, []byte(fmt.Sprintf(
		`{"coordBaseURL":%q,"groupId":"g1","memberId":"m1","memberKeyHex":%q}`,
		srv.URL, validMemberKey)), 0o600); err != nil {
		t.Fatalf("write req: %v", err)
	}

	se, out, errw := newTestSession(t, "xpub "+reqFile+"\nquit\n")
	se.loop()
	if strings.Contains(errw.String(), "error:") {
		t.Fatalf("unexpected error: %s", errw.String())
	}
	var got struct {
		ECDSAPubkeyHex string `json:"ecdsaPubkeyHex"`
		ChaincodeHex   string `json:"chaincodeHex"`
	}
	if err := json.Unmarshal(bytes.TrimSpace(out.Bytes()), &got); err != nil {
		t.Fatalf("decode: %v (%s)", err, out.String())
	}
	if got.ECDSAPubkeyHex != hex.EncodeToString(wantPub) || got.ChaincodeHex != hex.EncodeToString(wantCC) {
		t.Fatalf("xpub roundtrip mismatch: %+v", got)
	}
}

func TestSessionXpubCommand_UsageError(t *testing.T) {
	se, _, errw := newTestSession(t, "xpub\nquit\n")
	se.loop()
	if !strings.Contains(errw.String(), "usage: xpub <req-file>") {
		t.Fatalf("missing usage message: %s", errw.String())
	}
}
