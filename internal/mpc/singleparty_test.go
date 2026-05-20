package mpc

import (
	"context"
	"crypto/ecdsa"
	"crypto/rand"
	"math/big"
	"testing"
	"time"

	"github.com/bnb-chain/tss-lib/v3/tss"
)

// runSinglePartyKeygen wires n KeygenParty instances through in-memory
// channels: each party's outbound messages are wire-round-tripped (WireBytes
// then ParseWireMessage with the sender's PartyID, mirroring what a real
// transport does) and delivered to the addressed parties via Update. It
// returns the collected shares ordered by PartyIndex.
func runSinglePartyKeygen(t *testing.T, ctx context.Context, parties []*KeygenParty) []Share {
	t.Helper()
	for _, p := range parties {
		if err := p.Start(); err != nil {
			t.Fatalf("Start party %s: %v", p.PartyID().Id, err)
		}
	}
	for _, p := range parties {
		go forwardKeygenOutbound(t, ctx, p, parties)
	}
	shares := make([]Share, len(parties))
	for i, p := range parties {
		share, err := p.Done(ctx)
		if err != nil {
			t.Fatalf("Done party %s: %v", p.PartyID().Id, err)
		}
		shares[i] = share
	}
	return shares
}

func forwardKeygenOutbound(t *testing.T, ctx context.Context, src *KeygenParty, all []*KeygenParty) {
	t.Helper()
	for {
		select {
		case <-ctx.Done():
			return
		case msg, ok := <-src.Out():
			if !ok {
				return
			}
			deliverKeygenMessage(t, msg, src, all)
		}
	}
}

func deliverKeygenMessage(t *testing.T, msg tss.Message, src *KeygenParty, all []*KeygenParty) {
	t.Helper()
	bz, _, err := msg.WireBytes()
	if err != nil {
		t.Errorf("WireBytes from %s: %v", src.PartyID().Id, err)
		return
	}
	from := msg.GetFrom()
	dest := msg.GetTo()
	broadcast := dest == nil || msg.IsBroadcast()
	for _, p := range all {
		if p.PartyID().Id == src.PartyID().Id {
			continue
		}
		if !broadcast {
			matched := false
			for _, d := range dest {
				if d.Id == p.PartyID().Id {
					matched = true
					break
				}
			}
			if !matched {
				continue
			}
		}
		parsed, perr := tss.ParseWireMessage(bz, from, msg.IsBroadcast())
		if perr != nil {
			t.Errorf("ParseWireMessage to %s: %v", p.PartyID().Id, perr)
			return
		}
		if uerr := p.Update(parsed); uerr != nil {
			t.Errorf("Update %s: %v", p.PartyID().Id, uerr)
			return
		}
	}
}

