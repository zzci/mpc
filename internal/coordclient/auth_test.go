package coordclient

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"net/http"
	"strconv"
	"testing"
	"time"

	"github.com/btcsuite/btcd/btcec/v2"

	"github.com/royqta/mcp-wallet/internal/contract"
)

// independentDigest recomputes the member-auth digest from the api.md B1 / S-001
// spec text WITHOUT calling the production helpers, so a divergence between the
// client and the documented scheme is caught (this is the byte-for-byte
// contract with internal/server/coord/auth.go).
func independentDigest(memberID, method, groupID string, params []byte, ts int64, nonce []byte) [32]byte {
	bound := []byte(method + "|" + groupID + "|")
	bound = append(bound, params...)
	bh := sha256.Sum256(bound)

	lp := func(b, v []byte) []byte {
		var p [4]byte
		binary.BigEndian.PutUint32(p[:], uint32(len(v)))
		return append(append(b, p[:]...), v...)
	}
	var pre []byte
	pre = append(pre, []byte("TSS-COORD-MEMBER-AUTH-v1")...)
	pre = append(pre, 0x00)
	pre = lp(pre, []byte(memberID))
	pre = lp(pre, []byte(method))
	pre = lp(pre, bh[:])
	var tb [8]byte
	binary.BigEndian.PutUint64(tb[:], uint64(ts))
	pre = append(pre, tb[:]...)
	pre = lp(pre, nonce)
	return sha256.Sum256(pre)
}

func TestMemberAuthDigest_MatchesSpec(t *testing.T) {
	params := []byte(`{"groupId":"g1","memberId":"m1"}`)
	ts := int64(1_700_000_000_000)
	nonce := []byte("0123456789abcdef")

	got := memberAuthDigest("m1", "B5:heartbeat", boundHash("B5:heartbeat", "g1", params), ts, nonce)
	want := independentDigest("m1", "B5:heartbeat", "g1", params, ts, nonce)
	if got != want {
		t.Fatalf("digest mismatch:\n got %x\nwant %x", got, want)
	}
}

func TestSignRequest_HeaderFormatAndVerifiable(t *testing.T) {
	priv, err := btcec.NewPrivateKey()
	if err != nil {
		t.Fatalf("key: %v", err)
	}
	fixedTS := time.UnixMilli(1_700_000_123_456)
	c, err := New("https://coord.example", "g1", "m1", priv,
		WithClock(func() time.Time { return fixedTS }))
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	body := []byte(`{"groupId":"g1","memberId":"m1","relayPeerID":"p"}`)
	req, _ := http.NewRequestWithContext(context.Background(), http.MethodPost, "https://coord.example/v1/members/self/heartbeat", bytes.NewReader(body))
	if err := c.signRequest(req, "B5:heartbeat", body); err != nil {
		t.Fatalf("signRequest: %v", err)
	}

	if req.Header.Get(headerMemberID) != "m1" {
		t.Fatalf("member id header = %q", req.Header.Get(headerMemberID))
	}
	if got := req.Header.Get(headerMemberTS); got != strconv.FormatInt(fixedTS.UnixMilli(), 10) {
		t.Fatalf("ts header = %q want %d", got, fixedTS.UnixMilli())
	}
	nonce, err := base64.StdEncoding.DecodeString(req.Header.Get(headerMemberNonce))
	if err != nil || len(nonce) != nonceLen {
		t.Fatalf("nonce header bad: %v len=%d", err, len(nonce))
	}
	sig, err := base64.StdEncoding.DecodeString(req.Header.Get(headerMemberSig))
	if err != nil || len(sig) == 0 {
		t.Fatalf("sig header bad: %v", err)
	}

	// The signature must verify against the documented digest with the
	// member public key (the same check coord performs).
	d := independentDigest("m1", "B5:heartbeat", "g1", body, fixedTS.UnixMilli(), nonce)
	if err := contract.VerifyDigest(c.MemberPublicKey(), d, sig); err != nil {
		t.Fatalf("server-side verify failed: %v", err)
	}
}

func TestSignRequest_NonceUniquePerCall(t *testing.T) {
	priv, _ := btcec.NewPrivateKey()
	c, _ := New("https://x", "g", "m", priv)
	seen := map[string]bool{}
	for i := 0; i < 64; i++ {
		req, _ := http.NewRequestWithContext(context.Background(), http.MethodGet, "https://x/v1/groups/g/pending", nil)
		if err := c.signRequest(req, "B3:pending", []byte("")); err != nil {
			t.Fatalf("sign: %v", err)
		}
		n := req.Header.Get(headerMemberNonce)
		if seen[n] {
			t.Fatalf("nonce reused at iteration %d: %s", i, n)
		}
		seen[n] = true
	}
}

func TestLoadIdentityKey_LenGuard(t *testing.T) {
	if _, err := loadIdentityKey([]byte{1, 2, 3}); err == nil {
		t.Fatal("expected error for short key")
	}
	priv, _ := btcec.NewPrivateKey()
	raw := priv.Serialize()
	got, err := loadIdentityKey(raw)
	if err != nil {
		t.Fatalf("loadIdentityKey: %v", err)
	}
	if !bytes.Equal(got.Serialize(), raw) {
		t.Fatal("round-trip key mismatch")
	}
}
