package cli

import (
	"context"
	cryptorand "crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	btcec "github.com/btcsuite/btcd/btcec/v2"
	"github.com/libp2p/go-libp2p/core/crypto"
	"github.com/libp2p/go-libp2p/core/peer"
	ma "github.com/multiformats/go-multiaddr"

	"github.com/bnb-chain/tss-lib/v3/ecdsa/keygen"

	"github.com/zzci/mpc/internal/contract"
	"github.com/zzci/mpc/internal/transport"
)

// DeviceConfig is the JSON a member subprocess is launched with. Peers are
// discovered by a filesystem rendezvous (RendezvousDir): every device only
// learns the others' peerIDs and member pubkeys — never a direct address — so
// the only path to a peer is the relay circuit ("all traffic via the relay" by construction,
// not by convention).
type DeviceConfig struct {
	Index         int      `json:"index"`
	N             int      `json:"n"`
	Threshold     int      `json:"threshold"`
	GroupID       string   `json:"groupId"`
	RelayPeerID   string   `json:"relayPeerId"`
	RelayAddrs    []string `json:"relayAddrs"`
	PSKHex        string   `json:"pskHex"`
	GroupPubHex   string   `json:"groupPubHex"`  // wallet-group cap-token issuer pubkey (unused by member)
	MemberKeyHex  string   `json:"memberKeyHex"` // secp256k1 member identity (senderAuth)
	GroupKeyHex   string   `json:"groupKeyHex"`  // wallet-group key (mints this device's cap token)
	Signers       []int    `json:"signers"`      // participating indices for the sign phase
	DigestHex     string   `json:"digestHex"`    // 32-byte digest to sign
	RendezvousDir string   `json:"rendezvousDir"`
	ResultPath    string   `json:"resultPath"`
}

// DeviceResult is what a member subprocess writes on exit.
type DeviceResult struct {
	Index          int    `json:"index"`
	GroupPubHex    string `json:"groupPubHex"`    // uncompressed, post-keygen
	ResharedPubHex string `json:"resharedPubHex"` // uncompressed, post-reshare
	SigRHex        string `json:"sigRHex"`
	SigSHex        string `json:"sigSHex"`
	SigV           int    `json:"sigV"`
	Signed         bool   `json:"signed"`
	AllViaRelay    bool   `json:"allViaRelay"`
	Err            string `json:"err"`
}

type rendezvousEntry struct {
	Index     int    `json:"index"`
	PeerID    string `json:"peerId"`
	MemberPub string `json:"memberPub"` // hex secp256k1
}

// preParamGenTimeout bounds the real safe-prime / Paillier-secret search on
// the production path. The search is inherently multi-minute; the relay's
// circuit-v2 Duration cap is now keygen-aware (internal/server/relay limits)
// so a proof-enabled networked keygen fits the connection window.
const preParamGenTimeout = 5 * time.Minute

// fixturePreParamsHook lets the E2E acceptance carrier (cli package _test.go)
// substitute tss-lib's bundled fixtures for this device so the multi-minute
// safe-prime search is skipped under the binding `go test -race ./...` gate.
// It is nil in the shipped binary: tss-lib's LoadKeygenTestFixtures is
// test-utility code and is NEVER referenced from a compilable product path
// (RA-001 P1-2 — the fixture loader stays out of cmd/cli). Activating it also
// requires the explicit non-production marker (insecureMPCAllowed); production
// always takes the real-generation branch below.
var fixturePreParamsHook func(index, n int) (*keygen.LocalPreParams, error)

// preParamsFor returns this device's keygen pre-params. Production: each device
// generates its own Paillier/safe-prime params locally and freshly (custody
// invariant — never server-supplied, RA-001 P1-2). Dev/test only: the E2E
// carrier injects bundled fixtures via fixturePreParamsHook, and only when the
// explicit non-production marker is set (security.md invariant #10 discipline).
func preParamsFor(index, n int) (*keygen.LocalPreParams, error) {
	if fixturePreParamsHook != nil && insecureMPCAllowed() {
		return fixturePreParamsHook(index, n)
	}
	genCtx, cancel := context.WithTimeout(context.Background(), preParamGenTimeout)
	defer cancel()
	pp, err := keygen.GeneratePreParamsWithContext(genCtx)
	if err != nil {
		return nil, fmt.Errorf("cli: generate pre-params for device %d: %w", index, err)
	}
	return pp, nil
}

