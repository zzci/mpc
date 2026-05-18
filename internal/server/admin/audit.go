package admin

import (
	"context"
	"encoding/json"
	"net"
	"net/http"
	"time"
)

// audit appends one admin_audit row via the D-001 store. The table is
// append-only by construction (coorddb exposes only AppendAdminAudit; this
// package exposes no update/delete route) so the administrator cannot tamper
// with the trail (admin.md §4/§7bis, database.md §6). params is marshalled to
// JSON and MUST NOT contain secrets — callers pass only non-secret descriptors
// (never the unlock passphrase or a rotated PSK value).
//
// admin_audit lives in the encrypted store, so a write needs UNLOCKED. Under
// LOCKED the underlying AppendAdminAudit returns coorddb.ErrLocked; unlock
// attempts are instead recorded to the process log (see unlock.go) and the
// success is back-filled here once UNLOCKED (admin.md §8 "成功后补记").
func (s *Server) audit(ctx context.Context, r *http.Request, sc scope, action string, params map[string]any) error {
	var p string
	if params != nil {
		b, err := json.Marshal(params)
		if err != nil {
			return err
		}
		p = string(b)
	}
	return s.store.AppendAdminAudit(ctx, adminID(r, sc), action, p,
		clientIP(r), time.Now().UTC().Format(time.RFC3339))
}

// clientIP is the audit `src_ip` (database.md §6 "来源"). RemoteAddr is
// host:port; strip the port. Reverse-proxy header trust is a deployment
// concern (admin.md §5 non-public) so X-Forwarded-For is intentionally not
// honored here — the recorded source is the direct peer.
func clientIP(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}
