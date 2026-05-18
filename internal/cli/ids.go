package cli

import (
	"fmt"
	"math/big"
	"sort"

	"github.com/bnb-chain/tss-lib/v3/ecdsa/keygen"
	"github.com/bnb-chain/tss-lib/v3/tss"
)

// Cross-process tss PartyID derivation.
//
// tss.GenerateTestPartyIDs draws a random base key, so two processes never
// agree on the party set — unusable for a multi-process carrier. Instead every
// device derives the SAME ordered party set from public, deterministic inputs:
// keygen uses fixed small keys 1..n; signing/resharing reuse the keygen
// ShareIDs carried (publicly) in every save-data's Ks vector. A device then
// locates itself by matching its own Id / ShareID. This mirrors how
// internal/mpc builds its in-process party sets, just made identical across
// processes (its helpers are unexported, so the carrier rebuilds them).

// partyID is the stable wire identity of a device within a session: the tss
// PartyID.Id string, which equals the 1-based device index as decimal text.
func partyTag(index int) string { return fmt.Sprintf("%d", index+1) }

// keygenParties builds the deterministic keygen party set for n devices: party
// i carries Id/Moniker = i+1 and secret-sharing key = i+1 (distinct, nonzero).
// SortPartyIDs assigns Index; the order is stable because the keys are 1..n.
func keygenParties(n int) tss.SortedPartyIDs {
	un := make(tss.UnSortedPartyIDs, n)
	for i := 0; i < n; i++ {
		tag := partyTag(i)
		un[i] = tss.NewPartyID(tag, tag, big.NewInt(int64(i+1)))
	}
	return tss.SortPartyIDs(un)
}

// findSelf returns the PartyID in pids whose Id == tag, or an error.
func findSelf(pids tss.SortedPartyIDs, tag string) (*tss.PartyID, error) {
	for _, p := range pids {
		if p.Id == tag {
			return p, nil
		}
	}
	return nil, fmt.Errorf("cli: party %q not in derived set", tag)
}

// signingParties rebuilds the signer party set for the given participating
// device indices, deterministically across all signers. Every keygen share's
// Ks vector publicly lists all devices' share IDs in keygen-sorted order, so a
// signer that knows the participant index set can reconstruct every signer's
// PartyID (Id/Moniker = idx+1, key = Ks[idx]) without contacting the others.
func signingParties(sd *keygen.LocalPartySaveData, participants []int) (tss.SortedPartyIDs, error) {
	if len(sd.Ks) == 0 {
		return nil, fmt.Errorf("cli: save data has no Ks vector")
	}
	idx := append([]int(nil), participants...)
	sort.Ints(idx)
	un := make(tss.UnSortedPartyIDs, 0, len(idx))
	for _, j := range idx {
		if j < 0 || j >= len(sd.Ks) {
			return nil, fmt.Errorf("cli: signer index %d out of range (Ks=%d)", j, len(sd.Ks))
		}
		tag := partyTag(j)
		un = append(un, tss.NewPartyID(tag, tag, sd.Ks[j]))
	}
	return tss.SortPartyIDs(un), nil
}

// newReshareParties is the deterministic new committee for resharing. Its keys
// are offset (n+1 .. 2n) so they never collide with the old committee's keygen
// ShareIDs (1 .. n), keeping the two committees unambiguous on the wire.
func newReshareParties(n int) tss.SortedPartyIDs {
	un := make(tss.UnSortedPartyIDs, n)
	for i := 0; i < n; i++ {
		tag := partyTag(i)
		un[i] = tss.NewPartyID(tag, tag, big.NewInt(int64(i+1+n)))
	}
	return tss.SortPartyIDs(un)
}

// oldReshareParties rebuilds the original committee for resharing from the Ks
// vector (all n original devices participate as the old committee here), each
// keyed by its keygen ShareID so old-committee identities match the shares.
func oldReshareParties(sd *keygen.LocalPartySaveData) (tss.SortedPartyIDs, error) {
	if len(sd.Ks) == 0 {
		return nil, fmt.Errorf("cli: save data has no Ks vector")
	}
	un := make(tss.UnSortedPartyIDs, len(sd.Ks))
	for i := range sd.Ks {
		tag := partyTag(i)
		un[i] = tss.NewPartyID(tag, tag, sd.Ks[i])
	}
	return tss.SortPartyIDs(un), nil
}
