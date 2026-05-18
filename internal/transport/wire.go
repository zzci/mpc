package transport

import (
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"

	"github.com/zzci/mpc/internal/contract"
)

// maxFrame caps a single directed-stream MpcMessage frame. tss-lib keygen
// rounds (Paillier/Schnorr proofs) are the largest payloads; 8 MiB is well
// above any real round while still bounding memory of a hostile peer.
const maxFrame = 8 << 20

// marshalMessage encodes an MpcMessage as JSON. The contract struct already
// carries the protocol.md §3 field set with stable json tags; signatures and
// session isolation never depend on this wire form (they are derived from
// logical values in internal/contract, per docs/spec/envelope-canonical.md
// S-001), so a plain JSON envelope is sufficient and self-describing.
func marshalMessage(msg *contract.MpcMessage) ([]byte, error) {
	b, err := json.Marshal(msg)
	if err != nil {
		return nil, fmt.Errorf("transport: marshal MpcMessage: %w", err)
	}
	return b, nil
}

// unmarshalMessage decodes a JSON MpcMessage frame.
func unmarshalMessage(b []byte) (*contract.MpcMessage, error) {
	var msg contract.MpcMessage
	if err := json.Unmarshal(b, &msg); err != nil {
		return nil, fmt.Errorf("transport: unmarshal MpcMessage: %w", err)
	}
	return &msg, nil
}

// writeFrame writes a length-prefixed (4-byte big-endian) JSON frame.
func writeFrame(w io.Writer, msg *contract.MpcMessage) error {
	b, err := marshalMessage(msg)
	if err != nil {
		return err
	}
	if len(b) > maxFrame {
		return fmt.Errorf("transport: outbound frame %d exceeds %d", len(b), maxFrame)
	}
	var hdr [4]byte
	binary.BigEndian.PutUint32(hdr[:], uint32(len(b)))
	if _, err := w.Write(hdr[:]); err != nil {
		return fmt.Errorf("transport: write frame header: %w", err)
	}
	if _, err := w.Write(b); err != nil {
		return fmt.Errorf("transport: write frame body: %w", err)
	}
	return nil
}

// readFrame reads one length-prefixed JSON frame and decodes it. An
// over-length prefix is rejected before any allocation.
func readFrame(r io.Reader) (*contract.MpcMessage, error) {
	var hdr [4]byte
	if _, err := io.ReadFull(r, hdr[:]); err != nil {
		return nil, fmt.Errorf("transport: read frame header: %w", err)
	}
	n := binary.BigEndian.Uint32(hdr[:])
	if n == 0 || n > maxFrame {
		return nil, fmt.Errorf("transport: invalid frame length %d", n)
	}
	buf := make([]byte, n)
	if _, err := io.ReadFull(r, buf); err != nil {
		return nil, fmt.Errorf("transport: read frame body: %w", err)
	}
	return unmarshalMessage(buf)
}
