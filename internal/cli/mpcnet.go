package cli

import (
	"context"
	"fmt"
	"math/big"
	"sync"
	"time"

	"github.com/libp2p/go-libp2p/core/peer"

	"github.com/bnb-chain/tss-lib/v3/common"
	"github.com/bnb-chain/tss-lib/v3/ecdsa/keygen"
	"github.com/bnb-chain/tss-lib/v3/ecdsa/resharing"
	"github.com/bnb-chain/tss-lib/v3/ecdsa/signing"
	"github.com/bnb-chain/tss-lib/v3/tss"

	"github.com/royqta/mcp-wallet/internal/contract"
	"github.com/royqta/mcp-wallet/internal/mpc"
	"github.com/royqta/mcp-wallet/internal/transport"
)

// Network MPC driver: it drives real tss-lib keygen / signing / resharing
// parties whose only transport is internal/transport (libp2p Noise + pnet +
// circuit-relay v2), i.e. the network analogue of internal/mpc's in-process
// channel pump (whose generic runner is unexported, so the carrier rebuilds
// the minimal pump it needs). tss WireBytes ride inside contract.MpcMessage,
// so session isolation / senderAuth / version are enforced by the same C-001 +
// M-005 path a production device uses; no MPC payload is produced here.

// peerTable maps a device's party tag (PartyID.Id == "1".."n") to its
// libp2p peer, so a directed tss message is delivered to the right device.
type peerTable map[string]peer.ID

// sigResult is the {R,S,V} a signing session yields.
type sigResult struct {
	R [32]byte
	S [32]byte
	V byte
}

// sendOut serializes one outbound tss message and routes it over the session.
//
// Every message — broadcast included — is delivered as a directed relay-circuit
// stream (Session.SendTo), the path the N-002 relay integration test proves
// works over circuit-relay v2. A broadcast is fanned out to each peer with
// IsBroadcast=true preserved (so the receiver still tss-parses it as a
// broadcast). This deliberately does not use GossipSub: its mesh avoids
// limited (relayed) connections, and routing every MPC message through a
// directed circuit stream makes "全程经 relay" hold for the broadcast rounds
// too, not only the point-to-point ones. self is always excluded.
func sendOut(ctx context.Context, sess *transport.Session, self string, peers peerTable, msg tss.Message) error {
	bz, _, err := msg.WireBytes()
	if err != nil {
		return fmt.Errorf("cli: wire bytes: %w", err)
	}
	broadcast := msg.IsBroadcast() || msg.GetTo() == nil
	if broadcast {
		for tag, to := range peers {
			if tag == self {
				continue
			}
			mm := &contract.MpcMessage{From: self, To: []string{tag}, IsBroadcast: true, Payload: bz}
			if err := sendWithRetry(ctx, sess, to, mm); err != nil {
				return fmt.Errorf("cli: broadcast to %q: %w", tag, err)
			}
		}
		return nil
	}
	for _, dst := range msg.GetTo() {
		if dst.Id == self {
			continue
		}
		to, ok := peers[dst.Id]
		if !ok {
			return fmt.Errorf("cli: no peer for party %q", dst.Id)
		}
		mm := &contract.MpcMessage{From: self, To: []string{dst.Id}, Payload: bz}
		if err := sendWithRetry(ctx, sess, to, mm); err != nil {
			return fmt.Errorf("cli: send to %q: %w", dst.Id, err)
		}
	}
	return nil
}

// sendWithRetry retries a directed send a few times: a freshly established
// circuit-relay stream can briefly be unready while libp2p finishes the
// reservation/hole-punch handshake, and a transient stream-open failure must
// not abort the whole MPC round.
func sendWithRetry(ctx context.Context, sess *transport.Session, to peer.ID, mm *contract.MpcMessage) error {
	const attempts = 8
	var err error
	for i := 0; i < attempts; i++ {
		// Per-attempt deadline: opening the first stream on a fresh
		// circuit-relay connection can stall while libp2p settles the
		// reservation; without this an unlucky send would block on the
		// whole-protocol context instead of retrying.
		actx, cancel := context.WithTimeout(ctx, 20*time.Second)
		err = sess.SendTo(actx, to, mm)
		cancel()
		if err == nil {
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(time.Duration(250*(i+1)) * time.Millisecond):
		}
	}
	return err
}

