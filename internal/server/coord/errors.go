package coord

import (
	"errors"
	"net/http"

	"github.com/zzci/mpc/internal/server/coorddb"
)

// apiError carries the docs/design/contract/api.md C-table code/HTTP status plus an
// operator-safe message (the message MUST NOT leak sensitive data, api.md:79).
// It is the single type every handler returns on failure so the error envelope
// {error:{code,message,requestId?}} is produced in exactly one place.
type apiError struct {
	status    int
	code      string
	message   string
	requestID string // optional, echoed in the envelope when set
}

func (e *apiError) Error() string { return e.code + ": " + e.message }

// The C-table codes (docs/design/contract/api.md:65-79, group-provisioning.md §8).
const (
	codeInvalidEnvelope = "INVALID_ENVELOPE"
	codeUnauthenticated = "UNAUTHENTICATED"
	codeForbidden       = "FORBIDDEN"
	codeNotFound        = "NOT_FOUND"
	codeStateConflict   = "STATE_CONFLICT"
	codeLegacyNoHD      = "LEGACY_NO_HD"
	codeExpectedMember  = "EXPECTED_MEMBER_MISMATCH"
	codeExpired         = "EXPIRED"
	codeRateLimited     = "RATE_LIMITED"
	codeLocked          = "LOCKED"
	codeInternal        = "INTERNAL"
)

func errInvalidEnvelope(msg string) *apiError {
	return &apiError{status: http.StatusBadRequest, code: codeInvalidEnvelope, message: msg}
}

func errUnauthenticated(msg string) *apiError {
	return &apiError{status: http.StatusUnauthorized, code: codeUnauthenticated, message: msg}
}

func errForbidden(msg string) *apiError {
	return &apiError{status: http.StatusForbidden, code: codeForbidden, message: msg}
}

func errNotFound(msg string) *apiError {
	return &apiError{status: http.StatusNotFound, code: codeNotFound, message: msg}
}

func errStateConflict(msg string) *apiError {
	return &apiError{status: http.StatusConflict, code: codeStateConflict, message: msg}
}

// errLegacyNoHD is the api.md C-table 409 LEGACY_NO_HD returned by B8
// when the group's chaincode is NULL: HD applies only to groups created
// post-address-derivation rollout; legacy groups remain single-address
// and non-HD (docs/design/mcp/address-derivation.md §F5/§8).
func errLegacyNoHD() *apiError {
	return &apiError{
		status:  http.StatusConflict,
		code:    codeLegacyNoHD,
		message: "group predates HD; multi-group remains the multi-address path",
	}
}

// errExpectedMemberMismatch is the api.md C-table 409 EXPECTED_MEMBER_MISMATCH
// returned by B9/B10/B11 when an identity is not present in
// coord.external.expected_members for the target group (distributed-mpc R3
// strict-set, prevents self-join). Message is operator-safe — never leaks
// which key was rejected (api.md:79).
func errExpectedMemberMismatch(msg string) *apiError {
	return &apiError{status: http.StatusConflict, code: codeExpectedMember, message: msg}
}

func errExpired(msg string) *apiError {
	return &apiError{status: http.StatusGone, code: codeExpired, message: msg}
}

func errRateLimited(msg string) *apiError {
	return &apiError{status: http.StatusTooManyRequests, code: codeRateLimited, message: msg}
}

// errLocked is the fail-closed response when the coord store is LOCKED. Per
// docs/design/contract/api.md:81-84 and group-provisioning.md §9 every A/B/groups
// data endpoint returns 503 LOCKED and leaks nothing.
func errLocked() *apiError {
	return &apiError{status: http.StatusServiceUnavailable, code: codeLocked, message: "coord store is locked"}
}

// errInternal returns a generic 5xx; the detailed cause is logged, never sent
// to the client (api.md:79 — message must not leak internals).
func errInternal() *apiError {
	return &apiError{status: http.StatusInternalServerError, code: codeInternal, message: "internal error"}
}

// asAPIError normalizes any error to an *apiError, defaulting to 500 INTERNAL
// so an unexpected error never leaks its Go message to the client.
func asAPIError(err error) *apiError {
	var ae *apiError
	if errors.As(err, &ae) {
		return ae
	}
	// A store relocked between the lockGate check and a DB op must still
	// fail-closed as 503 LOCKED, never leak as 500 (docs/design/server/server.md
	// C9b, api.md:81-84).
	if errors.Is(err, coorddb.ErrLocked) {
		return errLocked()
	}
	return errInternal()
}
