import process from 'node:process'
import { z } from 'zod'

// Validate-at-startup config (pma-bun: never read Bun.env in business logic).
// All fields are test-harness wiring for E2E-001; nothing here is a product
// secret store. mTLS is out of scope for the in-process E2E — api_key is the
// supported coord.external.auth mode for the double.

const EnvSchema = z.object({
  // coord base URL, e.g. http://127.0.0.1:8080 (no trailing slash, no /v1).
  COORD_BASE_URL: z.string().url(),
  // api_key presented to coord.external.auth (api.md A1).
  COORD_API_KEY: z.string().min(1).optional(),
  // Header name carrying the api key; coord side decides the exact name.
  COORD_API_KEY_HEADER: z.string().min(1).default('X-Api-Key'),
  // Result delivery mode (api.md A4): webhook server or A3 longpoll.
  RESULT_MODE: z.enum(['webhook', 'longpoll']).default('longpoll'),
  // Webhook listener bind (RESULT_MODE=webhook). 0 → ephemeral port.
  WEBHOOK_HOST: z.string().default('127.0.0.1'),
  WEBHOOK_PORT: z.coerce.number().int().min(0).max(65535).default(0),
  WEBHOOK_PATH: z.string().startsWith('/').default('/extsvc/callback'),
  // Anti-forgery callback auth (user ruling 2026-05-19; server.md
  // change-summary item 4 / api.md A4). The verifier side of coord's dual mode:
  //   - WEBHOOK_SECRET set → signature mode: recompute HMAC-SHA256 over
  //     "<X-MCP-Timestamp>.<raw body>", constant-time compare to the
  //     X-MCP-Signature v1 hex, and reject outside the skew window.
  //   - else WEBHOOK_API_KEY set → token mode: constant-time compare the
  //     Authorization: Bearer value.
  //   - neither set → auth disabled (the longpoll E2E never POSTs here; the
  //     anti-forgery proof is the dedicated webhook-server unit test).
  WEBHOOK_SECRET: z.string().min(1).optional(),
  WEBHOOK_API_KEY: z.string().min(1).optional(),
  // Replay/forgery window for the signed timestamp, seconds. YELLOW: the
  // L1 design fixes the signed-timestamp scheme but does not specify the
  // *verifier-side* tolerance (coord.ttl.skew_tolerance is coord's own
  // expiry skew, a different axis), so this defaults to ±300s per the
  // dispatch instruction — flagged for L1 confirmation.
  WEBHOOK_SKEW_S: z.coerce.number().int().positive().default(300),
  // Longpoll wait seconds and overall result deadline.
  LONGPOLL_WAIT_S: z.coerce.number().int().positive().default(25),
  RESULT_DEADLINE_MS: z.coerce.number().int().positive().default(120_000),
  // Control-plane HTTP port for the E2E-001 harness (bun run start). 0 →
  // ephemeral; the harness sets PORT and polls GET /healthz for readiness.
  PORT: z.coerce.number().int().min(0).max(65535).default(0),
  // Proposer identity key (test-only, deterministic default = the golden
  // proposer key '11'*32 so the harness/coord can predict the proposer
  // pubkey). The double self-constructs proposerSig with this key.
  MOCKEXT_PROPOSER_PRIVKEY_HEX: z
    .string()
    .regex(/^[0-9a-f]{64}$/i, 'must be 64 hex chars')
    .default('1111111111111111111111111111111111111111111111111111111111111111'),
})

export type Config = z.infer<typeof EnvSchema>

export function loadConfig(env: NodeJS.ProcessEnv = process.env): Config {
  const parsed = EnvSchema.safeParse(env)
  if (!parsed.success)
    throw new Error(`mock-extsvc config invalid: ${parsed.error.message}`)
  return parsed.data
}
