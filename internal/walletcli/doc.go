// Package walletcli is the PC wallet-party front-end: a thin, long-lived
// command shell over the exact same github.com/zzci/mpc/sdk surface the
// mobile app drives through gomobile bind. It adds no cryptographic logic —
// every wallet operation delegates 1:1 to the audited SDK, so the PC party
// and a mobile party run the identical engine and have the identical
// capabilities (and the identical shared boundaries: the SDK holds the
// committee in memory for the process lifetime, so signing/resharing happen
// within the session that ran keygen/import, mirroring the RN host that
// keeps the SDK handle for the wallet's lifetime).
//
// It is deliberately separate from internal/cli (the P1/P2/P3 E2E acceptance
// carrier): that package and the `cli member` subcommand are untouched so
// the E2E/CI gates keep passing.
package walletcli
