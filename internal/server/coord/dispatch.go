package coord

import (
	"sync"

	"github.com/zzci/mpc/internal/contract"
)

// dispatchHub delivers START to signer devices via the B6 long-poll channel
// (api.md:57). Push (FCM/APNs) is the preferred wake but needs external
// services; long-poll is the in-process-testable path and the contractual
// fallback. A START is buffered per (groupId, memberId) so a signer that polls
// slightly after dispatch still receives it.
type dispatchHub struct {
	mu      sync.Mutex
	pending map[string][]contract.StartSigning // key: groupID\x00memberID
	waiters map[string][]chan contract.StartSigning
}

func newDispatchHub() *dispatchHub {
	return &dispatchHub{
		pending: map[string][]contract.StartSigning{},
		waiters: map[string][]chan contract.StartSigning{},
	}
}

func dispatchKey(groupID, memberID string) string { return groupID + "\x00" + memberID }

// publish hands a START to each signer: it wakes a waiting long-poll if one is
// parked, otherwise buffers for the next poll.
func (h *dispatchHub) publish(groupID string, st contract.StartSigning) {
	h.mu.Lock()
	defer h.mu.Unlock()
	for _, mid := range st.Signers {
		k := dispatchKey(groupID, mid)
		if ws := h.waiters[k]; len(ws) > 0 {
			ws[0] <- st
			h.waiters[k] = ws[1:]
			continue
		}
		h.pending[k] = append(h.pending[k], st)
	}
}

// take returns a buffered START for the member if present (non-blocking).
func (h *dispatchHub) take(groupID, memberID string) (contract.StartSigning, bool) {
	h.mu.Lock()
	defer h.mu.Unlock()
	k := dispatchKey(groupID, memberID)
	if q := h.pending[k]; len(q) > 0 {
		st := q[0]
		h.pending[k] = q[1:]
		return st, true
	}
	return contract.StartSigning{}, false
}

// wait parks a long-poll waiter; the returned channel receives the next START
// for the member. cancel removes the waiter when the poll times out.
func (h *dispatchHub) wait(groupID, memberID string) (<-chan contract.StartSigning, func()) {
	ch := make(chan contract.StartSigning, 1)
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
