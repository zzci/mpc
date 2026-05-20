/**
 * DM-6 acceptance-suite gates (distributed-mpc-impl.md §G + L1
 * "implementation parallel, live-run serial" rule). Each test in this
 * directory selects the gate matching its dependency profile; an unmet
 * gate causes the test to `skip` with the precise unmet dependency
 * instead of failing — mirroring the E2E-001/E2E-002 pattern.
 *
 * Two gates:
 *
 *  - `attestationOnlyGate()` — requires only the live coord with B11
 *    attestation + the DM-6 commit primitive (both already in main).
 *    The test drives synthetic attestations directly; no real MPC
 *    binaries are needed.
 *
 *  - `realMpcGate()` — requires DM-5's PC CLI host_transport (which
 *    drives true multi-process keygen/sign/reshare across containers).
 *    Until DM-5 lands the DM-6 §G keygen / sign / reshare scenarios
 *    `skip` with the unmet dependency.
 */
import { existsSync, readdirSync, readFileSync } from 'node:fs'
import { join } from 'node:path'
import process from 'node:process'
import { hasGoToolchain, repoRoot } from '../../src/lib/go-build.ts'

export interface Gate {
  ok: boolean
  reason: string
}

function goModPresent(): boolean {
  return existsSync(join(repoRoot(), 'go.mod'))
}

/**
 * Probe the in-tree presence of a Go symbol — used to detect whether a
 * downstream layer (DM-5) has landed without spawning the toolchain.
 */
function symbolPresentIn(dir: string, ...needles: string[]): boolean {
  try {
    for (const f of readdirSync(join(repoRoot(), dir))) {
      if (!f.endsWith('.go') || f.endsWith('_test.go'))
        continue
      const src = readFileSync(join(repoRoot(), dir, f), 'utf8')
      for (const n of needles) {
        if (src.includes(n))
          return true
      }
    }
  }
  catch {
    return false
  }
  return false
}

/** DM-6 commit primitive present in coorddb. */
function dm6CommitPresent(): boolean {
  return symbolPresentIn('internal/server/coorddb', 'CommitAttestationQuorum')
}

/** DM-5 host_transport present (real PC-CLI keygen/sign/reshare wiring). */
function dm5HostTransportPresent(): boolean {
  return symbolPresentIn('internal/cli', 'host_transport')
}

export async function attestationOnlyGate(): Promise<Gate> {
  if (!goModPresent())
    return { ok: false, reason: 'go.mod not found above e2e/' }
  if (!(await hasGoToolchain()))
    return { ok: false, reason: 'Go toolchain unavailable (cannot build cmd/server)' }
  if (!dm6CommitPresent())
    return { ok: false, reason: 'DM-6 CommitAttestationQuorum not merged into coorddb' }
  return { ok: true, reason: '' }
}

export async function realMpcGate(): Promise<Gate> {
  if (process.env.E2E_DMPC !== '1')
    return { ok: false, reason: 'E2E_DMPC != 1 (set E2E_DMPC=1 to run real-MPC tests)' }
  const att = await attestationOnlyGate()
  if (!att.ok)
    return att
  if (!dm5HostTransportPresent()) {
    return {
      ok: false,
      reason: 'DM-5 PC CLI host_transport not merged into internal/cli (impl §B DM-5 prereq for §G real-MPC tests)',
    }
  }
  return { ok: true, reason: '' }
}
