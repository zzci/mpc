package sdk

import api "github.com/zzci/mpc/internal/mobileapi"

// WrapForTest is the exported test seam that wraps a *mobileapi.SDK into
// the public SDK facade so tests in downstream packages (walletcli, e2e
// harnesses) can drive synthetic instances. It is intentionally NOT a
// _test.go-only helper because Go does not export _test symbols across
// packages; the name reflects the test-only intent.
func WrapForTest(inner *api.SDK) *SDK {
	return &SDK{inner: inner}
}
