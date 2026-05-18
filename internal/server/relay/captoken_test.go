package relay

import (
	"encoding/base64"
	"errors"
	"testing"
	"time"

	btcec "github.com/btcsuite/btcd/btcec/v2"

	"github.com/zzci/mpc/internal/contract"
)

func mustGroupKey(t *testing.T) (*btcec.PrivateKey, string) {
	t.Helper()
	k, err := btcec.NewPrivateKey()
	if err != nil {
		t.Fatalf("gen key: %v", err)
	}
	return k, base64.StdEncoding.EncodeToString(k.PubKey().SerializeCompressed())
}

// signedToken returns a CapToken signed by priv over its canonical digest.
func signedToken(t *testing.T, priv *btcec.PrivateKey, tok *contract.CapToken) *contract.CapToken {
	t.Helper()
	d, err := capTokenDigest(tok)
	if err != nil {
		t.Fatalf("digest: %v", err)
	}
	tok.GroupSig = contract.SignDigest(priv, d)
	return tok
}

func validToken(scope contract.CapScope) *contract.CapToken {
	now := time.Now().UnixMilli()
	return &contract.CapToken{
		GroupID:   "group-1",
		MemberID:  "member-1",
		Scope:     scope,
		NotBefore: now - 1000,
		NotAfter:  now + 60_000,
		Nonce:     []byte("nonce-aaaa"),
	}
}

func TestNewTrustAnchors(t *testing.T) {
	_, pub := mustGroupKey(t)

	if _, err := newTrustAnchors("config", []string{pub}); err != nil {
		t.Fatalf("config source: unexpected err %v", err)
	}
	if _, err := newTrustAnchors("coord-sync", []string{pub}); !errors.Is(err, errCoordSyncUnsupported) {
		t.Fatalf("coord-sync must be rejected (relay coord-independent), got %v", err)
	}
	if _, err := newTrustAnchors("config", nil); err == nil {
		t.Fatal("empty anchor set must error")
	}
	if _, err := newTrustAnchors("config", []string{"!!not-base64!!"}); err == nil {
		t.Fatal("bad base64 must error")
	}
}

func TestCapTokenDigestDeterministicAndBound(t *testing.T) {
	// Derive every variant from one base value so "identical" tokens are
	// byte-identical (validToken() reads time.Now(), so calling it twice
	// would yield different NotBefore/NotAfter and a spurious mismatch).
	base := *validToken(contract.ScopeRelayReserve)

	a, b := base, base
	da, _ := capTokenDigest(&a)
	db, _ := capTokenDigest(&b)
	if da != db {
		t.Fatal("identical tokens must hash identically")
	}

	b.MemberID = "member-2"
	db, _ = capTokenDigest(&b)
	if da == db {
		t.Fatal("memberId change must change digest")
	}

	// groupSig must not feed the digest (it is the signature over it).
	c := base
	c.GroupSig = []byte("anything")
	dc, _ := capTokenDigest(&c)
	if dc != da {
		t.Fatal("groupSig must be excluded from the preimage")
	}
}

func TestVerifyCapToken(t *testing.T) {
	priv, pub := mustGroupKey(t)
	ta, err := newTrustAnchors("config", []string{pub})
	if err != nil {
		t.Fatalf("anchors: %v", err)
	}
	now := time.Now()

	ok := signedToken(t, priv, validToken(contract.ScopeRelayReserve))
	if err := ta.verifyCapToken(ok, contract.ScopeRelayReserve, now, time.Second); err != nil {
		t.Fatalf("valid token must verify: %v", err)
	}

	// Wrong scope.
	if err := ta.verifyCapToken(ok, contract.ScopeRendezvousRegister, now, time.Second); !errors.Is(err, ErrUntrustedToken) {
		t.Fatalf("scope mismatch must fail, got %v", err)
	}

	// Tampered field after signing → signature no longer matches.
	tampered := signedToken(t, priv, validToken(contract.ScopeRelayReserve))
	tampered.MemberID = "attacker"
	if err := ta.verifyCapToken(tampered, contract.ScopeRelayReserve, now, time.Second); !errors.Is(err, ErrUntrustedToken) {
		t.Fatalf("tampered token must fail, got %v", err)
	}

	// Signed by an untrusted key.
	other, _ := mustGroupKey(t)
	foreign := signedToken(t, other, validToken(contract.ScopeRelayReserve))
	if err := ta.verifyCapToken(foreign, contract.ScopeRelayReserve, now, time.Second); !errors.Is(err, ErrUntrustedToken) {
		t.Fatalf("untrusted signer must fail, got %v", err)
	}

	// Expired / not-yet-valid (outside skew).
	exp := validToken(contract.ScopeRelayReserve)
	exp.NotAfter = now.UnixMilli() - 10_000
	exp = signedToken(t, priv, exp)
	if err := ta.verifyCapToken(exp, contract.ScopeRelayReserve, now, time.Second); !errors.Is(err, ErrUntrustedToken) {
		t.Fatalf("expired token must fail, got %v", err)
	}
	fut := validToken(contract.ScopeRelayReserve)
	fut.NotBefore = now.UnixMilli() + 10_000
	fut = signedToken(t, priv, fut)
	if err := ta.verifyCapToken(fut, contract.ScopeRelayReserve, now, time.Second); !errors.Is(err, ErrUntrustedToken) {
		t.Fatalf("not-yet-valid token must fail, got %v", err)
	}
}
