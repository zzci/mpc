package relay

import (
	"fmt"
	"strconv"
	"strings"
	"time"
)

// bandwidth_per_conn (server.md R4 layer 3) is a human rate like "1MiB/s".
// circuit-relay v2 exposes only per-circuit caps (RelayLimit.Data bytes per
// direction, RelayLimit.Duration before reset) — not a token-bucket rate — so
// the configured rate is translated into a per-circuit Data budget over the
// limit Duration window: a circuit cannot move more than rate×duration bytes
// before it is reset, which preserves the anti-DoS bound. H-003 reviewed
// this and intentionally does NOT add an exact per-connection token-bucket:
// circuit-relay v2 offers only Data/Duration (no rate primitive), and the
// Data-over-Duration budget is the accepted bandwidth cap for threat
// model A. An empty value leaves circuitv2's default limit untouched.

var byteUnits = map[string]int64{
	"B":   1,
	"KB":  1_000,
	"MB":  1_000_000,
	"GB":  1_000_000_000,
	"KIB": 1 << 10,
	"MIB": 1 << 20,
	"GIB": 1 << 30,
}

// parseBytesPerSec parses values like "1MiB/s", "512KB/s", "2097152" (raw
// bytes/s). Returns 0 when s is empty (caller keeps the circuitv2 default).
func parseBytesPerSec(s string) (int64, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0, nil
	}
	s = strings.TrimSpace(strings.TrimSuffix(s, "/s"))

	num := strings.TrimRightFunc(s, func(r rune) bool {
		return (r >= 'A' && r <= 'Z') || (r >= 'a' && r <= 'z')
	})
	unit := strings.ToUpper(strings.TrimSpace(s[len(num):]))
	num = strings.TrimSpace(num)

	v, err := strconv.ParseFloat(num, 64)
	if err != nil {
		return 0, fmt.Errorf("relay: bad bandwidth value %q: %w", s, err)
	}
	if v < 0 {
		return 0, fmt.Errorf("relay: negative bandwidth %q", s)
	}
	mul := int64(1)
	if unit != "" {
		m, ok := byteUnits[unit]
		if !ok {
			return 0, fmt.Errorf("relay: unknown bandwidth unit %q", unit)
		}
		mul = m
	}
	return int64(v * float64(mul)), nil
}

// parseCircuitDuration parses the configured per-circuit max duration (Go
// duration, e.g. "10m"). Returns 0 when s is empty (caller keeps circuitv2's
// 2m default). security.md invariant #10 / RA-001 P1-1: a production networked
// keygen/reshare carries the multi-minute Paillier modulus/factor ZK proofs,
// so this cap is relaxed to fit a proof-enabled session within one circuit
// rather than dropping the proofs to fit the default 2m window.
func parseCircuitDuration(s string) (time.Duration, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0, nil
	}
	d, err := time.ParseDuration(s)
	if err != nil {
		return 0, fmt.Errorf("relay: bad circuit_max_duration %q: %w", s, err)
	}
	if d <= 0 {
		return 0, fmt.Errorf("relay: circuit_max_duration must be positive, got %q", s)
	}
	return d, nil
}
