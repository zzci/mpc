package relay

import (
	"testing"
	"time"
)

func TestParseBytesPerSec(t *testing.T) {
	cases := []struct {
		in   string
		want int64
		bad  bool
	}{
		{"", 0, false},
		{"1MiB/s", 1 << 20, false},
		{"512KiB/s", 512 << 10, false},
		{"2MB/s", 2_000_000, false},
		{"1000B/s", 1000, false},
		{"2097152", 2097152, false},
		{"1.5MiB/s", int64(1.5 * float64(1<<20)), false},
		{"10ZB/s", 0, true},
		{"-1MiB/s", 0, true},
		{"abc/s", 0, true},
	}
	for _, c := range cases {
		got, err := parseBytesPerSec(c.in)
		if c.bad {
			if err == nil {
				t.Errorf("%q: expected error", c.in)
			}
			continue
		}
		if err != nil {
			t.Errorf("%q: unexpected error %v", c.in, err)
			continue
		}
		if got != c.want {
			t.Errorf("%q: got %d want %d", c.in, got, c.want)
		}
	}
}

func TestParseCircuitDuration(t *testing.T) {
	cases := []struct {
		in      string
		want    time.Duration
		wantErr bool
	}{
		{"", 0, false},   // empty -> keep circuitv2 2m default
		{"  ", 0, false}, // blank -> default
		{"10m", 10 * time.Minute, false},
		{"90s", 90 * time.Second, false},
		{"1h30m", 90 * time.Minute, false},
		{"0s", 0, true},  // non-positive rejected
		{"-5m", 0, true}, // negative rejected
		{"abc", 0, true}, // unparseable rejected
	}
	for _, c := range cases {
		got, err := parseCircuitDuration(c.in)
		if c.wantErr {
			if err == nil {
				t.Errorf("parseCircuitDuration(%q): want error, got %v", c.in, got)
			}
			continue
		}
		if err != nil {
			t.Errorf("parseCircuitDuration(%q): unexpected error %v", c.in, err)
			continue
		}
		if got != c.want {
			t.Errorf("parseCircuitDuration(%q) = %v, want %v", c.in, got, c.want)
		}
	}
}

// TestCircuitDurationIsKeygenAware guards the security.md #10 / RA-001 P1-1
// intent: the configured default must comfortably exceed the libp2p circuitv2
// 2m reset window so a proof-enabled networked keygen is not reset mid-session.
func TestCircuitDurationIsKeygenAware(t *testing.T) {
	d, err := parseCircuitDuration("10m") // mirrors node config defaults()
	if err != nil {
		t.Fatal(err)
	}
	if d <= 2*time.Minute {
		t.Fatalf("keygen-aware cap must exceed the 2m circuitv2 default, got %v", d)
	}
}
