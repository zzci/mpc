package coord

import (
	"errors"
	"net/http"
)

// submitResult is the B7 result path (api.md:60-63) and the C8 "false result /
// forged RSV" defense. coord verifies {R,S,V} under the group's ecdsa_pubkey
// over the request's own digest32 BEFORE accepting: a valid result walks the
// C3 tail DISPATCHED->SIGNING->SIGNED->RETURNED and is returned to the
// external service; an invalid one drives FAILED and no result is leaked. The
// first valid report wins; later duplicates are idempotent.
func (c *Coord) submitResult(w http.ResponseWriter, r *http.Request, rec *storedRequest, memberID string, rsv []byte) {
	// Serialize with the engine (sweep / quorum) on this request so a
	// concurrent signer-offline rollback cannot interleave between the
	// SIGNING/SIGNED/RETURNED transitions and make us report a false SIGNED.
	m := c.engine.lockFor(rec.RequestID)
	m.Lock()
	defer m.Unlock()
	// rec was loaded before the lock; re-read the authoritative status.
	fresh, err := c.db.loadRequest(r.Context(), rec.RequestID)
	if err != nil {
		c.writeErr(w, asAPIError(err))
		return
	}
	rec = fresh

	switch rec.Status {
	case stReturned:
		c.writeJSON(w, http.StatusOK, map[string]string{"status": stReturned})
		return
	case stExpired, stRejected, stFailed:
		c.writeErr(w, errStateConflict("request already in a terminal state"))
		return
	}
	// The reporter must be one of the dispatched signers (api.md:62 "reported by one of the
	// designated signers"); a non-signer cannot drive the result.
	if !contains(decodeSigners(rec.SignersJSON), memberID) {
		c.writeErr(w, errForbidden("reporter is not a dispatched signer"))
		return
	}
	// C6(b): expiry is re-checked before accepting {R,S,V}.
	if c.isExpired(rec.ExpiryMs) {
		c.engine.toTerminal(r.Context(), rec.RequestID, rec.Status, stExpired, nil, "expired")
		c.writeErr(w, errExpired("request expired before result"))
		return
	}

	g, err := c.db.group(r.Context(), rec.GroupID)
	if err != nil {
		c.writeErr(w, asAPIError(err))
		return
	}
	if verr := verifyRSV(g.ECDSAPubkey, rec.Digest32, rsv); verr != nil {
		// Forged / mismatched RSV -> FAILED, nothing leaked (C8, api.md:30).
		c.engine.toTerminal(r.Context(), rec.RequestID, rec.Status, stFailed, nil, "rsv verification failed")
		c.writeErr(w, errInvalidEnvelope("rsv failed group-pubkey verification"))
		return
	}

	from := rec.Status
	if from == stDispatched {
		if err := c.transition(r.Context(), rec.RequestID, stDispatched, stSigning,
			"member:"+memberID, "result reported"); err != nil {
			c.conflictOrErr(w, r, err)
			return
		}
		from = stSigning
	}
	if err := c.resultTx(r.Context(), rec.RequestID, from, stSigned, rsv, "", nowISO(c)); err != nil {
		c.conflictOrErr(w, r, err)
		return
	}
	if err := c.transition(r.Context(), rec.RequestID, stSigned, stReturned,
		"coord", "verified and returned"); err != nil && !errors.Is(err, errConflict) {
		c.writeErr(w, asAPIError(err))
		return
	}
	c.reportTerminal(r.Context(), rec.RequestID, stReturned, rsv, "")
	c.writeJSON(w, http.StatusOK, map[string]string{"status": stReturned})
}

// conflictOrErr maps a transition outcome honestly: a missed status guard
// means another path (e.g. a signer-offline rollback) advanced the request, so
// report the request's actual current status instead of asserting success.
func (c *Coord) conflictOrErr(w http.ResponseWriter, r *http.Request, err error) {
	if !errors.Is(err, errConflict) {
		c.writeErr(w, asAPIError(err))
		return
	}
	st, found, serr := c.db.requestStatus(r.Context(), r.PathValue("requestId"))
	if serr != nil || !found {
		c.writeErr(w, errStateConflict("request state changed concurrently"))
		return
	}
	c.writeJSON(w, http.StatusOK, map[string]string{"status": st})
}

func contains(ss []string, s string) bool {
	for _, x := range ss {
		if x == s {
			return true
		}
	}
	return false
}
