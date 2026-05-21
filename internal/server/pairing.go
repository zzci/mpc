package server

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"sort"
	"sync"
	"time"
)

// PairingTicket is one outstanding device-enrollment slot. The operator
// (admin-api) creates it; a new device consumes it by POSTing its identity
// pubkey to coord's public enrollment endpoint. Tokens are short-lived,
// single-use, and never sent over the wire as anything other than a path
// segment (no body / no header echo).
type PairingTicket struct {
	Token     string     // 64-hex (32 random bytes); path-safe
	GroupID   string     // optional — when set, enrollment auto-adds the device to this group
	Label     string     // free-form operator note (e.g. "Alice's iPhone")
	CreatedAt time.Time  // wall clock at create
	ExpiresAt time.Time  // wall clock at expiry; consume past this is refused
	UsedAt    *time.Time // first-and-only consume time; nil while pending
	UsedBy    string     // identity pubkey hex of the device that consumed
}

// PairingStore is an in-memory, process-lifetime pairing-token table.
// Persistence is intentionally NOT required: tokens are short-lived
// (default 10 minutes), losing them on restart is harmless — the operator
// simply mints fresh ones. Concurrency-safe.
type PairingStore struct {
	mu  sync.Mutex
	m   map[string]*PairingTicket
	now func() time.Time
}

// NewPairingStore returns a fresh store. now is injectable for tests; pass
// time.Now in production.
func NewPairingStore(now func() time.Time) *PairingStore {
	if now == nil {
		now = time.Now
	}
	return &PairingStore{m: make(map[string]*PairingTicket), now: now}
}

// Create mints a new token. ttl must be positive; label/groupID may be empty.
// The returned ticket is a copy — callers may not mutate the store via it.
func (s *PairingStore) Create(groupID, label string, ttl time.Duration) (PairingTicket, error) {
	if ttl <= 0 {
		return PairingTicket{}, fmt.Errorf("pairing: ttl must be positive")
	}
	var b [32]byte
	if _, err := rand.Read(b[:]); err != nil {
		return PairingTicket{}, fmt.Errorf("pairing: random: %w", err)
	}
	tok := hex.EncodeToString(b[:])
	now := s.now()
	t := &PairingTicket{
		Token:     tok,
		GroupID:   groupID,
		Label:     label,
		CreatedAt: now,
		ExpiresAt: now.Add(ttl),
	}
	s.mu.Lock()
	s.m[tok] = t
	s.mu.Unlock()
	return *t, nil
}

// Get returns a copy of the ticket and whether it exists. Stale (expired,
// but not yet GCd) tickets are returned with ok=true so the caller can
// surface a precise "expired" diagnostic instead of "unknown".
func (s *PairingStore) Get(token string) (PairingTicket, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	t, ok := s.m[token]
	if !ok {
		return PairingTicket{}, false
	}
	return *t, true
}

// Consume marks the token as used by identityHex. It returns the ticket
// (with UsedAt/UsedBy populated) on success, or a precise error:
//   - ErrPairingUnknown    — token does not exist
//   - ErrPairingExpired    — token expired
//   - ErrPairingUsed       — token already consumed (replay)
//
// On success the ticket is left in the store (UsedAt set) so subsequent
// look-ups can audit who claimed it; subsequent Consume calls return
// ErrPairingUsed.
func (s *PairingStore) Consume(token, identityHex string) (PairingTicket, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	t, ok := s.m[token]
	if !ok {
		return PairingTicket{}, ErrPairingUnknown
	}
	now := s.now()
	if t.UsedAt != nil {
		return *t, ErrPairingUsed
	}
	if !now.Before(t.ExpiresAt) {
		return *t, ErrPairingExpired
	}
	t.UsedAt = &now
	t.UsedBy = identityHex
	return *t, nil
}

// Delete revokes a token. Returns true if a ticket existed (regardless of
// pending/used state); false if the token was unknown. Removing a used
// ticket erases the audit trail — admin UI should warn before doing so.
func (s *PairingStore) Delete(token string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.m[token]; !ok {
		return false
	}
	delete(s.m, token)
	return true
}

// List returns a snapshot of all tickets, newest-first. Used by the admin
// pairing index page; the caller may not mutate the slice elements.
func (s *PairingStore) List() []PairingTicket {
	s.mu.Lock()
	out := make([]PairingTicket, 0, len(s.m))
	for _, t := range s.m {
		out = append(out, *t)
	}
	s.mu.Unlock()
	sort.Slice(out, func(i, j int) bool { return out[i].CreatedAt.After(out[j].CreatedAt) })
	return out
}

// GC removes tickets that are both expired AND not used. Used tickets are
// kept (audit). A caller running this periodically (e.g. once an hour)
// bounds memory growth without losing recent enrollment history.
func (s *PairingStore) GC() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	n := 0
	now := s.now()
	for k, t := range s.m {
		if t.UsedAt == nil && !now.Before(t.ExpiresAt) {
			delete(s.m, k)
			n++
		}
	}
	return n
}

// Pairing-domain errors. Wrappers (e.g. coord enroll handler) map these
// to HTTP statuses.
var (
	ErrPairingUnknown = pairingErr("pairing: unknown token")
	ErrPairingExpired = pairingErr("pairing: token expired")
	ErrPairingUsed    = pairingErr("pairing: token already consumed")
)

type pairingErr string

func (e pairingErr) Error() string { return string(e) }
