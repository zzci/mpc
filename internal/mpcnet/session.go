package mpcnet

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/bnb-chain/tss-lib/v3/tss"
	"github.com/libp2p/go-libp2p/core/peer"

	"github.com/zzci/mpc/internal/contract"
	"github.com/zzci/mpc/internal/mpc"
)

// Pump loops that wire one mpc.* single-party / reshare party to a Transport.
// The wire layout (MpcMessage envelope; 'O' / 'N' / 'B' committee marker for
// resharing) is bit-identical to internal/cli/mpcnet.go — the cli E2E carrier
// stays untouched (distributed-mpc-impl.md §B DM-2: COPY + GENERALIZE).
//
// senderAuth + sessionId isolation + protocol-version negotiation are enforced
// by the transport layer (contract.AcceptInbound / contract.SignSenderAuth on
// the Session prepare path; protocol.md §2.bis,86); the engine sees only the
// gated, verified Inbound stream and never re-checks identity on top.

const (
	// sendAttempts retries a directed send a few times: a freshly established
	// circuit-relay stream can briefly be unready while libp2p finishes the
	// reservation / hole-punch handshake, and a transient stream-open failure
	// must not abort the whole MPC round. Mirrors internal/cli/mpcnet.go.
	sendAttempts = 8

	// sendAttemptTimeout bounds one stream-open attempt: opening the first
	// stream on a fresh circuit-relay connection can stall while libp2p
	// settles the reservation; without this an unlucky send would block on
	// the whole-protocol context instead of retrying. Mirrors cli/mpcnet.go.
	sendAttemptTimeout = 20 * time.Second
)

// singleParty is the receive seam of a single-party tss session — both
// mpc.KeygenParty and mpc.SignParty satisfy it. It lets the same pump drive
// either protocol without copying transport glue twice.
type singleParty interface {
	Start() error
	Update(tss.ParsedMessage) error
	Out() <-chan tss.Message
	PartyID() *tss.PartyID
	Peers() tss.SortedPartyIDs
}

// sendWithRetry retries a directed MpcMessage send with a short exponential
// backoff. The per-attempt deadline keeps a stalled reservation from blocking
// the whole protocol context (see cli/mpcnet.go: same rationale).
func sendWithRetry(ctx context.Context, tr Transport, to peer.ID, mm *contract.MpcMessage) error {
	var err error
	for i := 0; i < sendAttempts; i++ {
		actx, cancel := context.WithTimeout(ctx, sendAttemptTimeout)
		err = tr.SendTo(actx, to, mm)
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

// sendSingle serializes one outbound tss message from a single-party session
// and routes it: every message — broadcast included — is delivered as a
// directed relay-circuit send (one per destination peer) so the zero-trust
// relay path (protocol.md §2) covers broadcasts the same way it covers
// point-to-point messages. self is always excluded.
func sendSingle(ctx context.Context, tr Transport, self string, peers PeerTable, msg tss.Message) error {
	bz, _, err := msg.WireBytes()
	if err != nil {
		return fmt.Errorf("wire bytes: %w", err)
	}
	broadcast := msg.IsBroadcast() || msg.GetTo() == nil
	if broadcast {
		for tag, to := range peers {
			if tag == self {
				continue
			}
			mm := &contract.MpcMessage{From: self, To: []string{tag}, IsBroadcast: true, Payload: bz}
			if err := sendWithRetry(ctx, tr, to, mm); err != nil {
				return fmt.Errorf("broadcast to %q: %w", tag, err)
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
			return fmt.Errorf("no peer for party %q", dst.Id)
		}
		mm := &contract.MpcMessage{From: self, To: []string{dst.Id}, Payload: bz}
		if err := sendWithRetry(ctx, tr, to, mm); err != nil {
			return fmt.Errorf("send to %q: %w", dst.Id, err)
		}
	}
	return nil
}

// applyInboundSingle parses one inbound MpcMessage against the named peer set
// and feeds the parsed message into the local single-party state. Defence in
// depth: unknown senders and malformed payloads are dropped silently — the
// transport gate has already rejected them at the security boundary, this is
// the same posture cli/mpcnet.go takes.
func applyInboundSingle(mm *contract.MpcMessage, self string, pids tss.SortedPartyIDs, party singleParty) {
	if mm == nil || mm.From == self {
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
		return
	}
	parsed, err := tss.ParseWireMessage(mm.Payload, from, mm.IsBroadcast)
	if err != nil {
		return
	}
	_ = party.Update(parsed) // mpc party returns wrapped error already; transport gate keeps malice out
}

// pumpSingle drives one keygen / signing party to completion against tr: it
// fans the party's outbound messages onto the transport and feeds verified
// inbound messages back. The caller invokes party.Done() to collect the
// result; pumpSingle returns when ctx is cancelled or a fatal protocol error
// surfaces from the underlying tss state.
//
// Lifecycle: pumpSingle launches a single inbound drain goroutine plus a
// caller-blocking outbound loop. When done() finishes (signalled via stop),
// pumpSingle drains any residual Out() messages (a tss party emits its final-
// round messages to outCh *before* its end signal) and waits for in-flight
// sends to flush. Mirrors cli/mpcnet.go pumpOne's E2E-tested invariants.
func pumpSingle(ctx context.Context, tr Transport, party singleParty, peers PeerTable, stop <-chan struct{}) error {
	self := party.PartyID().Id
	pids := party.Peers()

	if err := party.Start(); err != nil {
		return fmt.Errorf("start party: %w", err)
	}

	var sendWG sync.WaitGroup
	rerr := make(chan error, 1)
	dispatch := func(m tss.Message) {
		sendWG.Add(1)
		go func() {
			defer sendWG.Done()
			if err := sendSingle(ctx, tr, self, peers, m); err != nil {
				select {
				case rerr <- err:
				default:
				}
			}
		}()
	}

	// inbound drain runs until ctx is done — the transport's Inbound channel
	// stays open for the session's lifetime; the engine, not the pump, owns
	// session lifecycle.
	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			case in, ok := <-tr.Inbound():
				if !ok {
					return
				}
				applyInboundSingle(in.Msg, self, pids, party)
			}
		}
	}()

	for {
		select {
		case <-ctx.Done():
			sendWG.Wait()
			return fmt.Errorf("protocol cancelled: %w", ctx.Err())
		case err := <-rerr:
			sendWG.Wait()
			return err
		case msg := <-party.Out():
			dispatch(msg)
		case <-stop:
			// Drain any residual outbound emitted before the party's end
			// signal, then wait for every dispatched send to flush.
			drainResidual(party.Out(), dispatch)
			flushed := make(chan struct{})
			go func() { sendWG.Wait(); close(flushed) }()
			select {
			case <-flushed:
			case <-ctx.Done():
			}
			// Surface a send error observed during the final drain so the
			// caller doesn't ship a result while the peer is starved.
			select {
			case err := <-rerr:
				return err
			default:
				return nil
			}
		}
	}
}

