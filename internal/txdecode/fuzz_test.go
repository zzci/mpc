package txdecode

import (
	"crypto/sha256"
	"encoding/hex"
	"testing"
)

// The safety-critical property under fuzzing: Decode must never return a
// verified Result whose facts are not bound to req.Digest32. We check it
// directly by independently obtaining the decoder's recomputed digest and
// asserting success implies recomputed == digest32 (no mis-sign), and that an
// unbindable input is always rejected (decode bug degrades to rejection).

func FuzzEVMNoMisSign(f *testing.F) {
	tx, _ := hex.DecodeString(eip155RLP)
	dg, _ := hex.DecodeString(eip155Digest)
	f.Add(tx, dg)
	f.Add([]byte{eip1559TxType, 0xc0}, make([]byte, 32))
	f.Add([]byte{}, make([]byte, 32))
	f.Add([]byte{0xff}, make([]byte, 32))

	dec := &evmDecoder{chain: ChainETH, expectChainID: chainIDEth}
	f.Fuzz(func(t *testing.T, txb, dgb []byte) {
		res, err := New().Decode(req("eth", txb, dgb))

		facts, rec, rerr := dec.Recompute(txb)

		if err == nil {
			// A success MUST be digest-bound and carry facts.
			if res == nil || !res.DigestVerified || res.Facts == nil {
				t.Fatalf("success without bound facts: res=%v", res)
			}
			if len(dgb) != 32 || rerr != nil {
				t.Fatalf("accepted an unbindable tx: rerr=%v len=%d", rerr, len(dgb))
			}
			for i := 0; i < 32; i++ {
				if rec[i] != dgb[i] {
					t.Fatalf("MIS-SIGN: accepted tx whose recomputed digest != digest32")
				}
			}
			_ = facts
		} else if res != nil {
			t.Fatalf("error path must return no facts, got %v", res)
		}
	})
}

func FuzzTRONNoMisSign(f *testing.F) {
	owner := addr21(0x11)
	msg := pbBytes(1, owner)
	msg = append(msg, pbBytes(2, addr21(0x22))...)
	msg = append(msg, pbVarint(3, 42)...)
	raw := rawData4Fuzz(tronTransferContract, msg)
	d := sha256.Sum256(raw)
	f.Add(raw, d[:])
	f.Add([]byte{0x08, 0xff}, make([]byte, 32))
	f.Add([]byte{}, make([]byte, 32))

	f.Fuzz(func(t *testing.T, txb, dgb []byte) {
		res, err := New().Decode(req("tron", txb, dgb))
		if err == nil {
			if res == nil || !res.DigestVerified || res.Facts == nil {
				t.Fatalf("success without bound facts")
			}
			want := sha256.Sum256(txb)
			if len(dgb) != 32 {
				t.Fatalf("accepted non-32 digest")
			}
			for i := 0; i < 32; i++ {
				if want[i] != dgb[i] {
					t.Fatalf("MIS-SIGN: TRON accepted tx whose sha256(raw)!=digest32")
				}
			}
		} else if res != nil {
			t.Fatalf("error path must return no facts")
		}
	})
}

// rawData4Fuzz is rawData without a *testing.T (seed corpus helper).
func rawData4Fuzz(ctype uint64, msg []byte) []byte {
	c := pbVarint(1, ctype)
	c = append(c, pbBytes(2, tronAny("type.googleapis.com/protocol.TransferContract", msg))...)
	return pbBytes(11, c)
}