func TestSinglePartyKeygen_ThreeWay(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()

	pre := loadTestPreParams(t, testParties)
	parties := make([]*KeygenParty, testParties)
	for i := 0; i < testParties; i++ {
		p, err := newKeygenPartyInternal(ctx, SinglePartyKeygenConfig{
			Threshold:  testThreshold,
			Parties:    testParties,
			PartyIndex: i,
			PreParams:  &pre[i],
		}, true)
		if err != nil {
			t.Fatalf("NewKeygenParty %d: %v", i, err)
		}
		parties[i] = p
	}

	shares := runSinglePartyKeygen(t, ctx, parties)

	// Every party converges on the same ECDSAPub, and each share is its
	// OWN share_i (Xi distinct across parties).
	var wantX, wantY *big.Int
	xiSeen := make(map[string]int)
	for i, sh := range shares {
		if sh.Moniker != partyTagFor(i) {
			t.Fatalf("share %d moniker %q != expected %q", i, sh.Moniker, partyTagFor(i))
		}
		sd, err := UnmarshalSaveData(sh.SaveData)
		if err != nil {
			t.Fatalf("Unmarshal share %d: %v", i, err)
		}
		if sd.Xi == nil || sd.Xi.Sign() == 0 {
			t.Fatalf("share %d Xi is zero/nil — not a valid own share", i)
		}
		key := sd.Xi.String()
		if prev, dup := xiSeen[key]; dup {
			t.Fatalf("share %d Xi collides with share %d", i, prev)
		}
		xiSeen[key] = i
		if i == 0 {
			wantX, wantY = sd.ECDSAPub.X(), sd.ECDSAPub.Y()
			continue
		}
		if sd.ECDSAPub.X().Cmp(wantX) != 0 || sd.ECDSAPub.Y().Cmp(wantY) != 0 {
			t.Fatalf("share %d public key disagrees with party 0", i)
		}
	}

	// Cross-API check: the all-n Sign path consumes these single-party
	// produced shares unchanged — proving the single-party API output is
	// wire-compatible with the existing share format.
	digest := make([]byte, digestLen)
	if _, err := rand.Read(digest); err != nil {
		t.Fatalf("rand digest: %v", err)
	}
	sig, err := Sign(ctx, SignConfig{
		SessionID: "single-keygen-xcheck",
		Threshold: testThreshold,
		Shares:    shares[:testThreshold+1],
		Digest:    digest,
	})
	if err != nil {
		t.Fatalf("Sign over single-party shares: %v", err)
	}
	pk := ecdsa.PublicKey{Curve: tss.S256(), X: wantX, Y: wantY}
	r := new(big.Int).SetBytes(sig.R[:])
	s := new(big.Int).SetBytes(sig.S[:])
	if !ecdsa.Verify(&pk, digest, r, s) {
		t.Fatal("ecdsa.Verify failed for single-party-generated shares")
	}
}

// runSinglePartySign wires n SignParty instances through in-memory channels.
func runSinglePartySign(t *testing.T, ctx context.Context, parties []*SignParty) []Signature {
	t.Helper()
	for _, p := range parties {
		if err := p.Start(); err != nil {
			t.Fatalf("Start signer %s: %v", p.PartyID().Id, err)
		}
	}
	for _, p := range parties {
		go forwardSignOutbound(t, ctx, p, parties)
	}
	sigs := make([]Signature, len(parties))
	for i, p := range parties {
		sig, err := p.Done(ctx)
		if err != nil {
			t.Fatalf("Done signer %s: %v", p.PartyID().Id, err)
		}
		sigs[i] = sig
	}
	return sigs
}

func forwardSignOutbound(t *testing.T, ctx context.Context, src *SignParty, all []*SignParty) {
	t.Helper()
	for {
		select {
		case <-ctx.Done():
			return
		case msg, ok := <-src.Out():
			if !ok {
				return
			}
			deliverSignMessage(t, msg, src, all)
		}
	}
}

func deliverSignMessage(t *testing.T, msg tss.Message, src *SignParty, all []*SignParty) {
	t.Helper()
	bz, _, err := msg.WireBytes()
	if err != nil {
		t.Errorf("WireBytes from %s: %v", src.PartyID().Id, err)
		return
	}
	from := msg.GetFrom()
	dest := msg.GetTo()
	broadcast := dest == nil || msg.IsBroadcast()
	for _, p := range all {
		if p.PartyID().Id == src.PartyID().Id {
			continue
		}
		if !broadcast {
			matched := false
			for _, d := range dest {
				if d.Id == p.PartyID().Id {
					matched = true
					break
				}
			}
			if !matched {
				continue
			}
		}
		parsed, perr := tss.ParseWireMessage(bz, from, msg.IsBroadcast())
		if perr != nil {
			t.Errorf("ParseWireMessage to %s: %v", p.PartyID().Id, perr)
			return
		}
		if uerr := p.Update(parsed); uerr != nil {
			t.Errorf("Update %s: %v", p.PartyID().Id, uerr)
			return
		}
	}
}

