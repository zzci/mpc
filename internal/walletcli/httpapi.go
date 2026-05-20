package walletcli

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/zzci/mpc/sdk"
)

// httpTokenEnv optionally carries a bearer token required on every endpoint;
// it is mandatory when the listen address is not loopback (fail-closed, same
// philosophy as admin-api hardening). The keystore passphrase is never
// accepted over HTTP — only from passphraseEnv.
const httpTokenEnv = "MPC_WALLET_HTTP_TOKEN"

// serveHTTP runs the PC wallet party as an HTTP service: one long-lived SDK
// handle (mobile-host parity) exposed over the same operations as the
// interactive session. Returns a process exit code.
func serveHTTP(args []string) int {
	fs := flag.NewFlagSet("serve", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	listen := fs.String("listen", "127.0.0.1:8787", "listen address (loopback unless a bearer token is set)")
	ksDir := fs.String("keystore", os.Getenv(keystoreEnv), "device keystore directory (or $"+keystoreEnv+")")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if *ksDir == "" {
		wf(os.Stderr, "error: --keystore (or $%s) is required\n", keystoreEnv)
		return 2
	}
	token := os.Getenv(httpTokenEnv)
	if !isLoopbackAddr(*listen) && token == "" {
		wf(os.Stderr, "error: non-loopback --listen %q requires $%s (fail-closed)\n", *listen, httpTokenEnv)
		return 2
	}

	s, err := sdk.NewSDK(*ksDir)
	if err != nil {
		wf(os.Stderr, "error: open keystore: %v\n", err)
		return 1
	}
	srv := &httpServer{sdk: s, token: token, pending: map[string]*pendingSign{}}

	mux := http.NewServeMux()
	mux.HandleFunc("/v1/health", srv.guard(srv.health))
	mux.HandleFunc("/v1/version", srv.guard(srv.versionH))
	mux.HandleFunc("/v1/keygen", srv.guard(srv.keygenH))
	mux.HandleFunc("/v1/reshare", srv.guard(srv.reshareH))
	mux.HandleFunc("/v1/import", srv.guard(srv.importH))
	mux.HandleFunc("/v1/export", srv.guard(srv.exportH))
	mux.HandleFunc("/v1/fetch", srv.guard(srv.fetchH))
	mux.HandleFunc("/v1/wire", srv.guard(srv.wireH))
	mux.HandleFunc("/v1/sign", srv.guard(srv.signH))
	mux.HandleFunc("/v1/sign/", srv.guard(srv.signDecisionH))

	hs := &http.Server{
		Addr:              *listen,
		Handler:           mux,
		ReadHeaderTimeout: 10 * time.Second,
	}
	wf(os.Stderr, "%s — HTTP on %s, keystore %s (token auth: %v)\n",
		version, *listen, *ksDir, token != "")
	if err := hs.ListenAndServe(); err != nil {
		wf(os.Stderr, "error: http serve: %v\n", err)
		return 1
	}
	return 0
}

// isLoopbackAddr reports whether host:port binds only the loopback interface.
func isLoopbackAddr(addr string) bool {
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		return false
	}
	if host == "" {
		return false // wildcard / all interfaces
	}
	if host == "localhost" {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

type httpServer struct {
	sdk     *sdk.SDK
	token   string
	mu      sync.Mutex
	pending map[string]*pendingSign
}

// pendingSign is an in-flight two-step signing request awaiting an explicit
// approve/reject decision (WYSIWYS preserved over HTTP).
type pendingSign struct {
	ss *sdk.SignSession
	cb *signCB
}

// guard enforces the optional bearer token (constant-time) before any handler.
func (h *httpServer) guard(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if h.token != "" {
			got := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
			if subtle.ConstantTimeCompare([]byte(got), []byte(h.token)) != 1 {
				writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
				return
			}
		}
		next(w, r)
	}
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}

func httpErr(w http.ResponseWriter, code int, format string, a ...any) {
	writeJSON(w, code, map[string]string{"error": fmt.Sprintf(format, a...)})
}

// requirePOST + JSON body decode; returns false if it already wrote a response.
func decodeBody(w http.ResponseWriter, r *http.Request, dst any) bool {
	if r.Method != http.MethodPost {
		httpErr(w, http.StatusMethodNotAllowed, "POST required")
		return false
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(dst); err != nil {
		httpErr(w, http.StatusBadRequest, "invalid JSON body: %v", err)
		return false
	}
	return true
}

func httpPassphrase(w http.ResponseWriter) (string, bool) {
	p := os.Getenv(passphraseEnv)
	if p == "" {
		httpErr(w, http.StatusPreconditionFailed, "$%s is not set on the server", passphraseEnv)
		return "", false
	}
	return p, true
}

func (h *httpServer) health(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (h *httpServer) versionH(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"version": version})
}

func (h *httpServer) keygenH(w http.ResponseWriter, r *http.Request) {
	var req struct{ Threshold, Parties int }
	if !decodeBody(w, r, &req) {
		return
	}
	pass, ok := httpPassphrase(w)
	if !ok {
		return
	}
	summary, err := keygenOp(h.sdk, req.Threshold, req.Parties, pass, discard{})
	if err != nil {
		httpErr(w, http.StatusBadGateway, "keygen: %v", err)
		return
	}
	writeJSON(w, http.StatusOK, json.RawMessage(summary))
}

func (h *httpServer) reshareH(w http.ResponseWriter, r *http.Request) {
	var req struct{ OldThreshold, NewThreshold, NewParties int }
	if !decodeBody(w, r, &req) {
		return
	}
	pass, ok := httpPassphrase(w)
	if !ok {
		return
	}
	summary, err := reshareOp(h.sdk, req.OldThreshold, req.NewThreshold, req.NewParties, pass, discard{})
	if err != nil {
		httpErr(w, http.StatusBadGateway, "reshare: %v", err)
		return
	}
	writeJSON(w, http.StatusOK, json.RawMessage(summary))
}

func (h *httpServer) importH(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Blob string `json:"blob"` // base64
	}
	if !decodeBody(w, r, &req) {
		return
	}
	blob, err := base64.StdEncoding.DecodeString(req.Blob)
	if err != nil {
		httpErr(w, http.StatusBadRequest, "blob must be base64: %v", err)
		return
	}
	pass, ok := httpPassphrase(w)
	if !ok {
		return
	}
	moniker, err := importOp(h.sdk, blob, pass)
	if err != nil {
		httpErr(w, http.StatusBadGateway, "import: %v", err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"imported": moniker})
}

