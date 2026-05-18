package contract

import (
	"bytes"
	"encoding/hex"
	"testing"
)

// emptyMetaHashHex is the well-known SHA-256("") fixed by S-001 §4.2.
const emptyMetaHashHex = "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855"

func TestEmptyMetaHash_KnownConstant(t *testing.T) {
	if got := hex.EncodeToString(EmptyMetaHash[:]); got != emptyMetaHashHex {
		t.Fatalf("EmptyMetaHash = %s, want %s", got, emptyMetaHashHex)
	}
}

func TestMetaHash_AbsentIsEmpty(t *testing.T) {
	got, err := MetaHash(nil)
	if err != nil {
		t.Fatalf("nil businessInfo: %v", err)
	}
	if got != EmptyMetaHash {
		t.Fatal("nil businessInfo must hash to EmptyMetaHash")
	}
}

// TestMetaHashFromJSON_AbsentForms: missing/empty/null all collapse to
// EmptyMetaHash, while an empty object does NOT (S-001 §4.2).
func TestMetaHashFromJSON_AbsentForms(t *testing.T) {
	for _, raw := range []string{"", "   ", "null", " null "} {
		got, err := MetaHashFromJSON([]byte(raw))
		if err != nil {
			t.Fatalf("%q: %v", raw, err)
		}
		if got != EmptyMetaHash {
			t.Fatalf("%q: want EmptyMetaHash", raw)
		}
	}
	got, err := MetaHashFromJSON([]byte("{}"))
	if err != nil {
		t.Fatalf("empty object: %v", err)
	}
	if got == EmptyMetaHash {
		t.Fatal("empty JSON object must NOT equal EmptyMetaHash")
	}
}

// TestMetaHashFromJSON_KeyOrderIrrelevant is the JCS guarantee: the
// same logical object with different key order / whitespace hashes equally.
func TestMetaHashFromJSON_KeyOrderIrrelevant(t *testing.T) {
	a := `{"title":"Pay","summary":"to X","refs":{"b":"2","a":"1"}}`
	b := `{ "refs": { "a":"1", "b":"2" }, "summary":"to X", "title":"Pay" }`
	ha, err := MetaHashFromJSON([]byte(a))
	if err != nil {
		t.Fatalf("a: %v", err)
	}
	hb, err := MetaHashFromJSON([]byte(b))
	if err != nil {
		t.Fatalf("b: %v", err)
	}
	if ha != hb {
		t.Fatal("JCS metaHash must be independent of key order/whitespace")
	}
}

func TestMetaHash_StructMatchesJSON(t *testing.T) {
	bi := &BusinessInfo{Title: "Pay", Summary: "to X", Items: []string{"a", "b"}}
	fromStruct, err := MetaHash(bi)
	if err != nil {
		t.Fatalf("struct: %v", err)
	}
	fromJSON, err := MetaHashFromJSON([]byte(`{"items":["a","b"],"summary":"to X","title":"Pay"}`))
	if err != nil {
		t.Fatalf("json: %v", err)
	}
	if fromStruct != fromJSON {
		t.Fatal("struct and equivalent JSON object must yield the same metaHash")
	}
}

func TestVerifyMetaHash(t *testing.T) {
	bi := &BusinessInfo{Title: "Pay"}
	h, err := MetaHash(bi)
	if err != nil {
		t.Fatalf("compute: %v", err)
	}
	req := sampleRequest()
	req.BusinessInfo = bi
	req.MetaHash = h[:]
	if err := VerifyMetaHash(&req); err != nil {
		t.Fatalf("matching metaHash rejected: %v", err)
	}

	req.MetaHash = bytes.Repeat([]byte{0xFF}, 32)
	if err := VerifyMetaHash(&req); err == nil {
		t.Fatal("tampered metaHash accepted")
	}

	absent := sampleRequest()
	absent.BusinessInfo = nil
	absent.MetaHash = EmptyMetaHash[:]
	if err := VerifyMetaHash(&absent); err != nil {
		t.Fatalf("absent businessInfo with EmptyMetaHash rejected: %v", err)
	}
}
