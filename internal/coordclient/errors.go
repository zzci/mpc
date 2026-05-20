package coordclient

import (
	"errors"
	"fmt"
)

// APIError is the typed form of the coord error envelope
// {error:{code,message,requestId?}} (docs/design/contract/api.md:79) together with
// the HTTP status. Handlers map the api.md C-table; the client surfaces the
// code so callers branch on the contract, not on HTTP numbers or messages.
type APIError struct {
	Status    int
	Code      string
	Message   string
	RequestID string
}

func (e *APIError) Error() string {
	if e.RequestID != "" {
		return fmt.Sprintf("coord %d %s: %s (requestId=%s)", e.Status, e.Code, e.Message, e.RequestID)
	}
	return fmt.Sprintf("coord %d %s: %s", e.Status, e.Code, e.Message)
}

// The api.md C-table codes (docs/design/contract/api.md:65-79). Callers compare
// APIError.Code or use errors.Is with the sentinels below.
const (
	CodeInvalidEnvelope = "INVALID_ENVELOPE"
	CodeUnauthenticated = "UNAUTHENTICATED"
	CodeForbidden       = "FORBIDDEN"
	CodeNotFound        = "NOT_FOUND"
	CodeStateConflict   = "STATE_CONFLICT"
	CodeLegacyNoHD      = "LEGACY_NO_HD"
	CodeExpired         = "EXPIRED"
	CodeRateLimited     = "RATE_LIMITED"
	CodeLocked          = "LOCKED"
	CodeInternal        = "INTERNAL"
)

// Sentinels for errors.Is. ErrLocked and ErrExpired carry contract meaning:
// per api.md:84 a client MUST treat 503 LOCKED as a backoff-retry signal, not
// a terminal failure; per api.md:50/97 an expired item yields 410 EXPIRED on
// decision/result and is terminal for that request.
var (
	// ErrLocked is the coord-store-locked condition (503 LOCKED). It is
	// retryable with backoff and never a terminal request outcome
	// (api.md:81-84).
	ErrLocked = errors.New("coordclient: coord store locked (503 LOCKED)")
	// ErrExpired is returned when the request TTL has elapsed (410 EXPIRED);
	// the request is terminal and MUST NOT be retried (api.md:50/74).
	ErrExpired = errors.New("coordclient: request expired (410 EXPIRED)")
	// ErrStateConflict is 409: the request is not in a state that allows the
	// operation (already terminal / already dispatched, api.md:73).
	ErrStateConflict = errors.New("coordclient: state conflict (409)")
	// ErrUnauthenticated is 401: member signature/auth rejected (api.md:71).
	ErrUnauthenticated = errors.New("coordclient: unauthenticated (401)")
	// ErrForbidden is 403: cross-group or unauthorized (api.md:72).
	ErrForbidden = errors.New("coordclient: forbidden (403)")
	// ErrNotFound is 404: requestId/group unknown (api.md:72).
	ErrNotFound = errors.New("coordclient: not found (404)")
	// ErrLegacyNoHD is 409 LEGACY_NO_HD (api.md C-table): the group predates
	// the address-derivation rollout and remains single-address /
	// non-HD; the caller MUST fall back to the multi-group path (F5,
	// address-derivation.md §8). Not retryable.
	ErrLegacyNoHD = errors.New("coordclient: legacy non-HD group (409 LEGACY_NO_HD)")
)

// Is bridges *APIError to the sentinels so callers can write
// errors.Is(err, coordclient.ErrLocked) regardless of how the error was wrapped.
func (e *APIError) Is(target error) bool {
	switch target {
	case ErrLocked:
		return e.Code == CodeLocked
	case ErrExpired:
		return e.Code == CodeExpired
	case ErrStateConflict:
		return e.Code == CodeStateConflict
	case ErrUnauthenticated:
		return e.Code == CodeUnauthenticated
	case ErrForbidden:
		return e.Code == CodeForbidden
	case ErrNotFound:
		return e.Code == CodeNotFound
	case ErrLegacyNoHD:
		return e.Code == CodeLegacyNoHD
	default:
		return false
	}
}

// retryable reports whether the error is a transient coord condition that a
// bounded backoff retry may clear: 503 LOCKED (api.md:84), 5xx INTERNAL
// (api.md:77 "retryable"), and 429 RATE_LIMITED (transient by definition).
// Terminal 4xx (envelope/auth/forbidden/not-found/conflict/expired) are not
// retried.
func retryable(e *APIError) bool {
	switch e.Code {
	case CodeLocked, CodeInternal, CodeRateLimited:
		return true
	default:
		return e.Status >= 500
	}
}