// applyInbound parses one inbound MpcMessage back into a tss message and feeds
// it to whichever local party the sender addressed (selectParty resolves the
// old/new committee for resharing; a single-party protocol ignores the flags).
func applyInbound(
	in transport.Inbound, self string, pids tss.SortedPartyIDs,
	selectParty func(broadcastToOld, toOldOnly bool) []tss.Party,
	errCh chan<- *tss.Error,
) {
	mm := in.Msg
	if mm.From == self {
		return
	}
	var from *tss.PartyID
	for _, p := range pids {
		if p.Id == mm.From {
			from = p
			break
		}
	}
	if from == nil {
		return // unknown sender for this session: drop (defence in depth)
	}
	parsed, err := tss.ParseWireMessage(mm.Payload, from, mm.IsBroadcast)
	if err != nil {
		return // malformed payload: drop, never crash the device
	}
	toOldOnly := parsed.IsToOldCommittee()
	toBoth := parsed.IsToOldAndNewCommittees()
	for _, party := range selectParty(toBoth, toOldOnly) {
		if party.PartyID().Id == from.Id {
			continue
		}
		if _, uerr := party.Update(parsed); uerr != nil {
			errCh <- uerr
			return
		}
	}
}

// runKeygen runs this device's keygen party (index/n/t) over the session and
// returns its serialized share. preParams are this device's locally generated
// Paillier/safe-prime params (custody invariant: never server-supplied).
func runKeygen(
	ctx context.Context, sess *transport.Session, peers peerTable,
	index, n, threshold int, pre *keygen.LocalPreParams,
) (mpc.Share, error) {
	pids := keygenParties(n)
	self, err := findSelf(pids, partyTag(index))
	if err != nil {
		return mpc.Share{}, err
	}
	params := tss.NewParameters(tss.S256(), tss.NewPeerContext(pids), self, n, threshold)
	// Networked keygen: Paillier modulus/factor ZK proofs run by default and
	// may be skipped ONLY under the explicit non-production marker (security.md
	// invariant #10, RA-001 P1-1). Production/release/CI keep the proofs ON —
	// they are GG18/GG20's core defence against a malicious member's crafted
	// Paillier key. tss-lib's no-proof mode stays a dev/test-only fast path so
	// the E2E carrier can finish inside the relay's circuit-v2 Duration cap
	// (now keygen-aware; see internal/server/relay limits). Fail-closed: with
	// no marker the proofs cannot be off.
	applyKeygenProofPolicy(params)
	outCh := make(chan tss.Message, 4*n)
	endCh := make(chan *keygen.LocalPartySaveData, 1)
	party := keygen.NewLocalParty(params, outCh, endCh, *pre)

	sd, err := pumpOne(ctx, party, self.Id, pids, sess, peers, outCh, endCh)
	if err != nil {
		return mpc.Share{}, err
	}
	bz, err := mpc.MarshalSaveData(sd)
	if err != nil {
		return mpc.Share{}, fmt.Errorf("cli: marshal share: %w", err)
	}
	return mpc.Share{Moniker: self.Id, SaveData: bz}, nil
}

// runSign runs one threshold-signing session: the participating devices sign
// the 32-byte digest and every signer returns the same low-S {R,S,V}.
func runSign(
	ctx context.Context, sess *transport.Session, peers peerTable,
	index, threshold int, participants []int, share mpc.Share, digest []byte,
) (sigResult, error) {
	sd, err := mpc.UnmarshalSaveData(share.SaveData)
	if err != nil {
		return sigResult{}, fmt.Errorf("cli: load share: %w", err)
	}
	pids, err := signingParties(sd, participants)
	if err != nil {
		return sigResult{}, err
	}
	self, err := findSelf(pids, partyTag(index))
	if err != nil {
		return sigResult{}, err
	}
	params := tss.NewParameters(tss.S256(), tss.NewPeerContext(pids), self, len(pids), threshold)
	outCh := make(chan tss.Message, len(pids))
	endCh := make(chan *common.SignatureData, 1)
	party := signing.NewLocalParty(new(big.Int).SetBytes(digest), params, *sd, outCh, endCh, len(digest))

	res, err := pumpOne(ctx, party, self.Id, pids, sess, peers, outCh, endCh)
	if err != nil {
		return sigResult{}, err
	}
	if len(res.SignatureRecovery) == 0 {
		return sigResult{}, fmt.Errorf("cli: signing produced no recovery id")
	}
	var out sigResult
	copy(out.R[:], res.R)
	copy(out.S[:], res.S)
	out.V = res.SignatureRecovery[0]
	return out, nil
}

