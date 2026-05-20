package walletcli

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/btcsuite/btcd/btcec/v2"
	"github.com/libp2p/go-libp2p/core/crypto"
	"github.com/libp2p/go-libp2p/core/peer"
	ma "github.com/multiformats/go-multiaddr"

	"github.com/zzci/mpc/internal/cli"
	"github.com/zzci/mpc/sdk"
)

// ops are the shell-agnostic wallet operations: each delegates to the shared
// SDK and returns a machine-readable result string (JSON or hex), so the
// interactive session and the HTTP server are two thin front-ends over one
// implementation. The keystore passphrase is always supplied by the caller
// from the process environment, never from operator/remote input.
//
// DM-5 wiring (distributed-mpc-impl.md §B): keygenOp / signOp / reshareOp
// build a real libp2p host (cli.HostTransport) from $MPC_WALLET_HOST_* and
// pass it to the SDK as the Go→host outbound wire bridge. The SDK and
// HostTransport together drive the device's view of the n-party MPC ceremony
// over Noise + circuit-relay v2 — the same transport the cli member
// subprocess (E2E-001) uses, and the same wire shape the mobile native
// bridge implements.
//
// When the host env is incomplete the ops fail loud with a clear "DM-5 host
// transport not configured" error rather than starting an MPC that has no
// peers and no way to send bytes (the pre-DM-5 placeholder behaviour).
// Wiring of $MPC_WALLET_HOST_* is the operator's responsibility; a future
// coord-driven dispatch can derive them from a START event without touching
// this file's surface (every host-build call site goes through buildHost).

// $MPC_WALLET_HOST_* names. Listed as constants so the readHostEnv error
// messages name the offending variable by its public symbol, and to keep
// the unit tests grepping for one source of truth.
const (
	envHostPSK         = "MPC_WALLET_HOST_PSK_HEX"
	envHostMemberKey   = "MPC_WALLET_HOST_MEMBER_KEY_HEX"
	envHostRelayPeerID = "MPC_WALLET_HOST_RELAY_PEER_ID"
	envHostRelayAddrs  = "MPC_WALLET_HOST_RELAY_ADDRS"
	envHostPeersJSON   = "MPC_WALLET_HOST_PEERS_JSON"
	envHostMemberSet   = "MPC_WALLET_HOST_MEMBER_SET"
	envHostGroupID     = "MPC_WALLET_HOST_GROUP_ID"
	envHostPartyIndex  = "MPC_WALLET_HOST_PARTY_INDEX"
	envHostThreshold   = "MPC_WALLET_HOST_THRESHOLD"
)

// hostEnv is the parsed-and-validated view of the $MPC_WALLET_HOST_* surface.
// It is per-operator (one device's perspective on its group) and reused
// across keygen / sign / reshare so the operator can chain operations under
// the same environment. SessionID is per-operation and is therefore NOT a
// field of hostEnv — it flows through the call signature.
type hostEnv struct {
	psk         []byte
	hostKey     crypto.PrivKey
	memberKey   *btcec.PrivateKey
	relay       peer.AddrInfo
	peers       cli.PeerTable
	peerPubKeys map[string][]byte
	memberSet   []string
	groupID     string
	partyIndex  int
	threshold   int
}

// peerEnvEntry is the per-tag JSON shape the operator supplies as
// $MPC_WALLET_HOST_PEERS_JSON: a tag → {peerID, secp256k1-pub-hex} map.
type peerEnvEntry struct {
	PeerID string `json:"peerID"`
	Pub    string `json:"pub"`
}

