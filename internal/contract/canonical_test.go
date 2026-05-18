package contract

import (
	"bytes"
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

// sampleRequest is a fully populated, valid envelope used as a baseline; tests
// mutate copies of it.
func sampleRequest() SigningRequest {
	d := bytes.Repeat([]byte{0xAB}, 32)
	m := bytes.Repeat([]byte{0xCD}, 32)
	return SigningRequest{
		Version:     EnvelopeVersionV1,
		RequestID:   "123e4567-e89b-12d3-a456-426614174000",
		GroupID:     "grp-1",
		Chain:       "ethereum",
		UnsignedTx:  []byte("\x01\x02\x03raw-tx"),
		Digest32:    d,
		Proposer:    "proposer-A",
		CreatedAt:   1_700_000_000_000,
		Expiry:      1_700_000_900_000,
		MetaHash:    m,
		ProposerSig: nil,
	}
}

func TestCanonicalBytes_Deterministic(t *testing.T) {
	req := sampleRequest()
	p1, err := CanonicalBytes(&req)
	if err != nil {
		t.Fatalf("preimage: %v", err)
	}
	p2, err := CanonicalBytes(&req)
	if err != nil {
		t.Fatalf("preimage: %v", err)
	}
	if !bytes.Equal(p1, p2) {
		t.Fatal("preimage not deterministic")
	}
	if !bytes.HasPrefix(p1, canonicalDomain) {
		t.Fatal("preimage missing domain prefix")
	}
}

// TestCanonicalBytes_JSONRoundTripEquivalence is the S-001 §8 core property:
// the same logical envelope submitted as JSON yields the same preimage as the
// in-memory value (signatures are wire-format independent).
func TestCanonicalBytes_JSONRoundTripEquivalence(t *testing.T) {
	req := sampleRequest()
	want, err := CanonicalBytes(&req)
	if err != nil {
		t.Fatalf("preimage: %v", err)
	}

	raw, err := json.Marshal(&req)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var decoded SigningRequest
	if err := json.Unmarshal(raw, &decoded); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	got, err := CanonicalBytes(&decoded)
	if err != nil {
		t.Fatalf("preimage: %v", err)
	}
	if !bytes.Equal(want, got) {
		t.Fatal("JSON round-trip changed the canonical preimage")
	}
}

// TestCanonicalBytes_FieldSensitivity asserts every covered field changes
// the preimage (proposerSig is intentionally excluded; businessInfo is bound
// only via metaHash, per S-001 §3).
func TestCanonicalBytes_FieldSensitivity(t *testing.T) {
	base, err := CanonicalBytes(ptr(sampleRequest()))
	if err != nil {
		t.Fatalf("base: %v", err)
	}
	mutations := map[string]func(*SigningRequest){
		"version":    func(r *SigningRequest) { r.Version = 2 },
		"requestId":  func(r *SigningRequest) { r.RequestID = "00000000-0000-0000-0000-000000000001" },
		"groupId":    func(r *SigningRequest) { r.GroupID = "grp-2" },
		"chain":      func(r *SigningRequest) { r.Chain = "tron" },
		"unsignedTx": func(r *SigningRequest) { r.UnsignedTx = []byte("other") },
		"digest32":   func(r *SigningRequest) { r.Digest32 = bytes.Repeat([]byte{0x01}, 32) },
		"proposer":   func(r *SigningRequest) { r.Proposer = "proposer-B" },
		"createdAt":  func(r *SigningRequest) { r.CreatedAt = 1 },
		"expiry":     func(r *SigningRequest) { r.Expiry = 1 },
		"metaHash":   func(r *SigningRequest) { r.MetaHash = bytes.Repeat([]byte{0x02}, 32) },
	}
	for name, mut := range mutations {
		t.Run(name, func(t *testing.T) {
			r := sampleRequest()
			mut(&r)
			got, err := CanonicalBytes(&r)
			if err != nil {
				t.Fatalf("preimage: %v", err)
			}
			if bytes.Equal(base, got) {
				t.Fatalf("mutating %s did not change preimage", name)
			}
		})
	}

	// proposerSig must NOT affect the preimage (it signs P; it is not in P).
	r := sampleRequest()
	r.ProposerSig = []byte("signature-bytes")
	got, err := CanonicalBytes(&r)
	if err != nil {
		t.Fatalf("preimage: %v", err)
	}
	if !bytes.Equal(base, got) {
		t.Fatal("proposerSig leaked into preimage")
	}
}

// TestCanonicalBytes_NoConcatenationAmbiguity guards the length-prefix
// invariant: moving bytes across a variable-field boundary must change P.
func TestCanonicalBytes_NoConcatenationAmbiguity(t *testing.T) {
	a := sampleRequest()
	a.GroupID, a.Chain = "ab", "cd"
	b := sampleRequest()
	b.GroupID, b.Chain = "a", "bcd"
	pa, _ := CanonicalBytes(&a)
	pb, _ := CanonicalBytes(&b)
	if bytes.Equal(pa, pb) {
		t.Fatal("adjacent variable fields are not unambiguously delimited")
	}
}

func TestCanonicalBytes_Errors(t *testing.T) {
	cases := map[string]func(*SigningRequest){
		"short digest32": func(r *SigningRequest) { r.Digest32 = []byte{0x01} },
		"short metaHash": func(r *SigningRequest) { r.MetaHash = []byte{0x02} },
		"bad uuid len":   func(r *SigningRequest) { r.RequestID = "not-a-uuid" },
		"bad uuid hex":   func(r *SigningRequest) { r.RequestID = "zz3e4567-e89b-12d3-a456-426614174000" },
	}
	for name, mut := range cases {
		t.Run(name, func(t *testing.T) {
			r := sampleRequest()
			mut(&r)
			if _, err := CanonicalBytes(&r); !errors.Is(err, ErrInvalidEnvelope) {
				t.Fatalf("err = %v, want ErrInvalidEnvelope", err)
			}
		})
	}
	if _, err := CanonicalBytes(nil); !errors.Is(err, ErrInvalidEnvelope) {
		t.Fatalf("nil: err = %v, want ErrInvalidEnvelope", err)
	}
}

func TestUUIDBytes_FormsEquivalent(t *testing.T) {
	hyphen := "123e4567-e89b-12d3-a456-426614174000"
	plain := "123e4567e89b12d3a456426614174000"
	upper := strings.ToUpper(hyphen)
	hb, err := uuidBytes(hyphen)
	if err != nil {
		t.Fatalf("hyphen: %v", err)
	}
	pb, err := uuidBytes(plain)
	if err != nil {
		t.Fatalf("plain: %v", err)
	}
	ub, err := uuidBytes(upper)
	if err != nil {
		t.Fatalf("upper: %v", err)
	}
	if len(hb) != 16 {
		t.Fatalf("len = %d, want 16", len(hb))
	}
	if !bytes.Equal(hb, pb) || !bytes.Equal(hb, ub) {
		t.Fatal("UUID string forms must decode to identical 16 bytes")
	}
}

// TestNFC_Normalization pins that canonically-equivalent Unicode strings
// (precomposed vs decomposed) produce the same preimage.
func TestNFC_Normalization(t *testing.T) {
	precomposed := sampleRequest()
	precomposed.Proposer = "é" // é (U+00E9)
	decomposed := sampleRequest()
	decomposed.Proposer = "é" // e + combining acute (U+0065 U+0301)
	pp, _ := CanonicalBytes(&precomposed)
	pd, _ := CanonicalBytes(&decomposed)
	if !bytes.Equal(pp, pd) {
		t.Fatal("NFC normalization not applied to string fields")
	}
}

func ptr(r SigningRequest) *SigningRequest { return &r }