// runReshare runs this device as both an old-committee and a new-committee
// party (all n devices participate in both committees, t -> t' unchanged here)
// so the wallet master public key is preserved while shares are refreshed.
func runReshare(
	ctx context.Context, sess *transport.Session, peers peerTable,
	index, n, oldT, newT int, share mpc.Share, pre *keygen.LocalPreParams,
) (mpc.Share, error) {
	sd, err := mpc.UnmarshalSaveData(share.SaveData)
	if err != nil {
		return mpc.Share{}, fmt.Errorf("cli: load share: %w", err)
	}
	oldPIDs, err := oldReshareParties(sd)
	if err != nil {
		return mpc.Share{}, err
	}
	newPIDs := newReshareParties(n)
	oldSelf, err := findSelf(oldPIDs, partyTag(index))
	if err != nil {
		return mpc.Share{}, err
	}
	newSelf, err := findSelf(newPIDs, partyTag(index))
	if err != nil {
		return mpc.Share{}, err
	}
	oldCtx := tss.NewPeerContext(oldPIDs)
	newCtx := tss.NewPeerContext(newPIDs)

	outCh := make(chan tss.Message, 2*n)
	endCh := make(chan *keygen.LocalPartySaveData, 2)

	oldParams := tss.NewReSharingParameters(tss.S256(), oldCtx, newCtx, oldSelf, len(oldPIDs), oldT, n, newT)
	oldParty := resharing.NewLocalParty(oldParams, *sd, outCh, endCh)

	newParams := tss.NewReSharingParameters(tss.S256(), oldCtx, newCtx, newSelf, len(oldPIDs), oldT, n, newT)
	// Networked resharing: same Paillier-proof guardrail as keygen — proofs ON
	// by default, no-proof only under the explicit non-production marker
	// (security.md invariant #10, RA-001 P1-1; fail-closed).
	applyKeygenProofPolicy(newParams.Parameters)
	save := keygen.NewLocalPartySaveData(n)
	save.LocalPreParams = *pre
	newParty := resharing.NewLocalParty(newParams, save, outCh, endCh)

	out, err := pumpReshare(ctx, reshareParties{
		oldParty: oldParty, newParty: newParty,
		oldSelf: oldSelf, newSelf: newSelf,
		oldPIDs: oldPIDs, newPIDs: newPIDs,
		selfIdx: index, n: n,
	}, sess, peers, outCh, endCh)
	if err != nil {
		return mpc.Share{}, err
	}
	bz, err := mpc.MarshalSaveData(out)
	if err != nil {
		return mpc.Share{}, fmt.Errorf("cli: marshal reshared share: %w", err)
	}
	return mpc.Share{Moniker: newSelf.Id, SaveData: bz}, nil
}

// pumpOne drives a single-party protocol (keygen/signing) to completion: it
// fans the party's outbound messages onto the session and feeds verified
// inbound messages back, returning the party's end result.
func pumpOne[E any](
	ctx context.Context, party tss.Party, self string, pids tss.SortedPartyIDs,
	sess *transport.Session, peers peerTable,
	outCh <-chan tss.Message, endCh <-chan E,
) (E, error) {
	var zero E
	errCh := make(chan *tss.Error, 1)
	go func() {
		if err := party.Start(); err != nil {
			errCh <- err
		}
	}()
	sel := func(_, _ bool) []tss.Party { return []tss.Party{party} }
	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			case in, ok := <-sess.Inbound():
				if !ok {
					return
				}
				applyInbound(in, self, pids, sel, errCh)
			}
		}
	}()
	for {
		select {
		case <-ctx.Done():
			return zero, fmt.Errorf("cli: protocol cancelled: %w", ctx.Err())
		case err := <-errCh:
			return zero, fmt.Errorf("cli: tss error: %w", err)
		case msg := <-outCh:
			if err := sendOut(ctx, sess, self, peers, msg); err != nil {
				return zero, err
			}
		case res := <-endCh:
			return res, nil
		}
	}
}

// reshareParties is this device's resharing state: it runs an old-committee
// and a new-committee party concurrently. The committees share device tags but
// have distinct tss keys (old = keygen ShareIDs 1..n, new = n+1..2n), so a
// plain tag is ambiguous on the wire — every reshare MpcMessage.From is
// prefixed 'O'/'N' to name the sender's committee, and the receiver rebuilds
// the correct-committee PartyID before tss-parsing. Same-device cross-committee
// messages (old_i -> new_i) are delivered in-process, never over the network.
type reshareParties struct {
	oldParty, newParty tss.Party
	oldSelf, newSelf   *tss.PartyID
	oldPIDs, newPIDs   tss.SortedPartyIDs
	selfIdx, n         int
}