// RunDeviceInProc runs one device's protocol in-process and returns its
// result (no os.Exit, no result file). The E2E carrier runs the devices as
// goroutines — each with its own libp2p host/peer — through a real node-relay
// subprocess, instead of OS subprocesses: under the binding full-tree `go test
// -race ./...` gate the ~12 parallel race-instrumented package suites starve
// OS-forked children's scheduler so the libp2p/relay handshake cannot finish;
// goroutine devices remove that inter-process starvation while staying fully
// race-instrumented (more coverage) and keeping real Noise + real circuit-relay
// v2 through the real relay process. The transport is closed asynchronously so
// a graceful libp2p shutdown can never wedge the caller.
func RunDeviceInProc(ctx context.Context, cfg DeviceConfig) DeviceResult {
	res := DeviceResult{Index: cfg.Index, AllViaRelay: true}
	if err := runDevice(ctx, cfg, &res); err != nil {
		res.Err = err.Error()
	}
	return res
}

// RunDevice runs one device's protocol, writes its DeviceResult, then hard
// os.Exits. The hard exit is deliberate: under heavy parallel load a graceful
// libp2p host shutdown can stall on leftover relay-circuit goroutines after
// peers have already exited; a member subprocess has no post-result work, so
// it must never let teardown delay process exit (that stall once timed out the
// orchestrator at the 9-min ctx). A watchdog guarantees an exit even if the
// protocol itself wedges.
func RunDevice(ctx context.Context, cfg DeviceConfig) {
	done := make(chan struct{})
	res := DeviceResult{Index: cfg.Index, AllViaRelay: true}
	go func() {
		if err := runDevice(ctx, cfg, &res); err != nil {
			res.Err = err.Error()
		}
		close(done)
	}()
	var out DeviceResult
	select {
	case <-done:
		out = res // runDevice goroutine has returned: safe to read
	case <-time.After(8 * time.Minute):
		out = DeviceResult{Index: cfg.Index, Err: "cli: device watchdog timeout"}
	case <-ctx.Done():
		out = DeviceResult{Index: cfg.Index, Err: "cli: context cancelled: " + ctx.Err().Error()}
	}
	raw, _ := json.MarshalIndent(out, "", " ")
	_ = os.WriteFile(cfg.ResultPath, raw, 0o600)
	_ = os.Stderr.Sync()
	os.Exit(0) // do not block process exit on libp2p/host teardown
}

func dbg(idx int, format string, a ...any) {
	fmt.Fprintf(os.Stderr, "[dev %d %s] "+format+"\n",
		append([]any{idx, time.Now().Format("15:04:05.000")}, a...)...)
}

