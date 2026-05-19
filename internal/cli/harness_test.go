package cli

import (
	"bufio"
	"context"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	btcecdsa "github.com/btcsuite/btcd/btcec/v2/ecdsa"
)

// E2E test harness: it builds the real node binary and runs the node relay
// role as a separate OS subprocess; the wallet devices run as goroutines, each
// with its own libp2p host/peer, dialing only through that real relay. Nothing
// here stubs libp2p, the relay, or tss-lib — the only non-production
// substitution is tss-lib's bundled keygen pre-params (skips the multi-minute
// safe-prime search; docs/design/testing.md §6 sanctions "build on tss-lib's existing test mode",
// and the no-server-supplied-preparams custody red line is unaffected — each
// device loads its own locally).

const harnessPSKHex = "0f0e0d0c0b0a09080706050403020100ffeeddccbbaa99887766554433221100"

// real ETH EIP-155 spec signing digest (the same external anchor internal/
// txdecode and E-001 pin): signing it and recovering the group master public
// key cross-verifies {R,S,V}/low-S against a genuine chain digest.
const eip155Digest = "daf5a779ae972f972197303d7b574746c7ef83eadac0f2791ad23db92e4c8e53"

// eip155RLP is the EIP-155 spec example signing RLP whose keccak256 is
// eip155Digest (same external anchor internal/txdecode / E-001 pin); used as a
// real ETH unsignedTx so the device tx-decode recomputes a digest that binds.
const eip155RLP = "ec098504a817c800825208943535353535353535353535353535353535353535880de0b6b3a764000080018080"

func repoRoot(t *testing.T) string {
	t.Helper()
	out, err := exec.CommandContext(context.Background(), "go", "env", "GOMOD").Output()
	if err != nil {
		t.Fatalf("go env GOMOD: %v", err)
	}
	gomod := strings.TrimSpace(string(out))
	if gomod == "" || gomod == os.DevNull {
		t.Fatal("not in a go module")
	}
	return filepath.Dir(gomod)
}

func buildBinary(t *testing.T, root, pkg, out string) {
	t.Helper()
	cmd := exec.CommandContext(context.Background(), "go", "build", "-o", out, pkg)
	cmd.Dir = root
	cmd.Env = append(os.Environ(), "CGO_ENABLED=1")
	if b, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("build %s: %v\n%s", pkg, err, b)
	}
}

type relayHandle struct {
	cmd    *exec.Cmd
	PeerID string
	Addrs  []string
}

// startRelay launches the node binary in the relay role and parses its
// structured stderr for the ephemeral peer id + listen addrs.
func startRelay(t *testing.T, ctx context.Context, root, nodeBin, workdir, groupPubB64 string) *relayHandle {
	t.Helper()
	cfg := fmt.Sprintf(`log: {level: info, format: json}
relay:
  enable: true
  listen: ["/ip4/127.0.0.1/tcp/0"]
  pnet_psk_ref: "env:RELAY_PSK"
  token_verify: {source: config, group_pubkeys: ["%s"]}
  rendezvous: {enable: false}
  limits: {reservation_per_token: 8, reservation_per_group: 16, bandwidth_per_conn: "4MiB/s"}
`, groupPubB64)
	cfgPath := filepath.Join(workdir, "node.yaml")
	if err := os.WriteFile(cfgPath, []byte(cfg), 0o600); err != nil {
		t.Fatal(err)
	}
	cmd := exec.CommandContext(ctx, nodeBin)
	cmd.Dir = workdir
	cmd.Env = append(os.Environ(), "NODE_CONFIG="+cfgPath, "RELAY_PSK="+harnessPSKHex)
	stderr, err := cmd.StderrPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := cmd.Start(); err != nil {
		t.Fatalf("start relay: %v", err)
	}
	// Guarantee the relay subprocess is killed and reaped even on a failed or
	// panicking test, so a full-tree run never leaves a stray node process.
	t.Cleanup(func() {
		if cmd.Process != nil {
			_ = cmd.Process.Kill()
			_ = cmd.Wait()
		}
	})
	h := &relayHandle{cmd: cmd}
	got := make(chan struct{})
	go func() {
		sc := bufio.NewScanner(stderr)
		sc.Buffer(make([]byte, 64<<10), 1<<20)
		for sc.Scan() {
			var rec struct {
				Msg   string   `json:"msg"`
				Peer  string   `json:"peer"`
				Addrs []string `json:"addrs"`
			}
			if json.Unmarshal(sc.Bytes(), &rec) == nil && rec.Msg == "relay: started" {
				h.PeerID = rec.Peer
				h.Addrs = rec.Addrs
				close(got)
				return
			}
		}
	}()
	select {
	case <-got:
	case <-time.After(30 * time.Second):
		_ = cmd.Process.Kill()
		t.Fatal("relay did not report 'relay: started' within 30s")
	}
	if h.PeerID == "" || len(h.Addrs) == 0 {
		t.Fatalf("relay started with empty peer/addrs: %+v", h)
	}
	return h
}

// recoverPub runs secp256k1 ecrecover over (digest, R, S, V) and returns the
// recovered uncompressed public key, exactly how an ETH/BSC consumer verifies
// {R,S,V} (V+27 || R || S compact form).
func recoverPub(t *testing.T, digest []byte, rHex, sHex string, v int) []byte {
	t.Helper()
	r, _ := hex.DecodeString(rHex)
	s, _ := hex.DecodeString(sHex)
	compact := make([]byte, 65)
	compact[0] = byte(v) + 27
	copy(compact[1:33], r)
	copy(compact[33:65], s)
	pub, _, err := btcecdsa.RecoverCompact(compact, digest)
	if err != nil {
		t.Fatalf("ecrecover failed: %v", err)
	}
	return pub.SerializeUncompressed()
}