func samePID(a, b *tss.PartyID) bool {
	return a != nil && b != nil && a.KeyInt().Cmp(b.KeyInt()) == 0
}

func byTag(pids tss.SortedPartyIDs, tag string) *tss.PartyID {
	for _, p := range pids {
		if p.Id == tag {
			return p
		}
	}
	return nil
}

// pumpReshare drives both local resharing parties. Outbound messages are
// committee-tagged and split into local (same device, other committee) and
// remote (relay-circuit) delivery; inbound messages are rebuilt against the
// sender's committee and applied to the old party, the new party, or both per
// the tss committee flags. Both committees emit an end result; the new
// committee's (Xi != nil) is the refreshed share returned.
func pumpReshare(
	ctx context.Context, rp reshareParties,
	sess *transport.Session, peers peerTable,
	outCh <-chan tss.Message, endCh <-chan *keygen.LocalPartySaveData,
) (*keygen.LocalPartySaveData, error) {
	errCh := make(chan *tss.Error, 256)
	rerr := make(chan error, 256) // transport/routing errors (plain error)
	var sendWG sync.WaitGroup     // tracks every in-flight outbound send
	go func() {
		if err := rp.newParty.Start(); err != nil {
			errCh <- err
		}
	}()
	go func() {
		if err := rp.oldParty.Start(); err != nil {
			errCh <- err
		}
	}()

	oTag, nTag := "O"+partyTag(rp.selfIdx), "N"+partyTag(rp.selfIdx)

	resolveFrom := func(w string) *tss.PartyID {
		if len(w) < 2 {
			return nil
		}
		switch w[0] {
		case 'O':
			return byTag(rp.oldPIDs, w[1:])
		case 'N':
			return byTag(rp.newPIDs, w[1:])
		default:
			return nil
		}
	}

	// applyTo feeds a parsed message to the explicitly addressed local
	// committee parties. The committee target is passed in, NOT read from
	// parsed: tss.ParseWireMessage only restores From+IsBroadcast, so a parsed
	// message's IsToOldCommittee/IsToOldAndNewCommittees are always false — the
	// committee routing must be carried on the wire (the To marker) instead.
	// Each Update runs in its own goroutine: tss-lib parties are safe under
	// concurrent Update (mirrors internal/mpc's updateParty fan-out) and an
	// Update that emits new outbound must never block the outCh drain (that
	// self-deadlock stalled the earlier synchronous design).
	applyTo := func(parsed tss.ParsedMessage, toOld, toNew bool) {
		from := parsed.GetFrom()
		if toOld && !samePID(from, rp.oldSelf) {
			go func() {
				if _, uerr := rp.oldParty.Update(parsed); uerr != nil {
					errCh <- uerr
				}
			}()
		}
		if toNew && !samePID(from, rp.newSelf) {
			go func() {
				if _, uerr := rp.newParty.Update(parsed); uerr != nil {
					errCh <- uerr
				}
			}()
		}
	}

	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			case in, ok := <-sess.Inbound():
				if !ok {
					return
				}
				from := resolveFrom(in.Msg.From)
				if from == nil || len(in.Msg.To) == 0 || in.Msg.To[0] == "" {
					continue
				}
				toOld, toNew := committeeTargets(in.Msg.To[0][0])
				parsed, perr := tss.ParseWireMessage(in.Msg.Payload, from, in.Msg.IsBroadcast)
				if perr != nil {
					continue
				}
				applyTo(parsed, toOld, toNew)
			}
		}
	}()

	// Keep draining outCh until BOTH local parties (old + new) have ended,
	// exactly like tss-lib's own runReshareProtocol (loop to ended==total).
	// A tss party emits its final-round messages to outCh *before* it ends;
	// returning on the first new-committee save (as the earlier version did)
	// abandons those still-unsent messages, starving whichever peer finalizes
	// last — that was the device-1 reshare hang under -race scheduling.
	// dispatch sends one outbound message in its own goroutine (so the drain
	// loop never blocks on a relay send or a local Update) while sendWG tracks
	// it, so a device can wait for every outbound — including the final round
	// the last peer needs — to flush before it returns and closes transport.
	dispatch := func(m tss.Message) {
		sendWG.Add(1)
		go func() {
			defer sendWG.Done()
			rp.routeReshareOut(ctx, sess, peers, oTag, nTag, m, applyTo, rerr)
		}()
	}

	var newSave *keygen.LocalPartySaveData
	ended := 0
	for {
		select {
		case <-ctx.Done():
			return nil, fmt.Errorf("cli: resharing cancelled: %w", ctx.Err())
		case err := <-errCh:
			return nil, fmt.Errorf("cli: tss resharing error: %w", err)
		case err := <-rerr:
			return nil, err
		case msg := <-outCh:
			dispatch(msg)
		case save := <-endCh:
			if save.Xi != nil {
				newSave = save // new committee's refreshed share
			}
			ended++
			// Mirror tss-lib runReshareProtocol: only finish once BOTH local
			// parties (old + new) have ended — by then the party has emitted
			// its final-round messages to outCh. Drain whatever is still
			// buffered, then wait for every outbound send to actually flush so
			// no peer that finalizes later is starved.
			if ended >= 2 && newSave != nil {
				for draining := true; draining; {
					select {
					case m := <-outCh:
						dispatch(m)
					default:
						draining = false
					}
				}
				flushed := make(chan struct{})
				go func() { sendWG.Wait(); close(flushed) }()
				select {
				case <-flushed:
				case <-ctx.Done():
				}
				return newSave, nil
			}
		}
	}
}

