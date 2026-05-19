package admin

import (
	"fmt"
	"net"
)

// minTokenLen is the floor on the read/control bearer secrets (P6 §4 read/
// control separation hardening): a short, guessable token makes the privilege
// boundary vacuous because the weaker scope can be brute-forced into the
// stronger one. 32 hex / base64 chars ≈ ≥128 bits is the deployment guidance;
// 16 is the hard reject threshold so a fat-fingered "x" cannot ship.
const minTokenLen = 16

// Config is the resolved admin-api runtime configuration. cmd/server builds it
// after secrets are resolved (env-only, never a committed literal), so this
// package never reads files/env itself and stays unit testable.
//
// admin.md §5: the admin-api is NOT public. Listen SHOULD be a loopback / VPN
// address; AllowedCIDRs adds an in-process IP allowlist so the non-public
// boundary is enforced by code, not only by deployment hygiene. Strong auth
// (mTLS / OIDC+2FA) is wired through the StrongAuth seam and WithTLS option
// (strongauth.go) — this struct stays free of file/secret IO.
type Config struct {
	// Listen is the admin-api bind address. Keep it off public interfaces.
	Listen string
	// ReadToken authorizes the read-only query surface (admin.md §4 read
	// privilege). Required; must be ≥ minTokenLen.
	ReadToken string
	// ControlToken authorizes abuse controls + unlock/relock (admin.md §4
	// control privilege). It is distinct from ReadToken so a leaked read
	// credential cannot drive controls; it also implies read access.
	// Required; must be ≥ minTokenLen.
	ControlToken string
	// AllowedCIDRs, when non-empty, restricts every endpoint to source IPs
	// inside one of these CIDRs (admin.md §5/§7bis "not public-internet reachable"). Empty means
	// no in-process allowlist — admissible only behind an external network
	// boundary; New logs a prominent hardening warning in that case.
	AllowedCIDRs []string
}

// validate rejects an unsafe Config. Both tokens are mandatory, must meet the
// length floor, and must differ, otherwise the read/control privilege
// separation (admin.md §4, §7bis "read/control privilege separation in effect") would be vacuous.
func (c Config) validate() error {
	if c.Listen == "" {
		return fmt.Errorf("admin: empty listen address")
	}
	if c.ReadToken == "" || c.ControlToken == "" {
		return fmt.Errorf("admin: read and control tokens are both required")
	}
	if len(c.ReadToken) < minTokenLen || len(c.ControlToken) < minTokenLen {
		return fmt.Errorf("admin: read and control tokens must each be ≥ %d chars (privilege-separation hardening)", minTokenLen)
	}
	if c.ReadToken == c.ControlToken {
		return fmt.Errorf("admin: read and control tokens must differ (privilege separation)")
	}
	if _, err := c.allowedNets(); err != nil {
		return err
	}
	return nil
}

// allowedNets parses AllowedCIDRs once into matchable networks. A malformed
// entry is a hard config error (fail-closed: never silently widen the
// non-public boundary).
func (c Config) allowedNets() ([]*net.IPNet, error) {
	nets := make([]*net.IPNet, 0, len(c.AllowedCIDRs))
	for _, cidr := range c.AllowedCIDRs {
		_, n, err := net.ParseCIDR(cidr)
		if err != nil {
			return nil, fmt.Errorf("admin: invalid AllowedCIDRs entry %q: %w", cidr, err)
		}
		nets = append(nets, n)
	}
	return nets, nil
}
