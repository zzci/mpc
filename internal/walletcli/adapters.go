package walletcli

import (
	"encoding/hex"
	"io"
)

// outcome is the terminal result of an async SDK operation: exactly one of
// resultJSON / err-equivalent is meaningful, signalled once on done.
type outcome struct {
	ok      bool
	payload string // OnResult summary JSON, or hex(rsv) for signing
	code    string // OnError code when !ok
	msg     string // OnError message when !ok
}

// progressCB adapts SDK KeyGen/Reshare callbacks to the terminal: OnProgress
// lines go to the progress writer (stderr), and the single terminal callback
// is delivered on done so the calling command can block until completion.
type progressCB struct {
	progress io.Writer
	done     chan outcome
}

func newProgressCB(progress io.Writer) *progressCB {
	return &progressCB{progress: progress, done: make(chan outcome, 1)}
}

// OnProgress reports a coarse stage label.
func (c *progressCB) OnProgress(stage string) {
	wf(c.progress, "  .. %s\n", stage)
}

// OnResult delivers the terminal success summary JSON.
func (c *progressCB) OnResult(summaryJSON string) {
	c.done <- outcome{ok: true, payload: summaryJSON}
}

// OnError delivers the terminal failure as a stable {code,msg} pair.
func (c *progressCB) OnError(code string, msg string) {
	c.done <- outcome{code: code, msg: msg}
}

// signCB adapts the SDK SignCallback. OnDecoded surfaces the WYSIWYS decode
// to the operator and unblocks the approval prompt; OnResult/OnError are the
// single terminal signal.
type signCB struct {
	progress io.Writer
	decoded  chan signDecode
	done     chan outcome
}

// signDecode carries the SDK's decoded view of the request for human review.
type signDecode struct {
	aFactsJSON   string
	bInfoJSON    string
	mismatchJSON string
}

func newSignCB(progress io.Writer) *signCB {
	return &signCB{
		progress: progress,
		decoded:  make(chan signDecode, 1),
		done:     make(chan outcome, 1),
	}
}

// OnDecoded reports the device-recomputed A-facts, business info and any
// A/B mismatch for the operator to review before approving.
func (c *signCB) OnDecoded(aFactsJSON string, bInfoJSON string, mismatchJSON string) {
	c.decoded <- signDecode{aFactsJSON: aFactsJSON, bInfoJSON: bInfoJSON, mismatchJSON: mismatchJSON}
}

// OnResult delivers the produced 65-byte compact {V||R||S} signature.
func (c *signCB) OnResult(rsv []byte) {
	c.done <- outcome{ok: true, payload: hex.EncodeToString(rsv)}
}

// OnError delivers the terminal failure as a stable {code,msg} pair.
func (c *signCB) OnError(code string, msg string) {
	c.done <- outcome{code: code, msg: msg}
}