func runDevice(ctx context.Context, cfg DeviceConfig, res *DeviceResult) error {
	psk, err := hex.DecodeString(cfg.PSKHex)
	if err != nil {
		return fmt.Errorf("cli: bad psk hex: %w", err)
	}
	memberKeyBytes, err := hex.DecodeString(cfg.MemberKeyHex)
	if err != nil {
		return fmt.Errorf("cli: bad member key hex: %w", err)
	}
	memberKey, _ := btcec.PrivKeyFromBytes(memberKeyBytes)
	groupKeyBytes, err := hex.DecodeString(cfg.GroupKeyHex)
	if err != nil {
		return fmt.Errorf("cli: bad group key hex: %w", err)
	}
	groupKey, _ := btcec.PrivKeyFromBytes(groupKeyBytes)

	hostPriv, _, err := crypto.GenerateEd25519Key(cryptorand.Reader)
	if err != nil {
		return fmt.Errorf("cli: gen host key: %w", err)
	}

	relayAddrs := make([]ma.Multiaddr, 0, len(cfg.RelayAddrs))
	for _, a := range cfg.RelayAddrs {
		m, merr := ma.NewMultiaddr(a)
		if merr != nil {
			return fmt.Errorf("cli: bad relay addr %q: %w", a, merr)
		}
		relayAddrs = append(relayAddrs, m)
	}
	relayPID, err := peer.Decode(cfg.RelayPeerID)
	if err != nil {
		return fmt.Errorf("cli: bad relay peer id: %w", err)
	}
	relayAI := peer.AddrInfo{ID: relayPID, Addrs: relayAddrs}

	t, err := transport.New(ctx, transport.Config{
		HostKey: hostPriv,
		PSK:     psk,
		Relays:  []peer.AddrInfo{relayAI},
	})
	if err != nil {
		return fmt.Errorf("cli: transport: %w", err)
	}
	// Best-effort, non-blocking: a graceful libp2p host close can stall on
	// in-flight relay-circuit streams to peers that already exited. The
	// process hard-exits via RunDevice, so runDevice must not block its return
	// (hence the protocol result is written) on transport teardown.
	defer func() { go func() { _ = t.Close() }() }()

	// Present the wallet-group cap token, then reserve a relay slot so the
	// other devices can reach this one through the circuit.
	dbg(cfg.Index, "transport up, peer=%s; presenting cap", t.ID())
	memberID := partyTag(cfg.Index)
	tok := mintCapToken(groupKey, cfg.GroupID, memberID, contract.ScopeRelayReserve)
	capCtx, capCancel := context.WithTimeout(ctx, 30*time.Second)
	err = presentCap(capCtx, t.Host(), relayAI, tok)
	capCancel()
	if err != nil {
		return err
	}
	rsvCtx, rsvCancel := context.WithTimeout(ctx, 30*time.Second)
	err = t.ReserveRelays(rsvCtx)
	rsvCancel()
	if err != nil {
		return fmt.Errorf("cli: reserve relay: %w", err)
	}
	dbg(cfg.Index, "relay reserved")

	// Filesystem rendezvous: publish only (peerID, memberPub) — no direct
	// address — then wait for all n peers.
	memberPub := memberKey.PubKey().SerializeCompressed()
	if err := publishRendezvous(cfg, t.ID(), memberPub); err != nil {
		return err
	}
	dbg(cfg.Index, "rendezvous published; awaiting %d peers", cfg.N)
	entries, err := awaitRendezvous(ctx, cfg)
	if err != nil {
		return err
	}
	dbg(cfg.Index, "rendezvous complete")
	peers := make(peerTable, len(entries))
	pubByTag := make(map[string][]byte, len(entries))
	for _, e := range entries {
		pid, derr := peer.Decode(e.PeerID)
		if derr != nil {
			return fmt.Errorf("cli: bad peer id for %d: %w", e.Index, derr)
		}
		tag := partyTag(e.Index)
		peers[tag] = pid
		pb, herr := hex.DecodeString(e.MemberPub)
		if herr != nil {
			return fmt.Errorf("cli: bad member pub for %d: %w", e.Index, herr)
		}
		pubByTag[tag] = pb
	}

	// Dial every other device strictly through the relay circuit.
	for _, e := range entries {
		if e.Index == cfg.Index {
			continue
		}
		cvCtx, cvCancel := context.WithTimeout(ctx, 45*time.Second)
		err = t.ConnectVia(cvCtx, relayPID, peers[partyTag(e.Index)])
		cvCancel()
		if err != nil {
			return fmt.Errorf("cli: connect via relay to %d: %w", e.Index, err)
		}
		dbg(cfg.Index, "connected via relay to dev %d", e.Index)
	}
	if !allConnsViaRelay(t, peers, cfg.Index) {
		res.AllViaRelay = false
		return fmt.Errorf("cli: a peer connection is not over the relay circuit")
	}

	resolve := func(tag string) ([]byte, error) {
		// Reshare prefixes From with the sender's committee ('O'/'N'); member
		// identity is per device, so strip it back to the device tag.
		if len(tag) >= 2 && (tag[0] == 'O' || tag[0] == 'N') {
			tag = tag[1:]
		}
		pb, ok := pubByTag[tag]
		if !ok {
			return nil, fmt.Errorf("cli: unknown member %q", tag)
		}
		return pb, nil
	}

	// --- keygen ---
	pre, err := preParamsFor(cfg.Index, cfg.N)
	if err != nil {
		return err
	}
	kgSess, err := t.JoinSession(ctx, transport.SessionConfig{
		SessionID: cfg.GroupID + ":keygen", MemberKey: memberKey, Resolve: resolve,
	})
	if err != nil {
		return fmt.Errorf("cli: join keygen session: %w", err)
	}
	dbg(cfg.Index, "keygen session joined; at barrier")
	if err := barrier(ctx, cfg.RendezvousDir, "keygen", cfg.Index, allIndices(cfg.N)); err != nil {
		_ = kgSess.Close()
		return err
	}
	dbg(cfg.Index, "keygen barrier passed; running keygen")
	share, err := runKeygen(ctx, kgSess, peers, cfg.Index, cfg.N, cfg.Threshold, pre)
	_ = kgSess.Close()
	dbg(cfg.Index, "keygen done (err=%v)", err)
	if err != nil {
		return fmt.Errorf("cli: keygen: %w", err)
	}
	pub, err := groupPubUncompressed(share)
	if err != nil {
		return err
	}
	res.GroupPubHex = hex.EncodeToString(pub)

	// --- sign (only participating signers) ---
	if isSigner(cfg.Signers, cfg.Index) {
		digest, derr := hex.DecodeString(cfg.DigestHex)
		if derr != nil || len(digest) != 32 {
			return fmt.Errorf("cli: bad digest hex")
		}
		sgSess, serr := t.JoinSession(ctx, transport.SessionConfig{
			SessionID: cfg.GroupID + ":sign", MemberKey: memberKey, Resolve: resolve,
		})
		if serr != nil {
			return fmt.Errorf("cli: join sign session: %w", serr)
		}
		if berr := barrier(ctx, cfg.RendezvousDir, "sign", cfg.Index, cfg.Signers); berr != nil {
			_ = sgSess.Close()
			return berr
		}
		signerPeers := make(peerTable, len(cfg.Signers))
		for _, s := range cfg.Signers {
			tag := partyTag(s)
			if pid, ok := peers[tag]; ok {
				signerPeers[tag] = pid
			}
		}
		dbg(cfg.Index, "sign barrier passed; running sign")
		sig, serr := runSign(ctx, sgSess, signerPeers, cfg.Index, cfg.Threshold, cfg.Signers, share, digest)
		_ = sgSess.Close()
		dbg(cfg.Index, "sign done (err=%v)", serr)
		if serr != nil {
			return fmt.Errorf("cli: sign: %w", serr)
		}
		res.SigRHex = hex.EncodeToString(sig.R[:])
		res.SigSHex = hex.EncodeToString(sig.S[:])
		res.SigV = int(sig.V)
		res.Signed = true
	}

	// --- reshare (all devices; master public key must be invariant) ---
	rsPre, err := preParamsFor(cfg.Index, cfg.N)
	if err != nil {
		return err
	}
	rsSess, err := t.JoinSession(ctx, transport.SessionConfig{
		SessionID: cfg.GroupID + ":reshare", MemberKey: memberKey, Resolve: resolve,
	})
	if err != nil {
		return fmt.Errorf("cli: join reshare session: %w", err)
	}
	if err := barrier(ctx, cfg.RendezvousDir, "reshare", cfg.Index, allIndices(cfg.N)); err != nil {
		_ = rsSess.Close()
		return err
	}
	dbg(cfg.Index, "reshare barrier passed; running reshare")
	newShare, err := runReshare(ctx, rsSess, peers, cfg.Index, cfg.N, cfg.Threshold, cfg.Threshold, share, rsPre)
	_ = rsSess.Close()
	dbg(cfg.Index, "reshare done (err=%v)", err)
	if err != nil {
		return fmt.Errorf("cli: reshare: %w", err)
	}
	rpub, err := groupPubUncompressed(newShare)
	if err != nil {
		return err
	}
	res.ResharedPubHex = hex.EncodeToString(rpub)
	return nil
}

