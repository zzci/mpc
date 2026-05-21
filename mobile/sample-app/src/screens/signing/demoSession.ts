// Builds a SignSession-shaped value when the bridge has not been started
// with a live START envelope. The shape mirrors @mcp/rn-bridge so the
// detail-screen footer always calls session.approve() / session.reject() —
// no UI path bypasses the bridge contract (docs/design/mcp/sdk.md §3).
// Only the bodies are no-ops; promise shape and call ordering are preserved.

import type { SignSession } from '../../sdk';

export function createDemoSignSession(sessionId: string): SignSession {
  return {
    sessionId,
    approve: () => Promise.resolve(),
    reject: () => Promise.resolve(),
  };
}
