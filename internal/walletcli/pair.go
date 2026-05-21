package walletcli

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/btcsuite/btcd/btcec/v2"
)

// pairHTTPTimeout caps every HTTP round-trip inside the pair flow. Both
// the GET config and POST enroll are fast operator-driven steps; a
// generous-but-bounded timeout keeps a wedged coord from hanging the
// wallet-cli REPL.
const pairHTTPTimeout = 15 * time.Second

// pairConfig is what the coord pairing-config endpoint returns. Field names
// MUST stay in sync with internal/server/coord.pairingPublicResponse so a
// round-trip JSON decode against either side works without translation.
type pairConfig struct {
	Token        string   `json:"token"`
	GroupID      string   `json:"groupId,omitempty"`
	Label        string   `json:"label,omitempty"`
	ExpiresAtMS  int64    `json:"expiresAtMs"`
	CoordBaseURL string   `json:"coordBaseUrl"`
	RelayPeerID  string   `json:"relayPeerID,omitempty"`
	RelayAddrs   []string `json:"relayAddrs,omitempty"`
}

// pairPersisted is the file format wallet-cli writes to <keystore>/pair.json
// after a successful enrollment. The identity private key is generated
// locally and never sent over the wire; only the public key is posted.
type pairPersisted struct {
	CoordBaseURL    string   `json:"coordBaseUrl"`
	GroupID         string   `json:"groupId,omitempty"`
	Label           string   `json:"label,omitempty"`
	RelayPeerID     string   `json:"relayPeerID,omitempty"`
	RelayAddrs      []string `json:"relayAddrs,omitempty"`
	IdentityPubHex  string   `json:"identityPubHex"`
	IdentityPrivHex string   `json:"identityPrivHex"`
	PairedAtMS      int64    `json:"pairedAtMs"`
}

// pairFilePath is the on-disk location of the persisted pairing record
// inside the keystore directory; relative names keep the file scoped to
// the same operator the keystore belongs to.
const pairFileName = "pair.json"

// cmdPair drives the operator-facing `pair <config-url>` shell command.
// It does three things:
//  1. GET the QR URL → unmarshal pairConfig (preview, no consume).
//  2. Generate a fresh secp256k1 identity keypair locally.
//  3. POST the pubkey to coord's enroll endpoint (consumes the ticket),
//     then write a pair.json record into the keystore directory.
//
// The keystore directory is taken from the session SDK (mobileapi.SDK's
// dir). Errors are surfaced to the user with se.fail; on success the
// summary line goes to se.out for piping.
func (se *session) cmdPair(args []string) {
	if len(args) != 1 {
		se.fail("usage: pair <config-url>")
		return
	}
	cfgURL := strings.TrimSpace(args[0])
	if cfgURL == "" {
		se.fail("empty config url")
		return
	}
	cfg, err := fetchPairConfig(cfgURL)
	if err != nil {
		se.fail("pair: fetch config: %v", err)
		return
	}
	identPriv, identPub, err := newIdentity()
	if err != nil {
		se.fail("pair: identity gen: %v", err)
		return
	}
	resp, err := postEnroll(cfg.CoordBaseURL, cfg.Token, identPub)
	if err != nil {
		se.fail("pair: enroll: %v", err)
		return
	}
	// resp may carry a label/groupId set on the ticket side; prefer those.
	rec := pairPersisted{
		CoordBaseURL:    resp.CoordBaseURL,
		GroupID:         resp.GroupID,
		Label:           resp.Label,
		RelayPeerID:     resp.RelayPeerID,
		RelayAddrs:      resp.RelayAddrs,
		IdentityPubHex:  identPub,
		IdentityPrivHex: identPriv,
		PairedAtMS:      time.Now().UnixMilli(),
	}
	if err := persistPair(se.keystoreDir, rec); err != nil {
		se.fail("pair: persist: %v", err)
		return
	}
	wf(se.out, "{\"paired\":true,\"groupId\":%q,\"coordBaseUrl\":%q,\"identityPubHex\":%q}\n",
		rec.GroupID, rec.CoordBaseURL, rec.IdentityPubHex)
}

// fetchPairConfig GETs the QR URL and decodes the response into a
// pairConfig. The HTTP client uses a tight timeout — pair fetch is a
// human-driven step, not an MPC ceremony.
func fetchPairConfig(url string) (pairConfig, error) {
	ctx, cancel := context.WithTimeout(context.Background(), pairHTTPTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return pairConfig{}, err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return pairConfig{}, err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return pairConfig{}, fmt.Errorf("status %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}
	var cfg pairConfig
	if err := json.NewDecoder(io.LimitReader(resp.Body, 64<<10)).Decode(&cfg); err != nil {
		return pairConfig{}, fmt.Errorf("decode: %w", err)
	}
	if cfg.Token == "" || cfg.CoordBaseURL == "" {
		return pairConfig{}, fmt.Errorf("config missing token/coordBaseUrl")
	}
	return cfg, nil
}

// postEnroll POSTs the device identity pubkey to coord's enrollment
// endpoint and decodes the response.
func postEnroll(coordBaseURL, token, identityPubHex string) (pairConfig, error) {
	ctx, cancel := context.WithTimeout(context.Background(), pairHTTPTimeout)
	defer cancel()
	url := strings.TrimRight(coordBaseURL, "/") + "/v1/pairing/" + token + "/enroll"
	body, _ := json.Marshal(map[string]string{"identityPubkey": identityPubHex})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return pairConfig{}, err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return pairConfig{}, err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return pairConfig{}, fmt.Errorf("status %d: %s", resp.StatusCode, strings.TrimSpace(string(b)))
	}
	var out pairConfig
	if err := json.NewDecoder(io.LimitReader(resp.Body, 64<<10)).Decode(&out); err != nil {
		return pairConfig{}, fmt.Errorf("decode: %w", err)
	}
	return out, nil
}

// newIdentity mints a secp256k1 keypair for the device. The private key
// is returned as a 32-byte hex string and the pubkey as the 33-byte
// compressed form, matching what coord's enroll handler accepts.
func newIdentity() (privHex, pubHex string, err error) {
	var buf [32]byte
	if _, err := rand.Read(buf[:]); err != nil {
		return "", "", err
	}
	priv, _ := btcec.PrivKeyFromBytes(buf[:])
	pub := priv.PubKey()
	return hex.EncodeToString(buf[:]), hex.EncodeToString(pub.SerializeCompressed()), nil
}

// persistPair writes the pairing record atomically (write to tmp, rename)
// to <keystoreDir>/pair.json with mode 0600. An existing file is replaced;
// the operator can pair against a different coord later by re-running.
func persistPair(keystoreDir string, rec pairPersisted) error {
	if keystoreDir == "" {
		return fmt.Errorf("keystore directory unknown")
	}
	if err := os.MkdirAll(keystoreDir, 0o700); err != nil {
		return err
	}
	out := filepath.Join(keystoreDir, pairFileName)
	tmp := out + ".tmp"
	b, err := json.MarshalIndent(rec, "", "  ")
	if err != nil {
		return err
	}
	if err := os.WriteFile(tmp, b, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, out)
}