// readHostEnv parses every $MPC_WALLET_HOST_* variable. A missing or malformed
// value returns a wrapped error naming the offending variable; well-formed
// input returns a fully-validated *hostEnv the ops can hand to buildHost
// without further checks.
func readHostEnv() (*hostEnv, error) {
	pskHex := os.Getenv(envHostPSK)
	if pskHex == "" {
		return nil, fmt.Errorf("DM-5 host transport not configured (set $%s)", envHostPSK)
	}
	psk, err := hex.DecodeString(pskHex)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", envHostPSK, err)
	}
	if len(psk) != 32 {
		return nil, fmt.Errorf("%s: must be 32-byte hex (got %d bytes)", envHostPSK, len(psk))
	}
	mkHex := os.Getenv(envHostMemberKey)
	if mkHex == "" {
		return nil, fmt.Errorf("%s: required", envHostMemberKey)
	}
	mkBytes, err := hex.DecodeString(mkHex)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", envHostMemberKey, err)
	}
	memberKey, _ := btcec.PrivKeyFromBytes(mkBytes)
	hostPriv, _, err := crypto.GenerateEd25519Key(rand.Reader)
	if err != nil {
		return nil, fmt.Errorf("generate host key: %w", err)
	}
	relayPeerIDStr := os.Getenv(envHostRelayPeerID)
	if relayPeerIDStr == "" {
		return nil, fmt.Errorf("%s: required", envHostRelayPeerID)
	}
	relayAddrsStr := os.Getenv(envHostRelayAddrs)
	if relayAddrsStr == "" {
		return nil, fmt.Errorf("%s: required", envHostRelayAddrs)
	}
	relayPID, err := peer.Decode(relayPeerIDStr)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", envHostRelayPeerID, err)
	}
	relayAddrs := make([]ma.Multiaddr, 0, 2)
	for _, raw := range strings.Split(relayAddrsStr, ",") {
		raw = strings.TrimSpace(raw)
		if raw == "" {
			continue
		}
		m, merr := ma.NewMultiaddr(raw)
		if merr != nil {
			return nil, fmt.Errorf("%s: %q: %w", envHostRelayAddrs, raw, merr)
		}
		relayAddrs = append(relayAddrs, m)
	}
	if len(relayAddrs) == 0 {
		return nil, fmt.Errorf("%s: must list at least one multiaddr", envHostRelayAddrs)
	}
	peersRaw := os.Getenv(envHostPeersJSON)
	if peersRaw == "" {
		return nil, fmt.Errorf("%s: required", envHostPeersJSON)
	}
	var peerMap map[string]peerEnvEntry
	if err := json.Unmarshal([]byte(peersRaw), &peerMap); err != nil {
		return nil, fmt.Errorf("%s: %w", envHostPeersJSON, err)
	}
	peers := make(cli.PeerTable, len(peerMap))
	peerPubKeys := make(map[string][]byte, len(peerMap))
	for tag, e := range peerMap {
		pid, perr := peer.Decode(e.PeerID)
		if perr != nil {
			return nil, fmt.Errorf("%s[%q].peerID: %w", envHostPeersJSON, tag, perr)
		}
		pubBytes, perr := hex.DecodeString(e.Pub)
		if perr != nil {
			return nil, fmt.Errorf("%s[%q].pub: %w", envHostPeersJSON, tag, perr)
		}
		peers[tag] = pid
		peerPubKeys[tag] = pubBytes
	}
	memberSetRaw := os.Getenv(envHostMemberSet)
	if memberSetRaw == "" {
		return nil, fmt.Errorf("%s: required", envHostMemberSet)
	}
	memberSet := strings.Split(memberSetRaw, ",")
	for i := range memberSet {
		memberSet[i] = strings.TrimSpace(memberSet[i])
		if memberSet[i] == "" {
			return nil, fmt.Errorf("%s: empty entry at index %d", envHostMemberSet, i)
		}
	}
	groupID := os.Getenv(envHostGroupID)
	if groupID == "" {
		return nil, fmt.Errorf("%s: required", envHostGroupID)
	}
	piRaw := os.Getenv(envHostPartyIndex)
	if piRaw == "" {
		return nil, fmt.Errorf("%s: required", envHostPartyIndex)
	}
	partyIndex, err := strconv.Atoi(piRaw)
	if err != nil || partyIndex < 0 {
		return nil, fmt.Errorf("%s: must be a non-negative integer (got %q)", envHostPartyIndex, piRaw)
	}
	thrRaw := os.Getenv(envHostThreshold)
	if thrRaw == "" {
		return nil, fmt.Errorf("%s: required", envHostThreshold)
	}
	threshold, err := strconv.Atoi(thrRaw)
	if err != nil || threshold < 1 {
		return nil, fmt.Errorf("%s: must be a positive integer (got %q)", envHostThreshold, thrRaw)
	}
	return &hostEnv{
		psk:         psk,
		hostKey:     hostPriv,
		memberKey:   memberKey,
		relay:       peer.AddrInfo{ID: relayPID, Addrs: relayAddrs},
		peers:       peers,
		peerPubKeys: peerPubKeys,
		memberSet:   memberSet,
		groupID:     groupID,
		partyIndex:  partyIndex,
		threshold:   threshold,
	}, nil
}

