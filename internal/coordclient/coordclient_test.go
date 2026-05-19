package coordclient

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"sync/atomic"
	"testing"
	"time"

	"github.com/btcsuite/btcd/btcec/v2"

	"github.com/zzci/mpc/internal/contract"
)

func mustKey(t *testing.T) *btcec.PrivateKey {
	t.Helper()
	k, err := btcec.NewPrivateKey()
	if err != nil {
		t.Fatalf("key: %v", err)
	}
	return k
}

// (TestRegisterPush_B2 removed: B2 register-push-token was deleted with
// the single-fixed-webhook ruling 2026-05-19.)

func TestHeartbeat_B5(t *testing.T) {
	priv := mustKey(t)
	m := newMockCoord(t, priv.PubKey().SerializeCompressed(), "g1")
	m.on(http.MethodPost, "heartbeat", func(w http.ResponseWriter, _ *http.Request, params []byte) {
		var b heartbeatBody
		if json.Unmarshal(params, &b) != nil || b.RelayPeerID != "peerX" {
			t.Errorf("unexpected heartbeat body: %s", params)
		}
		w.WriteHeader(http.StatusNoContent)
	})
	c := newTestClient(t, m, priv)
	if err := c.Heartbeat(context.Background(), "peerX"); err != nil {
		t.Fatalf("Heartbeat: %v", err)
	}
}

// uuidV4 returns a RFC4122 v4 UUID string (contract canonicalization requires
// requestId to be a UUID).
func uuidV4(t *testing.T) string {
	t.Helper()
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		t.Fatalf("uuid: %v", err)
	}
	b[6] = (b[6] & 0x0f) | 0x40
	b[8] = (b[8] & 0x3f) | 0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16])
}

// signedEnvelope builds a valid envelope whose proposer == hex(compressed
// pubkey), so VerifySelfDescribing exercises the contract reuse path.
func signedEnvelope(t *testing.T, _ string) (contract.SigningRequest, *btcec.PrivateKey) {
	t.Helper()
	pk := mustKey(t)
	env := contract.SigningRequest{
		Version:    contract.EnvelopeVersionV1,
		RequestID:  uuidV4(t),
		GroupID:    "g1",
		Chain:      "eth",
		UnsignedTx: []byte("rawtx"),
		Digest32:   make([]byte, 32),
		Proposer:   hex.EncodeToString(pk.PubKey().SerializeCompressed()),
		CreatedAt:  time.Now().UnixMilli(),
		Expiry:     time.Now().Add(time.Hour).UnixMilli(),
	}
	mh, err := contract.MetaHash(nil)
	if err != nil {
		t.Fatalf("metahash: %v", err)
	}
	env.MetaHash = mh[:]
	if err := contract.SignEnvelope(pk, &env); err != nil {
		t.Fatalf("sign envelope: %v", err)
	}
	return env, pk
}

// pendingItemJSON marshals an item the way coord's hPending does: explicit
// keys, base64 []byte, and NO "version" key (the api.md closure pending.go
// documents).
func pendingItemJSON(env contract.SigningRequest, status string, ttl int64) map[string]any {
	return map[string]any{
		"requestId":    env.RequestID,
		"groupId":      env.GroupID,
		"chain":        env.Chain,
		"unsignedTx":   env.UnsignedTx,
		"digest32":     env.Digest32,
		"proposer":     env.Proposer,
		"createdAt":    env.CreatedAt,
		"expiry":       env.Expiry,
		"metaHash":     env.MetaHash,
		"proposerSig":  env.ProposerSig,
		"status":       status,
		"remainingTTL": ttl,
	}
}

