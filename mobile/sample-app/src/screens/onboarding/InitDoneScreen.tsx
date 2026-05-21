import React, { useMemo } from 'react';
import { View, StyleSheet } from 'react-native';
import { Card, Hairline, KV, TrineMark, useTheme, spacing } from '../../ui';
import type { ThemeTokens } from '../../ui';
import { useI18n } from '../../i18n';
import { InitShell } from './InitShell';

export interface InitDoneScreenProps {
  readonly onContinue?: () => void;
}

export function InitDoneScreen({ onContinue }: InitDoneScreenProps): React.JSX.Element {
  const t = useTheme();
  const { t: T } = useI18n();
  const s = useMemo(() => makeStyles(t), [t]);
  return (
    <InitShell
      step={5}
      total={5}
      title={T.onboarding.doneTitle}
      subtitle={T.onboarding.doneSubtitle}
      onPrimary={onContinue}
      primaryLabel={T.onboarding.doneEnterInbox}
    >
      <View style={s.body}>
        <View style={s.mark}>
          <TrineMark size={92} glow />
        </View>
        <Card style={s.summary}>
          <KV k={T.onboarding.memberIdLabel} v="m0" mono accent />
          <Hairline />
          <KV k={T.onboarding.partyIndex} v="0" />
          <Hairline />
          <KV k={T.onboarding.coordLabel} v="coord.zzci.io" mono sub={T.onboarding.coordValueSub} />
          <Hairline />
          <KV k={T.onboarding.relayLabel} v="12D3KooW…7Q2x" mono sub={T.onboarding.relayValueSub} />
        </Card>
      </View>
    </InitShell>
  );
}

function makeStyles(_t: ThemeTokens) {
  return StyleSheet.create({
    body: { alignItems: 'center' },
    mark: { width: 104, height: 104, alignItems: 'center', justifyContent: 'center', marginBottom: spacing.lg },
    summary: { width: '100%', paddingHorizontal: spacing.md, paddingVertical: 2 },
  });
}
