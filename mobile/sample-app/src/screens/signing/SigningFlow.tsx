// Orchestrates the three-screen WYSIWYS signing flow:
// detail → progress → result. The SignSession is created up-front (demo by
// default, since opening from Inbox does not have a live bridge session)
// and threaded into the detail screen so approve / reject preserve the
// @mcp/rn-bridge contract (docs/design/mcp/sdk.md §3, DREV-001 D4-1).

import React, { useMemo, useState } from 'react';
import type { SigningEnvelope } from '../../data';
import type { SignSession } from '../../sdk';
import { createDemoSignSession } from './demoSession';
import { SignDetailScreen } from './SignDetailScreen';
import { SignProgressScreen } from './SignProgressScreen';
import { SignResultScreen } from './SignResultScreen';
import type { SignOutcome } from './SignResultScreen';

type Phase =
  | { readonly kind: 'detail' }
  | { readonly kind: 'progress' }
  | { readonly kind: 'result'; readonly outcome: SignOutcome };

export interface SigningFlowProps {
  readonly envelope: SigningEnvelope;
  /** Live session from sign(startJSON, …). When omitted a demo session
   *  with the same {sessionId, approve, reject} shape is used. */
  readonly session?: SignSession;
  readonly onExit: () => void;
}

export function SigningFlow({ envelope, session, onExit }: SigningFlowProps): React.JSX.Element {
  const liveSession = !!session;
  const activeSession = useMemo<SignSession>(
    () => session ?? createDemoSignSession(envelope.requestId),
    [session, envelope.requestId],
  );
  const [phase, setPhase] = useState<Phase>({ kind: 'detail' });

  if (phase.kind === 'detail') {
    return (
      <SignDetailScreen
        envelope={envelope}
        session={activeSession}
        liveSession={liveSession}
        onApprove={() => setPhase({ kind: 'progress' })}
        onReject={() =>
          setPhase({
            kind: 'result',
            outcome: { kind: 'rejected', reason: envelope.mismatchHint },
          })
        }
        onBack={onExit}
      />
    );
  }

  if (phase.kind === 'progress') {
    return (
      <SignProgressScreen
        envelope={envelope}
        onComplete={(rsvBase64) =>
          setPhase({ kind: 'result', outcome: { kind: 'signed', rsvBase64 } })
        }
        onCancel={() =>
          setPhase({ kind: 'result', outcome: { kind: 'rejected' } })
        }
      />
    );
  }

  return <SignResultScreen envelope={envelope} outcome={phase.outcome} onClose={onExit} />;
}
