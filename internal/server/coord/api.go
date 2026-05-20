package coord

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"
	"time"
)

// HTTP surface: docs/design/contract/api.md A (external) + B (member) and
// docs/spec/group-provisioning.md /v1/groups*. Stdlib net/http only — the
// repo baseline is go 1.25 and the go 1.22+ ServeMux (method+wildcard
// patterns, r.PathValue) covers routing with no dependency, so go.mod stays
// pinned (go 1.25, protobuf v1.36.6, libp2p v0.48.0) per PLAN.md §1.
//
// Every data endpoint is wrapped by lockGate: when the store is LOCKED it
// returns 503 LOCKED and nothing else (fail-closed, api.md:81-84,
// group-provisioning.md §9). Only /healthz is exempt and never returns data.

const maxBodyBytes = 1 << 20 // 1 MiB request-body cap (DoS guard)

func (c *Coord) router() http.Handler {
	mux := http.NewServeMux()

	// Health: always available, never leaks data (api.md:83 minimal health).
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	})

	// A — external business service. P6: extGate (per-IP rate limit) wraps
	// extAuth so an unauthenticated flood / API-key brute force is shed before
	// the auth check (docs/design/security.md §5).
	mux.Handle("POST /v1/requests", c.lockGate(c.extGate(c.extAuth(c.hIngest))))
	mux.Handle("GET /v1/requests/{requestId}", c.lockGate(c.extGate(c.extAuth(c.hStatus))))
	mux.Handle("GET /v1/requests/{requestId}/result", c.lockGate(c.extGate(c.extAuth(c.hResultLongpoll))))

	// B — member SDK. (B2 register-push-token removed with the
	// single-fixed-webhook ruling 2026-05-19: coord holds no push
	// credentials/tokens; an external notification channel owns delivery.)
	mux.Handle("POST /v1/members/self/heartbeat", c.lockGate(http.HandlerFunc(c.hHeartbeat)))
	mux.Handle("GET /v1/groups/{groupId}/pending", c.lockGate(http.HandlerFunc(c.hPending)))
	mux.Handle("POST /v1/requests/{requestId}/decision", c.lockGate(http.HandlerFunc(c.hDecision)))
	mux.Handle("GET /v1/groups/{groupId}/dispatch", c.lockGate(http.HandlerFunc(c.hDispatch)))
	mux.Handle("POST /v1/requests/{requestId}/result", c.lockGate(http.HandlerFunc(c.hResult)))
	// B8 (address-derivation.md §7 / api.md B8) — owning-member-only HD xpub
	// release. memberGate enforces B1 + groupId isolation; legacy groups
	// (chaincode NULL) surface 409 LEGACY_NO_HD; the chaincode is **never**
	// exposed on A surface (F1 hard constraint).
	mux.Handle("GET /v1/groups/{groupId}/xpub", c.lockGate(http.HandlerFunc(c.hXpub)))

	// B12 — group_derived_addresses (api.md B12, address-derivation.md §7.bis,
	// Q-A/B/C/D user ruling 2026-05-20). Owning-member-only (§F1 strict / §7.bis.3);
	// never an A-side route.
	mux.Handle("POST /v1/groups/{groupId}/derived/register", c.lockGate(http.HandlerFunc(c.hDerivedRegister)))
	mux.Handle("GET /v1/groups/{groupId}/derived", c.lockGate(http.HandlerFunc(c.hDerivedList)))

	// B9/B10/B11 — distributed-mpc coord event-orchestration (DM-4,
	// distributed-mpc.md §3/§3.bis/§3.ter, api.md B9-B11). Member-only
	// routes; the strict identity allowlist (coord.external.expected_members)
	// fail-closes any identity not pre-declared with EXPECTED_MEMBER_MISMATCH.
	mux.Handle("POST /v1/groups/{groupId}/keygen", c.lockGate(http.HandlerFunc(c.hKeygen)))
	mux.Handle("POST /v1/groups/{groupId}/reshare", c.lockGate(http.HandlerFunc(c.hReshare)))
	mux.Handle("PUT /v1/groups/{groupId}/attestation", c.lockGate(http.HandlerFunc(c.hAttestation)))

	// S-002 — group/membership provisioning (self-attesting payloads).
	mux.Handle("POST /v1/groups", c.lockGate(c.rateGate(http.HandlerFunc(c.hProvisionGroup))))
	mux.Handle("POST /v1/groups/{groupId}/membership", c.lockGate(c.rateGate(http.HandlerFunc(c.hMembership))))
	mux.Handle("GET /v1/groups/{groupId}", c.lockGate(http.HandlerFunc(c.hGroupPublic)))
	// api.md A1 — external business service address application. L1 (b) split
	// ruling (2026-05-18): a physically separate path + external auth chain
	// (lockGate→extGate→extAuth, mirroring the A-surface above), never
	// memberGate, with a minimal addresses-only response. Distinct method+path
	// from the §5.1 member route above — no ServeMux conflict.
	mux.Handle("GET /v1/groups/{groupId}/public", c.lockGate(c.extGate(c.extAuth(c.hGroupPublicExt))))

	return mux
}