func (h *httpServer) exportH(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Moniker string `json:"moniker"`
	}
	if !decodeBody(w, r, &req) {
		return
	}
	pass, ok := httpPassphrase(w)
	if !ok {
		return
	}
	blob, err := exportOp(h.sdk, req.Moniker, pass)
	if err != nil {
		httpErr(w, http.StatusBadGateway, "export: %v", err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{
		"moniker": req.Moniker,
		"blob":    base64.StdEncoding.EncodeToString(blob),
	})
}

func (h *httpServer) fetchH(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		httpErr(w, http.StatusMethodNotAllowed, "POST required")
		return
	}
	body, err := io.ReadAll(io.LimitReader(r.Body, 1<<20))
	if err != nil {
		httpErr(w, http.StatusBadRequest, "read body: %v", err)
		return
	}
	res, err := fetchOp(h.sdk, string(body))
	if err != nil {
		httpErr(w, http.StatusBadGateway, "fetch: %v", err)
		return
	}
	writeJSON(w, http.StatusOK, json.RawMessage(res))
}

func (h *httpServer) wireH(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Msg string `json:"msg"` // base64
	}
	if !decodeBody(w, r, &req) {
		return
	}
	b, err := base64.StdEncoding.DecodeString(req.Msg)
	if err != nil {
		httpErr(w, http.StatusBadRequest, "msg must be base64: %v", err)
		return
	}
	if err := wireOp(h.sdk, b); err != nil {
		httpErr(w, http.StatusBadGateway, "wire: %v", err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"wire": "accepted"})
}

// signH starts a signing request and returns the device-recomputed decode
// plus a session id; the signature is NOT produced until an explicit
// /v1/sign/{id}/approve call (WYSIWYS preserved — no silent auto-sign).
func (h *httpServer) signH(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Start json.RawMessage `json:"start"`
	}
	if !decodeBody(w, r, &req) {
		return
	}
	if len(req.Start) == 0 {
		httpErr(w, http.StatusBadRequest, "missing 'start' (coord START JSON)")
		return
	}
	cb := newSignCB(discard{})
	cfg, err := wrapSignConfig(req.Start)
	if err != nil {
		httpErr(w, http.StatusBadRequest, "wrap start: %v", err)
		return
	}
	ss := h.sdk.Sign(cfg, cliWire{}, cb)

	select {
	case d := <-cb.decoded:
		var idb [16]byte
		_, _ = rand.Read(idb[:])
		id := hex.EncodeToString(idb[:])
		h.mu.Lock()
		h.pending[id] = &pendingSign{ss: ss, cb: cb}
		h.mu.Unlock()
		writeJSON(w, http.StatusOK, map[string]any{
			"id":       id,
			"aFacts":   json.RawMessage(orNull(d.aFactsJSON)),
			"bInfo":    json.RawMessage(orNull(d.bInfoJSON)),
			"mismatch": json.RawMessage(orNull(d.mismatchJSON)),
		})
	case o := <-cb.done:
		if o.ok { // unexpected (no decode) but a result anyway
			writeJSON(w, http.StatusOK, map[string]string{"rsv": o.payload})
			return
		}
		httpErr(w, http.StatusBadGateway, "sign %s: %s", o.code, o.msg)
	}
}

// signDecisionH resolves /v1/sign/{id}/approve|reject.
func (h *httpServer) signDecisionH(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		httpErr(w, http.StatusMethodNotAllowed, "POST required")
		return
	}
	rest := strings.TrimPrefix(r.URL.Path, "/v1/sign/")
	id, action, found := strings.Cut(rest, "/")
	if !found || (action != "approve" && action != "reject") {
		httpErr(w, http.StatusNotFound, "want /v1/sign/{id}/approve|reject")
		return
	}
	h.mu.Lock()
	p, ok := h.pending[id]
	if ok {
		delete(h.pending, id)
	}
	h.mu.Unlock()
	if !ok {
		httpErr(w, http.StatusNotFound, "unknown or already-resolved sign id")
		return
	}
	if action == "approve" {
		p.ss.Approve()
	} else {
		p.ss.Reject()
	}
	o := <-p.cb.done
	if !o.ok {
		httpErr(w, http.StatusBadGateway, "sign %s: %s", o.code, o.msg)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"rsv": o.payload})
}

// orNull keeps the JSON response well-formed when the SDK passes an empty
// decode field.
func orNull(s string) string {
	if strings.TrimSpace(s) == "" {
		return "null"
	}
	return s
}
