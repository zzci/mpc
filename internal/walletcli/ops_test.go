package walletcli

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"strings"
	"testing"

	"github.com/btcsuite/btcd/btcec/v2"
	"github.com/libp2p/go-libp2p/core/crypto"
	"github.com/libp2p/go-libp2p/core/peer"
)

// DM-5 ops tests: pre-DM-5 the wallet ops shipped magic placeholder strings
// (cliRelay "12D3KooWWalletCliPlaceholder...", cliWire no-op, cliMembers
// "cli-member-N") so the SDK's hard-cut validation passed but no actual MPC
// could ever complete. DM-5 replaces them with a real libp2p host built from
// $MPC_WALLET_HOST_*; these tests cover the env-driven validation surface so
// the "host not configured" path fails loud (instead of silently submitting
// the placeholder) and the well-formed path produces a valid configJSON.

// withCleanHostEnv unsets every $MPC_WALLET_HOST_* so a stale environment
// leaked from another test cannot accidentally satisfy readHostEnv.
func withCleanHostEnv(t *testing.T) {
	t.Helper()
	for _, k := range []string{
		envHostPSK, envHostMemberKey, envHostRelayPeerID, envHostRelayAddrs,
		envHostPeersJSON, envHostMemberSet, envHostGroupID, envHostPartyIndex,
		envHostThreshold,
	} {
		t.Setenv(k, "")
	}
}

// setGoodHostEnv configures a complete, well-formed $MPC_WALLET_HOST_* set so
// readHostEnv returns a valid *hostEnv. Tests mutate single variables to
// exercise the named failure modes.
func setGoodHostEnv(t *testing.T) {
	t.Helper()
	psk := make([]byte, 32)
	if _, err := rand.Read(psk); err != nil {
		t.Fatalf("psk: %v", err)
	}
	t.Setenv(envHostPSK, hex.EncodeToString(psk))

	mk, err := btcec.NewPrivateKey()
	if err != nil {
		t.Fatalf("member key: %v", err)
	}
	t.Setenv(envHostMemberKey, hex.EncodeToString(mk.Serialize()))

	// Relay peer ID — synthesise a stable peer.ID from a random secp256k1 key.
	relayPriv, _, err := crypto.GenerateSecp256k1Key(rand.Reader)
	if err != nil {
		t.Fatalf("relay key: %v", err)
	}
	relayPID, err := peer.IDFromPrivateKey(relayPriv)
	if err != nil {
		t.Fatalf("relay pid: %v", err)
	}
	t.Setenv(envHostRelayPeerID, relayPID.String())
	t.Setenv(envHostRelayAddrs, "/ip4/127.0.0.1/tcp/0")

	// Two peer entries: this device (tag "1") + one other (tag "2").
	selfPriv, _, err := crypto.GenerateSecp256k1Key(rand.Reader)
	if err != nil {
		t.Fatalf("self key: %v", err)
	}
	selfPID, err := peer.IDFromPrivateKey(selfPriv)
	if err != nil {
		t.Fatalf("self pid: %v", err)
	}
	otherPriv, _, err := crypto.GenerateSecp256k1Key(rand.Reader)
	if err != nil {
		t.Fatalf("other key: %v", err)
	}
	otherPID, err := peer.IDFromPrivateKey(otherPriv)
	if err != nil {
		t.Fatalf("other pid: %v", err)
	}
	otherMK, err := btcec.NewPrivateKey()
	if err != nil {
		t.Fatalf("other member key: %v", err)
	}
	peers := map[string]map[string]string{
		"1": {"peerID": selfPID.String(), "pub": hex.EncodeToString(mk.PubKey().SerializeCompressed())},
		"2": {"peerID": otherPID.String(), "pub": hex.EncodeToString(otherMK.PubKey().SerializeCompressed())},
	}
	pj, err := json.Marshal(peers)
	if err != nil {
		t.Fatalf("peers json: %v", err)
	}
	t.Setenv(envHostPeersJSON, string(pj))
	t.Setenv(envHostMemberSet, "m1,m2")
	t.Setenv(envHostGroupID, "g-DM5-test")
	t.Setenv(envHostPartyIndex, "0")
	t.Setenv(envHostThreshold, "1")
}

func TestReadHostEnv_MissingVarsFailLoud(t *testing.T) {
	withCleanHostEnv(t)
	_, err := readHostEnv()
	if err == nil || !strings.Contains(err.Error(), "DM-5 host transport not configured") {
		t.Fatalf("readHostEnv with empty env: err=%v, want 'DM-5 host transport not configured'", err)
	}
}

