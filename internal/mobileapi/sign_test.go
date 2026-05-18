package mobileapi

import (
	"reflect"
	"testing"
	"time"

	"github.com/btcsuite/btcd/btcec/v2"
	btcecdsa "github.com/btcsuite/btcd/btcec/v2/ecdsa"

	"github.com/royqta/mcp-wallet/internal/contract"
)

// TestSignHappyPathCallbackOrder exercises the full WYSIWYS flow in-process:
// verify → decode → OnDecoded → host Approve (host→Go, D4-1) → MPC sign →
// OnResult. It asserts the callback order and that the {R,S,V} recovers the
// group master public key.
func TestSignHappyPathCallbackOrder(t *testing.T) {
	sdk := committeeSDK(t)
	sum := sharedCommittee(t).summary

	r := newRecorder()
	ss := sdk.Sign(buildStart(t, nil), signAdapter{r})
	r.waitDecoded(t)
	if code, msg, _, _ := r.result(); code != "" {
		t.Fatalf("decode stage errored: %s %s", code, msg)
	}
	ss.Approve()
	r.wait(t)

	code, msg, _, rsv := r.result()
	if code != "" {
		t.Fatalf("sign errored: %s %s", code, msg)
	}
	if want := []string{"decoded", "result"}; !reflect.DeepEqual(r.snapOrder(), want) {
		t.Fatalf("order=%v want %v", r.snapOrder(), want)
	}
	if len(rsv) != 65 {
		t.Fatalf("rsv len=%d want 65", len(rsv))
	}
	// rsv recovers exactly the group master public key.
	rec, _, err := btcecdsa.RecoverCompact(rsv, mustHex(t, eip155Digest))
	if err != nil {
		t.Fatalf("ecrecover: %v", err)
	}
	if got := hexEncode(rec.SerializeCompressed()); got != sum.GroupPubKey {
		t.Fatalf("recovered pub %s != group %s", got, sum.GroupPubKey)
	}
}

// TestSignSecurityHardReject asserts every security-class failure stops before
// OnDecoded with the contracted code and never enters MPC
// (docs/design/mcp/sdk.md §3/§5).
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
			sdk := newTestSDK(t)
			r := newRecorder()
			sdk.Sign(buildStart(t, tc.mutate), signAdapter{r})
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

// TestSignRejectByReviewer: a digest-bound envelope decodes, the human rejects
// (host→Go), and no signature is produced.
func TestSignRejectByReviewer(t *testing.T) {
	sdk := newTestSDK(t)
	r := newRecorder()
	ss := sdk.Sign(buildStart(t, nil), signAdapter{r})
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

// TestSignDecisionIdempotent: a second host decision after the first is a safe
// no-op (Approve/Reject guard).
func TestSignDecisionIdempotent(t *testing.T) {
	sdk := newTestSDK(t)
	r := newRecorder()
	ss := sdk.Sign(buildStart(t, nil), signAdapter{r})
	r.waitDecoded(t)
	ss.Reject()
	ss.Approve() // ignored: already decided
	ss.Reject()
	r.wait(t)
	if code, _, _, _ := r.result(); code != CodeRejected {
		t.Fatalf("code=%q want %q", code, CodeRejected)
	}
}