// buildHost wires a HostTransport for the named session and dials every
// configured peer through the relay. Callers MUST Close the returned host
// (typically in a deferred block) after the protocol finishes.
//
// All peer dials go through the relay (protocol.md §36, R4): the wallet
// never opens a direct connection to another device. A peer that is offline
// or has not reserved a relay slot fails its individual dial with a wrapped
// error — the MPC ceremony cannot proceed without every party, so a
// pre-protocol abort here is the correct shape.
func (env *hostEnv) buildHost(ctx context.Context, sessionID string) (*cli.HostTransport, error) {
	host, err := cli.NewHostTransport(ctx, cli.HostTransportConfig{
		HostKey:     env.hostKey,
		PSK:         env.psk,
		Relays:      []peer.AddrInfo{env.relay},
		MemberKey:   env.memberKey,
		SessionID:   sessionID,
		Peers:       env.peers,
		PeerPubKeys: env.peerPubKeys,
	})
	if err != nil {
		return nil, err
	}
	for tag, pid := range env.peers {
		if pid == host.PeerID() {
			continue
		}
		dctx, dcancel := context.WithTimeout(ctx, 30*time.Second)
		derr := host.ConnectVia(dctx, env.relay.ID, pid)
		dcancel()
		if derr != nil {
			_ = host.Close()
			return nil, fmt.Errorf("connect to peer %q via relay: %w", tag, derr)
		}
	}
	return host, nil
}

// keygenOp runs a t-of-n keygen and returns the SDK summary JSON. progress
// receives coarse stage labels (terminal stderr for the session; discarded
// for the HTTP server).
//
// The host transport is built from $MPC_WALLET_HOST_* and torn down at the
// end of the call; the SDK drives one mpc.KeygenParty for THIS device, the
// HostTransport ferries every other party's messages over libp2p, and the
// terminal callback is delivered on cb.done.
func keygenOp(s *sdk.SDK, threshold, parties int, passphrase string, progress io.Writer) (string, error) {
	env, err := readHostEnv()
	if err != nil {
		return "", err
	}
	if len(env.memberSet) != parties {
		return "", fmt.Errorf("memberSet length %d != parties %d (check $%s)", len(env.memberSet), parties, envHostMemberSet)
	}
	if env.partyIndex >= parties {
		return "", fmt.Errorf("partyIndex %d >= parties %d (check $%s)", env.partyIndex, parties, envHostPartyIndex)
	}
	if env.threshold != threshold {
		return "", fmt.Errorf("threshold %d != $%s %d", threshold, envHostThreshold, env.threshold)
	}
	sessionID := newSessionID("keygen")
	cb := newProgressCB(progress)
	ctx, cancel := context.WithTimeout(context.Background(), opTimeout)
	defer cancel()
	host, err := env.buildHost(ctx, sessionID)
	if err != nil {
		return "", err
	}
	defer func() { _ = host.Close() }()
	cfg := envelope(map[string]any{
		"groupId":    env.groupID,
		"sessionID":  sessionID,
		"partyIndex": env.partyIndex,
		"n":          parties,
		"t":          threshold,
		"memberSet":  env.memberSet,
		"relay":      relayJSON(env.relay),
		"role":       "keygen",
		"passphrase": passphrase,
	})
	s.KeyGen(cfg, host, cb)
	if perr := host.Pump(ctx, s); perr != nil {
		return "", perr
	}
	o := <-cb.done
	if !o.ok {
		return "", fmt.Errorf("%s: %s", o.code, o.msg)
	}
	return o.payload, nil
}

// reshareOp reshares the in-session committee and returns the summary JSON.
func reshareOp(s *sdk.SDK, oldT, newT, newN int, passphrase string, progress io.Writer) (string, error) {
	env, err := readHostEnv()
	if err != nil {
		return "", err
	}
	if len(env.memberSet) != newN {
		return "", fmt.Errorf("memberSet length %d != newN %d (check $%s)", len(env.memberSet), newN, envHostMemberSet)
	}
	if env.partyIndex >= newN {
		return "", fmt.Errorf("partyIndex %d >= newN %d (check $%s)", env.partyIndex, newN, envHostPartyIndex)
	}
	sessionID := newSessionID("reshare")
	cb := newProgressCB(progress)
	ctx, cancel := context.WithTimeout(context.Background(), opTimeout)
	defer cancel()
	host, err := env.buildHost(ctx, sessionID)
	if err != nil {
		return "", err
	}
	defer func() { _ = host.Close() }()
	cfg := envelope(map[string]any{
		"groupId":    env.groupID,
		"sessionID":  sessionID,
		"partyIndex": env.partyIndex,
		"n":          newN,
		"oldT":       oldT,
		"newT":       newT,
		"memberSet":  env.memberSet,
		"relay":      relayJSON(env.relay),
		"role":       "reshare",
		"passphrase": passphrase,
	})
	s.Reshare(cfg, host, cb)
	if perr := host.Pump(ctx, s); perr != nil {
		return "", perr
	}
	o := <-cb.done
	if !o.ok {
		return "", fmt.Errorf("%s: %s", o.code, o.msg)
	}
	return o.payload, nil
}

