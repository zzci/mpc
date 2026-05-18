/**
 * Builds the Go binaries the suite drives as subprocesses, mirroring
 * internal/cli `repoRoot`/`buildBinary` (CGO_ENABLED=1, cwd = module root).
 * Nothing here stubs Go — the real `cmd/node` and `cmd/cli` are compiled.
 */
import { existsSync } from 'node:fs'
import { dirname, join, resolve } from 'node:path'
import process from 'node:process'

/** Walk up from `e2e/` to the directory containing go.mod (the module root). */
export function repoRoot(): string {
  let dir = resolve(import.meta.dir, '..', '..', '..')
  for (let i = 0; i < 8; i++) {
    if (existsSync(join(dir, 'go.mod')))
      return dir
    const parent = dirname(dir)
    if (parent === dir)
      break
    dir = parent
  }
  throw new Error('go-build: could not locate go.mod above e2e/')
}

export async function hasGoToolchain(): Promise<boolean> {
  try {
    const p = Bun.spawn(['go', 'version'], { stdout: 'pipe', stderr: 'pipe' })
    return (await p.exited) === 0
  }
  catch {
    return false
  }
}

/** `go build -o <out> <pkg>` from the module root. Throws with build output. */
export async function buildBinary(root: string, pkg: string, out: string): Promise<string> {
  const proc = Bun.spawn(['go', 'build', '-o', out, pkg], {
    cwd: root,
    env: { ...process.env, CGO_ENABLED: '1' },
    stdout: 'pipe',
    stderr: 'pipe',
  })
  const code = await proc.exited
  if (code !== 0) {
    const err = await new Response(proc.stderr).text()
    const stdout = await new Response(proc.stdout).text()
    throw new Error(`go build ${pkg} failed (exit ${code}):\n${stdout}\n${err}`)
  }
  return out
}
