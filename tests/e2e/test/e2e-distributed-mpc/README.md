# e2e-distributed-mpc — DM-6 closure-gate acceptance suite

> Source: `docs/design/mcp/distributed-mpc-impl.md` §G (DM-6 closure gate) +
> `docs/design/mcp/distributed-mpc.md` §3.7 (cross-n-party attestation
> commit) + §7 hard-acceptance criteria.

The §G hard acceptance gate for the distributed-MPC engine. Each test
lives in its own file and is independently gated; the orchestrator
asserts the §G properties end-to-end against a real `node` coord +
real member subprocesses (or skip-gates them with the precise unmet
dependency, mirroring the E2E-001 / E2E-002 pattern).

## What it covers (§G)

1. **`attestation-quorum.test.ts`** — §G(4) attestation consistency.
   Drives the B11 endpoint with n synthesized attestations against a
   live coord, asserts the DM-6 same-tx commit fires (groups row +
   ATTESTATION_QUORUM_COMMITTED audit) once every expected_members
   identity has reported a consistent (groupPubkey, chaincode). A
   single disagreeing report leaves coord in NEEDS_RESHARE /
   INCONSISTENT — no commit. **Runs live today.**

2. **`keygen-3of3.test.ts`** — §G(1) real n-process keygen with each
   member holding only its own share. **Skip-gated on DM-5 PC CLI
   host_transport (not yet merged).** When unmet the test reports the
   precise unmet dependency; runs live once DM-5 lands.

3. **`sign-2of3.test.ts`** — §G(2) any t+1 signs / any ≤t fails. Same
   DM-5 gate.

4. **`reshare.test.ts`** — §G(3) reshare 3-of-3 rotate + 3→4 expansion;
   old shares erased on member side, chaincode invariant. Same DM-5
   gate.

## Deviation from `pma-bun` (intentional, inherited)

This package extends the parent `mcp-wallet-e2e` orchestrator and
inherits its deviation profile (test orchestrator, not a service —
see `tests/e2e/README.md`). All shared rules — Bun 1.2+, strict TS,
ESLint `@antfu/eslint-config`, `bun:test`, audited crypto only — apply.

## Running

```bash
# Single attestation-quorum test (live today):
bun test test/e2e-distributed-mpc/attestation-quorum.test.ts

# Full suite once DM-5 lands:
E2E_DMPC=1 bun test test/e2e-distributed-mpc/
```

`E2E_DMPC=1` unlocks the DM-5-gated tests; they `skip` with the precise
unmet dependency otherwise. The attestation-quorum test runs
independently of that env (it relies only on the coord + DM-6 commit,
both already in main).

## Closure rule (impl §B DM-6)

The DM-6 batch is `finalize` only when this suite is GREEN — the
attestation-quorum subset is the immediately-checkable closure
component; the DM-5-gated subset closes once DM-5 lands and they
flip live.
