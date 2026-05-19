import type { Config } from './config.ts'
import type { ResultPayload } from './contract/index.ts'
import { ResultPayloadSchema } from './contract/index.ts'
import { verifyWebhookAuth } from './webhook-auth.ts'

// A4 webhook receiver: a minimal Bun.serve that accepts coord's terminal
// callback POST and resolves a per-requestId waiter. It enforces coord's
// dual-mode anti-forgery callback auth (user ruling 2026-05-19) before
// trusting the payload: an unverified POST is rejected 401 and never
// delivered, so a forged {requestId,status,RSV} cannot drive the double.
// Binds to localhost for the in-process E2E.

export class WebhookServer {
  private server: ReturnType<typeof Bun.serve> | undefined
  private readonly waiters = new Map<string, (p: ResultPayload) => void>()
  private readonly received = new Map<string, ResultPayload>()

  constructor(private readonly cfg: Config) {}

  get callbackURL(): string {
    if (!this.server)
      throw new Error('webhook server not started')
    return `http://${this.cfg.WEBHOOK_HOST}:${this.server.port}${this.cfg.WEBHOOK_PATH}`
  }

  start(): void {
    if (this.server)
      return
    this.server = Bun.serve({
      hostname: this.cfg.WEBHOOK_HOST,
      port: this.cfg.WEBHOOK_PORT,
      fetch: async (req) => {
        const url = new URL(req.url)
        if (req.method !== 'POST' || url.pathname !== this.cfg.WEBHOOK_PATH)
          return new Response('not found', { status: 404 })
        // Read the exact bytes for HMAC verification before parsing — the
        // signature binds the raw body, so parse-then-reserialize would
        // break verification.
        const rawBody = await req.text()
        const auth = verifyWebhookAuth(this.cfg, req.headers, rawBody, Math.floor(Date.now() / 1000))
        if (!auth.ok)
          return new Response(`unauthorized: ${auth.reason}`, { status: 401 })
        let payload: ResultPayload
        try {
          payload = ResultPayloadSchema.parse(JSON.parse(rawBody))
        }
        catch {
          return new Response('bad payload', { status: 400 })
        }
        this.deliver(payload)
        return new Response(null, { status: 204 })
      },
    })
  }

  private deliver(p: ResultPayload): void {
    this.received.set(p.requestId, p)
    const w = this.waiters.get(p.requestId)
    if (w) {
      this.waiters.delete(p.requestId)
      w(p)
    }
  }

  /** Resolve when coord posts the terminal callback for requestId, or reject on deadline. */
  waitForResult(requestId: string, deadlineMs: number): Promise<ResultPayload> {
    const already = this.received.get(requestId)
    if (already)
      return Promise.resolve(already)
    return new Promise<ResultPayload>((resolve, reject) => {
      const timer = setTimeout(() => {
        this.waiters.delete(requestId)
        reject(new Error(`webhook timeout waiting for ${requestId}`))
      }, deadlineMs)
      this.waiters.set(requestId, (p) => {
        clearTimeout(timer)
        resolve(p)
      })
    })
  }

  stop(): void {
    this.server?.stop(true)
    this.server = undefined
  }
}
