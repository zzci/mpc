package admin

import (
	"net"
	"net/http"
)

// netGate enforces the non-public boundary in-process (admin.md §5 "不对公网
// 暴露:仅内网 / VPN / mTLS / IP 允许列表", §7bis "admin-ui 非公网可达"). When
// an allowlist is configured every request whose source IP is outside all
// allowed CIDRs is rejected 403 before auth — so a misconfigured listener or a
// bypassed external boundary still fails closed instead of exposing the
// surface. With no allowlist it is a pass-through (the external network
// boundary is then the deployment's responsibility; Start warns loudly).
//
// X-Forwarded-For is intentionally NOT honored (same stance as audit.go
// clientIP): admin-api is non-public, so the trusted source is the direct
// peer; trusting a forwarded header here would let a client spoof its way
// into the allowlist.
func (s *Server) netGate(next http.Handler) http.Handler {
	if len(s.allowNets) == 0 {
		return next
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ip := remoteIP(r)
		if ip == nil || !ipAllowed(ip, s.allowNets) {
			s.log.Warn("admin request rejected: source not in allowlist", "src", clientIP(r))
			s.writeErr(w, errForbidden("source address not permitted"))
			return
		}
		next.ServeHTTP(w, r)
	})
}

// remoteIP parses the direct peer IP from RemoteAddr (host:port; falls back to
// the raw value for malformed input → treated as not-allowed by the caller).
func remoteIP(r *http.Request) net.IP {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		host = r.RemoteAddr
	}
	return net.ParseIP(host)
}

func ipAllowed(ip net.IP, nets []*net.IPNet) bool {
	for _, n := range nets {
		if n.Contains(ip) {
			return true
		}
	}
	return false
}

// isLoopbackListen reports whether the listen host is a loopback / unspecified
// host explicitly bound to localhost. Used only for the startup posture
// warning, not enforcement.
func isLoopbackListen(addr string) bool {
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		host = addr
	}
	if host == "" || host == "0.0.0.0" || host == "::" {
		return false // wildcard bind: treat as public-facing for the warning
	}
	if host == "localhost" {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}
