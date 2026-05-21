package admin

import (
	"context"
	"encoding/hex"
	"fmt"
	"testing"

	"github.com/zzci/mpc/internal/server/coorddb"
)

func seedGroupWithIdentity(t *testing.T, store *coorddb.Store, groupID string, identity []byte) {
	t.Helper()
	if err := store.ProvisionGroup(context.Background(),
		coorddb.GroupRecord{
			GroupID: groupID, ECDSAPubkey: []byte{1}, GroupPubkey: []byte{2},
			ThresholdT: 1, PartiesN: 1, CreatedAt: "1700000000000",
		},
		[]coorddb.MemberRecord{{MemberID: "m1", IdentityPubkey: identity}}); err != nil {
		t.Fatalf("provision %s: %v", groupID, err)
	}
}

func validIdentity33() []byte {
	id := make([]byte, 33)
	id[0] = 0x02
	for i := 1; i < 33; i++ {
		id[i] = byte(i)
	}
	return id
}

func validIdentity33Other() []byte {
	id := make([]byte, 33)
	id[0] = 0x03
	for i := 1; i < 33; i++ {
		id[i] = byte(255 - i)
	}
	return id
}

func itemsCount(m map[string]any) int {
	if c, ok := m["count"].(float64); ok {
		return int(c)
	}
	return -1
}

func itemGroupIDs(m map[string]any) []string {
	ids := []string{}
	if items, ok := m["items"].([]any); ok {
		for _, it := range items {
			if row, ok := it.(map[string]any); ok {
				if gid, ok := row["groupId"].(string); ok {
					ids = append(ids, gid)
				}
			}
		}
	}
	return ids
}

func TestDevicesByIdentityHandlerHappyPath(t *testing.T) {
	s, store, _ := newServer(t)
	unlock(t, store)
	shared := validIdentity33()
	other := validIdentity33Other()
	seedGroupWithIdentity(t, store, "g-shared-1", shared)
	seedGroupWithIdentity(t, store, "g-shared-2", shared)
	seedGroupWithIdentity(t, store, "g-other", other)

	w, body := do(t, s.router(), "GET", "/api/devices/"+hex.EncodeToString(shared)+"/groups", controlTok, "")
	if w.Code != 200 {
		t.Fatalf("code=%d body=%v", w.Code, body)
	}
	if c := itemsCount(body); c != 2 {
		t.Fatalf("want 2 items got %d body=%v", c, body)
	}
	ids := itemGroupIDs(body)
	gotShared1, gotShared2, gotOther := false, false, false
	for _, id := range ids {
		switch id {
		case "g-shared-1":
			gotShared1 = true
		case "g-shared-2":
			gotShared2 = true
		case "g-other":
			gotOther = true
		}
	}
	if !gotShared1 || !gotShared2 {
		t.Fatalf("missing shared groups: %v", ids)
	}
	if gotOther {
		t.Fatalf("foreign identity leaked: %v", ids)
	}
}

func TestDevicesByIdentityHandlerNoMatch(t *testing.T) {
	s, store, _ := newServer(t)
	unlock(t, store)
	other := validIdentity33Other()
	w, body := do(t, s.router(), "GET", "/api/devices/"+hex.EncodeToString(other)+"/groups", controlTok, "")
	if w.Code != 200 {
		t.Fatalf("code=%d body=%v", w.Code, body)
	}
	if c := itemsCount(body); c != 0 {
		t.Fatalf("want count:0 got %d body=%v", c, body)
	}
}

func TestDevicesByIdentityHandlerBadHex(t *testing.T) {
	// Unlock the store so the bad-identity validation (in the handler)
	// runs ahead of the lockGate's 503 short-circuit. We want the precise
	// 400 bad_identity error code, not the LOCKED state's blanket 503.
	s, store, _ := newServer(t)
	unlock(t, store)
	for _, bad := range []string{"zz", "abcd", "deadbeef"} {
		w, body := do(t, s.router(), "GET", "/api/devices/"+bad+"/groups", controlTok, "")
		if w.Code != 400 {
			t.Fatalf("bad hex %q: code=%d body=%v", bad, w.Code, body)
		}
		errMap, _ := body["error"].(map[string]any)
		code, _ := errMap["code"].(string)
		if code != "bad_identity" {
			t.Fatalf("bad hex %q: code field = %q want bad_identity (body=%v)", bad, code, body)
		}
	}
}

func TestDevicesByIdentityHandlerLockedBefore(t *testing.T) {
	s, _, _ := newServer(t) // store stays LOCKED — no unlock call
	id := validIdentity33()
	w, _ := do(t, s.router(), "GET", "/api/devices/"+hex.EncodeToString(id)+"/groups", controlTok, "")
	if w.Code != 503 {
		t.Fatalf("locked-store should be 503, got %d", w.Code)
	}
}

// _ = fmt.Sprintf in case future debug lines want it.
var _ = fmt.Sprintf
