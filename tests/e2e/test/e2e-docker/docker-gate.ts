/**
 * E2E-002 live-run gate (docs/design/testing.md §3.3). The Docker isolated ring
 * runs only when every dependency is actually present; otherwise the test
 * `skip`s with the precise unmet reason — same "implementation parallel,
 * live-run serial" discipline as §3.1's gate (test/e2e/gate.ts), and a
 * SEPARATE env switch (`E2E_DOCKER=1`) so a default `bun test` keeps the fast
 * §3.1 path and unit suites untouched (zero E2E-001 regression).
 */
import { existsSync, readdirSync, readFileSync } from 'node:fs'
import { hostname } from 'node:os'
import { join } from 'node:path'
import process from 'node:process'
import { repoRoot } from '../../src/lib/go-build.ts'
import { locate } from '../../src/lib/mock-extsvc.ts'

async function hasDockerCompose(): Promise<boolean> {
  try {
    const p = Bun.spawn(['docker', 'compose', 'version'], { stdout: 'pipe', stderr: 'pipe' })
    return (await p.exited) === 0
  }
  catch {
    return false
  }
}

/**
 * The §3.3 ring works by attaching THIS harness process to the compose
 * network (`docker network connect <net> <hostname>`, docker-ring.ts) so it
 * resolves node/mock-extsvc by service DNS — the real cross-container path.
 * That requires the harness to itself be a Docker container. On a non-
 * container host (e.g. a GitHub-hosted `ubuntu-latest` runner, which runs the
 * job on the VM) `hostname` is not a container and the attach fails. Detect
 * the exact capability up front so CI skips honestly instead of false-failing.
 */
async function harnessIsAttachableContainer(): Promise<boolean> {
  try {
    const p = Bun.spawn(['docker', 'inspect', '--type', 'container', hostname()], {
      stdout: 'pipe',
      stderr: 'pipe',
    })
    return (await p.exited) === 0
  }
  catch {
    return false
  }
}

/** XA-001 deliverable: the external GET /v1/groups/{groupId}/public route. */
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

export async function dockerLiveGate(): Promise<Gate> {
  if (process.env.E2E_DOCKER !== '1')
    return { ok: false, reason: 'E2E_DOCKER != 1 (set E2E_DOCKER=1 for the §3.3 Docker isolated ring; default runs keep the §3.1 fast path)' }
  if (!existsSync(join(repoRoot(), 'go.mod')))
    return { ok: false, reason: 'go.mod not found above e2e/' }
  if (!(await hasDockerCompose()))
    return { ok: false, reason: 'docker compose unavailable (cannot build/run the isolated topology)' }
  if (!(await harnessIsAttachableContainer()))
    return { ok: false, reason: `harness host "${hostname()}" is not a Docker container — the §3.3 ring attaches the harness to the compose network, which needs a containerized harness (e.g. GitHub-hosted ubuntu-latest runs jobs on the VM, not a container). Run E2E-002 in a container or locally.` }
  if (!xa001Merged())
    return { ok: false, reason: 'XA-001 GET /v1/groups/{groupId}/public not merged into internal/server/coord (live-run serial per L1)' }
  const m = locate()
  if (!m.available)
    return { ok: false, reason: m.reason ?? 'mock-extsvc (MEXT-001) unavailable' }
  return { ok: true, reason: '' }
}