// lockGate fail-closes every data endpoint when the store is LOCKED
// (docs/design/server/server.md C9b). It is the outermost wrapper so no handler
// body or auth runs while locked.
func (c *Coord) lockGate(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !c.store.IsUnlocked() {
			c.writeErr(w, errLocked())
			return
		}
		r.Body = http.MaxBytesReader(w, r.Body, maxBodyBytes)
		next.ServeHTTP(w, r)
	})
}

// extAuth enforces external-service auth (api.md A1) via the hardened
// checkExternalAuth gate: external auth is fixed api_key (user ruling
// 2026-05-19), constant-time compared, and an absent key is an explicit
// reject. The envelope-level proposerSig (verified in ingest) remains the
// cryptographic binding regardless of transport auth.
func (c *Coord) extAuth(next http.HandlerFunc) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := c.checkExternalAuth(r); err != nil {
			c.writeErr(w, asAPIError(err))
			return
		}
		next(w, r)
	})
}

// --- A endpoints ---------------------------------------------------------

func (c *Coord) hIngest(w http.ResponseWriter, r *http.Request) {
	raw, err := io.ReadAll(r.Body)
	if err != nil {
		c.writeErr(w, errInvalidEnvelope("unreadable body"))
		return
	}
	res, ierr := c.ingest(r.Context(), raw)
	if ierr != nil {
		c.writeErr(w, asAPIError(ierr))
		return
	}
	c.writeJSON(w, http.StatusAccepted, map[string]string{
		"requestId": res.RequestID, "status": res.Status,
	})
}

func (c *Coord) hStatus(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("requestId")
	rec, err := c.db.loadRequest(r.Context(), id)
	if errors.Is(err, sql.ErrNoRows) {
		c.writeErr(w, errNotFound("unknown requestId"))
		return
	}
	if err != nil {
		c.writeErr(w, asAPIError(err))
		return
	}
	out := map[string]any{"requestId": rec.RequestID, "status": rec.Status}
	if rec.FailReason != "" {
		out["fail_reason"] = rec.FailReason
	}
	if rec.Status == stReturned && len(rec.ResultRSV) > 0 {
		out["result"] = map[string]string{"rsv": b64(rec.ResultRSV)}
	}
	c.writeJSON(w, http.StatusOK, out)
}

// hResultLongpoll is the A4 longpoll: block until terminal or ?wait= elapses,
// then return the same shape as hStatus (api.md:29).
func (c *Coord) hResultLongpoll(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("requestId")
	rec, err := c.db.loadRequest(r.Context(), id)
	if errors.Is(err, sql.ErrNoRows) {
		c.writeErr(w, errNotFound("unknown requestId"))
		return
	}
	if err != nil {
		c.writeErr(w, asAPIError(err))
		return
	}
	if !isTerminal(rec.Status) {
		wait := parseWait(r, 25*time.Second)
		ch, cancel := c.results.wait(id)
		defer cancel()
		select {
		case <-ch:
		case <-time.After(wait):
		case <-r.Context().Done():
		}
		if rec, err = c.db.loadRequest(r.Context(), id); err != nil {
			c.writeErr(w, asAPIError(err))
			return
		}
	}
	out := map[string]any{"requestId": rec.RequestID, "status": rec.Status}
	if rec.FailReason != "" {
		out["reason"] = rec.FailReason
	}
	if rec.Status == stReturned && len(rec.ResultRSV) > 0 {
		out["rsv"] = b64(rec.ResultRSV)
	}
	c.writeJSON(w, http.StatusOK, out)
}

// --- B endpoints ---------------------------------------------------------

// heartbeatBody is the B5 payload (api.md:53).
type heartbeatBody struct {
	GroupID     string `json:"groupId"`
	MemberID    string `json:"memberId"`
	RelayPeerID string `json:"relayPeerID"`
}