func TestSinglePartySign_TwoOfThree(t *testing.T) {
	shares, pubX, pubY := keygenShares(t)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	digest := make([]byte, digestLen)
	if _, err := rand.Read(digest); err != nil {
		t.Fatalf("rand digest: %v", err)
	}

	// Pick signers 0 and 2 (skip 1) — exercises the deterministic signer
	// rebuild from Ks for a non-contiguous participant set.
	participants := []int{0, 2}
	parties := make([]*SignParty, len(participants))
	for i, idx := range participants {
		p, err := NewSignParty(ctx, SinglePartySignConfig{
			SessionID:    "single-sign-test",
			Threshold:    testThreshold,
			PartyIndex:   idx,
			Participants: participants,
			Share:        shares[idx],
			Digest:       digest,
		})
		if err != nil {
			t.Fatalf("NewSignParty %d: %v", idx, err)
		}
		parties[i] = p
	}

	sigs := runSinglePartySign(t, ctx, parties)

	// All signers converge on the same signature and it verifies under
	// the group master key.
	for i := 1; i < len(sigs); i++ {
		if sigs[i] != sigs[0] {
			t.Fatalf("signer %d returned a divergent signature", i)
		}
	}
	pk := ecdsa.PublicKey{Curve: tss.S256(), X: pubX, Y: pubY}
	r := new(big.Int).SetBytes(sigs[0].R[:])
	s := new(big.Int).SetBytes(sigs[0].S[:])
	if !ecdsa.Verify(&pk, digest, r, s) {
		t.Fatal("ecdsa.Verify failed for single-party signatures")
	}
}

// runSinglePartyReshare wires n ReshareParty instances through in-memory
// channels. Same-device cross-committee delivery (own old -> own new) is
// applied in-process; everything else is wire-round-tripped against the
// SENDER's committee PartyID (resolved via msg.GetFrom().Id).
func runSinglePartyReshare(t *testing.T, ctx context.Context, parties []*ReshareParty) []Share {
	t.Helper()
	for _, p := range parties {
		if err := p.Start(); err != nil {
			t.Fatalf("Start reshare %s: %v", p.OldPartyID().Id, err)
		}
	}
	for _, p := range parties {
		go forwardReshareOutbound(t, ctx, p, parties)
	}
	shares := make([]Share, len(parties))
	for i, p := range parties {
		share, err := p.Done(ctx)
		if err != nil {
			t.Fatalf("Done reshare %d: %v", i, err)
		}
		shares[i] = share
	}
	return shares
}

func forwardReshareOutbound(t *testing.T, ctx context.Context, src *ReshareParty, all []*ReshareParty) {
	t.Helper()
	for {
		select {
		case <-ctx.Done():
			return
		case msg, ok := <-src.Out():
			if !ok {
				return
			}
			deliverReshareMessage(t, msg, src, all)
		}
	}
}

func deliverReshareMessage(t *testing.T, msg tss.Message, src *ReshareParty, all []*ReshareParty) {
	t.Helper()
	bz, _, err := msg.WireBytes()
	if err != nil {
		t.Errorf("WireBytes from %s: %v", msg.GetFrom().Id, err)
		return
	}
	from := msg.GetFrom()
	fromOld := samePID(from, src.OldPartyID())
	toOld := msg.IsToOldCommittee() || msg.IsToOldAndNewCommittees()
	toNew := !msg.IsToOldCommittee() || msg.IsToOldAndNewCommittees()
	broadcast := msg.IsBroadcast() || msg.GetTo() == nil
	for _, dst := range all {
		if toOld {
			peerOldID := dst.OldPartyID()
			if !samePID(peerOldID, from) && (broadcast || destIncludes(msg.GetTo(), peerOldID)) {
				deliverOne(t, dst.UpdateOld, bz, from, msg.IsBroadcast(), peerOldID.Id)
			}
		}
		if toNew {
			peerNewID := dst.NewPartyID()
			// Same-device cross-committee (own old -> own new) needs the
			// sender's old PID even on the in-process self path so the new
			// committee party recognises the sender.
			if !samePID(peerNewID, from) && (broadcast || destIncludes(msg.GetTo(), peerNewID)) {
				senderID := from
				if dst == src && !fromOld {
					senderID = src.NewPartyID()
				}
				deliverOne(t, dst.UpdateNew, bz, senderID, msg.IsBroadcast(), peerNewID.Id)
			}
		}
	}
}

