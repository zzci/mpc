package keystore

import (
	"errors"
	"strings"
	"unicode"
)

// minPassphraseLen is the length floor for a passphrase that seals new data.
// It is one clause of the policy, not the whole of it; see validatePassphrase.
const minPassphraseLen = 12

// ErrWeakPassphrase is returned when a passphrase offered for sealing fails
// the strength policy. It is enforced only on the write path (Store.Save,
// ExportShare): opening pre-existing data never applies the policy, so an
// older blob — or a deliberately wrong passphrase — still surfaces as
// ErrDecrypt rather than this error, which would otherwise act as an oracle.
var ErrWeakPassphrase = errors.New("keystore: passphrase too weak: need >= 12 chars, varied, not a common phrase")

// commonWeakBases are lowercased substrings that mark a passphrase as
// guessable regardless of length. Kept deliberately small and dependency-free
// (no go.mod change): the goal is to reject the obvious, not to substitute for
// a full breach-corpus check, which is out of scope for a leaf module.
var commonWeakBases = []string{
	"password", "passphrase", "qwerty", "letmein", "iloveyou",
	"admin", "secret", "welcome", "abcabc", "111111", "123456",
}

// validatePassphrase enforces the weak-passphrase rejection policy applied
// before any new shard is sealed. A passphrase passes when it is long enough,
// draws on enough distinct characters to rule out trivial repetition or
// sequences, and embeds no well-known weak base. A canonical
// "correct horse battery staple" style passphrase passes; "pw",
// "aaaaaaaaaaaa" and "password1234" do not.
func validatePassphrase(p string) error {
	if len(p) < minPassphraseLen {
		return ErrWeakPassphrase
	}
	uniq := make(map[rune]struct{}, len(p))
	var hasLower, hasUpper, hasDigit, hasOther bool
	for _, r := range p {
		uniq[r] = struct{}{}
		switch {
		case unicode.IsLower(r):
			hasLower = true
		case unicode.IsUpper(r):
			hasUpper = true
		case unicode.IsDigit(r):
			hasDigit = true
		default:
			hasOther = true
		}
	}
	classes := 0
	for _, ok := range []bool{hasLower, hasUpper, hasDigit, hasOther} {
		if ok {
			classes++
		}
	}
	// Enough distinct runes rules out "aaaaaaaaaaaa" / "121212121212".
	if len(uniq) < 8 {
		return ErrWeakPassphrase
	}
	// Require either >= 2 character classes or a long (passphrase-style)
	// secret: this accepts an all-lowercase word string only when it is
	// long enough to carry the entropy itself.
	if classes < 2 && len(p) < 20 {
		return ErrWeakPassphrase
	}
	low := strings.ToLower(p)
	for _, base := range commonWeakBases {
		if strings.Contains(low, base) {
			return ErrWeakPassphrase
		}
	}
	return nil
}