// drainResidual sweeps any messages already buffered in out and dispatches
// each via the same send path. The sweep is non-blocking: when out is empty
// the function returns immediately.
func drainResidual(out <-chan tss.Message, dispatch func(tss.Message)) {
	for {
		select {
		case m := <-out:
			dispatch(m)
		default:
			return
		}
	}
}

// --- Reshare pump ---

// committeeMarker / committeeTargets carry the resharing destination committee
// across the wire (one leading byte on the To tag): 'O' old only, 'N' new
// only, 'B' both. tss.ParseWireMessage drops the routing flags on the receive
// side, so the carrier rebuilds them from this out-of-band marker.
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

func partyTagFor(index int) string { return fmt.Sprintf("%d", index+1) }

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

// reshareDispatch carries the inputs routeReshareOut needs without dragging in
// the larger mpc.ReshareParty type, so the wire-routing helpers stay testable
// in isolation if needed.
type reshareDispatch struct {
	tr      Transport
	peers   PeerTable
	oldSelf *tss.PartyID
	newSelf *tss.PartyID
	oTag    string // wire-from prefix for THIS device's old-committee party
	nTag    string // wire-from prefix for THIS device's new-committee party
	selfIdx int
	n       int
	apply   func(parsed tss.ParsedMessage, toOld, toNew bool)
}