func destIncludes(dest []*tss.PartyID, want *tss.PartyID) bool {
	for _, d := range dest {
		if samePID(d, want) {
			return true
		}
	}
	return false
}

func deliverOne(t *testing.T, apply func(tss.ParsedMessage) error, bz []byte, from *tss.PartyID, isB bool, targetID string) {
	t.Helper()
	parsed, perr := tss.ParseWireMessage(bz, from, isB)
	if perr != nil {
		t.Errorf("ParseWireMessage to %s: %v", targetID, perr)
		return
	}
	if uerr := apply(parsed); uerr != nil {
		t.Errorf("Update %s: %v", targetID, uerr)
	}
}

func TestSinglePartyReshare_ThreeToThree(t *testing.T) {
	old, wantX, wantY := makeOldCommittee(t, testThreshold, testParties)

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()

	pre := loadTestPreParams(t, testParties)
	parties := make([]*ReshareParty, testParties)
	for i := 0; i < testParties; i++ {
		p, err := newReshareInternal(ctx, SinglePartyReshareConfig{
			OldThreshold: testThreshold,
			NewThreshold: testThreshold,
			Parties:      testParties,
			PartyIndex:   i,
			OldShare:     old[i],
			PreParams:    &pre[i],
		}, true)
		if err != nil {
			t.Fatalf("NewReshareParty %d: %v", i, err)
		}
		parties[i] = p
	}

	newShares := runSinglePartyReshare(t, ctx, parties)

	// Public key invariant + share IDs rotated (new ShareIDs disjoint from old).
	oldIDs := make(map[string]bool)
	for _, sh := range old {
		sd, _ := UnmarshalSaveData(sh.SaveData)
		oldIDs[sd.ShareID.String()] = true
	}
	for i, sh := range newShares {
		sd, err := UnmarshalSaveData(sh.SaveData)
		if err != nil {
			t.Fatalf("Unmarshal new %d: %v", i, err)
		}
		if sd.ECDSAPub.X().Cmp(wantX) != 0 || sd.ECDSAPub.Y().Cmp(wantY) != 0 {
			t.Fatalf("reshared party %d public key drift", i)
		}
		if oldIDs[sd.ShareID.String()] {
			t.Fatalf("reshared party %d reuses old ShareID", i)
		}
	}

	// The refreshed committee can sign for the unchanged master key.
	signWithCommittee(t, newShares, testThreshold, wantX, wantY)
}

func TestSinglePartyKeygenRejectsBadParams(t *testing.T) {
	ctx := context.Background()
	cases := []struct {
		name string
		cfg  SinglePartyKeygenConfig
	}{
		{"too few parties", SinglePartyKeygenConfig{Threshold: 1, Parties: 1, PartyIndex: 0}},
		{"threshold not below n", SinglePartyKeygenConfig{Threshold: 3, Parties: 3, PartyIndex: 0}},
		{"threshold too low", SinglePartyKeygenConfig{Threshold: 0, Parties: 3, PartyIndex: 0}},
		{"party index negative", SinglePartyKeygenConfig{Threshold: 1, Parties: 3, PartyIndex: -1}},
		{"party index out of range", SinglePartyKeygenConfig{Threshold: 1, Parties: 3, PartyIndex: 3}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := NewKeygenParty(ctx, tc.cfg); err == nil {
				t.Fatalf("expected error for %s", tc.name)
			}
		})
	}
}

