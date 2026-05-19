package contract

import "fmt"

// SessionID strong isolation (docs/design/contract/protocol.md:53): an inbound
// MpcMessage whose sessionId differs from the active session is dropped
// unconditionally, defeating replay and cross-session ("cross-talk") injection.
// sessionId equals requestId for signing, or the keygen/reshare session ID
// (docs/design/contract/protocol.md:49).

// SameSession reports whether msg belongs to the expected session.
func SameSession(msg *MpcMessage, expectedSessionID string) bool {
	return msg.SessionID == expectedSessionID
}

// AcceptInbound is the receive-side gate for an MpcMessage: it enforces
// version support then sessionId isolation. A non-nil error means the message
// MUST be dropped, not processed (docs/design/contract/protocol.md:53,86). It does
// not verify senderAuth — call VerifySenderAuth with the sender's member key
// once the sender is resolved.
func AcceptInbound(msg *MpcMessage, expectedSessionID string) error {
	if err := CheckMpcVersion(msg); err != nil {
		return err
	}
	if !SameSession(msg, expectedSessionID) {
		return fmt.Errorf("%w: got %q, want %q", ErrSessionMismatch, msg.SessionID, expectedSessionID)
	}
	return nil
}
