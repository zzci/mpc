package contract

import (
	"errors"
	"testing"

	"github.com/btcsuite/btcd/btcec/v2"
)

func TestSignVerifyEnvelope_RoundTrip(t *testing.T) {
	priv, err := btcec.NewPrivateKey()
	if err != nil {
		t.Fatalf("key: %v", err)
	}
	pub := priv.PubKey().SerializeCompressed()

	req := sampleRequest()
	if err := SignEnvelope(priv, &req); err != nil {
		t.Fatalf("sign: %v", err)
	}
	if len(req.ProposerSig) == 0 {
		t.Fatal("ProposerSig not set")
	}
	if err := VerifyProposerSig(&req, pub); err != nil {
		t.Fatalf("verify: %v", err)
	}
}

func TestVerifyProposerSig_TamperRejected(t *testing.T) {
	priv, _ := btcec.NewPrivateKey()
	pub := priv.PubKey().SerializeCompressed()
	req := sampleRequest()
	if err := SignEnvelope(priv, &req); err != nil {
		t.Fatalf("sign: %v", err)
	}

	tampered := req
	tampered.Expiry = req.Expiry + 1 // covered field -> digest changes
	if err := VerifyProposerSig(&tampered, pub); !errors.Is(err, ErrBadSignature) {
		t.Fatalf("tampered envelope: err = %v, want ErrBadSignature", err)
	}

	other, _ := btcec.NewPrivateKey()
	if err := VerifyProposerSig(&req, other.PubKey().SerializeCompressed()); !errors.Is(err, ErrBadSignature) {
		t.Fatalf("wrong key: err = %v, want ErrBadSignature", err)
	}
}

// TestProposerSig_BusinessInfoBoundViaMetaHash: businessInfo is not in the
// preimage, but tampering it while keeping metaHash makes VerifyMetaHash fail
// (S-001 §3 transitive binding); changing metaHash breaks proposerSig.
func TestProposerSig_BusinessInfoBoundViaMetaHash(t *testing.T) {
	priv, _ := btcec.NewPrivateKey()
	pub := priv.PubKey().SerializeCompressed()

	bi := &BusinessInfo{Title: "Pay 1 ETH"}
	h, _ := MetaHash(bi)
	req := sampleRequest()
	req.BusinessInfo = bi
	req.MetaHash = h[:]
	if err := SignEnvelope(priv, &req); err != nil {
		t.Fatalf("sign: %v", err)
	}

	// Swap businessInfo, keep stale metaHash: proposerSig still verifies
	// (metaHash unchanged) but the metaHash==H(businessInfo) check catches it.
	req.BusinessInfo = &BusinessInfo{Title: "Pay 100 ETH"}
	if err := VerifyProposerSig(&req, pub); err != nil {
		t.Fatalf("proposerSig should still verify (metaHash unchanged): %v", err)
	}
	if err := VerifyMetaHash(&req); err == nil {
		t.Fatal("VerifyMetaHash must reject swapped businessInfo")
	}
}

func TestSenderAuth_RoundTripAndTamper(t *testing.T) {
	priv, _ := btcec.NewPrivateKey()
	pub := priv.PubKey().SerializeCompressed()

	msg := &MpcMessage{
		Version:   MpcVersionV1,
		SessionID: "123e4567-e89b-12d3-a456-426614174000",
		From:      "party-1",
		Round:     2,
		Payload:   []byte("tss-wire-bytes"),
	}
	if err := SignSenderAuth(priv, msg); err != nil {
		t.Fatalf("sign senderAuth: %v", err)
	}
	if err := VerifySenderAuth(msg, pub); err != nil {
		t.Fatalf("verify senderAuth: %v", err)
	}

	cases := map[string]func(*MpcMessage){
		"sessionId": func(m *MpcMessage) { m.SessionID = "deadbeef-0000-0000-0000-000000000000" },
		"round":     func(m *MpcMessage) { m.Round = 3 },
		"payload":   func(m *MpcMessage) { m.Payload = []byte("forged") },
	}
	for name, mut := range cases {
		t.Run(name, func(t *testing.T) {
			c := *msg
			mut(&c)
			if err := VerifySenderAuth(&c, pub); !errors.Is(err, ErrBadSignature) {
				t.Fatalf("tampered %s: err = %v, want ErrBadSignature", name, err)
			}
		})
	}
}
