package mobileapi

import (
	"reflect"
	"testing"
	"time"

	"github.com/btcsuite/btcd/btcec/v2"
	btcecdsa "github.com/btcsuite/btcd/btcec/v2/ecdsa"

	"github.com/zzci/mpc/internal/contract"
)

// TestSignHappyPathCallbackOrder exercises the full DM-3 WYSIWYS signing
// flow over a distributed ring fabric: every signer device verifies →
// decodes → emits OnDecoded → host Approves (host→Go, D4-1) → drives one
// mpc.SignParty against the wire → emits OnResult. It asserts the callback
// order, that each device produces the same {R,S,V}, and that the
// signature recovers the group master public key.
func TestSignHappyPathCallbackOrder(t *testing.T) {
	sdks, fabric, c := committeeSDKs(t)
	st := buildStart(t, testMembers[:testThreshold+1], nil)

	// Drive every signer device concurrently; their wire pumps engage
	// only after each device's host Approve.
	signerCount := testThreshold + 1
	recs := make([]*recorder, signerCount)
	sessions := make([]*SignSession, signerCount)
	for i := 0; i < signerCount; i++ {
		cfg := signConfigPayload(c.groupID, i, testParties, testThreshold, testMembers, testRelay, st)
		recs[i] = newRecorder()
		sessions[i] = sdks[i].Sign(marshalConfig(t, cfg), fabric.wcFor(i), signAdapter{recs[i]})
	}
	// Wait for OnDecoded on every device, then approve in lockstep so
	// the MPC ring sees every signer drive its part of the protocol.
	for i := 0; i < signerCount; i++ {
		recs[i].waitDecoded(t)
		if code, msg, _, _ := recs[i].result(); code != "" {
			t.Fatalf("device %d decode stage errored: %s %s", i, code, msg)
		}
	}
	for i := 0; i < signerCount; i++ {
		sessions[i].Approve()
	}
	for i := 0; i < signerCount; i++ {
		recs[i].wait(t)
	}
	fabric.assertNoErrs(t)

	var rsv []byte
	for i := 0; i < signerCount; i++ {
		code, msg, _, sig := recs[i].result()
		if code != "" {
			t.Fatalf("device %d sign errored: %s %s", i, code, msg)
		}
		if want := []string{"decoded", "result"}; !reflect.DeepEqual(recs[i].snapOrder(), want) {
			t.Fatalf("device %d order=%v want %v", i, recs[i].snapOrder(), want)
		}
		if len(sig) != 65 {
			t.Fatalf("device %d rsv len=%d want 65", i, len(sig))
		}
		if rsv == nil {
			rsv = sig
		} else if !reflect.DeepEqual(rsv, sig) {
			t.Fatalf("device %d produced a different signature than device 0", i)
		}
	}
	// rsv recovers exactly the group master public key.
	rec, _, err := btcecdsa.RecoverCompact(rsv, mustHex(t, eip155Digest))
	if err != nil {
		t.Fatalf("ecrecover: %v", err)
	}
	if got := hexEncode(rec.SerializeCompressed()); got != c.summary.GroupPubKey {
		t.Fatalf("recovered pub %s != group %s", got, c.summary.GroupPubKey)
	}
}

// TestSignSecurityHardReject asserts every security-class failure stops
// before OnDecoded with the contracted code and never enters MPC
// (docs/design/mcp/sdk.md §3/§5). The rejection fires before any share or
// wire pump is touched, so a fresh single-party SDK with a no-op wire is
// sufficient.
func TestSignSecurityHardReject(t *testing.T) {
	cases := []struct {
		name   string
		mutate func(*contract.StartSigning, *btcec.PrivateKey)
		want   string
	}{
		{"unsupported version", func(st *contract.StartSigning, _ *btcec.PrivateKey) {
			st.Envelope.Version = 999
		}, CodeUnsupportedVersion},
		{"tampered proposer sig", func(st *contract.StartSigning, p *btcec.PrivateKey) {
			_ = contract.SignEnvelope(p, &st.Envelope)
			st.Envelope.ProposerSig[len(st.Envelope.ProposerSig)-1] ^= 0xff
		}, CodeBadProposerSig},
		{"metaHash mismatch", func(st *contract.StartSigning, _ *btcec.PrivateKey) {
			st.Envelope.MetaHash = make([]byte, 32) // not EmptyMetaHash
		}, CodeInvalidEnvelope},
		{"requestId inconsistent", func(st *contract.StartSigning, _ *btcec.PrivateKey) {
			st.RequestID = "other"
		}, CodeInvalidEnvelope},
		{"digest mismatch", func(st *contract.StartSigning, _ *btcec.PrivateKey) {
			st.Envelope.Digest32[0] ^= 0xff
		}, CodeDigestMismatch},
		{"decode rejected", func(st *contract.StartSigning, _ *btcec.PrivateKey) {
			st.Envelope.UnsignedTx = []byte{0x00, 0x01, 0x02}
		}, CodeDecodeRejected},
		{"unsupported chain", func(st *contract.StartSigning, _ *btcec.PrivateKey) {
			st.Envelope.Chain = "solana"
		}, CodeUnsupportedChain},
		{"no positive expiry", func(st *contract.StartSigning, _ *btcec.PrivateKey) {
			st.Envelope.Expiry = 0
			st.Deadline = 0
		}, CodeInvalidEnvelope},
		{"expired", func(st *contract.StartSigning, _ *btcec.PrivateKey) {
			past := time.Now().UnixMilli() - 1
			st.Envelope.Expiry = past
			st.Deadline = past
		}, CodeExpired},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			// No keygen: a security reject must fire before MPC regardless of
			// whether this device holds a share.
			sdk := newTestSDK(t, 0)
			r := newRecorder()
			st := buildStart(t, nil, tc.mutate)
			cfg := signConfigPayload("g1", 0, testParties, testThreshold, testMembers, testRelay, st)
			sdk.Sign(marshalConfig(t, cfg), noopWire{}, signAdapter{r})
			r.wait(t)
			code, msg, _, _ := r.result()
			if code != tc.want {
				t.Fatalf("code=%q want %q (msg=%s)", code, tc.want, msg)
			}
			for _, o := range r.snapOrder() {
				if o == "decoded" {
					t.Fatal("OnDecoded must not fire on a security reject")
				}
			}
		})
	}
}