func TestSinglePartySignRejectsBadParams(t *testing.T) {
	shares, pubX, pubY := keygenShares(t)
	ctx := context.Background()
	pubKey := &ecdsa.PublicKey{Curve: tss.S256(), X: pubX, Y: pubY}
	cases := []struct {
		name string
		cfg  SinglePartySignConfig
	}{
		{"empty session id", SinglePartySignConfig{Threshold: 1, PartyIndex: 0, Participants: []int{0, 1}, Share: shares[0], Digest: make([]byte, 32)}},
		{"short digest", SinglePartySignConfig{SessionID: "s", Threshold: 1, PartyIndex: 0, Participants: []int{0, 1}, Share: shares[0], Digest: make([]byte, 31)}},
		{"threshold too low", SinglePartySignConfig{SessionID: "s", Threshold: 0, PartyIndex: 0, Participants: []int{0, 1}, Share: shares[0], Digest: make([]byte, 32)}},
		{"too few participants", SinglePartySignConfig{SessionID: "s", Threshold: 1, PartyIndex: 0, Participants: []int{0}, Share: shares[0], Digest: make([]byte, 32)}},
		{"self not in participants", SinglePartySignConfig{SessionID: "s", Threshold: 1, PartyIndex: 2, Participants: []int{0, 1}, Share: shares[2], Digest: make([]byte, 32)}},
		{
			"half-set kdd (delta nil)",
			SinglePartySignConfig{SessionID: "s", Threshold: 1, PartyIndex: 0, Participants: []int{0, 1}, Share: shares[0], Digest: make([]byte, 32), ChildPub: pubKey},
		},
		{
			"half-set kdd (childpub nil)",
			SinglePartySignConfig{SessionID: "s", Threshold: 1, PartyIndex: 0, Participants: []int{0, 1}, Share: shares[0], Digest: make([]byte, 32), KeyDerivationDelta: big.NewInt(1)},
		},
		{
			"corrupt share",
			SinglePartySignConfig{SessionID: "s", Threshold: 1, PartyIndex: 0, Participants: []int{0, 1}, Share: Share{Moniker: "1", SaveData: []byte("not json")}, Digest: make([]byte, 32)},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := NewSignParty(ctx, tc.cfg); err == nil {
				t.Fatalf("expected error for %s", tc.name)
			}
		})
	}
}

func TestSinglePartyReshareRejectsBadParams(t *testing.T) {
	old, _, _ := makeOldCommittee(t, testThreshold, testParties)
	ctx := context.Background()
	cases := []struct {
		name string
		cfg  SinglePartyReshareConfig
	}{
		{"too few parties", SinglePartyReshareConfig{OldThreshold: 1, NewThreshold: 1, Parties: 1, PartyIndex: 0, OldShare: old[0]}},
		{"old threshold too low", SinglePartyReshareConfig{OldThreshold: 0, NewThreshold: 1, Parties: 3, PartyIndex: 0, OldShare: old[0]}},
		{"old threshold not below n", SinglePartyReshareConfig{OldThreshold: 3, NewThreshold: 1, Parties: 3, PartyIndex: 0, OldShare: old[0]}},
		{"new threshold too low", SinglePartyReshareConfig{OldThreshold: 1, NewThreshold: 0, Parties: 3, PartyIndex: 0, OldShare: old[0]}},
		{"new threshold not below n", SinglePartyReshareConfig{OldThreshold: 1, NewThreshold: 3, Parties: 3, PartyIndex: 0, OldShare: old[0]}},
		{"party index out of range", SinglePartyReshareConfig{OldThreshold: 1, NewThreshold: 1, Parties: 3, PartyIndex: 3, OldShare: old[0]}},
		{"corrupt old share", SinglePartyReshareConfig{OldThreshold: 1, NewThreshold: 1, Parties: 3, PartyIndex: 0, OldShare: Share{Moniker: "1", SaveData: []byte("not json")}}},
		// Party index disagrees with the share's own ShareID — must be caught.
		{"index/share mismatch", SinglePartyReshareConfig{OldThreshold: 1, NewThreshold: 1, Parties: 3, PartyIndex: 1, OldShare: old[0]}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := NewReshareParty(ctx, tc.cfg); err == nil {
				t.Fatalf("expected error for %s", tc.name)
			}
		})
	}
}
