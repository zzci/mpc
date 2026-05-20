package mpcnet

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"sync"
	"testing"
	"time"

	"github.com/btcsuite/btcd/btcec/v2"
	btcecdsa "github.com/btcsuite/btcd/btcec/v2/ecdsa"
	"github.com/libp2p/go-libp2p/core/crypto"
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/libp2p/go-libp2p/core/pnet"

	"github.com/bnb-chain/tss-lib/v3/ecdsa/keygen"

	"github.com/zzci/mpc/internal/contract"
	"github.com/zzci/mpc/internal/mpc"
	"github.com/zzci/mpc/internal/transport"
)

// DM-2 production-engine smoke (distributed-mpc-impl.md §C): 3 in-process
// devices, real libp2p stack (Noise + pnet + TCP loopback), real
// transport.Session — no relay (direct dials are sufficient for the smoke;
// the relay path is gated by cli/e2e_test.go separately).
//
// Skipped under -short so the routine developer loop pays nothing; the full
// hard gate (`-race -p1 -timeout=1200s`) exercises it. Paillier modulus/factor
// ZK proofs run with their production policy: mpc.NewKeygenParty enforces
// proofs-ON, and this smoke deliberately does NOT bypass that — production
// parity is part of the smoke's job (security.md invariant #10).

const (
	smokeN         = 3
	smokeThreshold = 1 // 2-of-3
	// eip155Digest is the canonical EIP-155 spec example digest the cli E2E
	// already pins (internal/xchaintest/xchain_test.go anchors the same RLP
	// → keccak256). Using the same anchor here ties the smoke's ecrecover
	// asserstion to a real-world chain digest.
	smokeDigestHex = "daf5a779ae972f972197303d7b574746c7ef83eadac0f2791ad23db92e4c8e53"
)

// --- libp2p test fabric -------------------------------------------------

func smokePSK(t *testing.T) pnet.PSK {
	t.Helper()
	psk := make([]byte, 32)
	if _, err := rand.Read(psk); err != nil {
		t.Fatalf("psk: %v", err)
	}
	return psk
}

func smokeHostKey(t *testing.T) crypto.PrivKey {
	t.Helper()
	priv, _, err := crypto.GenerateSecp256k1Key(rand.Reader)
	if err != nil {
		t.Fatalf("host key: %v", err)
	}
	return priv
}

func smokeMemberKey(t *testing.T) *btcec.PrivateKey {
	t.Helper()
	k, err := btcec.NewPrivateKey()
	if err != nil {
		t.Fatalf("member key: %v", err)
	}
	return k
}

// smokeFabric is the 3-device libp2p mesh the smoke runs against: one
// Transport per device, full-mesh direct dials (no relay), and a member-key
// table the per-session PeerResolver consults.
type smokeFabric struct {
	transports []*transport.Transport
	memberKeys map[string]*btcec.PrivateKey
	peers      PeerTable
}

func newSmokeFabric(ctx context.Context, t *testing.T) *smokeFabric {
	t.Helper()
	psk := smokePSK(t)

	keys := make(map[string]*btcec.PrivateKey, smokeN)
	for i := 0; i < smokeN; i++ {
		keys[partyTagFor(i)] = smokeMemberKey(t)
	}

	trs := make([]*transport.Transport, smokeN)
	for i := range trs {
		tr, err := transport.New(ctx, transport.Config{
			HostKey: smokeHostKey(t),
			PSK:     psk,
		})
		if err != nil {
			t.Fatalf("transport.New[%d]: %v", i, err)
		}
		t.Cleanup(func() { _ = tr.Close() })
		trs[i] = tr
	}

	// Full mesh: every device dials every other device once.
	for i, a := range trs {
		for j, b := range trs {
			if i == j {
				continue
			}
			if err := a.Connect(ctx, peer.AddrInfo{ID: b.ID(), Addrs: b.Addrs()}); err != nil {
				t.Fatalf("connect %d->%d: %v", i, j, err)
			}
		}
	}

	pt := make(PeerTable, smokeN)
	for i, tr := range trs {
		pt[partyTagFor(i)] = tr.ID()
	}

	return &smokeFabric{transports: trs, memberKeys: keys, peers: pt}
}

