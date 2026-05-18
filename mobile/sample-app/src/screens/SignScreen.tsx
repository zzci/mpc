// Signing example with the multi-party WYSIWYS approval flow
// (docs/design/mcp/sdk.md §3). sign(startJSON, cb) returns a SignSession; the
// SignCallback carries ONLY notifications — onDecoded surfaces the A/B/
// mismatch payloads, and the human decision goes back via the session's
// approve()/reject() (DREV-001 D4-1). On any security-class failure onError
// fires and onDecoded never does (hard reject, no MPC).
//
// SKELETON: call shape, callback contract and session reverse-path are
// aligned with the bridge; not exercised (B-005 scope: no run, no device).

import React, { useState } from 'react';
import { Text, View, Button } from 'react-native';
import { sign } from '../sdk';
import type { SdkError, SignSession } from '../sdk';
import ApprovalSheet, { DecodedView } from '../components/ApprovalSheet';

// Stand-in START envelope; real START arrives via coord_client / push and is
// envelope- and digest-validated inside Go before onDecoded (sdk.md §3).
const START_JSON = JSON.stringify({ sessionId: 'demo-session', unsignedTx: '0x' });
const COMMITTEE = ['device-a', 'device-b', 'device-c'];
const THIS_DEVICE = 'device-a';

export default function SignScreen(): React.JSX.Element {
  const [stage, setStage] = useState('idle');
  const [decoded, setDecoded] = useState<DecodedView | null>(null);
  const [session, setSession] = useState<SignSession | null>(null);

  const run = (): void => {
    setStage('starting');
    sign(START_JSON, {
      onDecoded: (aFacts, bInfo, mismatch) => {
        setDecoded({ aFacts, bInfo, mismatch });
        setStage('awaiting approval');
      },
      onResult: (rsvBase64) => {
        setDecoded(null);
        setStage(`signed: ${rsvBase64}`);
      },
      onError: (e: SdkError) => {
        setDecoded(null);
        setStage(`error: ${e.code} ${e.msg}`);
      },
    })
      .then(setSession)
      .catch((err) => setStage(`launch failed: ${String(err)}`));
  };

  return (
    <View>
      <Text>Sign</Text>
      <Text>stage: {stage}</Text>
      <Button title="Start signing" onPress={run} />
      {decoded && session && (
        <ApprovalSheet
          decoded={decoded}
          committee={COMMITTEE}
          thisDevice={THIS_DEVICE}
          onApprove={() => {
            setStage('approved — running MPC');
            void session.approve();
          }}
          onReject={() => {
            setStage('rejected');
            void session.reject();
          }}
        />
      )}
    </View>
  );
}
