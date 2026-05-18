package contract

import (
	"bytes"
	"encoding/json"
	"testing"
)

// TestCanonicalBytes_JSONProtobufEquivalence is the binding acceptance
// property (S-001 §8): the same logical envelope, encoded to JSON and to
// protobuf then decoded back, must yield byte-identical CanonicalBytes.
func TestCanonicalBytes_JSONProtobufEquivalence(t *testing.T) {
	src := sampleRequest()
	src.BusinessInfo = &BusinessInfo{Title: "Pay", Items: []string{"x", "y"}}
	h, err := MetaHash(src.BusinessInfo)
	if err != nil {
		t.Fatalf("metaHash: %v", err)
	}
	src.MetaHash = h[:]
	src.ProposerSig = []byte("opaque-sig-bytes")

	want, err := CanonicalBytes(&src)
	if err != nil {
		t.Fatalf("source CanonicalBytes: %v", err)
	}

	// JSON wire path (api.md A2).
	rawJSON, err := json.Marshal(&src)
	if err != nil {
		t.Fatalf("json marshal: %v", err)
	}
	var fromJSON SigningRequest
	if err := json.Unmarshal(rawJSON, &fromJSON); err != nil {
		t.Fatalf("json unmarshal: %v", err)
	}
	gotJSON, err := CanonicalBytes(&fromJSON)
	if err != nil {
		t.Fatalf("json CanonicalBytes: %v", err)
	}

	// protobuf wire path (protocol.md §5 START envelope).
	rawPB, err := src.MarshalProto()
	if err != nil {
		t.Fatalf("proto marshal: %v", err)
	}
	fromPB, err := UnmarshalProto(rawPB)
	if err != nil {
		t.Fatalf("proto unmarshal: %v", err)
	}
	gotPB, err := CanonicalBytes(fromPB)
	if err != nil {
		t.Fatalf("proto CanonicalBytes: %v", err)
	}

	if !bytes.Equal(want, gotJSON) {
		t.Fatal("JSON-decoded envelope changed CanonicalBytes")
	}
	if !bytes.Equal(want, gotPB) {
		t.Fatal("protobuf-decoded envelope changed CanonicalBytes")
	}
	// metaHash must also re-verify across both wire forms.
	if err := VerifyMetaHash(&fromJSON); err != nil {
		t.Fatalf("JSON path VerifyMetaHash: %v", err)
	}
	if err := VerifyMetaHash(fromPB); err != nil {
		t.Fatalf("protobuf path VerifyMetaHash: %v", err)
	}
}

func TestProto_RoundTrip(t *testing.T) {
	src := sampleRequest()
	src.BusinessInfo = &BusinessInfo{Title: "T", Refs: map[string]string{"a": "1"}}
	src.ProposerSig = []byte{0x01, 0x02, 0x03}

	raw, err := src.MarshalProto()
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	got, err := UnmarshalProto(raw)
	if err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if got.Version != src.Version || got.RequestID != src.RequestID ||
		got.GroupID != src.GroupID || got.Chain != src.Chain ||
		got.Proposer != src.Proposer || got.CreatedAt != src.CreatedAt ||
		got.Expiry != src.Expiry {
		t.Fatal("scalar fields not preserved")
	}
	if !bytes.Equal(got.UnsignedTx, src.UnsignedTx) || !bytes.Equal(got.Digest32, src.Digest32) ||
		!bytes.Equal(got.MetaHash, src.MetaHash) || !bytes.Equal(got.ProposerSig, src.ProposerSig) {
		t.Fatal("byte fields not preserved")
	}
	if got.BusinessInfo == nil || got.BusinessInfo.Title != "T" || got.BusinessInfo.Refs["a"] != "1" {
		t.Fatal("businessInfo not preserved")
	}
}

func TestUnmarshalProto_Malformed(t *testing.T) {
	if _, err := UnmarshalProto([]byte{0xff, 0xff, 0xff}); err == nil {
		t.Fatal("malformed protobuf accepted")
	}
}
