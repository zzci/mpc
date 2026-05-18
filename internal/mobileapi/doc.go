// Package mobileapi is the gomobile-friendly flat SDK surface
// (docs/design/mcp/sdk.md §2): every exported symbol uses only string / []byte /
// callback-interface / opaque-pointer types, so no tss-lib or other complex
// type is ever exported and there are no generics. The host obtains one *SDK
// and drives the whole MPC lifecycle through it: KeyGen / Sign / Reshare /
// OnWireMessage / ExportShare / ImportShare.
//
// Go→host notifications travel on the *Callback interfaces; the host→Go
// reverse direction (the human Approve/Reject decision) is *SignSession's
// methods, kept separate from the callback per DREV-001 D4-1.
package mobileapi
