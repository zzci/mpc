package mobileapi

import (
	"encoding/json"
	"fmt"

	"github.com/bnb-chain/tss-lib/v3/tss"

	"github.com/zzci/mpc/internal/contract"
)

// OnWireMessage is the host→Go feed for an MPC protocol message the transport
// layer received from a peer (docs/design/mcp/sdk.md §2). It is the R5
// receive-side security gate: a message is accepted only if its version is
// supported AND its sessionId names a live session this device is running.
// Any other message is dropped unconditionally — the cross-talk / replay
// defense of docs/design/contract/protocol.md:53 — and reported via a
// non-nil error so the host/transport can log the drop reason without
// acting on it.
//
// b is the JSON wire form of a contract.MpcMessage. Under DM-3 (distributed-
// mpc-impl.md §B), a gate pass delivers the parsed tss message into the
// running single-party engine via the active wire session's apply hook so
// the device's keygen / sign / reshare flow actually advances.
func (s *SDK) OnWireMessage(b []byte) error {
	var msg contract.MpcMessage
	if err := json.Unmarshal(b, &msg); err != nil {
		return fmt.Errorf("%s: malformed wire message: %w", CodeBadConfig, err)
	}
	if err := contract.CheckMpcVersion(&msg); err != nil {
		return fmt.Errorf("%s: %w", codeFor(err), err)
	}
	if _, live := s.lookupSession(msg.SessionID); !live {
		// SignSession liveness is the WYSIWYS-flow signal — a Sign call
		// registers / unregisters its SignSession around the human review
		// window. The active wire session check below is the MPC-engine
		// signal: keygen / reshare register a wireSession without a
		// SignSession entry. We accept the message if EITHER is live (the
		// R5 gate fires only when neither is live).
		ws := s.activeSession()
		if ws == nil || ws.sessionID != msg.SessionID {
			return fmt.Errorf("%s: %w", CodeSessionMismatch,
				fmt.Errorf("%w: no live session %q", contract.ErrSessionMismatch, msg.SessionID))
		}
	}
	if err := contract.AcceptInbound(&msg, msg.SessionID); err != nil {
		return fmt.Errorf("%s: %w", codeFor(err), err)
	}

	ws := s.activeSession()
	if ws == nil || ws.sessionID != msg.SessionID {
		// Sign's WYSIWYS gate may register a SignSession before the MPC
		// engine has installed the wire session (the human review window).
		// In that case the gate above accepted the message; here we drop
		// it silently — there is no engine yet to feed. This matches the
		// transport-layer posture: inbound queued before Start is benign.
		return nil
	}
	if msg.From != "" && msg.From == ws.self {
		// Defence in depth: a transport loopback must never trip the
		// protocol (mpc.KeygenParty.Update drops same-Id messages too).
		return nil
	}
	from := ws.resolveFn(msg.From)
	if from == nil {
		// Unknown sender for this session: drop. Mirrors cli/mpcnet.go's
		// applyInbound posture (defence in depth).
		return nil
	}
	parsed, perr := tss.ParseWireMessage(msg.Payload, from, msg.IsBroadcast)
	if perr != nil {
		return nil // malformed payload: drop, never crash the device
	}
	ws.applyFn(parsed, &msg)
	return nil
}
