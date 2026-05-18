// Package cli implements the P1/P2/P3 end-to-end acceptance carrier
// (docs/design/testing.md §3, docs/design/PLAN.md §3, docs/design/mcp/sdk.md §3): it
// orchestrates a multi-process 2-of-3 wallet running real tss-lib
// keygen/sign/reshare over libp2p Noise + pnet + circuit-relay v2 through a
// real node-relay subprocess, and drives the external-service envelope flow
// against the real coord role (it never participates in MPC). It reuses the
// already-merged internal/{mpc,transport,contract,txdecode} and
// internal/server/{relay,coord,coorddb} public surfaces; the only MPC
// orchestration it adds is the network message pump that internal/mpc keeps
// unexported. P0/P4 device/packaging gates are out of scope (see cmd/cli doc).
package cli