// prepareSign builds the SDK configJSON and the libp2p host for one signing
// session, leaving the WYSIWYS approve/reject loop and final Close to the
// caller. The caller is responsible for:
//
//  1. Starting host.Pump(ctx, sdk) after calling sdk.Sign(cfg, host, cb).
//  2. Calling host.Close() once the SignCallback has fired its terminal call
//     (OnResult or OnError).
//
// This split is what lets the interactive shell (cmdSign in walletcli.go) and
// the HTTP server (signH/signDecisionH in httpapi.go) share one wiring path
// even though their decision moments live in different threads/HTTP calls.
func prepareSign(ctx context.Context, startJSON []byte) (cfg string, host *cli.HostTransport, err error) {
	env, err := readHostEnv()
	if err != nil {
		return "", nil, err
	}
	var head struct {
		RequestID string `json:"requestId"`
	}
	if err := json.Unmarshal(startJSON, &head); err != nil {
		return "", nil, fmt.Errorf("parse start: %w", err)
	}
	if head.RequestID == "" {
		return "", nil, fmt.Errorf("start.requestId is empty")
	}
	if env.partyIndex >= len(env.memberSet) {
		return "", nil, fmt.Errorf("partyIndex %d out of memberSet range (n=%d)", env.partyIndex, len(env.memberSet))
	}
	host, err = env.buildHost(ctx, head.RequestID)
	if err != nil {
		return "", nil, err
	}
	cfg = envelope(map[string]any{
		"groupId":    env.groupID,
		"sessionID":  head.RequestID,
		"partyIndex": env.partyIndex,
		"n":          len(env.memberSet),
		"t":          env.threshold,
		"memberSet":  env.memberSet,
		"relay":      relayJSON(env.relay),
		"role":       "signer",
		"start":      json.RawMessage(startJSON),
	})
	return cfg, host, nil
}

// importOp restores a backup blob into the in-memory committee.
func importOp(s *sdk.SDK, blob []byte, passphrase string) (string, error) {
	moniker, err := s.ImportShare(blob, passphrase)
	if err != nil {
		return "", err
	}
	return moniker, nil
}

// exportOp produces a passphrase-encrypted backup of one held share.
func exportOp(s *sdk.SDK, moniker, passphrase string) ([]byte, error) {
	return s.ExportShare(moniker, passphrase)
}

// fetchOp queries coord transaction info (no MPC); reqJSON is passed through.
func fetchOp(s *sdk.SDK, reqJSON string) (string, error) {
	return s.FetchTransactions(reqJSON)
}

// wireOp feeds one received MPC wire message through the SDK receive gate.
// Under DM-5 this remains a manual-injection seam for tooling that imports
// MPC bytes out-of-band; the standard inbound path runs through the
// HostTransport's Pump loop and never visits this function.
func wireOp(s *sdk.SDK, msg []byte) error {
	return s.OnWireMessage(msg)
}

// discard is an io.Writer that drops progress lines (HTTP/non-tty callers).
type discard struct{}

func (discard) Write(p []byte) (int, error) { return len(p), nil }

// envelope canonicalises one configJSON build site: a stable function instead
// of three open-coded json.Marshal calls keeps the envelope shape consistent
// across keygen / sign / reshare. json.Marshal over a map[string]any of
// basic types cannot fail in normal Go; the recovery path emits a deliberately
// malformed envelope so the SDK's hard-cut catches the regression downstream.
func envelope(m map[string]any) string {
	b, err := json.Marshal(m)
	if err != nil {
		return fmt.Sprintf(`{"role":%q,"_error":%q}`, fmt.Sprint(m["role"]), err.Error())
	}
	return string(b)
}

// relayJSON projects the configured relay AddrInfo into the {peerID, addrs[]}
// shape the SDK's hard-cut validator expects (mobileapi/keygen.go relayConfig).
func relayJSON(ai peer.AddrInfo) map[string]any {
	addrs := make([]string, 0, len(ai.Addrs))
	for _, a := range ai.Addrs {
		addrs = append(addrs, a.String())
	}
	return map[string]any{"peerID": ai.ID.String(), "addrs": addrs}
}

// newSessionID composes a session id that is stable across one operation but
// unique across the wallet process so the R5 gate never mistakes two
// different invocations for the same session.
func newSessionID(op string) string {
	var b [8]byte
	_, _ = rand.Read(b[:])
	return fmt.Sprintf("walletcli-%s-%s", op, hex.EncodeToString(b[:]))
}

// opTimeout bounds one MPC operation end-to-end. The cli E2E carrier already
// runs against a much tighter relay-circuit Duration cap; this conservative
// ceiling exists so an interactive shell never hangs forever on a missing
// peer.
const opTimeout = 8 * time.Minute
