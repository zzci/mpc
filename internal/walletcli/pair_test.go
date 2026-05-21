package walletcli

import (
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// fakeCoord stands in for the coord pairing endpoints — it serves a
// canned config on GET …/config and accepts the enroll POST, echoing the
// posted identity back in the response (so the client can verify the
// round-trip). The handler counts calls so tests can assert flow.
type fakeCoord struct {
	configHits  int
	enrollHits  int
	postedIdent string
	cfg         pairConfig
}

func (f *fakeCoord) handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /v1/pairing/{token}/config", func(w http.ResponseWriter, r *http.Request) {
		f.configHits++
		// Echo the token from the URL into the config so the client always
		// gets a matching pair (the real coord populates this from the store).
		out := f.cfg
		out.Token = r.PathValue("token")
		_ = json.NewEncoder(w).Encode(out)
	})
	mux.HandleFunc("POST /v1/pairing/{token}/enroll", func(w http.ResponseWriter, r *http.Request) {
		f.enrollHits++
		var body struct {
			IdentityPubkey string `json:"identityPubkey"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			http.Error(w, "bad json", http.StatusBadRequest)
			return
		}
		if len(body.IdentityPubkey) != 66 { // 33 bytes hex
			http.Error(w, "bad identity len", http.StatusBadRequest)
			return
		}
		if _, err := hex.DecodeString(body.IdentityPubkey); err != nil {
			http.Error(w, "bad identity hex", http.StatusBadRequest)
			return
		}
		f.postedIdent = body.IdentityPubkey
		out := f.cfg
		out.Token = r.PathValue("token")
		_ = json.NewEncoder(w).Encode(out)
	})
	return mux
}

func TestCmdPairHappyPath(t *testing.T) {
	f := &fakeCoord{cfg: pairConfig{
		GroupID:     "groupA",
		Label:       "Alice's iPhone",
		ExpiresAtMS: 9_000_000_000_000,
		RelayPeerID: "12D3KooWFAKE",
		RelayAddrs:  []string{"/ip4/203.0.113.1/tcp/4001"},
	}}
	srv := httptest.NewServer(f.handler())
	defer srv.Close()
	f.cfg.CoordBaseURL = srv.URL

	dir := t.TempDir()
	out := &strings.Builder{}
	errw := &strings.Builder{}
	se := &session{out: out, errw: errw, keystoreDir: dir}
	se.cmdPair([]string{srv.URL + "/v1/pairing/aaaa1111/config"})

	if errw.Len() != 0 {
		t.Fatalf("unexpected stderr: %s", errw.String())
	}
	if !strings.Contains(out.String(), `"paired":true`) {
		t.Fatalf("stdout missing paired:true: %s", out.String())
	}
	if f.configHits != 1 || f.enrollHits != 1 {
		t.Fatalf("hits: config=%d enroll=%d (want 1/1)", f.configHits, f.enrollHits)
	}
	if len(f.postedIdent) != 66 {
		t.Fatalf("posted identity not 33-byte hex: %q", f.postedIdent)
	}
	// pair.json should now exist in the keystore dir with matching identity.
	b, err := os.ReadFile(filepath.Join(dir, pairFileName))
	if err != nil {
		t.Fatalf("pair.json missing: %v", err)
	}
	var rec pairPersisted
	if err := json.Unmarshal(b, &rec); err != nil {
		t.Fatalf("pair.json bad json: %v", err)
	}
	if rec.IdentityPubHex != f.postedIdent {
		t.Fatalf("persisted pub != posted: %q vs %q", rec.IdentityPubHex, f.postedIdent)
	}
	if rec.GroupID != "groupA" || rec.RelayPeerID != "12D3KooWFAKE" {
		t.Fatalf("persisted fields lost: %+v", rec)
	}
	if rec.IdentityPrivHex == "" || len(rec.IdentityPrivHex) != 64 {
		t.Fatalf("priv hex bad: %q", rec.IdentityPrivHex)
	}
}

func TestCmdPairUsage(t *testing.T) {
	errw := &strings.Builder{}
	se := &session{out: &strings.Builder{}, errw: errw}
	se.cmdPair(nil)
	if !strings.Contains(errw.String(), "usage:") {
		t.Fatalf("usage line missing: %s", errw.String())
	}
}

func TestCmdPairBadURL(t *testing.T) {
	errw := &strings.Builder{}
	se := &session{out: &strings.Builder{}, errw: errw, keystoreDir: t.TempDir()}
	se.cmdPair([]string{"http://127.0.0.1:1/never"})
	if !strings.Contains(errw.String(), "pair: fetch config") {
		t.Fatalf("unreachable URL should fail: %s", errw.String())
	}
}

func TestCmdPairServerRejects(t *testing.T) {
	// The fake refuses every config GET with 410 (e.g. expired ticket).
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "expired", http.StatusGone)
	}))
	defer srv.Close()
	errw := &strings.Builder{}
	se := &session{out: &strings.Builder{}, errw: errw, keystoreDir: t.TempDir()}
	se.cmdPair([]string{srv.URL + "/v1/pairing/x/config"})
	if !strings.Contains(errw.String(), "status 410") {
		t.Fatalf("server rejection not surfaced: %s", errw.String())
	}
}
