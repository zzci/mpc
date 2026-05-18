package txdecode

import (
	"fmt"
	"math/big"
	"strings"

	"github.com/royqta/mcp-wallet/internal/contract"
)

// A/B declarative cross-check keys (businessInfo.displayHints). The check is
// declarative: only keys actually present are checked, and a present hint that
// the digest-bound A-zone cannot confirm is itself a prominent discrepancy.
// B is out-of-band (proposer-signed but not chain-binding); a mismatch is a
// loud human-review warning, never a hard rejection — only digest mismatch
// hard-rejects (docs/design/PLAN.md §3 信任边界, docs/design/mcp/sdk.md §4).
const (
	hintChain  = "expectChain"
	hintTo     = "expectTo"     // effective payee (token recipient if a token transfer)
	hintValue  = "expectValue"  // native value, base units (wei/sun), decimal
	hintAmount = "expectAmount" // token amount, base units, decimal
	hintToken  = "expectToken"  // token contract address
	hintMethod = "expectMethod" // call kind, e.g. "erc20-transfer"
	hintFrom   = "expectFrom"   // funds source (TRON owner / transferFrom from)
)

// crossCheckAB compares businessInfo.displayHints against the A-zone facts and
// returns the discrepancies; each is also appended as a prominent Facts
// warning so the human reviewer sees A/B divergence loudly.
func crossCheckAB(f *Facts, bi *contract.BusinessInfo) []Mismatch {
	if bi == nil || len(bi.DisplayHints) == 0 {
		return nil
	}
	hints := bi.DisplayHints
	var ms []Mismatch

	add := func(field, expected, actual string) {
		ms = append(ms, Mismatch{Field: field, Expected: expected, Actual: actual})
		f.warn(fmt.Sprintf("A/B MISMATCH: %s — businessInfo claims %q, decoded tx shows %q", field, expected, actual))
	}

	payee, tokenAmount, token := effectiveTransfer(f)

	if want, ok := hints[hintChain]; ok {
		if c, valid := normalizeChain(want); !valid || c != f.Chain {
			add("chain", want, string(f.Chain))
		}
	}
	if want, ok := hints[hintTo]; ok && !addrEqual(want, payee) {
		add("to", want, orAbsent(payee))
	}
	if want, ok := hints[hintFrom]; ok && !addrEqual(want, f.From) {
		add("from", want, orAbsent(f.From))
	}
	if want, ok := hints[hintToken]; ok && !addrEqual(want, token) {
		add("token", want, orAbsent(token))
	}
	if want, ok := hints[hintValue]; ok && !bigEqual(want, f.Value) {
		add("value", want, bigStr(f.Value))
	}
	if want, ok := hints[hintAmount]; ok && !bigEqual(want, tokenAmount) {
		add("amount", want, bigStr(tokenAmount))
	}
	if want, ok := hints[hintMethod]; ok {
		got := ""
		if f.Call != nil {
			got = string(f.Call.Kind)
		}
		if !strings.EqualFold(strings.TrimSpace(want), got) {
			add("method", want, orAbsent(got))
		}
	}
	return ms
}

// effectiveTransfer resolves the payee/amount/token a human cares about: for a
// recognized token transfer it is the decoded call's recipient/amount/token;
// otherwise the native To/Value.
func effectiveTransfer(f *Facts) (payee string, amount *big.Int, token string) {
	if f.Call != nil {
		switch f.Call.Kind {
		case CallERC20Transfer, CallERC20TransferFrom:
			return f.Call.Recipient, f.Call.Amount, f.Call.TokenContract
		case CallERC20Approve:
			return f.Call.Recipient, f.Call.Amount, f.Call.TokenContract
		}
	}
	return f.To, f.Value, ""
}

func addrEqual(want, got string) bool {
	return strings.EqualFold(strings.TrimSpace(want), strings.TrimSpace(got))
}

func bigEqual(want string, got *big.Int) bool {
	w, ok := new(big.Int).SetString(strings.TrimSpace(want), 10)
	if !ok || got == nil {
		return false
	}
	return w.Cmp(got) == 0
}

func bigStr(b *big.Int) string {
	if b == nil {
		return "(absent)"
	}
	return b.String()
}

func orAbsent(s string) string {
	if s == "" {
		return "(absent)"
	}
	return s
}
