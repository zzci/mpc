package txdecode

import (
	"errors"
	"math/big"
	"testing"

	"github.com/royqta/mcp-wallet/internal/contract"
)

func TestEnvelopeValidation(t *testing.T) {
	d := New()
	if _, err := d.Decode(nil); !errors.Is(err, ErrInvalidEnvelope) {
		t.Errorf("nil req: %v", err)
	}
	if _, err := d.Decode(req("eth", []byte{1}, make([]byte, 31))); !errors.Is(err, ErrInvalidEnvelope) {
		t.Errorf("short digest32: %v", err)
	}
	if _, err := d.Decode(req("dogecoin", []byte{1}, make([]byte, 32))); !errors.Is(err, ErrUnsupportedChain) {
		t.Errorf("unknown chain: %v", err)
	}
}

func TestABCrossCheck(t *testing.T) {
	tx := mustHex(t, eip155RLP)
	dg := mustHex(t, eip155Digest)

	// Matching hints -> no mismatch, no A/B warning.
	good := req("eth", tx, dg)
	good.BusinessInfo = &contract.BusinessInfo{DisplayHints: map[string]string{
		hintChain: "ethereum",
		hintTo:    "0x3535353535353535353535353535353535353535",
		hintValue: new(big.Int).Exp(big.NewInt(10), big.NewInt(18), nil).String(),
	}}
	res, err := New().Decode(good)
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}
	if len(res.Mismatches) != 0 {
		t.Fatalf("expected no mismatch, got %+v", res.Mismatches)
	}

	// Diverging hints -> declarative mismatch + loud warning, but NOT a
	// rejection (digest still binds; only digest mismatch rejects).
	bad := req("eth", tx, dg)
	bad.BusinessInfo = &contract.BusinessInfo{DisplayHints: map[string]string{
		hintTo:    "0xdeadbeefdeadbeefdeadbeefdeadbeefdeadbeef",
		hintValue: "1",
	}}
	res, err = New().Decode(bad)
	if err != nil {
		t.Fatalf("Decode must still succeed on A/B mismatch: %v", err)
	}
	if len(res.Mismatches) != 2 {
		t.Fatalf("expected 2 mismatches, got %+v", res.Mismatches)
	}
	if !hasWarn(res.Facts, "A/B MISMATCH") {
		t.Errorf("expected loud A/B warning, got %v", res.Facts.Warnings)
	}
}

// fakeDecoder lets a test drive an arbitrary recomputed digest to prove the
// framework — not the decoder — enforces ==digest32 for overrides.
type fakeDecoder struct {
	facts  *Facts
	digest [32]byte
	err    error
}

func (f *fakeDecoder) Recompute([]byte) (*Facts, [32]byte, error) {
	return f.facts, f.digest, f.err
}

func TestPluggableOverrideStillBoundByDigest(t *testing.T) {
	var good [32]byte
	good[0] = 0xab
	fd := &fakeDecoder{facts: &Facts{Chain: ChainETH, TxType: TxEVMLegacy}, digest: good}

	d := New()
	d.Register(ChainETH, fd)

	// Override used and its digest matches -> accepted.
	if _, err := d.Decode(req("eth", []byte("x"), good[:])); err != nil {
		t.Fatalf("override with matching digest must pass: %v", err)
	}

	// Same override, envelope digest differs -> framework hard-rejects even
	// though the override "succeeded" (docs/design/mcp/sdk.md §4 binding).
	if _, err := d.Decode(req("eth", []byte("x"), make([]byte, 32))); !errors.Is(err, ErrDigestMismatch) {
		t.Fatalf("override must still be bound by digest, got %v", err)
	}

	// Override that errors -> hard reject, no facts.
	d.Register(ChainBSC, &fakeDecoder{err: errors.New("boom")})
	if res, err := d.Decode(req("bsc", []byte("x"), make([]byte, 32))); err == nil || res != nil {
		t.Fatalf("override error must reject with no facts: res=%v err=%v", res, err)
	}
}
