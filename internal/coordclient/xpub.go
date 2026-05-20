package coordclient

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
)

// Xpub is the api.md B8 response: HD extended public key for the group
// (Q_master, chaincode). Bytes are the verbatim decode of the server's hex
// fields — chaincode is exactly 32 bytes per docs/design/mcp/address-
// derivation.md §3/§4; the pubkey is the bytes provisioned by S-002
// (typically uncompressed secp256k1, accepted in either compressed or
// uncompressed form by `btcec.ParsePubKey` downstream).
type Xpub struct {
	ECDSAPubkey []byte
	Chaincode   []byte
}

// xpubResponse mirrors the B8 wire shape (hex-encoded fields, api.md:78-81).
type xpubResponse struct {
	ECDSAPubkeyHex string `json:"ecdsaPubkeyHex"`
	ChaincodeHex   string `json:"chaincodeHex"`
}

// Xpub pulls this client's group HD extended public key via api.md B8
// (GET /v1/groups/{groupId}/xpub). Owning-member-only by contract: the
// member-signed B-side authn is enforced server-side and reproduced
// byte-for-byte by signRequest (see auth.go), so the route is reachable only
// to the active member of the group.
//
// Legacy non-HD groups (chaincode NULL) surface as 409 LEGACY_NO_HD; the
// caller can branch on `apiErr.Code == "LEGACY_NO_HD"` or on the typed code
// `CodeLegacyNoHD` to fall back to the multi-group multi-address path
// (address-derivation.md §F5/§8). 503 LOCKED is retried by the shared retry
// policy.
func (c *Client) Xpub(ctx context.Context) (*Xpub, error) {
	raw, err := c.do(ctx, request{
		method:   http.MethodGet,
		path:     "/v1/groups/" + c.groupID + "/xpub",
		authVerb: "B8:xpub",
	})
	if err != nil {
		return nil, err
	}
	var body xpubResponse
	if err := json.Unmarshal(raw, &body); err != nil {
		return nil, fmt.Errorf("coordclient: decode xpub: %w", err)
	}
	pub, err := hex.DecodeString(body.ECDSAPubkeyHex)
	if err != nil {
		return nil, fmt.Errorf("coordclient: ecdsaPubkeyHex not hex: %w", err)
	}
	cc, err := hex.DecodeString(body.ChaincodeHex)
	if err != nil {
		return nil, fmt.Errorf("coordclient: chaincodeHex not hex: %w", err)
	}
	if len(cc) != 32 {
		return nil, fmt.Errorf("coordclient: chaincode must be 32 bytes, got %d", len(cc))
	}
	return &Xpub{ECDSAPubkey: pub, Chaincode: cc}, nil
}
