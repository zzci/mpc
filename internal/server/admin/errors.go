package admin

import (
	"errors"
	"net/http"

	"github.com/royqta/mcp-wallet/internal/server/coorddb"
)

// apiError carries an HTTP status, a stable code and an operator-safe message
// (the message MUST NOT leak internals or secrets). Every handler returns it
// on failure so the {error:{code,message}} envelope is produced in one place
// (same discipline as the coord package, docs/design/contract/api.md:79).
type apiError struct {
	status  int
	code    string
	message string
}

func (e *apiError) Error() string { return e.code + ": " + e.message }

const (
	codeBadRequest   = "BAD_REQUEST"
	codeUnauthorized = "UNAUTHENTICATED"
	codeForbidden    = "FORBIDDEN"
	codeNotFound     = "NOT_FOUND"
	codeRateLimited  = "RATE_LIMITED"
	codeLocked       = "LOCKED"
	codeInternal     = "INTERNAL"
)

func errBadRequest(msg string) *apiError {
	return &apiError{status: http.StatusBadRequest, code: codeBadRequest, message: msg}
}

func errUnauthorized(msg string) *apiError {
	return &apiError{status: http.StatusUnauthorized, code: codeUnauthorized, message: msg}
}

func errForbidden(msg string) *apiError {
	return &apiError{status: http.StatusForbidden, code: codeForbidden, message: msg}
}

func errNotFound(msg string) *apiError {
	return &apiError{status: http.StatusNotFound, code: codeNotFound, message: msg}
}

func errRateLimited(msg string) *apiError {
	return &apiError{status: http.StatusTooManyRequests, code: codeRateLimited, message: msg}
}

// errLocked is the fail-closed response when the coord store is LOCKED. Every
// data/control endpoint returns 503 LOCKED and leaks nothing; only unlock,
// lock-status and health are reachable while locked (admin.md §8,
// server.md C9b, api.md:81-84).
func errLocked() *apiError {
	return &apiError{status: http.StatusServiceUnavailable, code: codeLocked, message: "coord store is locked"}
}

func errInternal() *apiError {
	return &apiError{status: http.StatusInternalServerError, code: codeInternal, message: "internal error"}
}

// asAPIError normalizes any error to an *apiError. A store relocked between
// the lock gate and a DB op must still fail-closed as 503 LOCKED, never leak
// as 500 (server.md C9b). Unknown errors collapse to 500 INTERNAL so a Go
// error string never reaches the client.
func asAPIError(err error) *apiError {
	var ae *apiError
	if errors.As(err, &ae) {
		return ae
	}
	if errors.Is(err, coorddb.ErrLocked) {
		return errLocked()
	}
	return errInternal()
}
