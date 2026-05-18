import type { Config } from './config.ts'
import type { ResultPayload } from './contract/index.ts'
import { ResultPayloadSchema } from './contract/index.ts'

// A4 webhook receiver: a minimal Bun.serve that accepts coord's terminal
// callback POST and resolves a per-requestId waiter. Test-only; no auth/TLS —
// it binds to localhost for the in-process E2E.

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
        let payload: ResultPayload
        try {
          payload = ResultPayloadSchema.parse(await req.json())
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