func isSigner(signers []int, idx int) bool {
	for _, s := range signers {
		if s == idx {
			return true
		}
	}
	return false
}

// allConnsViaRelay asserts every peer connection is a /p2p-circuit path, so
// "all traffic via the relay" is verified, not assumed.
func allConnsViaRelay(t *transport.Transport, peers peerTable, self int) bool {
	for tag, pid := range peers {
		if tag == partyTag(self) {
			continue
		}
		conns := t.Host().Network().ConnsToPeer(pid)
		if len(conns) == 0 {
			return false
		}
		viaRelay := false
		for _, c := range conns {
			if strings.Contains(c.RemoteMultiaddr().String(), "p2p-circuit") {
				viaRelay = true
				break
			}
		}
		if !viaRelay {
			return false
		}
	}
	return true
}

// barrier synchronises a protocol phase across devices: a device signals it
// has joined the phase session (its inbound stream handler is installed) only
// after every participant has, so no party.Start() races ahead of a peer that
// cannot yet receive — over a relay circuit a too-early send is a hard stream
// error, not a retryable buffer.
func barrier(ctx context.Context, dir, phase string, self int, participants []int) error {
	mark := filepath.Join(dir, fmt.Sprintf("ready-%s-%d", phase, self))
	if err := os.WriteFile(mark, []byte("1"), 0o600); err != nil {
		return fmt.Errorf("cli: barrier mark %s: %w", phase, err)
	}
	deadline := time.Now().Add(60 * time.Second)
	for {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		all := true
		for _, p := range participants {
			if _, err := os.Stat(filepath.Join(dir, fmt.Sprintf("ready-%s-%d", phase, p))); err != nil {
				all = false
				break
			}
		}
		if all {
			return nil
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("cli: barrier %s timed out", phase)
		}
		time.Sleep(120 * time.Millisecond)
	}
}

