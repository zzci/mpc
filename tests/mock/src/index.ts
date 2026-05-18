import process from 'node:process'
import { loadConfig } from './config.ts'
import { MockExtSvc } from './extsvc.ts'

// Entry point for E2E-001 to launch the external-service double as a process.
// It exposes the building blocks via exports; when run directly it only
// validates config and prints the callback URL (webhook mode) so the E2E
// orchestrator can wire coord.external.result_callback. The actual request
// loop is driven by the E2E test via the exported MockExtSvc API.

export { loadConfig } from './config.ts'
export type { Config } from './config.ts'
export * from './contract/index.ts'
export { CoordClient, CoordError } from './coord-client.ts'
export { buildEnvelope, MockExtSvc } from './extsvc.ts'
export type { EnvelopeInput, RunOutcome } from './extsvc.ts'
export { extractRSV, isTerminal } from './result.ts'
export { verifyGroupRSV } from './verify.ts'
export type { RSVVerification } from './verify.ts'

if (import.meta.main) {
  const cfg = loadConfig()
  const svc = new MockExtSvc(cfg)
  if (cfg.RESULT_MODE === 'webhook')
    process.stdout.write(`${JSON.stringify({ callbackURL: svc.callbackURL })}\n`)
  else
    process.stdout.write(`${JSON.stringify({ resultMode: cfg.RESULT_MODE })}\n`)
  // Keep the process alive in webhook mode so coord can post back; the E2E
  // harness owns lifecycle and will terminate this process.
  if (cfg.RESULT_MODE !== 'webhook')
    svc.close()
}