// routeReshareOut delivers one outbound resharing message. Same-device cross-
// committee copies are applied in-process (no peer to dial); every other
// destination device gets a committee-tagged directed relay-circuit stream.
// It runs in its own goroutine; failures go to rerr, never a return value.
// Mirrors cli/mpcnet.go routeReshareOut.
func (rd reshareDispatch) route(ctx context.Context, msg tss.Message, rerr chan<- error) {
	bz, _, err := msg.WireBytes()
	if err != nil {
		select {
		case rerr <- fmt.Errorf("wire bytes: %w", err):
		default:
		}
		return
	}
	from := msg.GetFrom()
	wireFrom := rd.nTag
	if samePID(from, rd.oldSelf) {
		wireFrom = rd.oTag
	}
	isB := msg.IsBroadcast() || msg.GetTo() == nil

	// The destination committee is read from the OUTBOUND message (its flags
	// are valid here) and carried as a one-char To marker, because the wire
	// form loses it (see applyInboundReshare).
	toOld := msg.IsToOldCommittee() || msg.IsToOldAndNewCommittees()
	toNew := !msg.IsToOldCommittee() || msg.IsToOldAndNewCommittees()
	marker := committeeMarker(toOld, toNew)

	dests := map[int]bool{}
	if isB {
		for i := 0; i < rd.n; i++ {
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
		if d == rd.selfIdx {
			// Same device, other committee: deliver in-process; libp2p can't
			// dial self, and the sender committee's PartyID is this device's
			// own oldSelf / newSelf.
			selfFrom := rd.newSelf
			if wireFrom == rd.oTag {
				selfFrom = rd.oldSelf
			}
			parsed, perr := tss.ParseWireMessage(bz, selfFrom, isB)
			if perr != nil {
				select {
				case rerr <- fmt.Errorf("reshare self-parse: %w", perr):
				default:
				}
				return
			}
			rd.apply(parsed, toOld, toNew)
			continue
		}
		to, ok := rd.peers[partyTagFor(d)]
		if !ok {
			continue
		}
		mm := &contract.MpcMessage{
			From: wireFrom, To: []string{marker + partyTagFor(d)},
			IsBroadcast: isB, Payload: bz,
		}
		if serr := sendWithRetry(ctx, rd.tr, to, mm); serr != nil {
			select {
			case rerr <- fmt.Errorf("reshare send to %d: %w", d, serr):
			default:
			}
			return
		}
	}
}

// applyInboundReshare parses one inbound reshare MpcMessage and dispatches the
// parsed result to the correct local committee parties. Mirrors the inbound
// half of cli/mpcnet.go pumpReshare.
func applyInboundReshare(
	mm *contract.MpcMessage,
	oldPIDs, newPIDs tss.SortedPartyIDs,
	apply func(parsed tss.ParsedMessage, toOld, toNew bool),
) {
	if mm == nil || len(mm.From) < 2 || len(mm.To) == 0 || mm.To[0] == "" {
		return
	}
	var from *tss.PartyID
	switch mm.From[0] {
	case 'O':
		from = byTag(oldPIDs, mm.From[1:])
	case 'N':
		from = byTag(newPIDs, mm.From[1:])
	default:
		return
	}
	if from == nil {
		return
	}
	toOld, toNew := committeeTargets(mm.To[0][0])
	parsed, perr := tss.ParseWireMessage(mm.Payload, from, mm.IsBroadcast)
	if perr != nil {
		return
	}
	apply(parsed, toOld, toNew)
}

// pumpReshare drives both local resharing committee parties to completion. It
// keeps draining party.Out() until party.Done(ctx) returns (= both ended), so
// the final-round messages every peer needs to finalize are sent before the
// device closes its transport. Mirrors cli/mpcnet.go pumpReshare's E2E-tested
// invariants.
//
// The boundary between cli/mpcnet.go and this version is the seam: this
// version consumes mpc.ReshareParty's Out / UpdateOld / UpdateNew API instead
// of two raw tss.Party handles, and the master-pubkey invariant assertion
// lives in party.Done (DM-1, custody invariant from sdk.md §7).
func pumpReshare(ctx context.Context, tr Transport, party *mpc.ReshareParty, peers PeerTable, selfIdx, n int, stop <-chan struct{}) error {
	if err := party.Start(); err != nil {
		return fmt.Errorf("start reshare party: %w", err)
	}

	oTag := "O" + partyTagFor(selfIdx)
	nTag := "N" + partyTagFor(selfIdx)
	oldPIDs := party.OldPeers()
	newPIDs := party.NewPeers()
	oldSelf := party.OldPartyID()
	newSelf := party.NewPartyID()

	// applyTo feeds a parsed message into the explicitly addressed local
	// committee parties. Each Update runs in its own goroutine: tss-lib
	// parties are safe under concurrent Update (mirrors internal/mpc's
	// updateParty fan-out) and an Update that emits new outbound must never
	// block the outCh drain (that self-deadlock stalled an earlier
	// synchronous design — cli/mpcnet.go comment).
	applyTo := func(parsed tss.ParsedMessage, toOld, toNew bool) {
		if toOld {
			go func() { _ = party.UpdateOld(parsed) }()
		}
		if toNew {
			go func() { _ = party.UpdateNew(parsed) }()
		}
	}

	rd := reshareDispatch{
		tr: tr, peers: peers,
		oldSelf: oldSelf, newSelf: newSelf,
		oTag: oTag, nTag: nTag,
		selfIdx: selfIdx, n: n,
		apply: applyTo,
	}

	var sendWG sync.WaitGroup
	rerr := make(chan error, 1)
	dispatch := func(m tss.Message) {
		sendWG.Add(1)
		go func() {
			defer sendWG.Done()
			rd.route(ctx, m, rerr)
		}()
	}

	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			case in, ok := <-tr.Inbound():
				if !ok {
					return
				}
				applyInboundReshare(in.Msg, oldPIDs, newPIDs, applyTo)
			}
		}
	}()

	for {
		select {
		case <-ctx.Done():
			sendWG.Wait()
			return fmt.Errorf("resharing cancelled: %w", ctx.Err())
		case err := <-rerr:
			sendWG.Wait()
			return err
		case msg := <-party.Out():
			dispatch(msg)
		case <-stop:
			drainResidual(party.Out(), dispatch)
			flushed := make(chan struct{})
			go func() { sendWG.Wait(); close(flushed) }()
			select {
			case <-flushed:
			case <-ctx.Done():
			}
			select {
			case err := <-rerr:
				return err
			default:
				return nil
			}
		}
	}
}
