package mobileapi

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/zzci/mpc/internal/contract"
)

func TestOnWireMessageGate(t *testing.T) {
	sdk := newTestSDK(t)
	sdk.registerSession("live", newSignSession())

	mustMsg := func(m contract.MpcMessage) []byte {
		b, err := json.Marshal(m)
		if err != nil {
			t.Fatalf("marshal: %v", err)
		}
		return b
	}

	cases := []struct {
		name    string
		in      []byte
		wantSub string // "" => expect nil (accepted)
	}{
		{"malformed", []byte(`{not-json`), CodeBadConfig},
		{"bad version", mustMsg(contract.MpcMessage{Version: 999, SessionID: "live"}), CodeUnsupportedVersion},
		{"unknown session", mustMsg(contract.MpcMessage{Version: contract.MpcVersionV1, SessionID: "ghost"}), CodeSessionMismatch},
		{"accepted", mustMsg(contract.MpcMessage{Version: contract.MpcVersionV1, SessionID: "live"}), ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := sdk.OnWireMessage(tc.in)
			if tc.wantSub == "" {
				if err != nil {
					t.Fatalf("expected accept, got %v", err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tc.wantSub) {
				t.Fatalf("err=%v, want substring %q", err, tc.wantSub)
			}
		})
	}
}