func (c *Coord) hHeartbeat(w http.ResponseWriter, r *http.Request) {
	var b heartbeatBody
	raw, ok := c.readJSON(w, r, &b)
	if !ok {
		return
	}
	if !c.memberGate(w, r, b.GroupID, b.MemberID, "B5:heartbeat", raw) {
		return
	}
	if err := c.presence.Heartbeat(r.Context(), b.GroupID, b.MemberID, b.RelayPeerID); err != nil {
		c.writeErr(w, asAPIError(err))
		return
	}
	w.WriteHeader(http.StatusNoContent)
	// New liveness may complete a quorum: re-evaluate the group's pending set.
	go c.engine.onGroupEvent(context.WithoutCancel(r.Context()), b.GroupID)
}

func (c *Coord) hPending(w http.ResponseWriter, r *http.Request) {
	groupID := r.PathValue("groupId")
	memberID := r.Header.Get("X-Member-Id")
	if !c.memberGate(w, r, groupID, memberID, "B3:pending", []byte(r.URL.RawQuery)) {
		return
	}
	var sinceMs int64
	if s := r.URL.Query().Get("since"); s != "" {
		sinceMs, _ = strconv.ParseInt(s, 10, 64)
	}
	rows, err := c.db.pending(r.Context(), groupID, sinceMs)
	if err != nil {
		c.writeErr(w, asAPIError(err))
		return
	}
	items := make([]map[string]any, 0, len(rows))
	for i := range rows {
		rr := &rows[i]
		if c.isExpired(rr.ExpiryMs) {
			continue // never surface an expired item (C6, api.md:46)
		}
		env, berr := rebuildEnvelope(rr)
		if berr != nil {
			continue
		}
		items = append(items, map[string]any{
			"requestId":    env.RequestID,
			"groupId":      env.GroupID,
			"chain":        env.Chain,
			"unsignedTx":   env.UnsignedTx,
			"digest32":     env.Digest32,
			"proposer":     env.Proposer,
			"createdAt":    env.CreatedAt,
			"expiry":       env.Expiry,
			"businessInfo": env.BusinessInfo,
			"metaHash":     env.MetaHash,
			"proposerSig":  env.ProposerSig,
			"status":       rr.Status,
			"remainingTTL": c.remainingTTL(rr.ExpiryMs),
		})
	}
	c.writeJSON(w, http.StatusOK, map[string]any{
		"items": items, "serverTime": unixMillis(c.clock.Now()),
	})
}

// decisionBody is the B4 payload (api.md:49).
type decisionBody struct {
	MemberID string `json:"memberId"`
	Decision string `json:"decision"`
}

func (c *Coord) hDecision(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("requestId")
	var b decisionBody
	raw, ok := c.readJSON(w, r, &b)
	if !ok {
		return
	}
	if b.Decision != "approved" && b.Decision != "rejected" {
		c.writeErr(w, errInvalidEnvelope("decision must be approved or rejected"))
		return
	}
	rec, err := c.db.loadRequest(r.Context(), id)
	if errors.Is(err, sql.ErrNoRows) {
		c.writeErr(w, errNotFound("unknown requestId"))
		return
	}
	if err != nil {
		c.writeErr(w, asAPIError(err))
		return
	}
	if !c.memberGate(w, r, rec.GroupID, b.MemberID, "B4:decision:"+b.Decision, raw) {
		return
	}
	if rec.Status != stPending {
		c.writeErr(w, errStateConflict("request is not PENDING"))
		return
	}
	if c.isExpired(rec.ExpiryMs) {
		c.writeErr(w, errExpired("request expired"))
		return
	}
	if err := c.db.saveDecision(r.Context(), id, b.MemberID, b.Decision, c.authSig(r), nowISO(c)); err != nil {
		c.writeErr(w, asAPIError(err))
		return
	}
	c.engine.evaluate(r.Context(), id)
	st, _, _ := c.db.requestStatus(r.Context(), id)
	c.writeJSON(w, http.StatusOK, map[string]string{"status": st})
}

func (c *Coord) hDispatch(w http.ResponseWriter, r *http.Request) {
	groupID := r.PathValue("groupId")
	memberID := r.Header.Get("X-Member-Id")
	if !c.memberGate(w, r, groupID, memberID, "B6:dispatch", []byte(r.URL.RawQuery)) {
		return
	}
	if st, ok := c.hub.take(groupID, memberID); ok {
		c.writeJSON(w, http.StatusOK, st)
		return
	}
	wait := parseWait(r, 25*time.Second)
	ch, cancel := c.hub.wait(groupID, memberID)
	defer cancel()
	select {
	case st := <-ch:
		if st == nil {
			c.writeJSON(w, http.StatusOK, map[string]any{})
			return
		}
		c.writeJSON(w, http.StatusOK, st)
	case <-time.After(wait):
		c.writeJSON(w, http.StatusOK, map[string]any{})
	case <-r.Context().Done():
	}
}