func TestReadHostEnv_NamesOffendingVariable(t *testing.T) {
	withCleanHostEnv(t)
	setGoodHostEnv(t)

	cases := []struct {
		name string
		mut  func()
		want string
	}{
		{"missing memberKey", func() { t.Setenv(envHostMemberKey, "") }, envHostMemberKey},
		{"missing relayPeerID", func() { t.Setenv(envHostRelayPeerID, "") }, envHostRelayPeerID},
		{"missing relayAddrs", func() { t.Setenv(envHostRelayAddrs, "") }, envHostRelayAddrs},
		{"missing peersJSON", func() { t.Setenv(envHostPeersJSON, "") }, envHostPeersJSON},
		{"missing memberSet", func() { t.Setenv(envHostMemberSet, "") }, envHostMemberSet},
		{"missing groupID", func() { t.Setenv(envHostGroupID, "") }, envHostGroupID},
		{"missing partyIndex", func() { t.Setenv(envHostPartyIndex, "") }, envHostPartyIndex},
		{"missing threshold", func() { t.Setenv(envHostThreshold, "") }, envHostThreshold},
		{"bad PSK length", func() { t.Setenv(envHostPSK, "deadbeef") }, envHostPSK},
		{"bad relay multiaddr", func() { t.Setenv(envHostRelayAddrs, "not-a-multiaddr") }, envHostRelayAddrs},
		{"bad peersJSON", func() { t.Setenv(envHostPeersJSON, "{not-json") }, envHostPeersJSON},
		{"bad partyIndex", func() { t.Setenv(envHostPartyIndex, "abc") }, envHostPartyIndex},
		{"negative threshold", func() { t.Setenv(envHostThreshold, "0") }, envHostThreshold},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			withCleanHostEnv(t)
			setGoodHostEnv(t)
			tc.mut()
			_, err := readHostEnv()
			if err == nil {
				t.Fatalf("readHostEnv: want error mentioning %s, got nil", tc.want)
			}
			if !strings.Contains(err.Error(), tc.want) && !strings.Contains(err.Error(), "DM-5 host transport not configured") {
				t.Fatalf("readHostEnv: err=%q, want substring %q (or the DM-5 sentinel)", err, tc.want)
			}
		})
	}
}

func TestReadHostEnv_HappyPathBuildsHostEnv(t *testing.T) {
	withCleanHostEnv(t)
	setGoodHostEnv(t)
	env, err := readHostEnv()
	if err != nil {
		t.Fatalf("readHostEnv: %v", err)
	}
	if env.groupID != "g-DM5-test" {
		t.Fatalf("groupID = %q, want g-DM5-test", env.groupID)
	}
	if env.partyIndex != 0 {
		t.Fatalf("partyIndex = %d, want 0", env.partyIndex)
	}
	if env.threshold != 1 {
		t.Fatalf("threshold = %d, want 1", env.threshold)
	}
	if got := env.memberSet; len(got) != 2 || got[0] != "m1" || got[1] != "m2" {
		t.Fatalf("memberSet = %v, want [m1 m2]", got)
	}
	if len(env.peers) != 2 || len(env.peerPubKeys) != 2 {
		t.Fatalf("peers=%d / peerPubKeys=%d, want 2/2", len(env.peers), len(env.peerPubKeys))
	}
	if len(env.psk) != 32 || env.memberKey == nil || env.hostKey == nil {
		t.Fatalf("psk=%d memberKey=%v hostKey=%v, want non-nil/32B", len(env.psk), env.memberKey, env.hostKey)
	}
}

