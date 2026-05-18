package main

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/zzci/mpc/internal/node"
)

// swapRunners replaces the package role runners for the duration of a test and
// restores them afterwards.
func swapRunners(t *testing.T, relayFn, coordFn func(context.Context, node.Config) error) {
	t.Helper()
	origR, origC := runRelayFn, runCoordFn
	runRelayFn, runCoordFn = relayFn, coordFn
	t.Cleanup(func() { runRelayFn, runCoordFn = origR, origC })
}

func dualRoleCfg() node.Config {
	return node.Config{
		Relay: node.RelayConfig{Enable: true},
		Coord: node.CoordConfig{Enable: true},
	}
}

// TestRunDualRoleStartsBothConcurrently is the FIX-002 regression: with
// relay.enable && coord.enable, the old sequential dispatch ran runRelay first
// (which blocks until ctx is done) so runCoord was never reached. Both runners
// here block on ctx like the real relay.Run / coord.Start; the coord runner
// must observe that it started even while relay is still blocking.
func TestRunDualRoleStartsBothConcurrently(t *testing.T) {
	var relayStarted, coordStarted atomic.Bool
	block := func(started *atomic.Bool) func(context.Context, node.Config) error {
		return func(ctx context.Context, _ node.Config) error {
			started.Store(true)
			<-ctx.Done()
			return nil
		}
	}
	swapRunners(t, block(&relayStarted), block(&coordStarted))

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- run(ctx, dualRoleCfg()) }()

	// Both roles must be running concurrently. Before the fix coordStarted
	// would stay false forever because runRelay blocked the single thread of
	// dispatch.
	deadline := time.After(2 * time.Second)
	for !relayStarted.Load() || !coordStarted.Load() {
		select {
		case <-deadline:
			t.Fatalf("both roles did not start concurrently: relay=%v coord=%v",
				relayStarted.Load(), coordStarted.Load())
		case <-time.After(5 * time.Millisecond):
		}
	}

	// A signal (ctx cancel) must unblock both for a graceful, error-free stop.
	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("graceful dual-role shutdown returned error: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("run did not return after context cancel (a role ignored ctx)")
	}
}

// TestRunDualRoleEitherErrorFails: when one role fails, the other (blocking on
// ctx) is cancelled and the failure propagates out of run.
func TestRunDualRoleEitherErrorFails(t *testing.T) {
	wantErr := errors.New("relay boom")
	var coordStopped atomic.Bool
	swapRunners(t,
		func(_ context.Context, _ node.Config) error { return wantErr },
		func(ctx context.Context, _ node.Config) error {
			<-ctx.Done()
			coordStopped.Store(true)
			return nil
		},
	)

	done := make(chan error, 1)
	go func() { done <- run(context.Background(), dualRoleCfg()) }()

	select {
	case err := <-done:
		if !errors.Is(err, wantErr) {
			t.Fatalf("want relay error wrapped, got %v", err)
		}
		if !coordStopped.Load() {
			t.Fatal("coord was not cancelled when relay failed")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("run did not fail-fast when a role errored")
	}
}

// TestRunSingleRoleUnchanged: a single enabled role runs on ctx and the other
// runner is never invoked (behavior identical to before FIX-002).
func TestRunSingleRoleUnchanged(t *testing.T) {
	cases := []struct {
		name string
		cfg  node.Config
	}{
		{"relay-only", node.Config{Relay: node.RelayConfig{Enable: true}}},
		{"coord-only", node.Config{Coord: node.CoordConfig{Enable: true}}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var relayCalled, coordCalled atomic.Bool
			swapRunners(t,
				func(ctx context.Context, _ node.Config) error {
					relayCalled.Store(true)
					<-ctx.Done()
					return nil
				},
				func(ctx context.Context, _ node.Config) error {
					coordCalled.Store(true)
					<-ctx.Done()
					return nil
				},
			)

			ctx, cancel := context.WithCancel(context.Background())
			done := make(chan error, 1)
			go func() { done <- run(ctx, tc.cfg) }()

			time.Sleep(50 * time.Millisecond)
			cancel()
			select {
			case err := <-done:
				if err != nil {
					t.Fatalf("single-role run returned error: %v", err)
				}
			case <-time.After(2 * time.Second):
				t.Fatal("single-role run did not return after ctx cancel")
			}

			if tc.cfg.Relay.Enable && (!relayCalled.Load() || coordCalled.Load()) {
				t.Fatalf("relay-only: relay=%v coord=%v", relayCalled.Load(), coordCalled.Load())
			}
			if tc.cfg.Coord.Enable && (!coordCalled.Load() || relayCalled.Load()) {
				t.Fatalf("coord-only: coord=%v relay=%v", coordCalled.Load(), relayCalled.Load())
			}
		})
	}
}
