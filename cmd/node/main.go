// Command node 是 mcp-wallet 的单一可执行入口。relay / coord 两种角色由配置开关
// relay.enable / coord.enable 决定（node.yaml + TSSNODE_ 环境变量覆盖），可单开任一或双开；
// 非子命令、非 --role flag。
//
// 信任边界（docs/design/server/server.md）：合并同进程不削弱 relay 密码学零信任——
// Noise 端到端不在 relay 终结；coord 明文信封经「外部服务 → coord API」另一路径进入，
// 不经 relay 转发；两条数据路径进程内逻辑隔离。relay 角色无状态、不持分片、读不到 MPC
// 内容；coord 不参与 MPC、不持分片。
//
// 角色业务体由 N-002（relay）/ X-001（coord）实现；本文件仅做配置加载、校验与角色分发。
package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"golang.org/x/sync/errgroup"

	"github.com/royqta/mcp-wallet/internal/node"
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
