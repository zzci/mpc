// t-of-n ECDSA keygen example. Demonstrates the KeyGenCallback contract:
// onProgress* then exactly one of onResult / onError (docs/design/mcp/sdk.md §2).
// The committee summary (GroupSummary) carries the invariant groupPubKey.
//
// SKELETON: call shape and callback wiring are aligned with the bridge; the
// run is not exercised (B-005 scope: no run, no device).

import React, { useState } from 'react';
import { Text, View, Button } from 'react-native';
import { keyGen } from '../sdk';
import type { GroupSummary, KeygenConfig, SdkError } from '../sdk';

const CFG: KeygenConfig = { threshold: 1, parties: 3, passphrase: 'demo' };

export default function KeygenScreen(): React.JSX.Element {
  const [stage, setStage] = useState('idle');
  const [group, setGroup] = useState<GroupSummary | null>(null);

  const run = (): void => {
    setStage('starting');
    keyGen(CFG, {
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
      <Text>Keygen {CFG.threshold}-of-{CFG.parties}</Text>
      <Text>stage: {stage}</Text>
      {group && <Text>groupPubKey: {group.groupPubKey}</Text>}
      <Button title="Run keygen" onPress={run} />
    </View>
  );
}
