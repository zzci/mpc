package coord

import "time"

// Clock is the injectable time source. TTL/expiry is a first-class concern
// (docs/design/server/server.md C6) so every now-reading goes through Clock, which
// tests replace with a deterministic fake to exercise the (a)/(b) expiry gates
// without sleeping.
type Clock interface {
	Now() time.Time
}

type systemClock struct{}

func (systemClock) Now() time.Time { return time.Now().UTC() }

// unixMillis returns t as int64 unix milliseconds, the canonical envelope time
// unit (docs/spec/envelope-canonical.md §2.3 / protocol.md:18-19).
func unixMillis(t time.Time) int64 { return t.UnixMilli() }
