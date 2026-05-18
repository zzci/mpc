package contract

import (
	"fmt"
	"strconv"
	"strings"
)

// Version constants and protocol IDs follow docs/design/contract/protocol.md §7.
// version appears in three parallel places: SigningRequest, MpcMessage, and
// the libp2p protocol ID. Incompatible changes bump the major version; the
// receiver rejects an unrecognized version rather than guessing a downgrade
// (docs/design/contract/protocol.md:86).

const (
	// EnvelopeVersionV1 is the only SigningRequest.Version this build accepts.
	EnvelopeVersionV1 uint64 = 1
	// MpcVersionV1 is the only MpcMessage.Version this build accepts.
	MpcVersionV1 uint64 = 1

	// ProtocolMPCPrefix is the versioned libp2p protocol-ID prefix for the
	// keygen/sign/reshare message stream (docs/design/contract/protocol.md:41).
	ProtocolMPCPrefix = "/tss/mpc/"
	// ProtocolMPCV1 is this build's MPC protocol ID.
	ProtocolMPCV1 = ProtocolMPCPrefix + "1.0.0"
)

// CheckEnvelopeVersion returns ErrUnsupportedVersion unless req.Version is
// implemented by this build (docs/design/contract/protocol.md:86: reject, do not
// downgrade-guess).
func CheckEnvelopeVersion(req *SigningRequest) error {
	if req.Version != EnvelopeVersionV1 {
		return fmt.Errorf("%w: envelope version %d", ErrUnsupportedVersion, req.Version)
	}
	return nil
}

// CheckMpcVersion returns ErrUnsupportedVersion unless msg.Version is
// implemented by this build.
func CheckMpcVersion(msg *MpcMessage) error {
	if msg.Version != MpcVersionV1 {
		return fmt.Errorf("%w: mpc message version %d", ErrUnsupportedVersion, msg.Version)
	}
	return nil
}

// NegotiateMPCProtocol selects the highest /tss/mpc/x.y.z protocol ID common
// to local and remote, mirroring libp2p multi-protocol negotiation
// (docs/design/contract/protocol.md:86). Non-/tss/mpc and unparsable IDs are
// ignored; no common version yields ErrNoCommonProtocol.
func NegotiateMPCProtocol(local, remote []string) (string, error) {
	want := make(map[string]struct{}, len(remote))
	for _, r := range remote {
		want[r] = struct{}{}
	}
	best := ""
	var bestV [3]uint64
	for _, l := range local {
		if _, ok := want[l]; !ok {
			continue
		}
		v, ok := parseMPCVersion(l)
		if !ok {
			continue
		}
		if best == "" || semverLess(bestV, v) {
			best, bestV = l, v
		}
	}
	if best == "" {
		return "", ErrNoCommonProtocol
	}
	return best, nil
}

// parseMPCVersion extracts the x.y.z triple from a /tss/mpc/x.y.z protocol ID.
func parseMPCVersion(id string) ([3]uint64, bool) {
	rest, ok := strings.CutPrefix(id, ProtocolMPCPrefix)
	if !ok {
		return [3]uint64{}, false
	}
	parts := strings.Split(rest, ".")
	if len(parts) != 3 {
		return [3]uint64{}, false
	}
	var v [3]uint64
	for i, p := range parts {
		n, err := strconv.ParseUint(p, 10, 64)
		if err != nil {
			return [3]uint64{}, false
		}
		v[i] = n
	}
	return v, true
}

// semverLess reports whether a precedes b in major/minor/patch order.
func semverLess(a, b [3]uint64) bool {
	for i := 0; i < 3; i++ {
		if a[i] != b[i] {
			return a[i] < b[i]
		}
	}
	return false
}
