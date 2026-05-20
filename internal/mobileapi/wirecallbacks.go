package mobileapi

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/bnb-chain/tss-lib/v3/tss"

	"github.com/zzci/mpc/internal/contract"
)

// WireCallbacks is the host-supplied outbound bridge (Go→host) for one
// single-party MPC session (distributed-mpc.md §5, distributed-mpc-impl.md §B
// DM-3). Every tss message this device produces is delivered to the host as
// the JSON wire form of a contract.MpcMessage; the host owns transport (mobile
// native bridge or PC CLI libp2p) and is responsible for actually shipping the
// bytes to the addressed peers.
//
// The reverse direction (host→Go) is SDK.OnWireMessage, which feeds bytes the
// host received from the network back into this device's running session
// after the receive-side R5 gate (version + sessionId isolation, sdk.md §3 /
// protocol.md §2.bis).
//
// gomobile constraint: methods carry only flat types ([]byte). The host
// implementation routes by inspecting the MpcMessage.To tag set and resolving
// each tag to its current transport endpoint (peer.ID / native channel).
type WireCallbacks interface {
	// OnWireMessage forwards one serialized outbound MpcMessage. The payload
	// is the canonical JSON encoding (consistent with the inbound shape
	// SDK.OnWireMessage accepts), so a loopback in tests reduces to: host A's
	// OnWireMessage → host B's SDK.OnWireMessage.
	OnWireMessage(b []byte)
}

// wireSession holds the per-protocol routing state for a running single-party
// session: the canonical sessionId the R5 gate enforces, the device's own
// PartyID, the deterministic peer set used to parse inbound messages, and the
// inbound apply hook the protocol installs.
type wireSession struct {
	sessionID string
	self      string // this device's tss.PartyID.Id
	resolveFn func(from string) *tss.PartyID
	applyFn   func(parsed tss.ParsedMessage, mm *contract.MpcMessage)
}

// installSession registers s as the active wire session. ok is false when
// another session is already active — single-session-per-device is the same
// posture the transport layer enforces (transport/session.go: "one active
// session per Transport at a time").
func (s *SDK) installSession(ws *wireSession) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.active != nil {
		return false
	}
	s.active = ws
	return true
}

// removeSession clears the active wire session; safe to call after a session
// finished or aborted.
func (s *SDK) removeSession(sid string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.active != nil && s.active.sessionID == sid {
		s.active = nil
	}
}

// activeSession returns the current wire session (or nil). The lookup is
// cheap and read-only; OnWireMessage uses it under the SDK mutex to decide
// whether to route an inbound message.
func (s *SDK) activeSession() *wireSession {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.active
}

// emitOutbound serializes one outbound tss message for the active wire
// session and delivers it to the host via wc. self is the device's own
// PartyID.Id, used as the From tag on the wire; peerTags is the
// session-scoped peer set (party.Peers().Ids() excluding self the caller
// pre-sliced when needed) the broadcast path fans out over.
//
// Wire shape matches internal/cli/mpcnet.go sendSingle / mpcnet.sendSingle:
// every MPC message — broadcast included — is delivered as one directed
// envelope per destination peer (the IsBroadcast flag is preserved per
// envelope so the receiver still treats the payload as a broadcast). That
// keeps the session-scoping invariant true on the wire: a fabric / relay
// route message-to-peer never accidentally fans a broadcast onto SDKs that
// aren't in this session.
func emitOutbound(self, sessionID string, peerTags []string, m tss.Message, wc WireCallbacks) error {
	bz, _, err := m.WireBytes()
	if err != nil {
		return fmt.Errorf("wire bytes: %w", err)
	}
	broadcast := m.IsBroadcast() || m.GetTo() == nil
	if broadcast {
		for _, tag := range peerTags {
			if tag == self {
				continue
			}
			mm := contract.MpcMessage{
				Version:     contract.MpcVersionV1,
				SessionID:   sessionID,
				From:        self,
				To:          []string{tag},
				IsBroadcast: true,
				Payload:     bz,
			}
			if err := emitJSONLocal(mm, wc); err != nil {
				return err
			}
		}
		return nil
	}
	for _, d := range m.GetTo() {
		if d == nil || d.Id == self {
			continue
		}
		mm := contract.MpcMessage{
			Version:     contract.MpcVersionV1,
			SessionID:   sessionID,
			From:        self,
			To:          []string{d.Id},
			IsBroadcast: false,
			Payload:     bz,
		}
		if err := emitJSONLocal(mm, wc); err != nil {
			return err
		}
	}
	return nil
}

// emitJSONLocal marshals one MpcMessage and forwards it via wc. The name
// disambiguates from emitJSON in reshare.go; both share the same gomobile-
// flat fire-and-forget contract.
func emitJSONLocal(mm contract.MpcMessage, wc WireCallbacks) error {
	out, err := json.Marshal(mm)
	if err != nil {
		return fmt.Errorf("marshal MpcMessage: %w", err)
	}
	wc.OnWireMessage(out)
	return nil
}

// pumpOutbound drains party.Out() onto wc until ctx is cancelled or done is
// closed. The pump never blocks the protocol: any host-side send error is
// the host's problem (the host's OnWireMessage callback is fire-and-forget by
// gomobile contract).
//
// Lifecycle mirrors mpcnet.pumpSingle: after done closes the pump sweeps any
// residual messages in out (a tss party emits its final-round messages to the
// channel *before* its end signal) so the device flushes before tearing down.
func pumpOutbound(
	ctx context.Context,
	self, sessionID string,
	peerTags []string,
	out <-chan tss.Message,
	wc WireCallbacks,
	done <-chan struct{},
	rerr chan<- error,
) {
	emit := func(m tss.Message) {
		if err := emitOutbound(self, sessionID, peerTags, m, wc); err != nil {
			select {
			case rerr <- err:
			default:
			}
		}
	}
	for {
		select {
		case <-ctx.Done():
			return
		case m, ok := <-out:
			if !ok {
				return
			}
			emit(m)
		case <-done:
			drainOutbound(out, emit)
			return
		}
	}
}

// drainOutbound non-blockingly sweeps any messages already buffered in out and
// emits each via the same path. Mirrors mpcnet.drainResidual.
func drainOutbound(out <-chan tss.Message, emit func(tss.Message)) {
	for {
		select {
		case m := <-out:
			emit(m)
		default:
			return
		}
	}
}

// wirePump is the lifecycle owner the single-party flows install:
// emitter + R5-gated inbound feeder + final-drain semantics. Each protocol
// (keygen / sign / reshare) wires its party against one of these by
// supplying applyFn / resolveFn (and OldPIDs/NewPIDs for reshare).
type wirePump struct {
	sdk       *SDK
	sessionID string
	self      string
	peerTags  []string // session-scoped peer set for broadcast fan-out
	wc        WireCallbacks
	outCh     <-chan tss.Message
}

// start kicks off the outbound goroutine. The caller closes stop after the
// party finished to trigger the final residual drain.
func (p *wirePump) start(ctx context.Context, stop <-chan struct{}, rerr chan<- error) {
	go pumpOutbound(ctx, p.self, p.sessionID, p.peerTags, p.outCh, p.wc, stop, rerr)
}

// tagsOf returns the bare PartyID.Id slice of a sorted peer set; the wire
// pump consults it on broadcast fan-out.
func tagsOf(pids tss.SortedPartyIDs) []string {
	out := make([]string, 0, len(pids))
	for _, p := range pids {
		if p == nil {
			continue
		}
		out = append(out, p.Id)
	}
	return out
}
