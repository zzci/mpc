/**
 * Live-run gate (docs/design/testing.md §3.1 last paragraph, L1 ruling:
 * "implementation parallel, live-run serial"). The full ring runs only when
 * every upstream dependency is actually present; otherwise the test `skip`s
 * with the precise unmet dependency. This is the sanctioned serial final
 * step, NOT a failure.
 */
import { existsSync, readdirSync, readFileSync } from 'node:fs'
import { join } from 'node:path'
import process from 'node:process'
import { hasGoToolchain, repoRoot } from '../../src/lib/go-build.ts'
import { locate } from '../../src/lib/mock-extsvc.ts'

/** XA-001 deliverable: the new external GET /v1/groups/{groupId}/public route. */
function xa001Merged(): boolean {
  try {
    const coordDir = join(repoRoot(), 'internal', 'server', 'coord')
    for (const f of readdirSync(coordDir)) {
      if (!f.endsWith('.go') || f.endsWith('_test.go'))
        continue
      const src = readFileSync(join(coordDir, f), 'utf8')
      if (src.includes('/v1/groups/{groupId}/public') || src.includes('groups/{groupId}/public'))
        return true
    }
  }
  catch {
    return false
  }
  return false
}

export interface Gate {
  ok: boolean
  reason: string
}

export async function liveGate(): Promise<Gate> {
  if (process.env.E2E_LIVE !== '1')
    return { ok: false, reason: 'E2E_LIVE != 1 (unit-only run; set E2E_LIVE=1 for the live ring)' }
  if (!existsSync(join(repoRoot(), 'go.mod')))
    return { ok: false, reason: 'go.mod not found above e2e/' }
  if (!(await hasGoToolchain()))
    return { ok: false, reason: 'Go toolchain unavailable (cannot build cmd/server + cmd/cli)' }
  if (!xa001Merged()) {
    return {
      ok: false,
      reason: 'XA-001 GET /v1/groups/{groupId}/public not merged into internal/server/coord (live-run serial per L1)',
    }
  }
  const m = locate()
  if (!m.available)
    return { ok: false, reason: m.reason ?? 'mock-extsvc (MEXT-001) unavailable' }
  return { ok: true, reason: '' }
}
