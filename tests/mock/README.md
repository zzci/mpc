# mock-extsvc (external business service double, test-only)

> MEXT-001. **Not a product service** — only a test double for the
> "external business service side" of the E2E (`docs/design/testing.md
> §3.1`), located at `tests/mock/` (a sibling of `tests/e2e/`), following
> `pma-bun`.

## Responsibilities (testing.md §3.1, three capabilities)

- **(a) Submit the signing envelope**: `POST /v1/requests` (api.md A2),
  self-constructing `metaHash` and `proposerSig` **byte-for-byte
  identical** to the Go `internal/contract` canonical serialization.
- **(b) Request the group address**: `GET /v1/groups/{groupId}/public`
  (api.md A1 / XA-001), reading `evm_address` / `tron_address` /
  `ecdsa_pubkey`.
- **(c) Receive and verify {R,S,V}**: webhook (A4) or longpoll (A3),
  `ecrecover` against the real three-chain digest, comparing the derived
  ETH/BSC (EIP-55) and TRON (Base58Check) addresses, re-checking coord's
  group-pubkey gate (callback.go `verifyRSV`).
- **(c′) Anti-forgery callback verification** (user ruling 2026-05-19,
  api.md A4): the webhook receiver verifies coord's outbound callback auth
  before trusting the payload, cross-language reconciled with
  `internal/server/coord/webhookauth.go` — signature mode recomputes
  HMAC-SHA256(`"<unix>.<raw body>"`), constant-time compares, and rejects
  replay/forgery by the skew window; token mode constant-time compares
  `Authorization: Bearer`; an unverified callback is 401 and never
  delivered. See `src/webhook-auth.ts` and `test/webhook-auth.test.ts`.

## Cross-language byte-exactness guarantee

`testdata/golden.json` is generated from the **real Go packages**
(`internal/contract` + `internal/addr`) via `testdata/gen/main.go` and is
the authoritative anchor. `test/golden.test.ts` asserts the TS
implementation **byte-for-byte reproduces** the Go output for the
canonical pre-image, `metaHash` (RFC 8785 JCS), `proposerSig` (secp256k1
DER, RFC 6979), `EmptyMetaHash`, EVM/TRON address derivation, and RSV
recovery; any drift on either side fails CI.

Regenerate (after changing the contract canonicalization or addr
derivation; run from the repo root):

```bash
go run ./mock-extsvc/testdata/gen > mock-extsvc/testdata/golden.json
```

## Quality gates

```bash
bun install
bun run lint        # eslint @antfu
bun run typecheck   # tsc --noEmit (strict, noUncheckedIndexedAccess)
bun test            # bun:test — 67 cases (incl. webhook callback-auth anti-forgery cases)
bun test --coverage # >80 gate
```

## Configuration (Zod-validated at startup, see `src/config.ts`)

`COORD_BASE_URL` (required), `COORD_API_KEY` / `COORD_API_KEY_HEADER`,
`RESULT_MODE`=`webhook|longpoll`, `WEBHOOK_HOST/PORT/PATH`, callback auth
`WEBHOOK_SECRET` (signature mode, preferred) / `WEBHOOK_API_KEY` (Bearer
fallback) / `WEBHOOK_SKEW_S` (signed-timestamp replay window, default
±300s; **YELLOW**: the L1 design does not define the verifier-side
tolerance, set to 300s per the dispatch instruction pending L1
confirmation), `LONGPOLL_WAIT_S`, `RESULT_DEADLINE_MS`, `PORT` (control
-plane port), `MOCKEXT_PROPOSER_PRIVKEY_HEX` (proposer identity key,
test-only, deterministic default `'11'*32` so the harness/coord can
predict the proposer pubkey). mTLS is out of scope for the in-process
E2E; api_key is the supported `coord.external` inbound auth mode.

## HTTP control plane (E2E-001 subprocess topology, testing.md §3.1)

`bun run start` (= `bun src/server.ts`) brings up a long-lived control
plane (`src/server.ts` `ControlServer`, **a thin wrapper over the already
-verified library API, with zero changes to the byte-exact
crypto/contract**), reading env `PORT`/`COORD_BASE_URL`/`COORD_API_KEY`;
the harness owns its lifecycle (SIGKILL). Endpoints (contract
`e2e/src/lib/mock-extsvc.ts:42-55`):

| Method Path | Response |
|---|---|
| `GET /healthz` | `200 {status:"ok"}` (harness readiness poll, <20s) |
| `POST /control/request-address {groupId}` | `200 {ecdsaPubkeyB64,evmAddress,tronAddress}` (drives coord A1) |
| `POST /control/submit {groupId,chain,digest32Hex,unsignedTxHex,requestId,expiryMillis}` | `202 {requestId}` (drives coord A2, reusing byte-exact proposerSig/metaHash) |
| `GET /control/result/{requestId}` | `200 {status,rsvB64?,recovered?}` (coord A4 + verification; unknown id → 404) |

## E2E-001 library usage (alternative)

`MockExtSvc.run(input, proposerPriv)` runs the full loop: A1 fetch group
→ self-construct envelope and A2 submit → await terminal → on `RETURNED`
independently verify {R,S,V} against the A1 addresses; `EXPIRED/REJECTED/
FAILED` return without verification (§3.2 "external receives EXPIRED").