func allIndices(n int) []int {
	out := make([]int, n)
	for i := range out {
		out[i] = i
	}
	return out
}

func publishRendezvous(cfg DeviceConfig, id peer.ID, memberPub []byte) error {
	e := rendezvousEntry{Index: cfg.Index, PeerID: id.String(), MemberPub: hex.EncodeToString(memberPub)}
	raw, err := json.Marshal(e)
	if err != nil {
		return fmt.Errorf("cli: marshal rendezvous: %w", err)
	}
	p := filepath.Join(cfg.RendezvousDir, fmt.Sprintf("dev-%d.json", cfg.Index))
	tmp := p + ".tmp"
	if err := os.WriteFile(tmp, raw, 0o600); err != nil {
		return fmt.Errorf("cli: write rendezvous: %w", err)
	}
	return os.Rename(tmp, p) // atomic publish
}

func awaitRendezvous(ctx context.Context, cfg DeviceConfig) ([]rendezvousEntry, error) {
	deadline := time.Now().Add(60 * time.Second)
	for {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		var out []rendezvousEntry
		for i := 0; i < cfg.N; i++ {
			raw, err := os.ReadFile(filepath.Join(cfg.RendezvousDir, fmt.Sprintf("dev-%d.json", i)))
			if err != nil {
				out = nil
				break
			}
			var e rendezvousEntry
			if json.Unmarshal(raw, &e) != nil {
				out = nil
				break
			}
			out = append(out, e)
		}
		if len(out) == cfg.N {
			sort.Slice(out, func(a, b int) bool { return out[a].Index < out[b].Index })
			return out, nil
		}
		if time.Now().After(deadline) {
			return nil, fmt.Errorf("cli: rendezvous timed out waiting for %d devices", cfg.N)
		}
		time.Sleep(150 * time.Millisecond)
	}
}
