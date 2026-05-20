package cli

import (
	"bytes"
	"context"
	"fmt"
	"time"

	"github.com/zzci/mpc/internal/contract"
	"github.com/zzci/mpc/internal/mpc"
	"github.com/zzci/mpc/internal/transport"
)

// AD-2 network driver for the post-DKG chaincode commit-reveal protocol
// (docs/design/mcp/address-derivation.md §3). The cryptographic primitives
// live in internal/mpc/chaincode.go; this file is the transport seam — it
// rides existing contract.MpcMessage envelopes over a transport.Session so
// session isolation (sessionId) and senderAuth bind the commit-reveal exchange
// to the same C-001/M-005 path the real DKG uses. No new wire type, no new
// libp2p protocol, no new gossip topic (§3 "transport: 复用既有 libp2p Noise +
// circuit-relay v2 + rendezvous; 禁新增传输").
//
// MpcMessage.Round is repurposed as the commit-reveal phase discriminator (1
// = commit, 2 = reveal). Because senderAuth signs (sessionId, round, payload),
// a commit signature cannot be replayed as a reveal — the protocol's own
// hard-stop on bad signatures keeps a Byzantine peer from cross-phase abuse.

const (
	// chaincodeRoundCommit / chaincodeRoundReveal are the phase markers
	// stamped into MpcMessage.Round. Stable across this build's lifetime;
	// part of the on-wire layout, do not renumber.
	chaincodeRoundCommit uint32 = 1
	chaincodeRoundReveal uint32 = 2

	// chaincodeBarrierTimeout bounds collection of a single commit-reveal
	// phase. It is intentionally separate from the keygen timeout: post-DKG
	// the parties have already established relay-circuits, so the two
	// broadcasts that constitute commit-reveal complete in seconds; a peer
	// that cannot ship 32 bytes in two minutes is gone.
	chaincodeBarrierTimeout = 2 * time.Minute
)

// runChaincodeCommitReveal runs §3 steps 1-4 over sess: every device
// broadcasts C_j, then r_j, verifies all (C_j, r_j) pairs, and derives the
// shared 32-byte chaincode c. Strict abort on any failure (§3 step 5): no
// partial success, no retry within a session — a fresh group_id is required
// to retry.
//
// peers is the per-tag peer.ID table the keygen run just exercised (reused
// verbatim, no rendezvous re-publish). index is THIS device's 0-based
// position; n is the party count; groupID is the same identifier carried
// through DKG (bound into both the commitment preimage and the HKDF salt for
// cross-group-replay defence).
func runChaincodeCommitReveal(
	ctx context.Context, sess *transport.Session, peers peerTable,
	groupID string, index, n int,
) ([]byte, error) {
	if sess == nil {
		return nil, fmt.Errorf("cli: chaincode: nil session")
	}
	if n < 2 {
		return nil, fmt.Errorf("cli: chaincode: need >=2 parties, got %d", n)
	}
	if index < 0 || index >= n {
		return nil, fmt.Errorf("cli: chaincode: self index %d out of [0,%d)", index, n)
	}

	selfTag := partyTag(index)
	selfIdx1 := uint32(index + 1)

	// 1. Local randomness + commitment (§3 step 1).
	rSelf, err := mpc.GenerateChaincodeRandomness()
	if err != nil {
		return nil, fmt.Errorf("cli: chaincode rand: %w", err)
	}
	cSelf, err := mpc.ChaincodeCommit(groupID, selfIdx1, rSelf)
	if err != nil {
		return nil, fmt.Errorf("cli: chaincode commit: %w", err)
	}

	// 2. Broadcast own commitment to every other party (§3 step 1).
	if err := chaincodeBroadcast(ctx, sess, selfTag, peers, chaincodeRoundCommit, cSelf); err != nil {
		return nil, fmt.Errorf("cli: chaincode commit-broadcast: %w", err)
	}

	// 3. Collect commitments from all OTHER parties before revealing
	//    anything (§3 step 1 → step 2 ordering: a late party seeing peers'
	//    reveals cannot adapt its own once its commitment is locked, but
	//    only if every honest party also waits for all commitments first).
	//    Phase-skew defence (β2 fix, L1 ruling 2026-05-20): in docker / WAN
	//    deployments a peer that finished its own commits-collect may race
	//    ahead and ship its reveal before this party finishes collecting
	//    commits; the inbound channel is single-shot per message, so a
	//    reveal observed during commits-collect MUST be buffered into
	//    `pending` instead of dropped, otherwise reveals-collect will hang
	//    until barrier timeout (root cause of E2E-002 "2/3 reveals" stalls).
	commits := make(map[string][]byte, n)
	commits[selfTag] = cSelf
	pending := make(map[uint32]map[string][]byte) // round → from → payload
	if err := chaincodeCollect(ctx, sess, n, chaincodeRoundCommit, selfTag, mpc.ChaincodeCommitLen, commits, pending); err != nil {
		return nil, fmt.Errorf("cli: chaincode collect commits: %w", err)
	}

	// 4. Broadcast own reveal (§3 step 2).
	if err := chaincodeBroadcast(ctx, sess, selfTag, peers, chaincodeRoundReveal, rSelf); err != nil {
		return nil, fmt.Errorf("cli: chaincode reveal-broadcast: %w", err)
	}

	// 5. Collect reveals from all OTHER parties.
	reveals := make(map[string][]byte, n)
	reveals[selfTag] = rSelf
	if err := chaincodeCollect(ctx, sess, n, chaincodeRoundReveal, selfTag, mpc.ChaincodeRandLen, reveals, pending); err != nil {
		return nil, fmt.Errorf("cli: chaincode collect reveals: %w", err)
	}

	// 6. Verify every (C_j, r_j) pair (§3 step 3). Any mismatch ⇒ strict
	//    abort: §3 step 5 forbids partial success — the caller must retry
	//    with a fresh group_id, never within this session.
	orderedR := make([][]byte, n)
	for i := 0; i < n; i++ {
		tag := partyTag(i)
		rj, ok := reveals[tag]
		if !ok {
			return nil, fmt.Errorf("cli: chaincode missing reveal from party %s", tag)
		}
		cj, ok := commits[tag]
		if !ok {
			return nil, fmt.Errorf("cli: chaincode missing commit from party %s", tag)
		}
		if err := mpc.VerifyChaincodeCommit(groupID, uint32(i+1), rj, cj); err != nil {
			return nil, fmt.Errorf("cli: chaincode verify party %s: %w", tag, err)
		}
		orderedR[i] = rj
	}

	// 7. Derive c (§3 step 4) — deterministic for everyone who applied the
	//    same verified r_1..r_n in the same 1..n order.
	c, err := mpc.DeriveChaincode(groupID, orderedR)
	if err != nil {
		return nil, fmt.Errorf("cli: chaincode derive: %w", err)
	}
	return c, nil
}

