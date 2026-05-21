// Outcome screen reached after a SignSession either succeeds (rsvBase64)
// or is hard-rejected (no MPC). Maps the design's "signed" and "rejected"
// states to the visual system and points the user back to the inbox.

import React, { useMemo } from 'react';
import { StyleSheet, Text, View } from 'react-native';
import {
  ChainBadge,
  Hairline,
  Icon,
  PrimaryButton,
  Screen,
  TopBar,
  fontFamily,
  radius,
  spacing,
  useTheme,
} from '../../ui';
import type { ThemeTokens } from '../../ui';
import { useI18n } from '../../i18n';
import { WALLETS } from '../../data';
import type { SigningEnvelope } from '../../data';

export type SignOutcome =
  | { readonly kind: 'signed'; readonly rsvBase64: string }
  | { readonly kind: 'rejected'; readonly reason?: string }
  | { readonly kind: 'expired' };

export interface SignResultScreenProps {
  readonly envelope: SigningEnvelope;
  readonly outcome: SignOutcome;
  readonly onClose: () => void;
}

export function SignResultScreen({
  envelope,
  outcome,
  onClose,
}: SignResultScreenProps): React.JSX.Element {
  const t = useTheme();
  const { t: T } = useI18n();
  const s = useMemo(() => makeStyles(t), [t]);
  const wallet = WALLETS.find((w) => w.groupId === envelope.groupId);

  const tone = outcome.kind === 'signed' ? 'ok' : outcome.kind === 'rejected' ? 'danger' : 'warn';
  const tint = tone === 'ok' ? t.ok : tone === 'danger' ? t.danger : t.warn;
  const heroIcon = outcome.kind === 'signed' ? 'check' : outcome.kind === 'rejected' ? 'close' : 'clock';

  const title =
    outcome.kind === 'signed'
      ? T.signing.resultSignedTitle
      : outcome.kind === 'rejected'
        ? T.signing.resultRejectedTitle
        : T.signing.resultExpiredTitle;
  const subtitle =
    outcome.kind === 'signed'
      ? T.signing.resultSignedSub
      : outcome.kind === 'rejected'
        ? T.signing.resultRejectedSub
        : T.signing.resultExpiredSub;

  return (
    <Screen
      footer={
        <PrimaryButton label={T.signing.resultBackToInbox} onPress={onClose} />
      }
    >
      <TopBar title={T.signing.detailTitle} />

      <View style={s.bodyPad}>
        <View style={s.heroCard}>
          <View style={[s.heroIcon, { backgroundColor: `${tint}1a`, borderColor: `${tint}55` }]}>
            <Icon name={heroIcon} size={28} color={tint} />
          </View>
          <Text style={[s.heroTitle, { color: tint }]}>{title}</Text>
          <Text style={s.heroSub}>{subtitle}</Text>
        </View>

        <View style={s.metaCard}>
          <View style={s.metaHead}>
            <ChainBadge chain={envelope.chain} label={envelope.chainLabel} />
            <Text style={s.requestId} numberOfLines={1}>
              {envelope.requestId}
            </Text>
          </View>
          <Text style={s.metaTitle} numberOfLines={2}>
            {envelope.businessInfo.title}
          </Text>
          <Hairline />
          <View style={s.metaRow}>
            <Text style={s.metaLabel}>{T.signing.walletLabel}</Text>
            <Text style={s.metaValue} numberOfLines={1}>
              {wallet?.moniker ?? envelope.groupId}
            </Text>
          </View>
          <View style={s.metaRow}>
            <Text style={s.metaLabel}>{T.signing.toLabel}</Text>
            <Text style={[s.metaValue, s.mono]} numberOfLines={1} ellipsizeMode="middle">
              {envelope.unsignedTxSummary.to}
            </Text>
          </View>
          <View style={s.metaRow}>
            <Text style={s.metaLabel}>{T.signing.valueLabel}</Text>
            <Text style={s.metaValue}>{envelope.unsignedTxSummary.value}</Text>
          </View>
        </View>

        {outcome.kind === 'signed' ? (
          <View style={s.rsvCard}>
            <View style={s.rsvHead}>
              <Icon name="shield" size={14} color={t.ok} />
              <Text style={s.rsvHeadText}>{T.signing.resultRsvLabel}</Text>
            </View>
            <Text style={s.rsvBody} selectable>
              {outcome.rsvBase64}
            </Text>
          </View>
        ) : null}

        {outcome.kind === 'rejected' ? (
          <View style={s.reportCard}>
            <Text style={s.reportLabel}>{T.signing.resultReportLabel}</Text>
            <Text style={s.reportBody}>
              {outcome.reason ?? envelope.mismatchHint ?? T.signing.resultRejectedSub}
            </Text>
          </View>
        ) : null}
      </View>
    </Screen>
  );
}

