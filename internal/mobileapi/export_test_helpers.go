package mobileapi

import "github.com/zzci/mpc/internal/mpc"

// SetOwnShareForTest is the exported test seam over the internal
// setOwnShare path. It is the ONLY way packages outside mobileapi (e.g.
// walletcli) can prime an SDK with a synthetic share+group entry without
// driving a real keygen/reshare; production code stays gated behind
// setOwnShare's package-private name.
func (s *SDK) SetOwnShareForTest(groupID string, share mpc.Share, threshold, parties, partyIndex int, pubHex string) {
	s.setOwnShare(groupID, share, threshold, parties, partyIndex, pubHex)
}
