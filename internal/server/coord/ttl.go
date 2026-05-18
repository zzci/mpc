package coord

import "time"

// TTL / expiry is a first-class concern (docs/design/server/server.md C6). All time
// is UTC unix ms; comparisons apply the configured skew tolerance and judge
// conservatively outside it (C6(e)).

// isExpired reports whether expiryMs is past as of the coord clock, treating a
// request as still alive only while strictly before expiry minus skew slack
// (C6(e): outside tolerance, judge expired conservatively). This is the single
// predicate behind C6(a) (coord sweep -> EXPIRED) and C6(b) (re-checked before
// quorum dispatch).
func (c *Coord) isExpired(expiryMs int64) bool {
	now := unixMillis(c.clock.Now())
	skew := c.cfg.SkewTolerance.Milliseconds()
	return now >= expiryMs-skew
}

// remainingTTL returns the seconds left before expiry, never negative. It is
// echoed on B3/B6 responses (C6(c)) so a client can decide whether initiating
// is still worthwhile.
func (c *Coord) remainingTTL(expiryMs int64) int64 {
	now := unixMillis(c.clock.Now())
	left := (expiryMs - now) / 1000
	if left < 0 {
		return 0
	}
	return left
}

// dispatchDeadline clamps the configured dispatch timeout to the remaining TTL
// (docs/design/server/server.md C5: dispatch timeout must be < remaining TTL) and
// returns the absolute deadline plus its duration from now.
func (c *Coord) dispatchDeadline(expiryMs int64) (time.Time, time.Duration) {
	now := c.clock.Now()
	ttlLeft := time.Duration(expiryMs-unixMillis(now)) * time.Millisecond
	d := c.cfg.DispatchTimeout
	if ttlLeft < d {
		d = ttlLeft
	}
	if d < 0 {
		d = 0
	}
	return now.Add(d), d
}