function makeStyles(t: ThemeTokens) {
  return StyleSheet.create({
    bodyPad: { paddingHorizontal: spacing.lg, gap: spacing.md, paddingBottom: spacing.lg },
    heroCard: {
      alignItems: 'center',
      paddingVertical: spacing.xl,
      paddingHorizontal: spacing.lg,
      borderRadius: radius.xl,
      backgroundColor: t.surface,
      borderColor: t.hairline,
      borderWidth: StyleSheet.hairlineWidth,
      gap: 10,
      marginTop: spacing.sm,
    },
    heroIcon: {
      width: 64,
      height: 64,
      borderRadius: 32,
      alignItems: 'center',
      justifyContent: 'center',
      borderWidth: StyleSheet.hairlineWidth,
    },
    heroTitle: { fontSize: 18, fontWeight: '700', letterSpacing: 0.3 },
    heroSub: { color: t.text2, fontSize: 12.5, textAlign: 'center', lineHeight: 18, maxWidth: 260 },
    metaCard: {
      padding: spacing.md,
      borderRadius: radius.xl,
      backgroundColor: t.surface,
      borderColor: t.hairline,
      borderWidth: StyleSheet.hairlineWidth,
      gap: 6,
    },
    metaHead: { flexDirection: 'row', alignItems: 'center', gap: spacing.sm, marginBottom: 4 },
    requestId: { color: t.text3, fontSize: 11.5, fontFamily: fontFamily.mono, flexShrink: 1 },
    metaTitle: { color: t.text, fontSize: 14, fontWeight: '700', marginBottom: 4 },
    metaRow: {
      flexDirection: 'row',
      alignItems: 'center',
      justifyContent: 'space-between',
      paddingVertical: 8,
      gap: spacing.md,
    },
    metaLabel: { color: t.text3, fontSize: 11.5, letterSpacing: 0.3 },
    metaValue: { color: t.text, fontSize: 12.5, fontWeight: '600', maxWidth: 220, textAlign: 'right' },
    mono: { fontFamily: fontFamily.mono },
    rsvCard: {
      padding: spacing.md,
      borderRadius: radius.xl,
      backgroundColor: `${t.ok}10`,
      borderColor: `${t.ok}55`,
      borderWidth: StyleSheet.hairlineWidth,
      gap: 8,
    },
    rsvHead: { flexDirection: 'row', alignItems: 'center', gap: 8 },
    rsvHeadText: { color: t.ok, fontSize: 11.5, fontWeight: '700', letterSpacing: 0.4 },
    rsvBody: { color: t.text, fontSize: 11, fontFamily: fontFamily.mono, lineHeight: 16 },
    reportCard: {
      padding: spacing.md,
      borderRadius: radius.xl,
      backgroundColor: `${t.danger}10`,
      borderColor: `${t.danger}55`,
      borderWidth: StyleSheet.hairlineWidth,
      gap: 6,
    },
    reportLabel: { color: t.danger, fontSize: 11.5, fontWeight: '700', letterSpacing: 0.4, textTransform: 'uppercase' },
    reportBody: { color: t.text, fontSize: 12.5, lineHeight: 18 },
  });
}