// joinSmoke opens one Session per device for the given session ID, registers
// the shared PeerResolver, and returns the engine Transport seam for each.
// Sessions are closed via t.Cleanup when the test ends — one active session
// per Transport at a time (session.go:71).
func (f *smokeFabric) joinSmoke(ctx context.Context, t *testing.T, sid string) []Transport {
	t.Helper()
	resolve := func(partyID string) ([]byte, error) {
		// pumpReshare prefixes MpcMessage.From with the sender's committee
		// ('O' / 'N'); member identity is per device, so strip the marker
		// back to the bare device tag before lookup. cli/device.go applies
		// the same convention.
		tag := partyID
		if len(tag) >= 2 && (tag[0] == 'O' || tag[0] == 'N') {
			tag = tag[1:]
		}
		k, ok := f.memberKeys[tag]
		if !ok {
			return nil, contract.ErrBadSignature
		}
		return k.PubKey().SerializeCompressed(), nil
	}
	out := make([]Transport, smokeN)
	for i, tr := range f.transports {
		s, err := tr.JoinSession(ctx, transport.SessionConfig{
			SessionID: sid,
			MemberKey: f.memberKeys[partyTagFor(i)],
			Resolve:   resolve,
		})
		if err != nil {
			t.Fatalf("JoinSession[%d]: %v", i, err)
		}
		t.Cleanup(func() { _ = s.Close() })
		out[i] = FromSession(s)
	}
	return out
}

// peersFor returns a PeerTable scoped to `tags`, intersected with the fabric's
// full peer set. Callers pass the participating tag set (e.g. signers only).
func (f *smokeFabric) peersFor(tags []string) PeerTable {
	pt := make(PeerTable, len(tags))
	for _, tag := range tags {
		if id, ok := f.peers[tag]; ok {
			pt[tag] = id
		}
	}
	return pt
}

// loadSmokePreParams reuses tss-lib's bundled keygen fixtures so the safe-
// prime search is skipped. Paillier modulus / factor ZK proofs still run with
// their production policy; that is the proof cost the smoke is paying for.
func loadSmokePreParams(t *testing.T) []keygen.LocalPreParams {
	t.Helper()
	fx, _, err := keygen.LoadKeygenTestFixtures(smokeN)
	if err != nil {
		t.Fatalf("load keygen fixtures (run tss-lib keygen tests to generate them): %v", err)
	}
	out := make([]keygen.LocalPreParams, smokeN)
	for i := range out {
		out[i] = fx[i].LocalPreParams
	}
	return out
}

// --- the smoke ----------------------------------------------------------

