// Lost-member resharing example (docs/design/mcp/sdk.md §7). Reshare rebuilds the
// missing share onto a new committee; the master public key and addresses
// stay fixed. Same callback contract as keygen: onProgress* then exactly one
// of onResult / onError.
//
// SKELETON: call shape and callback wiring are aligned with the bridge; not
// exercised (B-005 scope: no run, no device).

import React, { useState } from 'react';
import { Text, View, Button } from 'react-native';
import { reshare } from '../sdk';
import type { GroupSummary, ReshareConfig, SdkError } from '../sdk';

const CFG: ReshareConfig = {
  oldThreshold: 1,
  newThreshold: 1,
  newParties: 3,
  passphrase: 'demo',
};

export default function ReshareScreen(): React.JSX.Element {
  const [stage, setStage] = useState('idle');
  const [group, setGroup] = useState<GroupSummary | null>(null);

  const run = (): void => {
    setStage('starting');
    reshare(CFG, {
      onProgress: (s) => setStage(s),
      onResult: (g) => {
        setGroup(g);
        setStage('done');
      },
      onError: (e: SdkError) => setStage(`error: ${e.code} ${e.msg}`),
    }).catch((err) => setStage(`launch failed: ${String(err)}`));
  };

  return (
    <View>
      <Text>Reshare (master pubkey invariant)</Text>
      <Text>stage: {stage}</Text>
      {group && <Text>groupPubKey: {group.groupPubKey}</Text>}
      <Button title="Run reshare" onPress={run} />
    </View>
  );
}
