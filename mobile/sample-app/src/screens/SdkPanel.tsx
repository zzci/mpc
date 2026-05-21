import React, { useMemo, useState } from 'react';
import { View, Text, StyleSheet } from 'react-native';
import { GhostButton, Screen, TopBar, useTheme, spacing } from '../ui';
import type { ThemeTokens } from '../ui';
import { useI18n } from '../i18n';
import KeygenScreen from './KeygenScreen';
import SignScreen from './SignScreen';
import ReshareScreen from './ReshareScreen';

// Internal developer panel for invoking the raw SDK flows from the existing
// example screens (sdk.ts pass-through to @mcp/rn-bridge). Foundation L3
// keeps newSDK / keyGen / sign / reshare / approve / reject / export-share
// / import-share reachable for later L3s to extend in place.

type Section = 'keygen' | 'sign' | 'reshare';

export interface SdkPanelProps {
  readonly onClose: () => void;
}

export function SdkPanel({ onClose }: SdkPanelProps): React.JSX.Element {
  const t = useTheme();
  const { t: T, tx } = useI18n();
  const s = useMemo(() => makeStyles(t), [t]);
  const [section, setSection] = useState<Section>('keygen');
  const label = sectionLabel(section, T);
  return (
    <Screen>
      <TopBar title={T.sdkPanel.title} onBack={onClose} />
      <View style={s.toolbar}>
        <GhostButton label={T.sdkPanel.keygen} onPress={() => setSection('keygen')} style={s.flex} />
        <GhostButton label={T.sdkPanel.sign} onPress={() => setSection('sign')} style={s.flex} />
        <GhostButton label={T.sdkPanel.reshare} onPress={() => setSection('reshare')} style={s.flex} />
      </View>
      <View style={s.body}>
        <Text style={s.activeLabel}>{tx(T.sdkPanel.activeFlow, { flow: label })}</Text>
        {section === 'keygen' ? <KeygenScreen /> : null}
        {section === 'sign' ? <SignScreen /> : null}
        {section === 'reshare' ? <ReshareScreen /> : null}
      </View>
    </Screen>
  );
}

function sectionLabel(section: Section, T: ReturnType<typeof useI18n>['t']): string {
  if (section === 'keygen') return T.sdkPanel.keygen;
  if (section === 'sign') return T.sdkPanel.sign;
  return T.sdkPanel.reshare;
}

function makeStyles(t: ThemeTokens) {
  return StyleSheet.create({
    toolbar: { flexDirection: 'row', gap: 8, paddingHorizontal: spacing.lg, marginTop: spacing.sm },
    flex: { flex: 1 },
    body: { paddingHorizontal: spacing.lg, marginTop: spacing.lg, gap: spacing.sm },
    activeLabel: {
      color: t.text3,
      fontSize: 11,
      fontWeight: '700',
      letterSpacing: 0.4,
    },
  });
}
