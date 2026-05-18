package mobileapi

import (
	"encoding/json"
	"fmt"

	"github.com/zzci/mpc/internal/contract"
)

// OnWireMessage is the host→Go feed for an MPC protocol message the transport
// layer received from a peer (docs/design/mcp/sdk.md §2). It is the receive-side
// security gate: a message is accepted only if its version is supported AND
// its sessionId names a live session this device is running. Any other
// message is dropped unconditionally — the 串话 / replay defense of
// docs/design/contract/protocol.md:53 — and reported via a non-nil error so the
// host/transport can log the drop reason without acting on it.
//
// b is the JSON wire form of a contract.MpcMessage. Networked delivery into a
// running MPC engine is wired by the transport task; the in-process signing
// path self-drives over Go channels, so a gate pass here is an accept-and-
// acknowledge with no further side effects.
func (s *SDK) OnWireMessage(b []byte) error {
	var msg contract.MpcMessage
	if err := json.Unmarshal(b, &msg); err != nil {
		return fmt.Errorf("%s: malformed wire message: %w", CodeBadConfig, err)
	}
	if err := contract.CheckMpcVersion(&msg); err != nil {
		return fmt.Errorf("%s: %w", codeFor(err), err)
	}
	if _, live := s.lookupSession(msg.SessionID); !live {
		return fmt.Errorf("%s: %w", CodeSessionMismatch,
			fmt.Errorf("%w: no live session %q", contract.ErrSessionMismatch, msg.SessionID))
	}
	if err := contract.AcceptInbound(&msg, msg.SessionID); err != nil {
		return fmt.Errorf("%s: %w", codeFor(err), err)
	}
	return nil
}
