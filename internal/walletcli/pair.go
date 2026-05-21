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

// pairFileName is the legacy single-record persisted pairing (pre
// multi-group). Kept readable for migration; new writes go to
// pairingsFileName.
const pairFileName = "pair.json"

// pairingsFileName is the multi-group persisted-pairings file: an array
// of pairPersisted, keyed by groupId or by paired-at-ms when groupId is
// empty. Atomically rewritten on every `pair` call (read-modify-write).
const pairingsFileName = "pairings.json"

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

// groupsView is one row in the `groups` listing — the merger of (a) the
// SDK in-memory groups (the device's keygen / reshare outcomes for this
// process) and (b) the persisted pairings (coord/relay bootstrap +
// identity keypair for B-side calls). A row may be present in only one
// side: persisted-only after pair but before keygen; SDK-only when a
// keygen ran without going through pair (e.g. CLI member harness).
type groupsView struct {
	GroupID        string `json:"groupId,omitempty"`
	Source         string `json:"source"` // "sdk", "pair", or "sdk+pair"
	Threshold      int    `json:"threshold,omitempty"`
	Parties        int    `json:"parties,omitempty"`
	PartyIndex     int    `json:"partyIndex,omitempty"`
	ECDSAPubHex    string `json:"ecdsaPubHex,omitempty"`
	Moniker        string `json:"moniker,omitempty"`
	CoordBaseURL   string `json:"coordBaseUrl,omitempty"`
	Label          string `json:"label,omitempty"`
	IdentityPubHex string `json:"identityPubHex,omitempty"`
	RelayPeerID    string `json:"relayPeerID,omitempty"`
	PairedAtMS     int64  `json:"pairedAtMs,omitempty"`
}

// cmdGroups prints the union of SDK groups and persisted pairings as JSON,
// one row per groupID. Operators read this to confirm a device's
// participation set ("which wallets does this PC sign for?") or to script
// against it from automation.
func (se *session) cmdGroups(args []string) {
	if len(args) != 0 {
		se.fail("usage: groups")
		return
	}
	rowsBySDK := map[string]groupsView{}
	sdkJSON, err := se.sdk.ListGroupsJSON()
	if err != nil {
		se.fail("groups: list sdk: %v", err)
		return
	}
	var sdkResp struct {
		Items []struct {
			GroupID     string `json:"groupId"`
			Threshold   int    `json:"threshold"`
			Parties     int    `json:"parties"`
			PartyIndex  int    `json:"partyIndex"`
			ECDSAPubHex string `json:"ecdsaPubHex"`
			Moniker     string `json:"moniker"`
		} `json:"items"`
	}
	if err := json.Unmarshal([]byte(sdkJSON), &sdkResp); err != nil {
		se.fail("groups: decode sdk: %v", err)
		return
	}
	for _, it := range sdkResp.Items {
		rowsBySDK[it.GroupID] = groupsView{
			GroupID: it.GroupID, Source: "sdk",
			Threshold: it.Threshold, Parties: it.Parties,
			PartyIndex: it.PartyIndex, ECDSAPubHex: it.ECDSAPubHex,
			Moniker: it.Moniker,
		}
	}
	persisted, err := loadPairings(se.keystoreDir)
	if err != nil {
		se.fail("groups: load pairings: %v", err)
		return
	}
	for _, p := range persisted {
		row, exists := rowsBySDK[p.GroupID]
		if exists {
			row.Source = "sdk+pair"
		} else {
			row = groupsView{GroupID: p.GroupID, Source: "pair"}
		}
		row.CoordBaseURL = p.CoordBaseURL
		row.Label = p.Label
		row.IdentityPubHex = p.IdentityPubHex
		row.RelayPeerID = p.RelayPeerID
		row.PairedAtMS = p.PairedAtMS
		rowsBySDK[p.GroupID] = row
	}
	out := make([]groupsView, 0, len(rowsBySDK))
	for _, r := range rowsBySDK {
		out = append(out, r)
	}
	body, err := json.MarshalIndent(map[string]any{"items": out}, "", "  ")
	if err != nil {
		se.fail("groups: marshal: %v", err)
		return
	}
	wl(se.out, string(body))
}

// loadPairings reads the multi-group pairings file. If the legacy
// pair.json (single record) is the only thing present, it is migrated
// in-place: read once, wrapped into a one-element list, and the legacy
// file is left in place (forward-compatible — older binaries can still
// read it) until the next call overwrites pairings.json with the merged
// list. Returns an empty slice when no file exists yet.
func loadPairings(keystoreDir string) ([]pairPersisted, error) {
	if keystoreDir == "" {
		return nil, fmt.Errorf("keystore directory unknown")
	}
	out := filepath.Join(keystoreDir, pairingsFileName)
	if b, err := os.ReadFile(out); err == nil {
		var list []pairPersisted
		if err := json.Unmarshal(b, &list); err != nil {
			return nil, fmt.Errorf("pairings.json bad json: %w", err)
		}
		return list, nil
	} else if !os.IsNotExist(err) {
		return nil, err
	}
	// Legacy single-record fallback.
	legacy := filepath.Join(keystoreDir, pairFileName)
	if b, err := os.ReadFile(legacy); err == nil {
		var rec pairPersisted
		if err := json.Unmarshal(b, &rec); err != nil {
			return nil, fmt.Errorf("legacy pair.json bad json: %w", err)
		}
		return []pairPersisted{rec}, nil
	} else if !os.IsNotExist(err) {
		return nil, err
	}
	return nil, nil
}

// persistPair appends or replaces a pairing record in the multi-group
// pairings.json file. Replacement key is groupID when present, otherwise
// the IdentityPubHex (which is unique per-pairing). The file is rewritten
// atomically (write to tmp, rename) with mode 0600.
func persistPair(keystoreDir string, rec pairPersisted) error {
	if keystoreDir == "" {
		return fmt.Errorf("keystore directory unknown")
	}
	if err := os.MkdirAll(keystoreDir, 0o700); err != nil {
		return err
	}
	list, err := loadPairings(keystoreDir)
	if err != nil {
		return err
	}
	// Replace by groupID match (preferred) or by identity pubkey.
	replaced := false
	for i := range list {
		match := false
		if rec.GroupID != "" && list[i].GroupID == rec.GroupID {
			match = true
		} else if rec.GroupID == "" && list[i].IdentityPubHex == rec.IdentityPubHex {
			match = true
		}
		if match {
			list[i] = rec
			replaced = true
			break
		}
	}
	if !replaced {
		list = append(list, rec)
	}
	out := filepath.Join(keystoreDir, pairingsFileName)
	tmp := out + ".tmp"
	b, err := json.MarshalIndent(list, "", "  ")
	if err != nil {
		return err
	}
	if err := os.WriteFile(tmp, b, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, out)
}
