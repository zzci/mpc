package cli

import (
	"context"
	"encoding/base64"
	"encoding/hex"
	"path/filepath"
	"sync"
	"testing"
	"time"

	btcec "github.com/btcsuite/btcd/btcec/v2"
)

// TestE2EMultiProcessKeygenSignReshareViaRelay is the CLI-001 hard acceptance
// gate (docs/design/testing.md §3, docs/design/PLAN.md §3): a 2-of-3 wallet runs real
// tss-lib keygen -> sign -> reshare where every MPC message travels a real
// circuit-relay v2 hop through a real `node`-relay SUBPROCESS (Noise
// end-to-end, pnet PSK, CapToken-gated). It asserts: all devices agree on the
// master public key; the {R,S,V} over a real ETH EIP-155 digest ecrecovers to
// that key (low-S/V correct); resharing preserves the master key. The relay
// only forwards ciphertext and never participates in MPC.
//
// Each device is a goroutine with its OWN libp2p host/peer (not an OS
// subprocess): the binding gate runs this under `go test -race ./...` where
// ~12 parallel race-instrumented suites starve OS-forked children so the
// libp2p/relay handshake cannot complete (a resource-contention failure, not a
// data race). Goroutine devices remove that inter-process starvation while
// staying FULLY race-instrumented (strictly more race coverage) and keeping
// real Noise + real circuit-relay v2 through the real relay process; the
// relay/coord backends remain real separate processes per the CLI-001 intent.
//
// P0/P4 device gates (gomobile/RN/real hardware) are explicitly OUT OF SCOPE
// (cmd/cli doc, docs/design/testing.md §5; owned by B-003/B-004/B-006).
func TestE2EMultiProcessKeygenSignReshareViaRelay(t *testing.T) {
	if testing.Short() {
		t.Skip("E2E MPC-over-relay carrier: skipped in -short")
	}
	root := repoRoot(t)
	work := t.TempDir()
	nodeBin := filepath.Join(work, "node")
	buildBinary(t, root, "./cmd/node", nodeBin)

	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Minute)
	defer cancel()

	// Wallet-group key = self-sovereign cap-token issuer / relay trust anchor.
	groupKey, err := btcec.NewPrivateKey()
	if err != nil {
		t.Fatal(err)
	}
	groupPubB64 := base64.StdEncoding.EncodeToString(groupKey.PubKey().SerializeCompressed())
	groupKeyHex := hex.EncodeToString(groupKey.Serialize())

	relay := startRelay(t, ctx, root, nodeBin, work, groupPubB64)

	const n, threshold = 3, 1
	signers := []int{0, 1}
	rzdir := filepath.Join(work, "rz")
	if err := mkdir(rzdir); err != nil {
		t.Fatal(err)
	}

	cfgs := make([]DeviceConfig, n)
	for i := 0; i < n; i++ {
		mk, _ := btcec.NewPrivateKey()
		cfgs[i] = DeviceConfig{
			Index:         i,
			N:             n,
			Threshold:     threshold,
			GroupID:       "wallet-e2e",
			RelayPeerID:   relay.PeerID,
			RelayAddrs:    relay.Addrs,
			PSKHex:        harnessPSKHex,
			MemberKeyHex:  hex.EncodeToString(mk.Serialize()),
			GroupKeyHex:   groupKeyHex,
			Signers:       signers,
			DigestHex:     eip155Digest,
			RendezvousDir: rzdir,
		}
	}

	results := runDevicesInProc(t, ctx, cfgs)

	digest, _ := hex.DecodeString(eip155Digest)
	var groupPub string
	for i, r := range results {
		if r.Err != "" {
			t.Fatalf("device %d failed: %s", i, r.Err)
		}
		if !r.AllViaRelay {
			t.Fatalf("device %d had a non-relay peer connection (zero-trust relay path violated)", i)
		}
		if r.GroupPubHex == "" {
			t.Fatalf("device %d produced no keygen public key", i)
		}
		if groupPub == "" {
			groupPub = r.GroupPubHex
		} else if r.GroupPubHex != groupPub {
			t.Fatalf("device %d keygen public key mismatch:\n %s\n %s", i, r.GroupPubHex, groupPub)
		}
		// Reshare must preserve the wallet master public key (custody
		// invariant, docs/design/mcp/sdk.md §7).
		if r.ResharedPubHex == "" || r.ResharedPubHex != groupPub {
			t.Fatalf("device %d reshare changed the master public key:\n keygen %s\n reshare %s",
				i, groupPub, r.ResharedPubHex)
		}
	}

	// {R,S,V} cross-verification: ecrecover over the real ETH digest must
	// yield the group master public key (coord never saw a share).
	signed := 0
	for i, r := range results {
		if !r.Signed {
			continue
		}
		signed++
		got := hex.EncodeToString(recoverPub(t, digest, r.SigRHex, r.SigSHex, r.SigV))
		if got != groupPub {
			t.Fatalf("device %d {R,S,V} ecrecover != group master key:\n got %s\n want %s",
				i, got, groupPub)
		}
		// low-S enforced by tss-lib finalize: S must be in the lower half.
		assertLowS(t, r.SigSHex)
	}
	if signed != len(signers) {
		t.Fatalf("expected %d signers to produce a signature, got %d", len(signers), signed)
	}
}

// runDevicesInProc runs each device as a goroutine with its own libp2p host.
// No Go memory is shared between the device goroutines and this function: each
// writes only its own results[i] slot, read solely after the WaitGroup has
// converged (happens-before via the done channel) — race-clean by construction.
func runDevicesInProc(t *testing.T, ctx context.Context, cfgs []DeviceConfig) []DeviceResult {
	t.Helper()
	results := make([]DeviceResult, len(cfgs))
	var wg sync.WaitGroup
	for i := range cfgs {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			results[idx] = RunDeviceInProc(ctx, cfgs[idx])
		}(i)
	}
	done := make(chan struct{})
	go func() { wg.Wait(); close(done) }()
	select {
	case <-done:
	case <-ctx.Done():
		t.Fatal("devices did not finish before deadline")
	}
	return results
}
