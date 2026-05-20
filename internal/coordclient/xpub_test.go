package coordclient

import (
	"bytes"
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"testing"
)

// AD-4: coordclient.Xpub against the api.md B8 contract — owning-member-only
// release of (Q_master, chaincode). The shared mock-coord re-verifies the
// member auth signature exactly as the server, so a successful Xpub call is
// also evidence the B-side signing path reaches /xpub byte-for-byte.

func TestXpub_B8_HappyPath(t *testing.T) {
	priv := mustKey(t)
	m := newMockCoord(t, priv.PubKey().SerializeCompressed(), "g1")

	wantPub := bytes.Repeat([]byte{0x04, 0xAA, 0xBB}, 11) // 33 bytes — any non-empty slice
	wantCC := bytes.Repeat([]byte{0x55}, 32)
	m.on(http.MethodGet, "xpub", func(w http.ResponseWriter, _ *http.Request, _ []byte) {
		_ = json.NewEncoder(w).Encode(map[string]string{
			"ecdsaPubkeyHex": hex.EncodeToString(wantPub),
			"chaincodeHex":   hex.EncodeToString(wantCC),
		})
	})

	c := newTestClient(t, m, priv)
	xp, err := c.Xpub(context.Background())
	if err != nil {
		t.Fatalf("Xpub: %v", err)
	}
	if !bytes.Equal(xp.ECDSAPubkey, wantPub) {
		t.Fatalf("pubkey mismatch: got %x want %x", xp.ECDSAPubkey, wantPub)
	}
	if !bytes.Equal(xp.Chaincode, wantCC) {
		t.Fatalf("chaincode mismatch: got %x want %x", xp.Chaincode, wantCC)
	}
}

func TestXpub_B8_LegacyNoHD_Surfaces409(t *testing.T) {
	priv := mustKey(t)
	m := newMockCoord(t, priv.PubKey().SerializeCompressed(), "g1")
	m.on(http.MethodGet, "xpub", func(w http.ResponseWriter, _ *http.Request, _ []byte) {
		m.writeErr(w, http.StatusConflict, CodeLegacyNoHD,
			"group predates HD; multi-group remains the multi-address path")
	})
	c := newTestClient(t, m, priv)
	_, err := c.Xpub(context.Background())
	if err == nil {
		t.Fatal("expected LEGACY_NO_HD error")
	}
	if !errors.Is(err, ErrLegacyNoHD) {
		t.Fatalf("err must satisfy ErrLegacyNoHD, got %v", err)
	}
	var ae *APIError
	if !errors.As(err, &ae) || ae.Code != CodeLegacyNoHD || ae.Status != http.StatusConflict {
		t.Fatalf("APIError shape: %+v", ae)
	}
}

func TestXpub_B8_LockedRetriesThenSucceeds(t *testing.T) {
	priv := mustKey(t)
	m := newMockCoord(t, priv.PubKey().SerializeCompressed(), "g1")
	var n int
	m.on(http.MethodGet, "xpub", func(w http.ResponseWriter, _ *http.Request, _ []byte) {
		n++
		if n < 2 {
			m.writeErr(w, http.StatusServiceUnavailable, CodeLocked, "locked")
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]string{
			"ecdsaPubkeyHex": hex.EncodeToString(bytes.Repeat([]byte{0x02}, 33)),
			"chaincodeHex":   hex.EncodeToString(bytes.Repeat([]byte{0x77}, 32)),
		})
	})
	c := newTestClient(t, m, priv)
	xp, err := c.Xpub(context.Background())
	if err != nil {
		t.Fatalf("Xpub after retry: %v", err)
	}
	if len(xp.Chaincode) != 32 || n < 2 {
		t.Fatalf("unexpected: chaincode=%dB attempts=%d", len(xp.Chaincode), n)
	}
}

func TestXpub_B8_RejectsBadChaincodeLength(t *testing.T) {
	priv := mustKey(t)
	m := newMockCoord(t, priv.PubKey().SerializeCompressed(), "g1")
	m.on(http.MethodGet, "xpub", func(w http.ResponseWriter, _ *http.Request, _ []byte) {
		_ = json.NewEncoder(w).Encode(map[string]string{
			"ecdsaPubkeyHex": hex.EncodeToString(bytes.Repeat([]byte{0x02}, 33)),
			"chaincodeHex":   hex.EncodeToString(bytes.Repeat([]byte{0x77}, 31)), // off by one
		})
	})
	c := newTestClient(t, m, priv)
	_, err := c.Xpub(context.Background())
	if err == nil {
		t.Fatal("expected 31-byte chaincode rejection")
	}
	if !strings.Contains(err.Error(), "32 bytes") {
		t.Fatalf("expected message to mention 32 bytes, got %v", err)
	}
}

func TestXpub_B8_BadHexRejected(t *testing.T) {
	priv := mustKey(t)
	m := newMockCoord(t, priv.PubKey().SerializeCompressed(), "g1")
	m.on(http.MethodGet, "xpub", func(w http.ResponseWriter, _ *http.Request, _ []byte) {
		_ = json.NewEncoder(w).Encode(map[string]string{
			"ecdsaPubkeyHex": "not-hex",
			"chaincodeHex":   hex.EncodeToString(bytes.Repeat([]byte{0x77}, 32)),
		})
	})
	c := newTestClient(t, m, priv)
	if _, err := c.Xpub(context.Background()); err == nil {
		t.Fatal("expected bad-hex pubkey rejection")
	}
}