// TestEngineSmoke_RealLibp2pKeygenSignReshare is the DM-2 production-engine
// smoke (distributed-mpc-impl.md §C). It drives all three engine entry points
// through their cross-cutting paths:
//
//   - RunKeygen — 3 devices, full Paillier proofs, real libp2p
//   - RunSign   — 2-of-3, real libp2p, ecrecover(digest, R, S, V) == master
//   - RunReshare — 3 devices, rotate mode (n=n', t=t'), master pubkey preserved
//
// Skipped under -short so the routine developer loop pays nothing; the full
// hard gate exercises it. Paillier modulus / factor ZK proofs run with the
// production policy (security.md invariant #10 — no ALLOW_INSECURE_MPC
// bypass) so this is a true production-parity smoke.
func TestEngineSmoke_RealLibp2pKeygenSignReshare(t *testing.T) {
	if testing.Short() {
		t.Skip("DM-2 mpcnet smoke: real libp2p keygen with Paillier proofs; skipped in -short")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()

	fabric := newSmokeFabric(ctx, t)
	pre := loadSmokePreParams(t)

	// --- keygen ---
	keygenSess := fabric.joinSmoke(ctx, t, "smoke-keygen")
	shares := make([]mpc.Share, smokeN)
	var wg sync.WaitGroup
	errs := make([]error, smokeN)
	for i := 0; i < smokeN; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			pp := pre[idx]
			sh, err := RunKeygen(ctx, keygenSess[idx], fabric.peers, KeygenConfig{
				PartyIndex: idx,
				Parties:    smokeN,
				Threshold:  smokeThreshold,
				PreParams:  &pp,
			})
			if err != nil {
				errs[idx] = err
				return
			}
			shares[idx] = sh
		}(i)
	}
	wg.Wait()
	for i, e := range errs {
		if e != nil {
			t.Fatalf("RunKeygen[%d]: %v", i, e)
		}
	}

	// Every party converges on the same master ECDSAPub.
	var wantX, wantY string
	for i, sh := range shares {
		sd, err := mpc.UnmarshalSaveData(sh.SaveData)
		if err != nil {
			t.Fatalf("UnmarshalSaveData[%d]: %v", i, err)
		}
		if sd.ECDSAPub == nil {
			t.Fatalf("share[%d] has no ECDSAPub", i)
		}
		x := hex.EncodeToString(sd.ECDSAPub.X().Bytes())
		y := hex.EncodeToString(sd.ECDSAPub.Y().Bytes())
		if i == 0 {
			wantX, wantY = x, y
		} else if x != wantX || y != wantY {
			t.Fatalf("share[%d] master pub drift: got (%s,%s), want (%s,%s)", i, x, y, wantX, wantY)
		}
	}
	masterPub := uncompressedPub(t, shares[0])

	// Close the keygen sessions so each transport can host a fresh signing
	// session — JoinSession is one-active-session-per-Transport (session.go:71).
	// t.Cleanup will Close again idempotently after the test ends.
	// The PeerResolver in joinSmoke captures the same member-key table, so the
	// senderAuth chain is unchanged across sessions.

	// --- sign (2-of-3) ---
	signers := []int{0, 1}
	signerTags := []string{partyTagFor(signers[0]), partyTagFor(signers[1])}
	signerPeers := fabric.peersFor(signerTags)
	signSess := fabric.joinSmoke(ctx, t, "smoke-sign")

	digest, err := hex.DecodeString(smokeDigestHex)
	if err != nil {
		t.Fatalf("decode digest: %v", err)
	}

	sigs := make([]mpc.Signature, len(signers))
	sigErrs := make([]error, len(signers))
	var swg sync.WaitGroup
	for k, idx := range signers {
		swg.Add(1)
		go func(slot, idx int) {
			defer swg.Done()
			sig, err := RunSign(ctx, signSess[idx], signerPeers, SignConfig{
				SessionID:    "smoke-sign",
				PartyIndex:   idx,
				Threshold:    smokeThreshold,
				Participants: signers,
				Share:        shares[idx],
				Digest:       digest,
			})
			if err != nil {
				sigErrs[slot] = err
				return
			}
			sigs[slot] = sig
		}(k, idx)
	}
	swg.Wait()
	for i, e := range sigErrs {
		if e != nil {
			t.Fatalf("RunSign[signer=%d]: %v", signers[i], e)
		}
	}

	// All signers must return the same {R,S,V}.
	for i := 1; i < len(sigs); i++ {
		if sigs[i] != sigs[0] {
			t.Fatalf("signer %d signature differs from signer %d: %+v vs %+v", signers[i], signers[0], sigs[i], sigs[0])
		}
	}

	// ecrecover(digest, R, S, V) must yield the group master public key —
	// the carrier's correctness anchor for the {R,S,V} output.
	recovered, _, err := btcecdsa.RecoverCompact(sigs[0].Compact(), digest)
	if err != nil {
		t.Fatalf("RecoverCompact: %v", err)
	}
	got := recovered.SerializeUncompressed()
	if hex.EncodeToString(got) != hex.EncodeToString(masterPub) {
		t.Fatalf("ecrecover != master pub:\n got %s\n want %s",
			hex.EncodeToString(got), hex.EncodeToString(masterPub))
	}

	// --- reshare (rotate mode, n=n', t=t') ---
	reshareSess := fabric.joinSmoke(ctx, t, "smoke-reshare")
	newShares := make([]mpc.Share, smokeN)
	reErrs := make([]error, smokeN)
	var rwg sync.WaitGroup
	for i := 0; i < smokeN; i++ {
		rwg.Add(1)
		go func(idx int) {
			defer rwg.Done()
			pp := pre[idx]
			ns, err := RunReshare(ctx, reshareSess[idx], fabric.peers, ReshareConfig{
				PartyIndex:   idx,
				Parties:      smokeN,
				OldThreshold: smokeThreshold,
				NewThreshold: smokeThreshold,
				OldShare:     shares[idx],
				PreParams:    &pp,
			})
			if err != nil {
				reErrs[idx] = err
				return
			}
			newShares[idx] = ns
		}(i)
	}
	rwg.Wait()
	for i, e := range reErrs {
		if e != nil {
			t.Fatalf("RunReshare[%d]: %v", i, e)
		}
	}

	// mpc.ReshareParty.Done() already enforces master-pubkey preservation
	// (sdk.md §7), but the smoke re-checks the invariant from the outside —
	// every reshared share must still report the same master ECDSAPub.
	wantPub := hex.EncodeToString(masterPub)
	for i, ns := range newShares {
		got := hex.EncodeToString(uncompressedPub(t, ns))
		if got != wantPub {
			t.Fatalf("reshared share[%d] master pub drift:\n got %s\n want %s", i, got, wantPub)
		}
	}
}

// uncompressedPub extracts the share's master public key in 65-byte
// uncompressed secp256k1 form (validated on-curve).
func uncompressedPub(t *testing.T, sh mpc.Share) []byte {
	t.Helper()
	sd, err := mpc.UnmarshalSaveData(sh.SaveData)
	if err != nil {
		t.Fatalf("UnmarshalSaveData: %v", err)
	}
	raw := make([]byte, 65)
	raw[0] = 0x04
	sd.ECDSAPub.X().FillBytes(raw[1:33])
	sd.ECDSAPub.Y().FillBytes(raw[33:65])
	pk, err := btcec.ParsePubKey(raw)
	if err != nil {
		t.Fatalf("master pub not on curve: %v", err)
	}
	return pk.SerializeUncompressed()
}