// TestSignRejectByReviewer: a digest-bound envelope decodes, the human
// rejects (host→Go), and no signature is produced. No shares are needed
// because rejection exits before the MPC stage.
func TestSignRejectByReviewer(t *testing.T) {
	sdk := newTestSDK(t, 0)
	r := newRecorder()
	st := buildStart(t, nil, nil)
	cfg := signConfigPayload("g1", 0, testParties, testThreshold, testMembers, testRelay, st)
	ss := sdk.Sign(marshalConfig(t, cfg), noopWire{}, signAdapter{r})
	r.waitDecoded(t)
	ss.Reject()
	r.wait(t)

	if code, _, _, _ := r.result(); code != CodeRejected {
		t.Fatalf("code=%q want %q", code, CodeRejected)
	}
	if want := []string{"decoded", "error"}; !reflect.DeepEqual(r.snapOrder(), want) {
		t.Fatalf("order=%v want %v", r.snapOrder(), want)
	}
}

// TestSignDecisionIdempotent: a second host decision after the first is a
// safe no-op (Approve/Reject guard).
func TestSignDecisionIdempotent(t *testing.T) {
	sdk := newTestSDK(t, 0)
	r := newRecorder()
	st := buildStart(t, nil, nil)
	cfg := signConfigPayload("g1", 0, testParties, testThreshold, testMembers, testRelay, st)
	ss := sdk.Sign(marshalConfig(t, cfg), noopWire{}, signAdapter{r})
	r.waitDecoded(t)
	ss.Reject()
	ss.Approve() // ignored: already decided
	ss.Reject()
	r.wait(t)
	if code, _, _, _ := r.result(); code != CodeRejected {
		t.Fatalf("code=%q want %q", code, CodeRejected)
	}
}

// TestSignRejectsBadConfig: DM-3 hard-cut for Sign. Every mandatory field
// missing → CodeBadConfig; the WYSIWYS contract is unreachable without the
// envelope.
func TestSignRejectsBadConfig(t *testing.T) {
	sdk := newTestSDK(t, 0)
	st := buildStart(t, nil, nil)
	good := signConfigPayload("g1", 0, testParties, testThreshold, testMembers, testRelay, st)

	type tc struct {
		name string
		mut  func(*signConfig)
	}
	cases := []tc{
		{"missing groupId", func(c *signConfig) { c.GroupID = nil }},
		{"missing sessionID", func(c *signConfig) { c.SessionID = nil }},
		{"missing partyIndex", func(c *signConfig) { c.PartyIndex = nil }},
		{"missing n", func(c *signConfig) { c.N = nil }},
		{"missing t", func(c *signConfig) { c.T = nil }},
		{"missing memberSet", func(c *signConfig) { c.MemberSet = nil }},
		{"missing relay", func(c *signConfig) { c.Relay = nil }},
		{"missing role", func(c *signConfig) { c.Role = nil }},
		{"missing start", func(c *signConfig) { c.Start = nil }},
		{"sessionID != requestId", func(c *signConfig) { v := "drift"; c.SessionID = &v }},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			cfg := good
			c.mut(&cfg)
			r := newRecorder()
			sdk.Sign(marshalConfig(t, cfg), noopWire{}, signAdapter{r})
			r.wait(t)
			if code, _, _, _ := r.result(); code != CodeBadConfig {
				t.Fatalf("code=%q want %q", code, CodeBadConfig)
			}
		})
	}
}
