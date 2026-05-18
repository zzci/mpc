package coord

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/zzci/mpc/internal/contract"
)

// ingestResult is the A2 ack (api.md:18).
type ingestResult struct {
	RequestID string
	Status    string
}

// maxUnsignedTxBytes caps the opaque unsignedTx blob. It is far above any real
// chain transaction (an ETH/TRON tx is well under a few KiB) yet bounds the
// per-envelope work and storage an abusive external service can force inside
// the 1 MiB body cap (docs/design/security.md §5; coord never parses unsignedTx —
// device-side tx-decode does — so a generous ceiling is sufficient).
const maxUnsignedTxBytes = 256 << 10

// validateEnvelopeShape performs the cheap structural gate of the P6
// proposerSig hardening: every field the proposer signs must be present and
// in-range before any signature work. Each failure is a 400 INVALID_ENVELOPE
// (api.md:21), preserving X-001 semantics — a valid envelope passes unchanged.
func validateEnvelopeShape(env *contract.SigningRequest) error {
	if env.Proposer == "" {
		return errInvalidEnvelope("missing proposer")
	}
	if len(env.Digest32) != 32 {
		return errInvalidEnvelope("digest32 must be 32 bytes")
	}
	if len(env.MetaHash) != 32 {
		return errInvalidEnvelope("metaHash must be 32 bytes")
	}
	if env.Expiry <= env.CreatedAt {
		return errInvalidEnvelope("expiry must be after createdAt")
	}
	if len(env.UnsignedTx) == 0 {
		return errInvalidEnvelope("missing unsignedTx")
	}
	if len(env.UnsignedTx) > maxUnsignedTxBytes {
		return errInvalidEnvelope("unsignedTx too large")
	}
	return nil
}

// ingest validates and enqueues an A2 envelope (docs/design/contract/api.md:13-21,
// docs/design/server/server.md C2). Validation order maps every failure to 400
// INVALID_ENVELOPE per api.md:21. It is idempotent on requestId: a repeat
// submission returns the original status without re-inserting (api.md:20,
// requestId is globally unique and one-time).
func (c *Coord) ingest(ctx context.Context, raw []byte) (ingestResult, error) {
	env := &contract.SigningRequest{}
	if err := json.Unmarshal(raw, env); err != nil {
		return ingestResult{}, errInvalidEnvelope("malformed JSON body")
	}

	// version (api.md D / protocol.md:86: reject unrecognized, no downgrade).
	if err := contract.CheckEnvelopeVersion(env); err != nil {
		return ingestResult{}, errInvalidEnvelope("unsupported envelope version")
	}
	// P6 proposerSig strong validation: reject structurally invalid envelopes
	// with a cheap check BEFORE the JCS canonicalization / EC verify, so a
	// malformed or oversized payload cannot make coord do crypto work for an
	// envelope that can never be valid (docs/design/security.md §5 DoS; the
	// cryptographic binding is unchanged — a well-formed envelope still goes
	// through the same VerifyMetaHash/VerifyProposerSig).
	if err := validateEnvelopeShape(env); err != nil {
		return ingestResult{}, err
	}
	// metaHash == H(businessInfo) (S-001 §4; coord JCS-normalizes once).
	if err := contract.VerifyMetaHash(env); err != nil {
		return ingestResult{}, errInvalidEnvelope("metaHash does not match businessInfo")
	}
	// proposerSig over the canonical preimage (S-001 §2.4). A malformed field
	// (digest32/metaHash length, bad UUID) surfaces here as ErrInvalidEnvelope.
	proposerPub, err := c.resolveProposer(env.Proposer)
	if err != nil {
		return ingestResult{}, errInvalidEnvelope("cannot resolve proposer key")
	}
	if err := contract.VerifyProposerSig(env, proposerPub); err != nil {
		if errors.Is(err, contract.ErrInvalidEnvelope) {
			return ingestResult{}, errInvalidEnvelope("envelope not canonicalizable")
		}
		return ingestResult{}, errInvalidEnvelope("invalid proposerSig")
	}
	// expiry already passed at submission time (api.md:21 -> 400, not 410).
	if c.isExpired(env.Expiry) {
		return ingestResult{}, errInvalidEnvelope("envelope already expired")
	}
	// groupId must be a provisioned group (api.md:21).
	if _, err := c.db.group(ctx, env.GroupID); err != nil {
		if errors.Is(err, errGroupNotFound) {
			return ingestResult{}, errInvalidEnvelope("unknown groupId")
		}
		return ingestResult{}, err
	}

	// Idempotency: same requestId -> original status, no re-insert.
	if st, found, err := c.db.requestStatus(ctx, env.RequestID); err != nil {
		return ingestResult{}, err
	} else if found {
		return ingestResult{RequestID: env.RequestID, Status: st}, nil
	}

	var businessRaw []byte
	if env.BusinessInfo != nil {
		businessRaw, _ = json.Marshal(env.BusinessInfo)
	}
	if err := c.db.insertEnvelope(ctx, env, businessRaw, nowISO(c)); err != nil {
		return ingestResult{}, err
	}
	c.log.Info("envelope ingested", "requestId", env.RequestID, "groupId", env.GroupID)
	go c.engine.evaluate(context.WithoutCancel(ctx), env.RequestID)
	return ingestResult{RequestID: env.RequestID, Status: stPending}, nil
}
