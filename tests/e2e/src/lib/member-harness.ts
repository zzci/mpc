/**
 * Drives the CLI-001 member harness as real OS subprocesses
 * (`cli member <config.json>`): each runs real tss-lib keygen/sign/reshare
 * over libp2p Noise + circuit-relay v2 through the real `node` relay. The
 * DeviceConfig / DeviceResult shapes are the JSON contract of
 * internal/cli/device.go (cmd/cli `member` subcommand) — not modified here.
 */
import { mkdirSync, readFileSync } from 'node:fs'
import { join } from 'node:path'

/** Mirrors internal/cli `DeviceConfig` (cmd/cli member subcommand input). */
export interface DeviceConfig {
  index: number
  n: number
  threshold: number
  groupId: string
  relayPeerId: string
  relayAddrs: string[]
  pskHex: string
  groupPubHex: string
  memberKeyHex: string
  groupKeyHex: string
  signers: number[]
  digestHex: string
  rendezvousDir: string
  resultPath: string
}

/** Mirrors internal/cli `DeviceResult`. */
export interface DeviceResult {
  index: number
  groupPubHex: string
  resharedPubHex: string
  sigRHex: string
  sigSHex: string
  sigV: number
  signed: boolean
  allViaRelay: boolean
  err: string
}

export interface MemberRunParams {
  cliBin: string
  workdir: string
  relayPeerId: string
  relayAddrs: string[]
  pskHex: string
  /** hex of the wallet-group key that mints each device's cap token. */
  groupKeyHex: string
  /** the relay/MPC group id (distinct from the coord groupId). */
  groupId: string
  n: number
  threshold: number
  signers: number[]
  /** the pinned real chain digest (anchor) every device signs. */
  digestHex: string
  /** hex secp256k1 identity (senderAuth) per device index. */
  memberKeyHexByIndex: string[]
  /** overall budget for the whole keygen+sign+reshare phase. */
  timeoutMs: number
}

/**
 * Spawn N `cli member` subprocesses, wait for all to exit, return their
 * DeviceResults (index-ordered). Mirrors internal/cli `runDevicesInProc`
 * but as OS subprocesses, since the E2E carrier drives compiled binaries.
 */
export async function runMembers(p: MemberRunParams): Promise<DeviceResult[]> {
  const rzdir = join(p.workdir, 'rz')
  mkdirSync(rzdir, { recursive: true })

  const procs = []
  const resultPaths: string[] = []
  for (let i = 0; i < p.n; i++) {
    const resultPath = join(p.workdir, `result-${i}.json`)
    resultPaths.push(resultPath)
    const cfg: DeviceConfig = {
      index: i,
      n: p.n,
      threshold: p.threshold,
      groupId: p.groupId,
      relayPeerId: p.relayPeerId,
      relayAddrs: p.relayAddrs,
      pskHex: p.pskHex,
      groupPubHex: '',
      memberKeyHex: p.memberKeyHexByIndex[i] ?? '',
      groupKeyHex: p.groupKeyHex,
      signers: p.signers,
      digestHex: p.digestHex,
      rendezvousDir: rzdir,
      resultPath,
    }
    const cfgPath = join(p.workdir, `member-${i}.json`)
    await Bun.write(cfgPath, JSON.stringify(cfg))
    procs.push(Bun.spawn([p.cliBin, 'member', cfgPath], {
      cwd: p.workdir,
      stdout: 'inherit',
      stderr: 'inherit',
    }))
  }

  const timeout = new Promise<never>((_, rej) =>
    setTimeout(() => rej(new Error(`runMembers: phase exceeded ${p.timeoutMs}ms`)), p.timeoutMs))
  await Promise.race([Promise.all(procs.map(async pr => pr.exited)), timeout])

  return resultPaths.map((rp, i) => {
    const raw = readFileSync(rp, 'utf8')
    const r = JSON.parse(raw) as DeviceResult
    if (r.index !== i)
      throw new Error(`runMembers: result ${i} has index ${r.index}`)
    return r
  })
}

/** The 65-byte chain compact `{R,S,V}` = [V+27 ‖ R ‖ S] for the B7 result. */
export function rsvCompact(r: DeviceResult): Uint8Array {
  const out = new Uint8Array(65)
  out[0] = (r.sigV & 0xFF) + 27
  out.set(hex32(r.sigRHex), 1)
  out.set(hex32(r.sigSHex), 33)
  return out
}

function hex32(h: string): Uint8Array {
  const b = new Uint8Array(32)
  const s = h.padStart(64, '0')
  for (let i = 0; i < 32; i++)
    b[i] = Number.parseInt(s.slice(i * 2, i * 2 + 2), 16)
  return b
}
