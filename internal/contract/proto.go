package contract

import (
	"encoding/json"
	"fmt"

	"google.golang.org/protobuf/encoding/protowire"
)

// Protobuf wire form of the envelope (docs/design/contract/protocol.md §1, carried
// by the START message §5). protoc / protoc-gen-go are unavailable in this
// environment, so the wire codec is built on the official
// google.golang.org/protobuf/encoding/protowire package — pure Go, no codegen
// tool, the mandated protobuf library. This is consistent with
// docs/spec/envelope-canonical.md §8: canonical signing/hashing bytes are
// derived from the decoded logical values (CanonicalBytes), never from these
// protobuf bytes, so field order / default-omission here cannot affect
// proposerSig. Full message-set codegen (MpcMessage libp2p framing) is owned
// by the transport task (M-005).
//
// Field numbers follow the protocol.md §1 field order.
const (
	fnVersion      = 1
	fnRequestID    = 2
	fnGroupID      = 3
	fnChain        = 4
	fnUnsignedTx   = 5
	fnDigest32     = 6
	fnProposer     = 7
	fnCreatedAt    = 8
	fnExpiry       = 9
	fnBusinessInfo = 10 // JCS/JSON bytes of the object; absent when nil
	fnMetaHash     = 11
	fnProposerSig  = 12
)

// MarshalProto encodes the envelope to its protobuf wire form. businessInfo is
// carried as its JSON object bytes (S-001 §4.1: the protobuf field carries the
// same logical object the JSON path submits); it is omitted when nil.
func (r *SigningRequest) MarshalProto() ([]byte, error) {
	var b []byte
	b = protowire.AppendTag(b, fnVersion, protowire.VarintType)
	b = protowire.AppendVarint(b, r.Version)
	b = appendProtoString(b, fnRequestID, r.RequestID)
	b = appendProtoString(b, fnGroupID, r.GroupID)
	b = appendProtoString(b, fnChain, r.Chain)
	b = appendProtoBytes(b, fnUnsignedTx, r.UnsignedTx)
	b = appendProtoBytes(b, fnDigest32, r.Digest32)
	b = appendProtoString(b, fnProposer, r.Proposer)
	b = protowire.AppendTag(b, fnCreatedAt, protowire.VarintType)
	b = protowire.AppendVarint(b, uint64(r.CreatedAt))
	b = protowire.AppendTag(b, fnExpiry, protowire.VarintType)
	b = protowire.AppendVarint(b, uint64(r.Expiry))
	if r.BusinessInfo != nil {
		raw, err := json.Marshal(r.BusinessInfo)
		if err != nil {
			return nil, fmt.Errorf("%w: marshal businessInfo: %w", ErrInvalidEnvelope, err)
		}
		b = appendProtoBytes(b, fnBusinessInfo, raw)
	}
	b = appendProtoBytes(b, fnMetaHash, r.MetaHash)
	b = appendProtoBytes(b, fnProposerSig, r.ProposerSig)
	return b, nil
}

// UnmarshalProto decodes a protobuf-wire envelope into the logical struct. The
// resulting value feeds CanonicalBytes identically to a JSON-decoded copy of
// the same logical envelope (S-001 §8).
func UnmarshalProto(data []byte) (*SigningRequest, error) {
	r := &SigningRequest{}
	for len(data) > 0 {
		num, typ, n := protowire.ConsumeTag(data)
		if n < 0 {
			return nil, fmt.Errorf("%w: bad protobuf tag", ErrInvalidEnvelope)
		}
		data = data[n:]
		switch num {
		case fnVersion:
			r.Version, data, n = consumeVarint(data, typ)
		case fnCreatedAt:
			var v uint64
			v, data, n = consumeVarint(data, typ)
			r.CreatedAt = int64(v)
		case fnExpiry:
			var v uint64
			v, data, n = consumeVarint(data, typ)
			r.Expiry = int64(v)
		case fnRequestID:
			r.RequestID, data, n = consumeString(data, typ)
		case fnGroupID:
			r.GroupID, data, n = consumeString(data, typ)
		case fnChain:
			r.Chain, data, n = consumeString(data, typ)
		case fnProposer:
			r.Proposer, data, n = consumeString(data, typ)
		case fnUnsignedTx:
			r.UnsignedTx, data, n = consumeBytes(data, typ)
		case fnDigest32:
			r.Digest32, data, n = consumeBytes(data, typ)
		case fnMetaHash:
			r.MetaHash, data, n = consumeBytes(data, typ)
		case fnProposerSig:
			r.ProposerSig, data, n = consumeBytes(data, typ)
		case fnBusinessInfo:
			var raw []byte
			raw, data, n = consumeBytes(data, typ)
			if n >= 0 {
				bi := &BusinessInfo{}
				if err := json.Unmarshal(raw, bi); err != nil {
					return nil, fmt.Errorf("%w: businessInfo: %w", ErrInvalidEnvelope, err)
				}
				r.BusinessInfo = bi
			}
		default:
			n = protowire.ConsumeFieldValue(num, typ, data)
			if n >= 0 {
				data = data[n:]
			}
		}
		if n < 0 {
			return nil, fmt.Errorf("%w: malformed protobuf field %d", ErrInvalidEnvelope, num)
		}
	}
	return r, nil
}

func appendProtoString(b []byte, num protowire.Number, s string) []byte {
	b = protowire.AppendTag(b, num, protowire.BytesType)
	return protowire.AppendString(b, s)
}

func appendProtoBytes(b []byte, num protowire.Number, v []byte) []byte {
	b = protowire.AppendTag(b, num, protowire.BytesType)
	return protowire.AppendBytes(b, v)
}

func consumeVarint(data []byte, typ protowire.Type) (uint64, []byte, int) {
	if typ != protowire.VarintType {
		return 0, data, -1
	}
	v, n := protowire.ConsumeVarint(data)
	if n < 0 {
		return 0, data, -1
	}
	return v, data[n:], n
}

func consumeString(data []byte, typ protowire.Type) (string, []byte, int) {
	if typ != protowire.BytesType {
		return "", data, -1
	}
	v, n := protowire.ConsumeString(data)
	if n < 0 {
		return "", data, -1
	}
	return v, data[n:], n
}

func consumeBytes(data []byte, typ protowire.Type) ([]byte, []byte, int) {
	if typ != protowire.BytesType {
		return nil, data, -1
	}
	v, n := protowire.ConsumeBytes(data)
	if n < 0 {
		return nil, data, -1
	}
	// ConsumeBytes aliases data; copy so the decoded struct owns its memory.
	out := make([]byte, len(v))
	copy(out, v)
	return out, data[n:], n
}