func TestPending_B3_CursorTTLAndEnvelopeReuse(t *testing.T) {
	priv := mustKey(t)
	m := newMockCoord(t, priv.PubKey().SerializeCompressed(), "g1")
	env, _ := signedEnvelope(t, "req-1")

	var gotQuery atomic.Value
	gotQuery.Store("")
	m.on(http.MethodGet, "pending", func(w http.ResponseWriter, r *http.Request, _ []byte) {
		gotQuery.Store(r.URL.RawQuery)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"items":      []any{pendingItemJSON(env, "PENDING", 3600)},
			"serverTime": int64(1700),
		})
	})
	c := newTestClient(t, m, priv)

	items, st, err := c.Pending(context.Background(), 0)
	if err != nil {
		t.Fatalf("Pending: %v", err)
	}
	if gotQuery.Load().(string) != "" {
		t.Fatalf("expected empty query for since=0, got %q", gotQuery.Load())
	}
	if st != 1700 || len(items) != 1 {
		t.Fatalf("serverTime=%d items=%d", st, len(items))
	}
	it := items[0]
	if it.Version != contract.EnvelopeVersionV1 {
		t.Fatalf("Version not restored: %d", it.Version)
	}
	if it.IsExpired() {
		t.Fatal("item with ttl 3600 must not be expired")
	}
	if err := it.VerifySelfDescribing(); err != nil {
		t.Fatalf("contract reuse verify failed: %v", err)
	}

	// Tamper → proposerSig must fail (contract reuse catches it).
	bad := it
	bad.Chain = "tron"
	if err := bad.VerifySelfDescribing(); err == nil {
		t.Fatal("tampered envelope must fail verification")
	}

	// since>0 ⇒ signed raw query is exactly since=<ms>.
	if _, _, err := c.Pending(context.Background(), 1700); err != nil {
		t.Fatalf("Pending cursor: %v", err)
	}
	if q := gotQuery.Load().(string); q != "since=1700" {
		t.Fatalf("cursor query = %q", q)
	}
}

func TestPending_ExpiredItemFlagged(t *testing.T) {
	priv := mustKey(t)
	m := newMockCoord(t, priv.PubKey().SerializeCompressed(), "g1")
	env, _ := signedEnvelope(t, "req-exp")
	m.on(http.MethodGet, "pending", func(w http.ResponseWriter, _ *http.Request, _ []byte) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"items":      []any{pendingItemJSON(env, "PENDING", 0)},
			"serverTime": int64(1),
		})
	})
	c := newTestClient(t, m, priv)
	items, _, err := c.Pending(context.Background(), 0)
	if err != nil {
		t.Fatalf("Pending: %v", err)
	}
	if !items[0].IsExpired() {
		t.Fatal("ttl 0 must be reported expired")
	}
}

func TestDecide_B4_AndExpiry(t *testing.T) {
	priv := mustKey(t)
	m := newMockCoord(t, priv.PubKey().SerializeCompressed(), "g1")
	m.on(http.MethodPost, "decision", func(w http.ResponseWriter, _ *http.Request, params []byte) {
		var b decisionBody
		_ = json.Unmarshal(params, &b)
		if b.Decision == "approved" {
			_ = json.NewEncoder(w).Encode(map[string]string{"status": "DISPATCHED"})
			return
		}
		m.writeErr(w, http.StatusGone, CodeExpired, "expired")
	})
	c := newTestClient(t, m, priv)

	st, err := c.Decide(context.Background(), "req-1", DecisionApproved)
	if err != nil || st != "DISPATCHED" {
		t.Fatalf("approve: st=%q err=%v", st, err)
	}
	_, err = c.Decide(context.Background(), "req-1", DecisionRejected)
	if !errors.Is(err, ErrExpired) {
		t.Fatalf("expected ErrExpired, got %v", err)
	}
	if _, err := c.Decide(context.Background(), "x", "maybe"); err == nil {
		t.Fatal("expected decision validation error")
	}
}

func TestReportResult_B7(t *testing.T) {
	priv := mustKey(t)
	m := newMockCoord(t, priv.PubKey().SerializeCompressed(), "g1")
	m.on(http.MethodPost, "result", func(w http.ResponseWriter, _ *http.Request, params []byte) {
		var b resultBody
		if json.Unmarshal(params, &b) != nil || len(b.RSV) != 65 {
			t.Errorf("bad result body: %s", params)
		}
		_ = json.NewEncoder(w).Encode(map[string]string{"status": "RETURNED"})
	})
	c := newTestClient(t, m, priv)
	st, err := c.ReportResult(context.Background(), "req-1", make([]byte, 65))
	if err != nil || st != "RETURNED" {
		t.Fatalf("ReportResult: st=%q err=%v", st, err)
	}
	if _, err := c.ReportResult(context.Background(), "req-1", nil); err == nil {
		t.Fatal("expected empty rsv error")
	}
}

