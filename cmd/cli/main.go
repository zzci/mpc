// Command cli is the P1/P2/P3 end-to-end acceptance carrier (docs/design/testing.md
// §3, docs/design/PLAN.md §3): a multi-process 2-of-3 wallet that runs real
// keygen/sign/reshare over libp2p Noise + circuit-relay v2, with the relay run
// as the node binary's relay role and a coord role driving the envelope flow.
//
// Subcommand `member <config.json>` runs ONE device subprocess: the E2E test
// (internal/cli) spawns N of these plus a node-relay subprocess, then asserts
// the cryptographic and protocol outcomes. P0/P4 device/packaging gates
// (gomobile .aar/.xcframework, RN bridge, real hardware) are explicitly OUT OF
// SCOPE here — they need a mobile toolchain unavailable in this environment
// and are owned by B-003/B-004/B-006 (docs/design/P0-report.md), per
// docs/design/testing.md §5.
//
// Any other invocation is the PC wallet party (internal/walletcli): an
// interactive session over the same github.com/zzci/mpc/sdk the mobile app
// drives, giving a PC the identical member capabilities and boundaries.
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/zzci/mpc/internal/cli"
	"github.com/zzci/mpc/internal/walletcli"
)

func main() {
	if len(os.Args) >= 2 && os.Args[1] == "member" {
		if len(os.Args) != 3 {
			fmt.Fprintln(os.Stderr, "usage: cli member <config.json>")
			os.Exit(2)
		}
		raw, err := os.ReadFile(os.Args[2])
		if err != nil {
			fmt.Fprintln(os.Stderr, "cli member: read config:", err)
			os.Exit(1)
		}
		var cfg cli.DeviceConfig
		if err := json.Unmarshal(raw, &cfg); err != nil {
			fmt.Fprintln(os.Stderr, "cli member: parse config:", err)
			os.Exit(1)
		}
		ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
		defer stop()
		cli.RunDevice(ctx, cfg)
		return
	}
	// Non-`member` invocations are the PC wallet party: a long-lived shell
	// over the same github.com/zzci/mpc/sdk the mobile app drives. The
	// `member` E2E carrier above is intentionally left untouched.
	os.Exit(walletcli.Run(os.Args[1:]))
}