// routeReshareOut delivers one outbound resharing message: same-device
// cross-committee copies are applied in-process (no peer to dial), every other
// destination device gets a committee-tagged directed relay-circuit stream.
// It runs in its own goroutine; failures go to errCh, never a return value.
func (rp reshareParties) routeReshareOut(
	ctx context.Context, sess *transport.Session, peers peerTable,
	oTag, nTag string, msg tss.Message, applyTo func(tss.ParsedMessage, bool, bool),
	rerr chan<- error,
) {
	bz, _, err := msg.WireBytes()
	if err != nil {
		rerr <- fmt.Errorf("cli: wire bytes: %w", err)
		return
	}
	from := msg.GetFrom()
	wireFrom := nTag
	if samePID(from, rp.oldSelf) {
		wireFrom = oTag
	}
	isB := msg.IsBroadcast() || msg.GetTo() == nil

	// The committee target is read from the OUTBOUND message (its flags are
	// valid here) and carried as a one-char To marker, because the wire form
	// loses it (see applyTo).
	toOld := msg.IsToOldCommittee() || msg.IsToOldAndNewCommittees()
	toNew := !msg.IsToOldCommittee() || msg.IsToOldAndNewCommittees()
	marker := committeeMarker(toOld, toNew)

	dests := map[int]bool{}
	if isB {
		for i := 0; i < rp.n; i++ {
			dests[i] = true
		}
	} else {
		for _, d := range msg.GetTo() {
			if idx := tagIndex(d.Id); idx >= 0 {
				dests[idx] = true
			}
		}
	}

	for d := range dests {
		if d == rp.selfIdx {
			// Same device, other committee: deliver in-process; the sender
			// committee's PartyID is this device's own oldSelf/newSelf.
			selfFrom := rp.newSelf
			if wireFrom == oTag {
				selfFrom = rp.oldSelf
			}
			parsed, perr := tss.ParseWireMessage(bz, selfFrom, isB)
			if perr != nil {
				rerr <- fmt.Errorf("cli: reshare self-parse: %w", perr)
				return
			}
			applyTo(parsed, toOld, toNew)
			continue
		}
		to, ok := peers[partyTag(d)]
		if !ok {
			continue // device not in peer table (should not happen for n devices)
		}
		mm := &contract.MpcMessage{
			From: wireFrom, To: []string{marker + partyTag(d)},
			IsBroadcast: isB, Payload: bz,
		}
		if serr := sendWithRetry(ctx, sess, to, mm); serr != nil {
			rerr <- fmt.Errorf("cli: reshare send to %d: %w", d, serr)
			return
		}
	}
}

// committeeMarker / committeeTargets carry the resharing destination committee
// across the wire (one leading byte on the To tag): 'O' old only, 'N' new
// only, 'B' both. This replaces the routing metadata tss.ParseWireMessage
// drops on the receive side.
func committeeMarker(toOld, toNew bool) string {
	switch {
	case toOld && toNew:
		return "B"
	case toOld:
		return "O"
	default:
		return "N"
	}
}

func committeeTargets(marker byte) (toOld, toNew bool) {
	switch marker {
	case 'B':
		return true, true
	case 'O':
		return true, false
	default: // 'N'
		return false, true
	}
}

func tagIndex(tag string) int {
	n := 0
	for _, c := range tag {
		if c < '0' || c > '9' {
			return -1
		}
		n = n*10 + int(c-'0')
	}
	if n <= 0 {
		return -1
	}
	return n - 1
}