func TestReceiveStart_B6(t *testing.T) {
	priv := mustKey(t)
	m := newMockCoord(t, priv.PubKey().SerializeCompressed(), "g1")
	env, _ := signedEnvelope(t, "req-start")
	st := contract.StartSigning{
		RequestID: env.RequestID,
		Envelope:  env,
		Signers:   []string{"m1", "m2"},
		SelfRole:  true,
		Deadline:  time.Now().Add(time.Hour).UnixMilli(),
	}
	empty := atomic.Bool{}
	m.on(http.MethodGet, "dispatch", func(w http.ResponseWriter, _ *http.Request, _ []byte) {
		if empty.Load() {
			_, _ = w.Write([]byte("{}"))
			return
		}
		_ = json.NewEncoder(w).Encode(st)
	})
	c := newTestClient(t, m, priv)

	got, ok, err := c.ReceiveStart(context.Background(), 5*time.Second)
	if err != nil || !ok || got == nil {
		t.Fatalf("ReceiveStart: ok=%v err=%v", ok, err)
	}
	if err := VerifyStartSelfDescribing(got); err != nil {
		t.Fatalf("VerifyStart reuse failed: %v", err)
	}
	if !NotExpired(got, time.Now().UnixMilli()) {
		t.Fatal("START must not be expired")
	}
	if _, in := SignerSet(got)["m1"]; !in {
		t.Fatal("m1 expected in signer set")
	}

	empty.Store(true)
	got, ok, err = c.ReceiveStart(context.Background(), 5*time.Second)
	if err != nil || ok || got != nil {
		t.Fatalf("empty dispatch: ok=%v got=%v err=%v", ok, got, err)
	}
}

func TestRetry_LockedThenSuccess(t *testing.T) {
	priv := mustKey(t)
	m := newMockCoord(t, priv.PubKey().SerializeCompressed(), "g1")
	var n int32
	m.on(http.MethodPost, "heartbeat", func(w http.ResponseWriter, _ *http.Request, _ []byte) {
		if atomic.AddInt32(&n, 1) < 3 {
			m.writeErr(w, http.StatusServiceUnavailable, CodeLocked, "locked")
			return
		}
		w.WriteHeader(http.StatusNoContent)
	})
	c := newTestClient(t, m, priv)
	if err := c.Heartbeat(context.Background(), "p"); err != nil {
		t.Fatalf("expected success after LOCKED retries, got %v", err)
	}
	if atomic.LoadInt32(&n) != 3 {
		t.Fatalf("expected 3 attempts, got %d", n)
	}
	// Each attempt must carry a distinct nonce (replay-safe across retries).
	if m.authCount() != 3 {
		t.Fatalf("expected 3 distinct authenticated attempts, got %d", m.authCount())
	}
}

func TestRetry_ExhaustionWrapsLocked(t *testing.T) {
	priv := mustKey(t)
	m := newMockCoord(t, priv.PubKey().SerializeCompressed(), "g1")
	m.on(http.MethodPost, "heartbeat", func(w http.ResponseWriter, _ *http.Request, _ []byte) {
		m.writeErr(w, http.StatusServiceUnavailable, CodeLocked, "locked")
	})
	c := newTestClient(t, m, priv)
	err := c.Heartbeat(context.Background(), "p")
	if !errors.Is(err, ErrLocked) {
		t.Fatalf("exhausted error must still satisfy errors.Is ErrLocked, got %v", err)
	}
}

func TestTerminalErrorNotRetried(t *testing.T) {
	priv := mustKey(t)
	m := newMockCoord(t, priv.PubKey().SerializeCompressed(), "g1")
	var n int32
	m.on(http.MethodPost, "heartbeat", func(w http.ResponseWriter, _ *http.Request, _ []byte) {
		atomic.AddInt32(&n, 1)
		m.writeErr(w, http.StatusForbidden, CodeForbidden, "cross group")
	})
	c := newTestClient(t, m, priv)
	err := c.Heartbeat(context.Background(), "p")
	if !errors.Is(err, ErrForbidden) {
		t.Fatalf("expected ErrForbidden, got %v", err)
	}
	if atomic.LoadInt32(&n) != 1 {
		t.Fatalf("terminal 403 must not retry, attempts=%d", n)
	}
}

func TestContextCancelAbortsBackoff(t *testing.T) {
	priv := mustKey(t)
	m := newMockCoord(t, priv.PubKey().SerializeCompressed(), "g1")
	m.on(http.MethodPost, "heartbeat", func(w http.ResponseWriter, _ *http.Request, _ []byte) {
		m.writeErr(w, http.StatusServiceUnavailable, CodeLocked, "locked")
	})
	c, _ := New(m.srv.URL, "g1", "m1", priv,
		WithRetryPolicy(RetryPolicy{MaxAttempts: 10, BaseDelay: 200 * time.Millisecond, MaxDelay: time.Second}))
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	start := time.Now()
	err := c.Heartbeat(ctx, "p")
	if err == nil {
		t.Fatal("expected context error")
	}
	if time.Since(start) > 2*time.Second {
		t.Fatalf("backoff did not abort on context cancel (%v)", time.Since(start))
	}
}
