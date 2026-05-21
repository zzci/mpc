package walletcli

import (
	"bufio"
	"context"
	"flag"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"

	"github.com/zzci/mpc/sdk"
)

// passphraseEnv is the only channel for the keystore passphrase: never a flag
// or argv (process table / shell history leak), consistent with the project's
// secret-injection discipline.
const passphraseEnv = "MPC_WALLET_PASSPHRASE"

// keystoreEnv is the fallback for --keystore.
const keystoreEnv = "MPC_WALLET_KEYSTORE"

// version is the wallet-CLI surface version (bumped with the command set).
const version = "wallet-cli/1 (sdk parity)"

// wf is fmt.Fprintf with the write error deliberately discarded (terminal I/O
// failure is not actionable here); a named helper keeps errcheck satisfied
// without scattering blank assignments.
func wf(w io.Writer, format string, a ...any) { _, _ = fmt.Fprintf(w, format, a...) }

// wl is fmt.Fprintln with the write error deliberately discarded.
func wl(w io.Writer, a ...any) { _, _ = fmt.Fprintln(w, a...) }

// Run is the PC wallet-party entry point (everything except the `cli member`
// E2E carrier dispatches here). It opens one SDK handle for the lifetime of
// the session and reads newline-delimited commands from stdin, mirroring the
// mobile RN host that keeps a single SDK handle for the wallet's lifetime.
// It returns a process exit code.
func Run(args []string) int {
	// `serve` is a subcommand (HTTP shell), not a flag — dispatch before
	// flag parsing. Everything else is the interactive session.
	if len(args) > 0 && args[0] == "serve" {
		return serveHTTP(args[1:])
	}

	fs := flag.NewFlagSet("wallet", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	ksDir := fs.String("keystore", os.Getenv(keystoreEnv), "device keystore directory (or $"+keystoreEnv+")")
	showVer := fs.Bool("version", false, "print version and exit")
	fs.Usage = func() {
		wf(os.Stderr, "usage:\n"+
			"  cli [--keystore DIR]            interactive session\n"+
			"  cli serve [--listen ADDR] [--keystore DIR]   HTTP service\n"+
			"  cli --version\n\n"+
			"PC wallet party over the shared mobile SDK. Passphrase is read\n"+
			"only from $%s; the HTTP service binds loopback unless\n"+
			"$"+httpTokenEnv+" is set.\n\n", passphraseEnv)
		fs.PrintDefaults()
		wf(os.Stderr, "\n"+helpText)
	}
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if *showVer {
		wl(os.Stdout, version)
		return 0
	}
	if *ksDir == "" {
		wf(os.Stderr, "error: --keystore (or $%s) is required\n", keystoreEnv)
		return 2
	}

	s, err := sdk.NewSDK(*ksDir)
	if err != nil {
		wf(os.Stderr, "error: open keystore: %v\n", err)
		return 1
	}

	se := &session{
		sdk:         s,
		keystoreDir: *ksDir,
		out:         os.Stdout,
		errw:        os.Stderr,
		in:          bufio.NewScanner(os.Stdin),
	}
	wf(os.Stderr, "%s — keystore %s. type 'help', 'quit' to exit.\n", version, *ksDir)
	return se.loop()
}

// session holds the single long-lived SDK handle and the operator I/O. out
// carries machine-readable results (JSON / hex); errw carries prompts,
// progress and errors. keystoreDir is also the persistence root for the
// pairing record (see pair.go).
type session struct {
	sdk         *sdk.SDK
	keystoreDir string
	out         io.Writer
	errw        io.Writer
	in          *bufio.Scanner
}

const helpText = `commands:
  keygen <t> <n>                 run t-of-n keygen; shares sealed to keystore
  import <backup-file>           import an ExportShare backup into this session
  export <moniker> <out-file>    export a held share as a passphrase backup
  sign <start-file>              decode + approve/reject + sign a coord START
  reshare <oldT> <newT> <newN>   reshare the in-session committee
  fetch <req-file>               query coord transaction info (no MPC)
  xpub <req-file>                fetch this group's HD xpub from coord (B8)
  address <i> <xpub-file>        offline-derive m/<i> ETH/BSC/TRON address
  wire <msg-file>                feed one received MPC wire message
  pair <config-url>              consume a coord pairing QR (fetch config, POST identity, persist)
  help                           show this help
  quit | exit                    leave the session
`

func (se *session) loop() int {
	for {
		wf(se.errw, "wallet> ")
		if !se.in.Scan() {
			wl(se.errw)
			return 0
		}
		fields := strings.Fields(se.in.Text())
		if len(fields) == 0 {
			continue
		}
		cmd, rest := fields[0], fields[1:]
		switch cmd {
		case "help":
			wf(se.errw, "%s", helpText)
		case "quit", "exit":
			return 0
		case "keygen":
			se.cmdKeygen(rest)
		case "import":
			se.cmdImport(rest)
		case "export":
			se.cmdExport(rest)
		case "sign":
			se.cmdSign(rest)
		case "reshare":
			se.cmdReshare(rest)
		case "fetch":
			se.cmdFetch(rest)
		case "xpub":
			se.cmdXpub(rest)
		case "address":
			se.cmdAddress(rest)
		case "wire":
			se.cmdWire(rest)
		case "pair":
			se.cmdPair(rest)
		default:
			wf(se.errw, "unknown command %q (try 'help')\n", cmd)
		}
	}
}

// passphrase returns the keystore passphrase from the environment, or an
// error if unset (operations that seal/open shares require it).
func (se *session) passphrase() (string, error) {
	p := os.Getenv(passphraseEnv)
	if p == "" {
		return "", fmt.Errorf("$%s is not set", passphraseEnv)
	}
	return p, nil
}

func (se *session) fail(format string, a ...any) {
	wf(se.errw, "error: "+format+"\n", a...)
}

func (se *session) cmdKeygen(args []string) {
	if len(args) != 2 {
		se.fail("usage: keygen <t> <n>")
		return
	}
	t, err1 := strconv.Atoi(args[0])
	n, err2 := strconv.Atoi(args[1])
	if err1 != nil || err2 != nil {
		se.fail("t and n must be integers")
		return
	}
	pass, err := se.passphrase()
	if err != nil {
		se.fail("%v", err)
		return
	}
	summary, err := keygenOp(se.sdk, t, n, pass, se.errw)
	if err != nil {
		se.fail("keygen %v", err)
		return
	}
	wl(se.out, summary)
}

func (se *session) cmdReshare(args []string) {
	if len(args) != 3 {
		se.fail("usage: reshare <oldT> <newT> <newN>")
		return
	}
	ot, e1 := strconv.Atoi(args[0])
	nt, e2 := strconv.Atoi(args[1])
	nn, e3 := strconv.Atoi(args[2])
	if e1 != nil || e2 != nil || e3 != nil {
		se.fail("oldT, newT, newN must be integers")
		return
	}
	pass, err := se.passphrase()
	if err != nil {
		se.fail("%v", err)
		return
	}
	summary, err := reshareOp(se.sdk, ot, nt, nn, pass, se.errw)
	if err != nil {
		se.fail("reshare %v", err)
		return
	}
	wl(se.out, summary)
}

func (se *session) cmdImport(args []string) {
	if len(args) != 1 {
		se.fail("usage: import <backup-file>")
		return
	}
	blob, err := os.ReadFile(args[0])
	if err != nil {
		se.fail("read backup: %v", err)
		return
	}
	pass, err := se.passphrase()
	if err != nil {
		se.fail("%v", err)
		return
	}
	moniker, err := importOp(se.sdk, blob, pass)
	if err != nil {
		se.fail("import: %v", err)
		return
	}
	wf(se.out, "{\"imported\":%q}\n", moniker)
}

func (se *session) cmdExport(args []string) {
	if len(args) != 2 {
		se.fail("usage: export <moniker> <out-file>")
		return
	}
	pass, err := se.passphrase()
	if err != nil {
		se.fail("%v", err)
		return
	}
	blob, err := exportOp(se.sdk, args[0], pass)
	if err != nil {
		se.fail("export: %v", err)
		return
	}
	if err := os.WriteFile(args[1], blob, 0o600); err != nil {
		se.fail("write backup: %v", err)
		return
	}
	wf(se.out, "{\"exported\":%q,\"file\":%q}\n", args[0], args[1])
}

func (se *session) cmdSign(args []string) {
	if len(args) != 1 {
		se.fail("usage: sign <start-file>")
		return
	}
	startJSON, err := os.ReadFile(args[0])
	if err != nil {
		se.fail("read start: %v", err)
		return
	}
	cb := newSignCB(se.errw)
	ctx, cancel := context.WithTimeout(context.Background(), opTimeout)
	defer cancel()
	cfg, host, err := prepareSign(ctx, startJSON)
	if err != nil {
		se.fail("prepare sign: %v", err)
		return
	}
	defer func() { _ = host.Close() }()
	ss := se.sdk.Sign(cfg, host, cb)
	if err := host.Pump(ctx, se.sdk); err != nil {
		se.fail("pump sign: %v", err)
		return
	}

	// The WYSIWYS gate: show the device-recomputed decode, then require an
	// explicit operator decision before any MPC runs. OnError may also fire
	// first (e.g. bad envelope) with no decode.
	select {
	case d := <-cb.decoded:
		wl(se.errw, "-- decoded (device-recomputed) --")
		wf(se.errw, "A-facts:  %s\n", d.aFactsJSON)
		wf(se.errw, "B-info:   %s\n", d.bInfoJSON)
		wf(se.errw, "mismatch: %s\n", d.mismatchJSON)
		wf(se.errw, "approve this signature? [y/N]: ")
		ans := ""
		if se.in.Scan() {
			ans = strings.ToLower(strings.TrimSpace(se.in.Text()))
		}
		if ans == "y" || ans == "yes" {
			ss.Approve()
		} else {
			ss.Reject()
		}
	case o := <-cb.done:
		se.reportSign(o)
		return
	}
	se.reportSign(<-cb.done)
}

func (se *session) reportSign(o outcome) {
	if o.ok {
		wf(se.out, "{\"rsv\":%q}\n", o.payload)
		return
	}
	se.fail("sign %s: %s", o.code, o.msg)
}

func (se *session) cmdFetch(args []string) {
	if len(args) != 1 {
		se.fail("usage: fetch <req-file>")
		return
	}
	reqJSON, err := os.ReadFile(args[0])
	if err != nil {
		se.fail("read req: %v", err)
		return
	}
	res, err := fetchOp(se.sdk, string(reqJSON))
	if err != nil {
		se.fail("fetch: %v", err)
		return
	}
	wl(se.out, res)
}

func (se *session) cmdXpub(args []string) {
	if len(args) != 1 {
		se.fail("usage: xpub <req-file>")
		return
	}
	reqJSON, err := os.ReadFile(args[0])
	if err != nil {
		se.fail("read req: %v", err)
		return
	}
	res, err := xpubOp(se.sdk, string(reqJSON))
	if err != nil {
		se.fail("xpub: %v", err)
		return
	}
	wl(se.out, res)
}

func (se *session) cmdAddress(args []string) {
	if len(args) != 2 {
		se.fail("usage: address <i> <xpub-file>")
		return
	}
	idx, err := strconv.ParseUint(args[0], 10, 32)
	if err != nil {
		se.fail("index must be a non-negative integer < 2^31")
		return
	}
	xpubJSON, err := os.ReadFile(args[1])
	if err != nil {
		se.fail("read xpub: %v", err)
		return
	}
	res, err := addressOp(string(xpubJSON), uint32(idx))
	if err != nil {
		se.fail("address: %v", err)
		return
	}
	wl(se.out, res)
}

func (se *session) cmdWire(args []string) {
	if len(args) != 1 {
		se.fail("usage: wire <msg-file>")
		return
	}
	b, err := os.ReadFile(args[0])
	if err != nil {
		se.fail("read msg: %v", err)
		return
	}
	if err := wireOp(se.sdk, b); err != nil {
		se.fail("wire: %v", err)
		return
	}
	wl(se.out, "{\"wire\":\"accepted\"}")
}
