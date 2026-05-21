import React, { useMemo } from 'react';
import { View, Text, StyleSheet } from 'react-native';
import { Card, Hairline, Icon, KV, SectionLabel, useTheme, spacing, radius, fontFamily } from '../../ui';
import type { ThemeTokens } from '../../ui';
import { useI18n } from '../../i18n';
import { InitShell } from './InitShell';

export interface InitConfirmScreenProps {
  readonly onBack?: () => void;
  readonly onNext?: () => void;
}

const PARSED = {
  coord: 'https://coord.zzci.io',
  tlsLine: 'mTLS · pinned cert SHA256 8a4f…',
  relayPID: '12D3KooW7Q2x…NfP9',
  relayAddrs: ['/dns4/relay.zzci.io/tcp/4001/wss', '/ip4/10.0.1.5/tcp/4001'],
  groupHint: 'grp_5a8e7b3c · Treasury main',
  bootstrapToken: 'btk_2J9XaQ…wKf3 (one-time)',
  expires: '2026-05-21 15:00 · +8 min',
};

export function InitConfirmScreen({ onBack, onNext }: InitConfirmScreenProps): React.JSX.Element {
  const t = useTheme();
  const { t: T } = useI18n();
  const s = useMemo(() => makeStyles(t), [t]);
  return (
    <InitShell
      step={3}
      total={5}
      title={T.onboarding.confirmTitle}
      subtitle={T.onboarding.confirmSubtitle}
      onBack={onBack}
      primaryLabel={T.onboarding.confirmPrimary}
      onPrimary={onNext}
    >
      <SectionLabel>{T.onboarding.sectionCoord}</SectionLabel>
      <Card style={s.card}>
        <KV k={T.onboarding.httpEndpoint} v={PARSED.coord} mono accent />
        <Hairline />
        <KV k={T.onboarding.authentication} v={T.onboarding.authValue} sub={PARSED.tlsLine} mono />
      </Card>

      <SectionLabel>{T.onboarding.sectionRelay}</SectionLabel>
      <Card style={s.card}>
        <KV k={T.onboarding.peerId} v={PARSED.relayPID} mono />
        <Hairline />
        <View style={s.addrBlock}>
          <Text style={s.addrHeading}>{T.onboarding.multiaddrs}</Text>
          {PARSED.relayAddrs.map((a) => (
            <View key={a} style={s.addr}>
              <Text style={s.addrText}>{a}</Text>
            </View>
          ))}
        </View>
      </Card>

      <SectionLabel>{T.onboarding.sectionBootstrap}</SectionLabel>
      <Card style={s.card}>
        <KV k={T.onboarding.oneTimeToken} v={PARSED.bootstrapToken} mono />
        <Hairline />
        <KV k={T.onboarding.expires} v={PARSED.expires} />
        <Hairline />
        <KV k={T.onboarding.targetWallet} v={PARSED.groupHint} sub={T.onboarding.targetWalletSub} />
      </Card>

      <View style={s.warn}>
        <Icon name="warn" size={14} color={t.warn} />
        <Text style={s.warnText}>
          <Text style={s.warnBold}>{T.onboarding.verifyWarnBold}</Text>
          {' '}
          {T.onboarding.verifyWarn}
        </Text>
      </View>
    </InitShell>
  );
}

function makeStyles(t: ThemeTokens) {
  return StyleSheet.create({
    card: { paddingHorizontal: spacing.md, paddingVertical: 2, marginBottom: spacing.md },
    addrBlock: { paddingVertical: 10 },
    addrHeading: { color: t.text2, fontSize: 12.5, marginBottom: 8 },
    addr: {
      paddingHorizontal: 10,
      paddingVertical: 7,
      borderRadius: 8,
      backgroundColor: t.surface2,
      marginBottom: 5,
    },
    addrText: { color: t.text, fontSize: 11.5, fontFamily: fontFamily.mono, letterSpacing: 0.2 },
    warn: {
      padding: 12,
      borderRadius: radius.md,
      backgroundColor: `${t.warn}1a`,
      borderColor: `${t.warn}55`,
      borderWidth: StyleSheet.hairlineWidth,
      flexDirection: 'row',
      alignItems: 'flex-start',
      gap: 8,
      marginTop: 10,
    },
    warnBold: { color: t.warn, fontWeight: '700' },
    warnText: { flex: 1, color: t.text2, fontSize: 11.5, lineHeight: 18 },
  });
}
