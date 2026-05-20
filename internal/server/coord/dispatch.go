package coord

import (
	"sync"
)

// dispatchHub delivers events to member devices via the B6 long-poll channel
// (api.md:57). The single notification webhook is an out-of-band nudge
// (ruling 2026-05-19); long-poll is the authoritative in-process-testable
// path. An event is buffered per (groupId, memberId) so a signer that polls
// slightly after dispatch still receives it.
//
// distributed-mpc.md / impl.md §B DM-4 extends the hub from the original
// sign-START-only shape to a multi-event-type bus: sign-START / keygen-START
// / reshare-START / attestation-ACK. The payload is stored as any (a
// JSON-marshalable value) and surfaced verbatim by the B6 dispatch handler.
// Event-type discrimination is the responsibility of the payload struct
// itself (each new event carries a "type" JSON field); the hub fan-out
// path is type-agnostic. Pre-DM-4 sign-START used contract.StartSigning,
// whose JSON shape has no "type" field — clients accept it as-is, so the
// addition of new event types is non-breaking for that consumer.
type dispatchHub struct {
	mu      sync.Mutex
	pending map[string][]any // key: groupID\x00memberID
	waiters map[string][]chan any
}

func newDispatchHub() *dispatchHub {
	return &dispatchHub{
		pending: map[string][]any{},
		waiters: map[string][]chan any{},
	}
}

func dispatchKey(groupID, memberID string) string { return groupID + "\x00" + memberID }

// publish hands an event to each named recipient: it wakes a waiting
// long-poll if one is parked, otherwise buffers for the next poll.
// recipients is the explicit fan-out list (memberIds) — the hub does not
// rely on payload-internal recipient fields, so new event types (which may
// not have a Signers slice) work uniformly.
func (h *dispatchHub) publish(groupID string, recipients []string, payload any) {
	h.mu.Lock()
	defer h.mu.Unlock()
	for _, mid := range recipients {
		k := dispatchKey(groupID, mid)
		if ws := h.waiters[k]; len(ws) > 0 {
			ws[0] <- payload
			h.waiters[k] = ws[1:]
			continue
		}
		h.pending[k] = append(h.pending[k], payload)
	}
}

// take returns a buffered event for the member if present (non-blocking).
func (h *dispatchHub) take(groupID, memberID string) (any, bool) {
	h.mu.Lock()
	defer h.mu.Unlock()
	k := dispatchKey(groupID, memberID)
	if q := h.pending[k]; len(q) > 0 {
		st := q[0]
		h.pending[k] = q[1:]
		return st, true
	}
	return nil, false
}

// wait parks a long-poll waiter; the returned channel receives the next
// event for the member. cancel removes the waiter when the poll times out.
func (h *dispatchHub) wait(groupID, memberID string) (<-chan any, func()) {
	ch := make(chan any, 1)
	k := dispatchKey(groupID, memberID)
	h.mu.Lock()
	if q := h.pending[k]; len(q) > 0 {
		ch <- q[0]
		h.pending[k] = q[1:]
		h.mu.Unlock()
		return ch, func() {}
	}
	h.waiters[k] = append(h.waiters[k], ch)
	h.mu.Unlock()
	return ch, func() {
		h.mu.Lock()
		defer h.mu.Unlock()
		ws := h.waiters[k]
		for i, w := range ws {
			if w == ch {
				h.waiters[k] = append(ws[:i:i], ws[i+1:]...)
				break
			}
		}
	}
}

// resultHub wakes external-service A4 long-poll waiters when a request reaches
// a terminal state (api.md:29).
type resultHub struct {
	mu      sync.Mutex
	waiters map[string][]chan struct{}
}

func newResultHub() *resultHub {
	return &resultHub{waiters: map[string][]chan struct{}{}}
}

func (r *resultHub) wait(requestID string) (<-chan struct{}, func()) {
	ch := make(chan struct{}, 1)
	r.mu.Lock()
	r.waiters[requestID] = append(r.waiters[requestID], ch)
	r.mu.Unlock()
	return ch, func() {
		r.mu.Lock()
		defer r.mu.Unlock()
		ws := r.waiters[requestID]
		for i, w := range ws {
			if w == ch {
				r.waiters[requestID] = append(ws[:i:i], ws[i+1:]...)
				break
			}
		}
	}
}

func (r *resultHub) signal(requestID string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, w := range r.waiters[requestID] {
		select {
		case w <- struct{}{}:
		default:
		}
	}
	delete(r.waiters, requestID)
}
