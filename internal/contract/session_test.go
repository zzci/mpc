package contract

import (
	"errors"
	"testing"
)

func TestAcceptInbound(t *testing.T) {
	const sid = "123e4567-e89b-12d3-a456-426614174000"
	ok := &MpcMessage{Version: MpcVersionV1, SessionID: sid}
	if err := AcceptInbound(ok, sid); err != nil {
		t.Fatalf("matching session rejected: %v", err)
	}

	wrong := &MpcMessage{Version: MpcVersionV1, SessionID: "other-session"}
	if err := AcceptInbound(wrong, sid); !errors.Is(err, ErrSessionMismatch) {
		t.Fatalf("cross-session: err = %v, want ErrSessionMismatch", err)
	}

	badVer := &MpcMessage{Version: 99, SessionID: sid}
	if err := AcceptInbound(badVer, sid); !errors.Is(err, ErrUnsupportedVersion) {
		t.Fatalf("bad version: err = %v, want ErrUnsupportedVersion", err)
	}

	if !SameSession(ok, sid) || SameSession(wrong, sid) {
		t.Fatal("SameSession mismatch")
	}
}