// resultBody is the B7 payload (api.md:61).
type resultBody struct {
	MemberID string `json:"memberId"`
	RSV      []byte `json:"rsv"`
}

func (c *Coord) hResult(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("requestId")
	var b resultBody
	raw, ok := c.readJSON(w, r, &b)
	if !ok {
		return
	}
	rec, err := c.db.loadRequest(r.Context(), id)
	if errors.Is(err, sql.ErrNoRows) {
		c.writeErr(w, errNotFound("unknown requestId"))
		return
	}
	if err != nil {
		c.writeErr(w, asAPIError(err))
		return
	}
	if !c.memberGate(w, r, rec.GroupID, b.MemberID, "B7:result", raw) {
		return
	}
	c.submitResult(w, r, rec, b.MemberID, b.RSV)
}

// --- S-002 endpoints -----------------------------------------------------

func (c *Coord) hProvisionGroup(w http.ResponseWriter, r *http.Request) {
	raw, err := io.ReadAll(r.Body)
	if err != nil {
		c.writeErr(w, errInvalidEnvelope("unreadable body"))
		return
	}
	status, aerr := c.provisionGroup(r.Context(), raw)
	if aerr != nil {
		c.writeErr(w, aerr)
		return
	}
	c.writeJSON(w, http.StatusCreated, map[string]string{
		"groupId": gjson(raw, "groupId"), "status": status,
	})
}

func (c *Coord) hMembership(w http.ResponseWriter, r *http.Request) {
	groupID := r.PathValue("groupId")
	raw, err := io.ReadAll(r.Body)
	if err != nil {
		c.writeErr(w, errInvalidEnvelope("unreadable body"))
		return
	}
	epoch, status, aerr := c.updateMembership(r.Context(), groupID, raw)
	if aerr != nil {
		c.writeErr(w, aerr)
		return
	}
	c.writeJSON(w, http.StatusOK, map[string]any{
		"groupId": groupID, "epoch": epoch, "status": status,
	})
}

func (c *Coord) hGroupPublic(w http.ResponseWriter, r *http.Request) {
	groupID := r.PathValue("groupId")
	memberID := r.Header.Get("X-Member-Id")
	if !c.memberGate(w, r, groupID, memberID, "S2:groupRead", []byte(r.URL.RawQuery)) {
		return
	}
	v, aerr := c.groupPublic(r.Context(), groupID)
	if aerr != nil {
		c.writeErr(w, aerr)
		return
	}
	c.writeJSON(w, http.StatusOK, v)
}

// groupPublicExtView is the api.md A1 GET /v1/groups/{groupId}/public response
// for an external business service ("request address"). It is a deliberately minimal,
// independent DTO — never groupPublicView — so no member / groupPubkey / epoch
// / activeMembers / degraded privileged group state can leak through the
// external surface (api.md:16, L1 (b) split ruling 2026-05-18). ecdsa_pubkey
// serializes as JSON base64 ([]byte), matching api.md "ecdsa_pubkey(b64)".
type groupPublicExtView struct {
	GroupID     string `json:"groupId"`
	ECDSAPubkey []byte `json:"ecdsa_pubkey"`
	EVMAddress  string `json:"evm_address"`
	TronAddress string `json:"tron_address"`
	ThresholdT  int    `json:"threshold_t"`
	PartiesN    int    `json:"parties_n"`
}

// hGroupPublicExt serves api.md A1 GET /v1/groups/{groupId}/public. Auth is the
// external chain (lockGate→extGate→extAuth, wired in router), never memberGate,
// and the response is the minimal addresses-only view physically isolated from
// the §5.1 member view so an auth-branch bug cannot leak privileged group
// state. The evm/tron values are G-001's persisted derived addresses, read
// only (no derivation, no coorddb mutation). Unknown groupId → 404, reachable
// here precisely because this path is not memberGate fail-closed (the point of
// the L1 (b) split).
func (c *Coord) hGroupPublicExt(w http.ResponseWriter, r *http.Request) {
	g, err := c.db.group(r.Context(), r.PathValue("groupId"))
	if errors.Is(err, errGroupNotFound) {
		c.writeErr(w, errNotFound("unknown groupId"))
		return
	}
	if err != nil {
		c.writeErr(w, asAPIError(err))
		return
	}
	c.writeJSON(w, http.StatusOK, &groupPublicExtView{
		GroupID:     g.GroupID,
		ECDSAPubkey: g.ECDSAPubkey,
		EVMAddress:  g.EVMAddress,
		TronAddress: g.TronAddress,
		ThresholdT:  g.ThresholdT,
		PartiesN:    g.PartiesN,
	})
}

