// Package sdk is the gomobile bind target: a thin, NON-internal
// re-export of internal/mobileapi.
//
// gomobile bind generates a `gobind` glue package OUTSIDE this module; Go's
// internal/ visibility rule forbids that generated code from importing
// github.com/zzci/mpc/internal/mobileapi directly (CI error:
// "use of internal package ... not allowed"). This package lives at a public
// import path, wraps the audited internal/mobileapi surface 1:1 with the
// same gomobile-flat signatures (string / []byte / error / callback
// interfaces / opaque pointers), and adds no logic — every call delegates.
// The cryptographic kernel stays in internal/mobileapi unchanged.
package sdk

import api "github.com/zzci/mpc/internal/mobileapi"

// SDK is the opaque device handle (wraps *internal/mobileapi.SDK).
type SDK struct{ inner *api.SDK }

// NewSDK opens (creating if absent) the device keystore at keystoreDir.
func NewSDK(keystoreDir string) (*SDK, error) {
	s, err := api.NewSDK(keystoreDir)
	if err != nil {
		return nil, err
	}
	return &SDK{inner: s}, nil
}

// --- WireCallbacks --------------------------------------------------------

// WireCallbacks mirrors internal/mobileapi.WireCallbacks: the host-supplied
// outbound bridge (Go→host) for one single-party MPC session
// (distributed-mpc-impl.md §B DM-3). The reverse direction (host→Go) is
// SDK.OnWireMessage.
type WireCallbacks interface {
	OnWireMessage(b []byte)
}

type wireCB struct{ cb WireCallbacks }

func (a wireCB) OnWireMessage(b []byte) { a.cb.OnWireMessage(b) }

// --- KeyGen ---------------------------------------------------------------

// KeyGenCallback mirrors internal/mobileapi.KeyGenCallback.
type KeyGenCallback interface {
	OnProgress(stage string)
	OnResult(summaryJSON string)
	OnError(code string, msg string)
}

type keyGenCB struct{ cb KeyGenCallback }

func (a keyGenCB) OnProgress(stage string)       { a.cb.OnProgress(stage) }
func (a keyGenCB) OnResult(summaryJSON string)   { a.cb.OnResult(summaryJSON) }
func (a keyGenCB) OnError(code string, m string) { a.cb.OnError(code, m) }

// KeyGen runs THIS device's share of a t-of-n ECDSA threshold keygen. The
// configJSON is the DM-3 hard-cut envelope (groupId, sessionID, partyIndex,
// n, t, memberSet, relay{peerID,addrs[]}, role, passphrase); wire is the
// host's outbound bridge (Go→host); outcomes arrive via cb.
func (s *SDK) KeyGen(configJSON string, wire WireCallbacks, cb KeyGenCallback) {
	s.inner.KeyGen(configJSON, wireCB{wire}, keyGenCB{cb})
}

// --- Sign -----------------------------------------------------------------

// SignCallback mirrors internal/mobileapi.SignCallback.
type SignCallback interface {
	OnDecoded(aFactsJSON string, bInfoJSON string, mismatchJSON string)
	OnResult(rsv []byte)
	OnError(code string, msg string)
}

type signCB struct{ cb SignCallback }

func (a signCB) OnDecoded(af string, bi string, mm string) { a.cb.OnDecoded(af, bi, mm) }
func (a signCB) OnResult(rsv []byte)                       { a.cb.OnResult(rsv) }
func (a signCB) OnError(code string, m string)             { a.cb.OnError(code, m) }

// SignSession is the opaque host→Go handle (wraps *internal SignSession).
type SignSession struct{ inner *api.SignSession }

// Approve records the human's approval (host→Go).
func (ss *SignSession) Approve() { ss.inner.Approve() }

// Reject records the human's rejection (host→Go).
func (ss *SignSession) Reject() { ss.inner.Reject() }

// Sign runs the device-side WYSIWYS signing flow on a background goroutine
// against the host-supplied wire transport. configJSON is the DM-3 envelope
// wrapping the coord-delivered StartSigning plus this device's session
// metadata; the returned SignSession is the host→Go Approve/Reject handle.
func (s *SDK) Sign(configJSON string, wire WireCallbacks, cb SignCallback) *SignSession {
	return &SignSession{inner: s.inner.Sign(configJSON, wireCB{wire}, signCB{cb})}
}

// --- Reshare --------------------------------------------------------------

// ReshareCallback mirrors internal/mobileapi.ReshareCallback.
type ReshareCallback interface {
	OnProgress(stage string)
	OnResult(summaryJSON string)
	OnError(code string, msg string)
}

type reshareCB struct{ cb ReshareCallback }

func (a reshareCB) OnProgress(stage string)       { a.cb.OnProgress(stage) }
func (a reshareCB) OnResult(summaryJSON string)   { a.cb.OnResult(summaryJSON) }
func (a reshareCB) OnError(code string, m string) { a.cb.OnError(code, m) }

// Reshare redistributes THIS device's share onto a new (t', n') committee;
// pubkey invariant. configJSON is the DM-3 envelope (groupId, sessionID,
// partyIndex, n, oldT, newT, memberSet, relay, role, passphrase).
func (s *SDK) Reshare(configJSON string, wire WireCallbacks, cb ReshareCallback) {
	s.inner.Reshare(configJSON, wireCB{wire}, reshareCB{cb})
}

// --- keystore + wire ------------------------------------------------------

// ExportShare produces a portable, passphrase-encrypted backup of one share.
func (s *SDK) ExportShare(moniker string, passphrase string) ([]byte, error) {
	return s.inner.ExportShare(moniker, passphrase)
}

// ImportShare restores a share from an ExportShare backup; returns its moniker.
func (s *SDK) ImportShare(blob []byte, passphrase string) (string, error) {
	return s.inner.ImportShare(blob, passphrase)
}

// OnWireMessage is the host→Go feed for a received MPC protocol message. The
// R5 gate (version + sessionId isolation) is enforced before the message is
// applied to the device's running single-party engine.
func (s *SDK) OnWireMessage(b []byte) error {
	return s.inner.OnWireMessage(b)
}

// --- FetchTransactions ----------------------------------------------------

// FetchTransactions queries transaction information through the coord member
// API for App listing/detail (does not enter MPC); 1:1 delegate of
// internal/mobileapi.FetchTransactions. See docs/design/mcp/sdk.md §2.1 for
// the reqJSON / result shape and the device-side A-zone recompute invariant.
func (s *SDK) FetchTransactions(reqJSON string) (string, error) {
	return s.inner.FetchTransactions(reqJSON)
}

// --- FetchXpub ------------------------------------------------------------

// FetchXpub pulls the HD extended public key for the caller's group through
// the coord member API (api.md B8); 1:1 delegate of
// internal/mobileapi.FetchXpub. See docs/design/mcp/address-derivation.md §7
// for the owning-member-only release contract and `wallet address <i>` for
// the offline derive flow that consumes the returned xpub.
func (s *SDK) FetchXpub(reqJSON string) (string, error) {
	return s.inner.FetchXpub(reqJSON)
}

// ListGroupsJSON returns a JSON document `{"items":[…]}` listing the
// groups this device has joined (user ruling 2026-05-18: multi-group =
// multi-address). Share material is not included; the metadata lets a
// host route subsequent KeyGen/Sign/Reshare calls against the right
// group via configJSON.GroupID.
func (s *SDK) ListGroupsJSON() (string, error) {
	return s.inner.ListGroupsJSON()
}
