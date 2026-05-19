package walletcli

import (
	"bufio"
	"bytes"
	"strings"
	"testing"

	"github.com/zzci/mpc/sdk"
)

// newTestSession builds a session over a real (empty) keystore with scripted
// stdin and captured output. Opening the keystore is cheap; no MPC runs.
func newTestSession(t *testing.T, script string) (*session, *bytes.Buffer, *bytes.Buffer) {
	t.Helper()
	s, err := sdk.NewSDK(t.TempDir())
	if err != nil {
		t.Fatalf("NewSDK: %v", err)
	}
	out, errw := &bytes.Buffer{}, &bytes.Buffer{}
	return &session{
		sdk:  s,
		out:  out,
		errw: errw,
		in:   bufio.NewScanner(strings.NewReader(script)),
	}, out, errw
}

func TestSessionHelpAndQuit(t *testing.T) {
	se, _, errw := newTestSession(t, "help\nquit\n")
	if rc := se.loop(); rc != 0 {
		t.Fatalf("loop rc = %d, want 0", rc)
	}
	if !strings.Contains(errw.String(), "commands:") {
		t.Fatalf("help not printed: %q", errw.String())
	}
}

func TestSessionEOFExitsZero(t *testing.T) {
	se, _, _ := newTestSession(t, "")
	if rc := se.loop(); rc != 0 {
		t.Fatalf("EOF rc = %d, want 0", rc)
	}
}

func TestSessionUnknownCommand(t *testing.T) {
	se, _, errw := newTestSession(t, "frobnicate\nquit\n")
	se.loop()
	if !strings.Contains(errw.String(), `unknown command "frobnicate"`) {
		t.Fatalf("unknown not reported: %q", errw.String())
	}
}

func TestSessionUsageErrors(t *testing.T) {
	se, _, errw := newTestSession(t, "keygen 2\nexport only\nreshare 1 2\nsign\nquit\n")
	se.loop()
	s := errw.String()
	for _, want := range []string{
		"usage: keygen <t> <n>",
		"usage: export <moniker> <out-file>",
		"usage: reshare <oldT> <newT> <newN>",
		"usage: sign <start-file>",
	} {
		if !strings.Contains(s, want) {
			t.Fatalf("missing %q in:\n%s", want, s)
		}
	}
}

func TestSessionPassphraseGuard(t *testing.T) {
	// $MPC_WALLET_PASSPHRASE unset → ops that seal/open shares must fail
	// fast with a clear message, before any SDK heavy work.
	t.Setenv(passphraseEnv, "")
	se, _, errw := newTestSession(t, "keygen 1 3\nquit\n")
	se.loop()
	if !strings.Contains(errw.String(), passphraseEnv+" is not set") {
		t.Fatalf("passphrase guard not enforced: %q", errw.String())
	}
}

func TestSessionKeygenBadIntegers(t *testing.T) {
	t.Setenv(passphraseEnv, "pw")
	se, _, errw := newTestSession(t, "keygen a b\nquit\n")
	se.loop()
	if !strings.Contains(errw.String(), "t and n must be integers") {
		t.Fatalf("int validation missing: %q", errw.String())
	}
}

func TestRunVersion(t *testing.T) {
	if rc := Run([]string{"--version"}); rc != 0 {
		t.Fatalf("--version rc = %d, want 0", rc)
	}
}

func TestRunRequiresKeystore(t *testing.T) {
	t.Setenv(keystoreEnv, "")
	if rc := Run(nil); rc != 2 {
		t.Fatalf("missing keystore rc = %d, want 2", rc)
	}
}

func TestProgressCBOutcome(t *testing.T) {
	cb := newProgressCB(&bytes.Buffer{})
	cb.OnProgress("computing")
	cb.OnResult(`{"ok":1}`)
	if o := <-cb.done; !o.ok || o.payload != `{"ok":1}` {
		t.Fatalf("OnResult outcome = %+v", o)
	}
	cb2 := newProgressCB(&bytes.Buffer{})
	cb2.OnError("BAD_CONFIG", "boom")
	if o := <-cb2.done; o.ok || o.code != "BAD_CONFIG" || o.msg != "boom" {
		t.Fatalf("OnError outcome = %+v", o)
	}
}

func TestSignCBChannels(t *testing.T) {
	cb := newSignCB(&bytes.Buffer{})
	cb.OnDecoded(`{"a":1}`, `{"b":2}`, `[]`)
	d := <-cb.decoded
	if d.aFactsJSON != `{"a":1}` || d.bInfoJSON != `{"b":2}` || d.mismatchJSON != `[]` {
		t.Fatalf("decoded = %+v", d)
	}
	cb.OnResult([]byte{0xab, 0xcd})
	if o := <-cb.done; !o.ok || o.payload != "abcd" {
		t.Fatalf("sign result = %+v", o)
	}
}