// chaincodeBroadcast fans one phase payload out to every other party,
// reusing the directed-relay-circuit send path runKeygen exercises. Same
// retry/backoff: a freshly established circuit-relay stream is briefly
// unready while the reservation/hole-punch handshake finishes, and a
// transient open-stream failure must not abort the whole protocol.
func chaincodeBroadcast(
	ctx context.Context, sess *transport.Session, self string,
	peers peerTable, round uint32, payload []byte,
) error {
	for tag, to := range peers {
		if tag == self {
			continue
		}
		mm := &contract.MpcMessage{
			From:        self,
			To:          []string{tag},
			IsBroadcast: true,
			Round:       round,
			Payload:     append([]byte(nil), payload...),
		}
		if err := sendWithRetry(ctx, sess, to, mm); err != nil {
			return fmt.Errorf("send round %d to %s: %w", round, tag, err)
		}
	}
	return nil
}

// chaincodeCollect drains sess.Inbound until every other party has shipped a
// well-formed payload for the named round. The map MUST already contain
// self's own contribution (so len(map) == n means done).
//
// Defences in depth on the receive side:
//   - buffer messages from a DIFFERENT phase (Round != expected): they
//     belong to a future phase a faster peer raced ahead into; the inbound
//     channel is single-shot per message, so dropping a future-phase
//     message would hang the next collect call until barrier timeout (β2
//     fix, L1 ruling 2026-05-20 — root cause of E2E-002 docker
//     "round 2 have 2/3" stalls under relay timing skew). Buffer to
//     `pending[round][from]` and reuse on next chaincodeCollect call.
//   - drop messages from an unknown party tag: defence-in-depth (the
//     transport's senderAuth already rejects unknown members, this is the
//     same posture applyInbound takes for keygen / signing wire messages)
//   - reject equivocation: a second, DIFFERENT payload from the same party
//     in the same round is a Byzantine attempt to fork the transcript →
//     strict abort (§3 step 5 abort condition)
//   - reject malformed length: 32 bytes is the schema for both commit and
//     reveal payloads; an off-length value cannot be folded silently
//
// `pending` is the cross-phase carryover buffer shared by sequential
// chaincodeCollect calls: a reveal observed mid-commit-collect lands in
// pending[chaincodeRoundReveal][from] and is replayed when the next call
// asks for reveals. Caller MUST pass the SAME pending map across the two
// rounds of one commit-reveal session.
func chaincodeCollect(
	ctx context.Context, sess *transport.Session, n int,
	round uint32, selfTag string, wantLen int,
	got map[string][]byte,
	pending map[uint32]map[string][]byte,
) error {
	// Drain any messages buffered during a prior phase for this round.
	if buf, ok := pending[round]; ok {
		for from, payload := range buf {
			if from == selfTag {
				continue
			}
			if _, dup := got[from]; dup {
				continue
			}
			got[from] = payload
		}
		delete(pending, round)
	}

	timer := time.NewTimer(chaincodeBarrierTimeout)
	defer timer.Stop()
	for len(got) < n {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-timer.C:
			return fmt.Errorf("round %d timed out (have %d/%d)", round, len(got), n)
		case in, ok := <-sess.Inbound():
			if !ok {
				return fmt.Errorf("round %d: session closed", round)
			}
			mm := in.Msg
			if mm == nil || mm.From == selfTag {
				continue
			}
			idx := tagIndex(mm.From)
			if idx < 0 || idx >= n {
				continue // unknown party — drop like applyInbound does
			}
			if len(mm.Payload) != wantLen {
				return fmt.Errorf("round %d: party %s sent %d bytes, want %d",
					round, mm.From, len(mm.Payload), wantLen)
			}
			if mm.Round != round {
				// Future-phase message from a faster peer — buffer for
				// the next chaincodeCollect call instead of dropping.
				buf, ok := pending[mm.Round]
				if !ok {
					buf = make(map[string][]byte)
					pending[mm.Round] = buf
				}
				if prev, exists := buf[mm.From]; exists {
					if !bytes.Equal(prev, mm.Payload) {
						return fmt.Errorf("round %d: party %s equivocated in buffered round %d",
							round, mm.From, mm.Round)
					}
					continue
				}
				buf[mm.From] = append([]byte(nil), mm.Payload...)
				continue
			}
			if prev, exists := got[mm.From]; exists {
				if !bytes.Equal(prev, mm.Payload) {
					return fmt.Errorf("round %d: party %s equivocated (two payloads)", round, mm.From)
				}
				continue // honest libp2p reshipment of the same payload
			}
			got[mm.From] = append([]byte(nil), mm.Payload...)
		}
	}
	return nil
}
