# mcp-wallet-e2e (E2E-001)

Full-ring end-to-end acceptance suite, Bun + TypeScript (user ruling 2026-05-18,
`docs/design/testing.md` §3.1). Lives under `tests/e2e/`; the sibling
`tests/mock/` is the external-business-service double and `tests/docker/`
holds the Docker-isolated topology.

## What it is

A Bun/TS test orchestrator that drives the real Go binaries as external
subprocesses and asserts the cryptographic + protocol outcomes in TypeScript:

- **member side** = the CLI-001 harness — `cli member <config.json>` Go
  subprocesses running real tss-lib keygen/sign/reshare over libp2p
  Noise + circuit-relay v2 through a real `node` relay subprocess.
- **external business service side** = `mock-extsvc` (MEXT-001, Bun/TS,
  separate module): submits the signing envelope, requests the group
  address, receives `{R,S,V}` and verifies it.
- **coord** = the real `node` coord role (X-001) subprocess, unlocked at
  runtime through the admin-api (A-001) `POST /admin/unlock`.

## Deviation from `pma-bun` (intentional, documented)

`pma-bun`'s baseline targets an API service (OpenAPIHono + Drizzle + Zod
boundary). This module is a **test orchestrator + assertion suite**, not a
service: it exposes no HTTP surface and persists nothing. It therefore keeps
only the cross-cutting `pma-bun` rules — Bun 1.2+, strict TS with
`noUncheckedIndexedAccess`, ESLint + `@antfu/eslint-config` (no Prettier),
`bun:test`, typed errors, no `console.log` in library paths, immutable
updates, small focused files — and omits the server stack. Crypto is delegated
to audited libraries (`@noble/curves`, `@noble/hashes`); nothing re-implements
a cryptographic primitive (`docs/design/testing.md` §6).

## Orchestration design (the binding seam)

The §3.1 ring needs the relay-transported member MPC **and** the coord
envelope flow to agree on one signature over one real chain digest. The
existing `cli member` binary signs a digest fixed at config time and the
member↔coord B-side (B-002) is unmerged — exactly the situation
`internal/cli/coordflow_test.go` already handles by hand-rolling the coord
client. This suite uses the same proven seam:

1. The shared anchor is a **pinned real ETH EIP-155 signing digest**
   (`docs/design/testing.md` §3.2 / E-001 / `internal/cli` `eip155Digest`). The
   `mock-extsvc` envelope's `digest32` and the member-harness `DigestHex` are
   the *same* real chain digest, so the relay-MPC signature and the coord
   envelope are cryptographically bound — identical to what
   `coordflow_test.go` does (it feeds coord a separately produced signature
   over `eip155Digest`).
2. Phase 1 — relay MPC: spawn the `node` relay + N `cli member` subprocesses;
   collect `masterPub` (all agree) and the real `{R,S,V}` over the anchor
   digest (ecrecover == masterPub, low-S, reshare-invariant).
3. Phase 2 — coord ring: unlock coord; provision the group with `masterPub`
   (S-002 canonical); `mock-extsvc` requests the address (`GET
   /v1/groups/{groupId}/public`, api.md A1 — **XA-001**); `mock-extsvc`
   submits the envelope (A2); the suite hand-rolls the member B-side
   (heartbeat + approve → quorum → B6 START → tx-decode → B7 result with the
   Phase-1 `{R,S,V}`); coord group-pubkey-verifies and returns;
   `mock-extsvc` receives `RETURNED` and verifies `{R,S,V}`.
4. EXPIRED path: an envelope with a past expiry is rejected and surfaces
   terminal `EXPIRED` to `mock-extsvc`.

No `cli`/`mock-extsvc`/Go source is modified by this module (zero file
overlap; separate worktree) — the suite is written in parallel with
FIX-001/XA-001/MEXT-001 per the L1 ruling (`docs/design/testing.md` §3.1, last
paragraph: "implementation parallel, live-run serial").

## Live-run gate (serial, per L1)

`test/unit/*` (byte-exact canonical/crypto vectors) run unconditionally and
are green now. The live ring (`test/e2e/*`) is **gated**: it runs only when
`E2E_LIVE=1` **and** the Go toolchain is present **and** `mock-extsvc` is
resolvable (env `MOCK_EXTSVC_DIR`, default `../mock-extsvc`) **and**
XA-001's `GET /v1/groups/{groupId}/public` route is merged. Otherwise each
live test `skip`s with the precise unmet dependency — this is the L1-ruled
"live-run serial" final step, not a failure.

```bash
bun install
bun run lint && bun run typecheck && bun run test   # unit only (deps unmerged)
E2E_LIVE=1 bun run test:e2e                          # after FIX-001+XA-001+MEXT-001 merged
```

## E2E-002 — Docker isolated-VM ring (`docs/design/testing.md` §3.3)

An **additive, harder** gate that runs the *same* §3.1 ring with every
simulated machine in its **own container** on a real Docker bridge network
(no `127.0.0.1` shortcut). It is purely additive: §3.1's E2E-001 above is
untouched and stays GREEN; this lives behind its own `E2E_DOCKER=1` switch.

New artifacts only (per §3.3 "reuse, don't modify"): `docker/Dockerfile.{node,
member,mock-extsvc}`, `docker/docker-compose.yml`, `docker/server.yaml`
(reference template), `docker/*-entrypoint.sh`, the Bun/TS container adapter
`src/lib/docker-ring.ts`, and `test/e2e-docker/`. The images build the
*unmodified* `cmd/server`, `cmd/cli` and the finalized `mock-extsvc` module;
no cryptographic/contract/MPC code is changed.

**Topology & isolation.** One `node` container (relay+coord dual-role,
`ALLOW_INSECURE_DB=1`, encryption off), N member-device containers (each with
its OWN private `/work` bind holding the secret `memberKeyHex`/`groupKeyHex`
and `result.json`), and a `mock-extsvc` container. Members reach the relay
only via `/dns4/node/tcp/4001`. Peer discovery + barriers use a shared,
**secret-free** `/rz` rendezvous volume (only peerIds + PUBLIC member pubkeys
+ `ready-*` markers); the real MPC traffic crosses containers over libp2p
Noise + circuit-relay v2. The test asserts the §3.1 ring + EXPIRED, plus the
§3.3 isolation properties (all-conns-via-relay across containers, no loopback
addr, per-device secret physical isolation, secret-free shared volume).

```bash
bun run lint && bun run typecheck                    # static (no Docker needed)
E2E_DOCKER=1 bun run test:e2e-docker                 # live isolated ring (Docker + deps merged)
```
