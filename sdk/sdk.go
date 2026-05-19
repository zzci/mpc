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

// KeyGen runs a t-of-n ECDSA threshold keygen; outcomes arrive via cb.
func (s *SDK) KeyGen(configJSON string, cb KeyGenCallback) {
	s.inner.KeyGen(configJSON, keyGenCB{cb})
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

// Sign runs the device-side WYSIWYS signing flow; returns a session handle.
func (s *SDK) Sign(startJSON string, cb SignCallback) *SignSession {
	return &SignSession{inner: s.inner.Sign(startJSON, signCB{cb})}
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

// Reshare redistributes the committee onto a new (t', n'); pubkey invariant.
func (s *SDK) Reshare(configJSON string, cb ReshareCallback) {
	s.inner.Reshare(configJSON, reshareCB{cb})
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

// OnWireMessage is the host→Go feed for a received MPC protocol message.
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
