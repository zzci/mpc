// Command node is the single executable entrypoint of mcp-wallet. The
// relay / coord roles are selected by the config switches relay.enable /
// coord.enable (node.yaml + TSSNODE_ env overrides); either or both may
// run. Not a subcommand, not a --role flag.
//
// Trust boundary (docs/design/server/server.md): co-locating in one
// process does not weaken the relay's cryptographic zero-trust — Noise
// is end-to-end and not terminated at the relay; coord's plaintext
// envelope enters via a separate path ("external service → coord API"),
// not via relay forwarding; the two data paths are logically isolated
// in-process. The relay role is stateless, holds no shares, cannot read
// MPC content; coord does not participate in MPC and holds no shares.
//
// The role bodies are implemented by N-002 (relay) / X-001 (coord);
// this file only does config load, validation and role dispatch.
package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"golang.org/x/sync/errgroup"

	"github.com/zzci/mpc/internal/node"
)

func main() {
	cfg, err := node.Load()
	if err != nil {
		fmt.Fprintln(os.Stderr, "node: load config:", err)
		os.Exit(1)
	}
	if err := cfg.Validate(); err != nil {
		fmt.Fprintln(os.Stderr, "node: invalid config:", err)
		os.Exit(1)
	}

	// One signal context is shared by every enabled role so SIGINT/SIGTERM
	// shuts them all down gracefully together. Roles run concurrently — the
	// earlier sequential dispatch let runRelay block forever, so the coord
	// role never started in the documented relay+coord dual-role deployment.
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	if err := run(ctx, cfg); err != nil {
		fmt.Fprintln(os.Stderr, "node:", err)
		os.Exit(1)
	}
}

// runRelayFn / runCoordFn indirect the role bodies so the dispatch logic in
// run can be unit-tested deterministically without standing up a libp2p host
// or a coord store (the FIX-002 defect was purely in dispatch sequencing).
var (
	runRelayFn = runRelay
	runCoordFn = runCoord
)

// run dispatches the enabled roles. With both enabled they run concurrently in
// an errgroup over a shared context: the first role to fail cancels the other
// (either-error → the whole node fails), and a signal unblocks both for a
// graceful stop. Single-role behavior is unchanged (the role just runs on ctx).
// The relay↔coord trust boundary is unaffected — each role still reads only its
// own config subtree and the two never cross-import (see this file's header).
func run(ctx context.Context, cfg node.Config) error {
	switch {
	case cfg.Relay.Enable && cfg.Coord.Enable:
		g, gctx := errgroup.WithContext(ctx)
		g.Go(func() error {
			if err := runRelayFn(gctx, cfg); err != nil {
				return fmt.Errorf("relay: %w", err)
			}
			return nil
		})
		g.Go(func() error {
			if err := runCoordFn(gctx, cfg); err != nil {
				return fmt.Errorf("coord: %w", err)
			}
			return nil
		})
		return g.Wait()
	case cfg.Relay.Enable:
		if err := runRelayFn(ctx, cfg); err != nil {
			return fmt.Errorf("relay: %w", err)
		}
	case cfg.Coord.Enable:
		if err := runCoordFn(ctx, cfg); err != nil {
			return fmt.Errorf("coord: %w", err)
		}
	}
	return nil
}
