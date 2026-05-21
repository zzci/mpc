// Root of the mcp integration example. Opens the device keystore handle
// once (newSDK), then offers the three MPC flows the SDK exposes. A plain
// state switch stands in for a navigator — keeping the example minimal and
// dependency-free (B-005 scope: skeleton structure only, not required to
// run).

import React, { useEffect, useState } from 'react';
import { SafeAreaView, ScrollView, Text, View, Button } from 'react-native';
import { newSDK, KEYSTORE_DIR } from './sdk';
import KeygenScreen from './screens/KeygenScreen';
import SignScreen from './screens/SignScreen';
import ReshareScreen from './screens/ReshareScreen';

type Tab = 'keygen' | 'sign' | 'reshare';

export default function App(): React.JSX.Element {
  const [tab, setTab] = useState<Tab>('keygen');
  const [ready, setReady] = useState(false);

  useEffect(() => {
    // PreParams / MPC run off the UI thread inside Go; newSDK only opens the
    // keystore-rooted handle (docs/design/mcp/sdk.md §5/§6).
    newSDK(KEYSTORE_DIR)
      .then(() => setReady(true))
      .catch(() => setReady(false));
  }, []);

  return (
    <SafeAreaView>
      <ScrollView>
        <Text>mcp Sample Wallet</Text>
        <Text>SDK handle: {ready ? 'open' : 'not ready'}</Text>
        <View>
          <Button title="Keygen" onPress={() => setTab('keygen')} />
          <Button title="Sign" onPress={() => setTab('sign')} />
          <Button title="Reshare" onPress={() => setTab('reshare')} />
        </View>
        {tab === 'keygen' && <KeygenScreen />}
        {tab === 'sign' && <SignScreen />}
        {tab === 'reshare' && <ReshareScreen />}
      </ScrollView>
    </SafeAreaView>
  );
}