// --- shared helpers ------------------------------------------------------

// memberGate verifies the B1 member signature (headers X-Member-*) bound to
// (method, params) and enforces groupId isolation: the member must be an
// active member of groupID. It writes the error response itself and reports
// ok=false on any failure.
func (c *Coord) memberGate(w http.ResponseWriter, r *http.Request, groupID, memberID, method string, params []byte) bool {
	if groupID == "" || memberID == "" {
		c.writeErr(w, errUnauthenticated("missing groupId or memberId"))
		return false
	}
	// P6: per-IP member rate limit, shed before the EC verify so an abusive
	// origin is capped regardless of the claimed memberId (docs/design/security.md
	// §5; keyed by IP to avoid spoofed-memberId amplification — see abuse.go).
	if !c.memberAllowed(clientIP(r)) {
		c.writeErr(w, errRateLimited("too many member requests"))
		return false
	}
	a, err := parseMemberAuth(r, memberID)
	if err != nil {
		c.writeErr(w, asAPIError(err))
		return false
	}
	idPub, ok, derr := c.db.activeMember(r.Context(), groupID, memberID)
	if derr != nil {
		c.writeErr(w, asAPIError(derr))
		return false
	}
	if !ok {
		c.writeErr(w, errForbidden("not an active member of this group"))
		return false
	}
	bound := append([]byte(method+"|"+groupID+"|"), params...)
	if err := c.verifyMemberAuth(a, method, hash(bound), idPub); err != nil {
		c.writeErr(w, asAPIError(err))
		return false
	}
	return true
}

func (c *Coord) authSig(r *http.Request) []byte {
	s, _ := base64.StdEncoding.DecodeString(r.Header.Get("X-Member-Sig"))
	return s
}

func parseMemberAuth(r *http.Request, memberID string) (memberAuthSig, error) {
	ts, err := strconv.ParseInt(r.Header.Get("X-Member-Ts"), 10, 64)
	if err != nil {
		return memberAuthSig{}, errUnauthenticated("missing or bad X-Member-Ts")
	}
	nonce, err := base64.StdEncoding.DecodeString(r.Header.Get("X-Member-Nonce"))
	if err != nil || len(nonce) == 0 {
		return memberAuthSig{}, errUnauthenticated("missing or bad X-Member-Nonce")
	}
	sig, err := base64.StdEncoding.DecodeString(r.Header.Get("X-Member-Sig"))
	if err != nil || len(sig) == 0 {
		return memberAuthSig{}, errUnauthenticated("missing or bad X-Member-Sig")
	}
	return memberAuthSig{MemberID: memberID, TS: ts, Nonce: nonce, Sig: sig}, nil
}

func parseWait(r *http.Request, def time.Duration) time.Duration {
	s := r.URL.Query().Get("wait")
	if s == "" {
		return def
	}
	if d, err := time.ParseDuration(s); err == nil && d > 0 && d <= 60*time.Second {
		return d
	}
	if n, err := strconv.Atoi(s); err == nil && n > 0 && n <= 60 {
		return time.Duration(n) * time.Second
	}
	return def
}

func hash(b []byte) []byte {
	h := sha256.Sum256(b)
	return h[:]
}

func gjson(raw []byte, key string) string {
	var m map[string]json.RawMessage
	if json.Unmarshal(raw, &m) != nil {
		return ""
	}
	var s string
	_ = json.Unmarshal(m[key], &s)
	return s
}

func (c *Coord) readJSON(w http.ResponseWriter, r *http.Request, v any) ([]byte, bool) {
	raw, err := io.ReadAll(r.Body)
	if err != nil {
		c.writeErr(w, errInvalidEnvelope("unreadable body"))
		return nil, false
	}
	if err := json.Unmarshal(raw, v); err != nil {
		c.writeErr(w, errInvalidEnvelope("malformed JSON body"))
		return nil, false
	}
	return raw, true
}

func (c *Coord) writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

// writeErr emits the api.md:79 error envelope {error:{code,message,requestId?}}
// with the mapped HTTP status; the message never carries internals.
func (c *Coord) writeErr(w http.ResponseWriter, e *apiError) {
	body := map[string]any{"code": e.code, "message": e.message}
	if e.requestID != "" {
		body["requestId"] = e.requestID
	}
	c.writeJSON(w, e.status, map[string]any{"error": body})
}