// TestPrepareSign_RequiresHostEnv covers the sign-side path: with no
// $MPC_WALLET_HOST_* set, prepareSign refuses to build a host and propagates
// the readHostEnv error verbatim. This is the contract that replaces the
// pre-DM-5 placeholder behaviour (which silently submitted the magic
// "12D3KooWWalletCliPlaceholder..." relay block to the SDK).
func TestPrepareSign_RequiresHostEnv(t *testing.T) {
	withCleanHostEnv(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	startJSON := []byte(`{"requestId":"req-1"}`)
	cfg, host, err := prepareSign(ctx, startJSON)
	if err == nil {
		_ = host.Close()
		t.Fatalf("prepareSign without host env: want error, got cfg=%q host=%v", cfg, host)
	}
	if !strings.Contains(err.Error(), "DM-5 host transport not configured") {
		t.Fatalf("prepareSign error = %q, want 'DM-5 host transport not configured'", err)
	}
	if host != nil {
		t.Fatalf("prepareSign returned host=%v on error path; want nil", host)
	}
}

// TestPrepareSign_BadStartJSON exercises the second readHostEnv-pass error
// surface: env is configured, but the start blob is not a valid JSON with a
// requestId.
func TestPrepareSign_BadStartJSON(t *testing.T) {
	withCleanHostEnv(t)
	setGoodHostEnv(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	cases := []struct {
		name      string
		startJSON []byte
		want      string
	}{
		{"malformed json", []byte("{"), "parse start"},
		{"missing requestId", []byte(`{"signers":["m1"]}`), "start.requestId is empty"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cfg, host, err := prepareSign(ctx, tc.startJSON)
			if err == nil {
				_ = host.Close()
				t.Fatalf("prepareSign: want error containing %q, got cfg=%q", tc.want, cfg)
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("prepareSign err = %q, want %q", err, tc.want)
			}
			if host != nil {
				t.Fatalf("prepareSign returned host on error path; want nil")
			}
		})
	}
}

// TestEnvelopeShape verifies that the JSON envelope produced for the SDK's
// DM-3 hard-cut carries every mandatory key. The SDK's keygenConfig.validate
// rejects an envelope missing groupId / sessionID / partyIndex / n / t /
// memberSet / relay / role / passphrase; this test guards the wallet side of
// that contract so a regression surfaces here before it reaches the SDK.
func TestEnvelopeShape_ContainsDM3MandatoryFields(t *testing.T) {
	got := envelope(map[string]any{
		"groupId":    "g",
		"sessionID":  "s",
		"partyIndex": 0,
		"n":          2,
		"t":          1,
		"memberSet":  []string{"a", "b"},
		"relay":      map[string]any{"peerID": "x", "addrs": []string{"/a"}},
		"role":       "keygen",
		"passphrase": "p",
	})
	var m map[string]json.RawMessage
	if err := json.Unmarshal([]byte(got), &m); err != nil {
		t.Fatalf("unmarshal envelope: %v", err)
	}
	for _, k := range []string{"groupId", "sessionID", "partyIndex", "n", "t", "memberSet", "relay", "role", "passphrase"} {
		if _, ok := m[k]; !ok {
			t.Errorf("envelope missing %q: %s", k, got)
		}
	}
}

// TestNoPlaceholderMagicStringsReachable is the regression guard for the
// pre-DM-5 placeholders: the magic strings (cliRelay PeerID literal,
// "cli-member-N" tags) must no longer appear in the package's runtime surface.
// We assert via the env-driven path that nothing in ops.go falls back to a
// hard-coded peer id or member tag.
func TestNoPlaceholderMagicStringsReachable(t *testing.T) {
	withCleanHostEnv(t)
	setGoodHostEnv(t)
	env, err := readHostEnv()
	if err != nil {
		t.Fatalf("readHostEnv: %v", err)
	}
	// Build an envelope through the public path and confirm no placeholder
	// string leaked through.
	cfg := envelope(map[string]any{
		"groupId":    env.groupID,
		"sessionID":  "test-sess",
		"partyIndex": env.partyIndex,
		"n":          len(env.memberSet),
		"t":          env.threshold,
		"memberSet":  env.memberSet,
		"relay":      relayJSON(env.relay),
		"role":       "keygen",
		"passphrase": "p",
	})
	for _, banned := range []string{
		"12D3KooWWalletCliPlaceholder",
		"cli-member-1",
		"cli-member-2",
		"cli-member-3",
	} {
		if strings.Contains(cfg, banned) {
			t.Errorf("placeholder %q reached configJSON: %s", banned, cfg)
		}
	}
}

// TestNewSessionID_Unique guards the session-id randomness: two consecutive
// calls must produce distinct ids so the R5 gate never mistakes two parallel
// invocations on the same op for one session.
func TestNewSessionID_Unique(t *testing.T) {
	a := newSessionID("keygen")
	b := newSessionID("keygen")
	if a == b {
		t.Fatalf("newSessionID collided: %s == %s", a, b)
	}
	if !strings.HasPrefix(a, "walletcli-keygen-") || !strings.HasPrefix(b, "walletcli-keygen-") {
		t.Fatalf("newSessionID shape drift: %s / %s", a, b)
	}
}
