package transport

import (
	"crypto/hmac"
	"crypto/sha256"
	"testing"
)

func TestRendezvousNamespaceDeterministic(t *testing.T) {
	secret := []byte("group-secret-A")

	got := RendezvousNamespace(secret)
	if got == "" {
		t.Fatal("namespace must not be empty")
	}
	if again := RendezvousNamespace(secret); again != got {
		t.Fatalf("namespace not deterministic: %q vs %q", got, again)
	}

	// Must equal base32(HMAC-SHA256(secret,"tss-group")) per
	// docs/design/contract/protocol.md:35.
	mac := hmac.New(sha256.New, secret)
	mac.Write([]byte("tss-group"))
	want := rendezvousEncoding.EncodeToString(mac.Sum(nil))
	if got != want {
		t.Fatalf("namespace = %q, want %q", got, want)
	}
}

func TestRendezvousNamespaceSecretIsolation(t *testing.T) {
	a := RendezvousNamespace([]byte("group-secret-A"))
	b := RendezvousNamespace([]byte("group-secret-B"))
	if a == b {
		t.Fatal("different group secrets must yield different namespaces")
	}
}
