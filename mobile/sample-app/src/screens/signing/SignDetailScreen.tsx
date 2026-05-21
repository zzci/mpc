// WYSIWYS signing detail. Surfaces the three onDecoded payloads
// (docs/design/mcp/sdk.md §3 / §4): A-zone re-derived facts, B-zone
// proposer-supplied businessInfo, and declarative A/B mismatches. The
// approve / reject footer calls session.approve() / session.reject()
// regardless of whether the SignSession is live (from sign(startJSON, …))
// or a demo placeholder — bridge semantics are preserved either way.

import React, { useMemo } from 'react';
import { StyleSheet, Text, View } from 'react-native';
import {
  ChainBadge,
  GhostButton,
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
import type { Strings } from '../../i18n';
import { WALLETS } from '../../data';
import type { MemberDecision, SigningEnvelope } from '../../data';
import { formatExpiry } from '../tabs/InboxScreen';
import type { SignSession } from '../../sdk';

export interface SignDetailScreenProps {
  readonly envelope: SigningEnvelope;
  readonly session: SignSession;
  readonly liveSession: boolean;
  readonly onApprove: () => void;
  readonly onReject: () => void;
  readonly onBack: () => void;
}

export function SignDetailScreen({
  envelope,
  session,
  liveSession,
  onApprove,
  onReject,
  onBack,
}: SignDetailScreenProps): React.JSX.Element {
  const t = useTheme();
  const { t: T } = useI18n();
  const s = useMemo(() => makeStyles(t), [t]);

  const wallet = WALLETS.find((w) => w.groupId === envelope.groupId);
  const decisions = envelope.decisions;
  const approvedCount = decisions.filter((d) => d.state === 'approved').length;
  const mismatch = !envelope.crossCheckOk || !envelope.metaHashOk;
  const expiryText = formatExpiry(envelope.expiresIn, T.inbox.expired);

  const handleApprove = (): void => {
    void session.approve();
    onApprove();
  };
  const handleReject = (): void => {
    void session.reject();
    onReject();
  };

  return (
    <Screen
      footer={
        <View style={s.footer}>
          <GhostButton
            label={mismatch ? T.signing.mismatchForceReject : T.signing.rejectCta}
            onPress={handleReject}
            icon="warn"
            style={mismatch ? s.rejectStrong : s.rejectStd}
          />
          <PrimaryButton
            label={T.signing.approveCta}
            onPress={handleApprove}
            disabled={mismatch}
            style={s.approve}
          />
        </View>
      }
    >
      <TopBar
        title={T.signing.detailTitle}
        onBack={onBack}
        backLabel={T.common.back}
        right={
          <View style={s.expiry}>
            <Icon name="clock" size={11} color={t.warn} />
            <Text style={s.expiryText}>{expiryText}</Text>
          </View>
        }
      />

      <View style={s.bodyPad}>
        <View style={s.headCard}>
          <View style={s.headRow}>
            <ChainBadge chain={envelope.chain} label={envelope.chainLabel} />
            <Text style={s.requestId} numberOfLines={1}>
              {envelope.requestId}
            </Text>
          </View>
          <Text style={s.title} numberOfLines={2}>
            {envelope.businessInfo.title}
          </Text>
          <View style={s.amountRow}>
            <Text style={s.amount}>{envelope.unsignedTxSummary.value}</Text>
            <Text style={s.fiat}>{envelope.unsignedTxSummary.valueFiat}</Text>
          </View>
          <View style={s.headMetaRow}>
            <MetaCell label={T.signing.walletLabel} value={wallet?.moniker ?? envelope.groupId} />
            <View style={s.metaDivider} />
            <MetaCell label={T.signing.proposer} value={envelope.proposerLabel} />
            <View style={s.metaDivider} />
            <MetaCell label={T.signing.receivedAt} value={envelope.receivedAt} />
          </View>
        </View>

        {mismatch ? <MismatchBanner envelope={envelope} /> : null}

        <ChecksSection envelope={envelope} />
        <ZoneA envelope={envelope} />
        <ZoneB envelope={envelope} />
        <DigestSection envelope={envelope} />
        <QuorumSection
          decisions={decisions}
          threshold={wallet?.threshold ?? envelope.decisions.length}
          parties={wallet?.parties ?? envelope.decisions.length}
          approvedCount={approvedCount}
        />

        <View style={s.sessionStrip}>
          <Icon name={liveSession ? 'shield' : 'info'} size={12} color={t.text3} />
          <Text style={s.sessionStripText} numberOfLines={1}>
            {liveSession ? T.signing.demoBannerLive : T.signing.demoBannerDemo}
            {'  ·  '}
            <Text style={s.sessionStripMono}>{session.sessionId}</Text>
          </Text>
        </View>
      </View>
    </Screen>
  );
}

interface MetaCellProps {
  readonly label: string;
  readonly value: string;
}

function MetaCell({ label, value }: MetaCellProps): React.JSX.Element {
  const t = useTheme();
  const s = useMemo(() => makeStyles(t), [t]);
  return (
    <View style={s.metaCell}>
      <Text style={s.metaLabel}>{label}</Text>
      <Text style={s.metaValue} numberOfLines={1}>
        {value}
      </Text>
    </View>
  );
}

function MismatchBanner({ envelope }: { readonly envelope: SigningEnvelope }): React.JSX.Element {
  const t = useTheme();
  const { t: T } = useI18n();
  const s = useMemo(() => makeStyles(t), [t]);
  return (
    <View style={s.mismatchCard} accessibilityRole="alert">
      <View style={s.mismatchHead}>
        <Icon name="warn" size={16} color={t.danger} />
        <Text style={s.mismatchTitle}>{T.signing.mismatchTitle}</Text>
      </View>
      <Text style={s.mismatchBody}>{envelope.mismatchHint ?? T.signing.mismatchBody}</Text>
    </View>
  );
}

function ChecksSection({ envelope }: { readonly envelope: SigningEnvelope }): React.JSX.Element {
  const t = useTheme();
  const { t: T } = useI18n();
  const s = useMemo(() => makeStyles(t), [t]);
  const rows: ReadonlyArray<{ ok: boolean; label: string }> = [
    { ok: envelope.metaHashOk, label: envelope.metaHashOk ? T.signing.metaHashOk : T.signing.metaHashBad },
    { ok: envelope.crossCheckOk, label: envelope.crossCheckOk ? T.signing.crossCheckOk : T.signing.crossCheckBad },
  ];
  return (
    <View style={s.section}>
      <Text style={s.sectionLabel}>{T.signing.checksHeading}</Text>
      <View style={s.checksCard}>
        {rows.map((r) => (
          <View key={r.label} style={s.checkRow}>
            <View style={[s.checkDot, r.ok ? s.checkDotOk : s.checkDotBad]}>
              <Icon name={r.ok ? 'check' : 'warn'} size={11} color={r.ok ? t.ok : t.danger} />
            </View>
            <Text style={[s.checkText, r.ok ? null : s.checkTextBad]}>{r.label}</Text>
          </View>
        ))}
      </View>
    </View>
  );
}

function ZoneA({ envelope }: { readonly envelope: SigningEnvelope }): React.JSX.Element {
  const t = useTheme();
  const { t: T } = useI18n();
  const s = useMemo(() => makeStyles(t), [t]);
  const tx = envelope.unsignedTxSummary;
  const toAccent = envelope.crossCheckOk ? T.signing.toLabelSafe : T.signing.toLabelDanger;
  return (
    <View style={s.section}>
      <View style={s.zoneHeader}>
        <View style={s.zoneBadgeA}>
          <Text style={s.zoneBadgeText}>A</Text>
        </View>
        <View style={s.zoneHeadText}>
          <Text style={s.zoneTitle}>{T.signing.aZoneHeading}</Text>
          <Text style={s.zoneSub}>{T.signing.aZoneSub}</Text>
        </View>
      </View>
      <View style={s.zoneCard}>
        <KvRow
          label={T.signing.toLabel}
          value={tx.to}
          mono
          tag={toAccent}
          tagTone={envelope.crossCheckOk ? 'ok' : 'danger'}
        />
        <Hairline />
        <KvRow label={T.signing.valueLabel} value={tx.value} />
        <Hairline />
        <KvRow label={T.signing.fiatLabel} value={tx.valueFiat} muted />
        <Hairline />
        <KvRow label={T.signing.nonceLabel} value={String(tx.nonce)} mono />
        <Hairline />
        <KvRow label={T.signing.gasLimitLabel} value={String(tx.gasLimit)} mono />
        <Hairline />
        <KvRow label={T.signing.gasPriceLabel} value={tx.gasPrice} mono />
        <Hairline />
        <KvRow
          label={T.signing.dataLabel}
          value={tx.data && tx.data !== '0x' ? tx.data : T.signing.emptyData}
          mono
          wrap
        />
      </View>
    </View>
  );
}

function ZoneB({ envelope }: { readonly envelope: SigningEnvelope }): React.JSX.Element {
  const t = useTheme();
  const { t: T } = useI18n();
  const s = useMemo(() => makeStyles(t), [t]);
  const bi = envelope.businessInfo;
  return (
    <View style={s.section}>
      <View style={s.zoneHeader}>
        <View style={s.zoneBadgeB}>
          <Text style={s.zoneBadgeText}>B</Text>
        </View>
        <View style={s.zoneHeadText}>
          <Text style={s.zoneTitle}>{T.signing.bZoneHeading}</Text>
          <Text style={s.zoneSub}>{T.signing.bZoneSub}</Text>
        </View>
      </View>
      <View style={s.zoneCardAdvisory}>
        <KvRow label={T.signing.orderLabel} value={bi.orderId} mono />
        <Hairline />
        <KvRow label={T.signing.operatorLabel} value={bi.operator} />
        <Hairline />
        <KvRow label={T.signing.memoLabel} value={bi.memo} wrap />
      </View>
    </View>
  );
}

function DigestSection({ envelope }: { readonly envelope: SigningEnvelope }): React.JSX.Element {
  const t = useTheme();
  const { t: T } = useI18n();
  const s = useMemo(() => makeStyles(t), [t]);
  return (
    <View style={s.section}>
      <View style={s.digestCard}>
        <View style={s.digestLeft}>
          <Text style={s.digestLabel}>{T.signing.digestLabel}</Text>
          <Text style={s.digestSub}>{T.signing.digestSub}</Text>
        </View>
        <Text style={s.digestValue} numberOfLines={2}>
          {envelope.digest32}
        </Text>
      </View>
    </View>
  );
}

interface QuorumSectionProps {
  readonly decisions: ReadonlyArray<MemberDecision>;
  readonly threshold: number;
  readonly parties: number;
  readonly approvedCount: number;
}

function QuorumSection({
  decisions,
  threshold,
  parties,
  approvedCount,
}: QuorumSectionProps): React.JSX.Element {
  const t = useTheme();
  const { t: T, tx } = useI18n();
  const s = useMemo(() => makeStyles(t), [t]);
  return (
    <View style={s.section}>
      <Text style={s.sectionLabel}>{T.signing.quorumHeading}</Text>
      <View style={s.quorumCard}>
        <Text style={s.quorumSub}>
          {tx(T.signing.quorumSub, { approved: approvedCount, threshold, parties })}
        </Text>
        <View style={s.quorumRows}>
          {decisions.map((d) => (
            <QuorumRow key={d.id} decision={d} T={T} />
          ))}
        </View>
      </View>
    </View>
  );
}

interface QuorumRowProps {
  readonly decision: MemberDecision;
  readonly T: Strings;
}

function QuorumRow({ decision, T }: QuorumRowProps): React.JSX.Element {
  const t = useTheme();
  const s = useMemo(() => makeStyles(t), [t]);
  const stateLabel =
    decision.state === 'approved'
      ? T.signing.memberApproved
      : decision.state === 'rejected'
        ? T.signing.memberRejected
        : T.signing.memberPending;
  const stateColor =
    decision.state === 'approved'
      ? t.ok
      : decision.state === 'rejected'
        ? t.danger
        : t.text3;
  return (
    <View style={[s.quorumRow, decision.self ? s.quorumRowSelf : null]}>
      <View style={s.quorumIdBox}>
        <Text style={[s.quorumId, decision.self ? s.quorumIdSelf : null]}>{decision.id}</Text>
      </View>
      <Text style={s.quorumLabel} numberOfLines={1}>
        {decision.self ? T.signing.memberThis : decision.id}
      </Text>
      <View style={[s.quorumState, { borderColor: `${stateColor}55`, backgroundColor: `${stateColor}1a` }]}>
        <View style={[s.quorumStateDot, { backgroundColor: stateColor }]} />
        <Text style={[s.quorumStateText, { color: stateColor }]}>{stateLabel}</Text>
      </View>
    </View>
  );
}

interface KvRowProps {
  readonly label: string;
  readonly value: string;
  readonly mono?: boolean;
  readonly muted?: boolean;
  readonly wrap?: boolean;
  readonly tag?: string;
  readonly tagTone?: 'ok' | 'danger';
}

function KvRow({ label, value, mono, muted, wrap, tag, tagTone }: KvRowProps): React.JSX.Element {
  const t = useTheme();
  const s = useMemo(() => makeStyles(t), [t]);
  const valueStyle = [
    s.kvValue,
    mono ? s.mono : null,
    muted ? s.kvValueMuted : null,
    wrap ? s.kvValueWrap : null,
  ];
  return (
    <View style={s.kvRow}>
      <Text style={s.kvLabel}>{label}</Text>
      <View style={s.kvRight}>
        <Text style={valueStyle} numberOfLines={wrap ? 3 : 1} ellipsizeMode={wrap ? 'tail' : 'middle'}>
          {value}
        </Text>
        {tag ? (
          <View
            style={[
              s.kvTag,
              {
                borderColor: tagTone === 'danger' ? `${t.danger}55` : `${t.ok}55`,
                backgroundColor: tagTone === 'danger' ? `${t.danger}1a` : `${t.ok}1a`,
              },
            ]}
          >
            <Text style={[s.kvTagText, { color: tagTone === 'danger' ? t.danger : t.ok }]}>{tag}</Text>
          </View>
        ) : null}
      </View>
    </View>
  );
}

function makeStyles(t: ThemeTokens) {
  return StyleSheet.create({
    bodyPad: { paddingHorizontal: spacing.lg, paddingBottom: spacing.lg },
    expiry: { flexDirection: 'row', alignItems: 'center', gap: 4 },
    expiryText: { color: t.warn, fontSize: 11, fontWeight: '700' },
    headCard: {
      padding: spacing.lg,
      borderRadius: radius.xl,
      backgroundColor: t.surface,
      borderColor: t.hairline,
      borderWidth: StyleSheet.hairlineWidth,
      marginTop: spacing.sm,
    },
    headRow: {
      flexDirection: 'row',
      alignItems: 'center',
      gap: spacing.sm,
      marginBottom: 10,
    },
    requestId: {
      color: t.text3,
      fontSize: 11.5,
      fontFamily: fontFamily.mono,
      flexShrink: 1,
    },
    title: {
      color: t.text,
      fontSize: 17,
      fontWeight: '700',
      letterSpacing: 0.2,
      lineHeight: 22,
    },
    amountRow: {
      flexDirection: 'row',
      alignItems: 'baseline',
      gap: spacing.sm,
      marginTop: 8,
    },
    amount: { color: t.text, fontSize: 26, fontWeight: '700', letterSpacing: -0.5 },
    fiat: { color: t.text3, fontSize: 12 },
    headMetaRow: {
      flexDirection: 'row',
      alignItems: 'center',
      marginTop: spacing.md,
      paddingTop: spacing.sm,
      borderTopColor: t.hairline,
      borderTopWidth: StyleSheet.hairlineWidth,
    },
    metaCell: { flex: 1, gap: 2, paddingHorizontal: 4 },
    metaLabel: { color: t.text3, fontSize: 9.5, letterSpacing: 0.4, textTransform: 'uppercase' },
    metaValue: { color: t.text2, fontSize: 11.5, fontWeight: '600' },
    metaDivider: { width: StyleSheet.hairlineWidth, height: 22, backgroundColor: t.hairline },
    section: { marginTop: spacing.lg, gap: spacing.sm },
    sectionLabel: {
      color: t.text2,
      fontSize: 11.5,
      fontWeight: '700',
      letterSpacing: 0.6,
      textTransform: 'uppercase',
      paddingHorizontal: 4,
    },
    zoneHeader: { flexDirection: 'row', alignItems: 'center', gap: spacing.sm, paddingHorizontal: 4 },
    zoneBadgeA: {
      width: 28,
      height: 28,
      borderRadius: 9,
      alignItems: 'center',
      justifyContent: 'center',
      backgroundColor: `${t.accent}1a`,
      borderColor: `${t.accent}55`,
      borderWidth: StyleSheet.hairlineWidth,
    },
    zoneBadgeB: {
      width: 28,
      height: 28,
      borderRadius: 9,
      alignItems: 'center',
      justifyContent: 'center',
      backgroundColor: t.surface2,
      borderColor: t.hairline,
      borderWidth: StyleSheet.hairlineWidth,
    },
    zoneBadgeText: { color: t.text, fontSize: 13, fontWeight: '800', letterSpacing: 0.4 },
    zoneHeadText: { flex: 1 },
    zoneTitle: { color: t.text, fontSize: 14, fontWeight: '700' },
    zoneSub: { color: t.text3, fontSize: 11, marginTop: 2, lineHeight: 15 },
    zoneCard: {
      padding: spacing.md,
      borderRadius: radius.xl,
      backgroundColor: t.surface,
      borderColor: `${t.accent}33`,
      borderWidth: StyleSheet.hairlineWidth,
    },
    zoneCardAdvisory: {
      padding: spacing.md,
      borderRadius: radius.xl,
      backgroundColor: t.surface,
      borderColor: t.hairline,
      borderWidth: StyleSheet.hairlineWidth,
      borderStyle: 'dashed',
    },
    kvRow: {
      flexDirection: 'row',
      alignItems: 'flex-start',
      paddingVertical: 10,
      gap: spacing.md,
    },
    kvLabel: { color: t.text3, fontSize: 11.5, width: 86, paddingTop: 1, letterSpacing: 0.3 },
    kvRight: { flex: 1, alignItems: 'flex-end' },
    kvValue: { color: t.text, fontSize: 13, fontWeight: '600', textAlign: 'right' },
    kvValueMuted: { color: t.text2, fontWeight: '500' },
    kvValueWrap: { textAlign: 'left' },
    mono: { fontFamily: fontFamily.mono, fontWeight: '600' as const },
    kvTag: {
      marginTop: 5,
      paddingHorizontal: 7,
      paddingVertical: 2,
      borderRadius: 6,
      borderWidth: StyleSheet.hairlineWidth,
    },
    kvTagText: { fontSize: 10, fontWeight: '700', letterSpacing: 0.3 },
    mismatchCard: {
      padding: spacing.md,
      borderRadius: radius.xl,
      backgroundColor: `${t.danger}14`,
      borderColor: `${t.danger}66`,
      borderWidth: StyleSheet.hairlineWidth,
      marginTop: spacing.lg,
    },
    mismatchHead: { flexDirection: 'row', alignItems: 'center', gap: 8, marginBottom: 6 },
    mismatchTitle: { color: t.danger, fontSize: 13, fontWeight: '700', letterSpacing: 0.2 },
    mismatchBody: { color: t.text2, fontSize: 12, lineHeight: 17 },
    checksCard: {
      padding: spacing.md,
      borderRadius: radius.xl,
      backgroundColor: t.surface,
      borderColor: t.hairline,
      borderWidth: StyleSheet.hairlineWidth,
      gap: 8,
    },
    checkRow: { flexDirection: 'row', alignItems: 'center', gap: 10, position: 'relative' },
    checkDot: {
      width: 22,
      height: 22,
      borderRadius: 11,
      alignItems: 'center',
      justifyContent: 'center',
      borderWidth: StyleSheet.hairlineWidth,
    },
    checkDotOk: { backgroundColor: `${t.ok}1a`, borderColor: `${t.ok}55` },
    checkDotBad: { backgroundColor: `${t.danger}1a`, borderColor: `${t.danger}55` },
    checkText: { color: t.text, fontSize: 12.5, fontWeight: '500', flex: 1 },
    checkTextBad: { color: t.danger, fontWeight: '600' },
    digestCard: {
      padding: spacing.md,
      borderRadius: radius.xl,
      backgroundColor: t.surface2,
      borderColor: t.hairline,
      borderWidth: StyleSheet.hairlineWidth,
      flexDirection: 'row',
      alignItems: 'center',
      gap: spacing.sm,
    },
    digestLeft: { flex: 1 },
    digestLabel: { color: t.text2, fontSize: 11, fontWeight: '700', letterSpacing: 0.5, textTransform: 'uppercase' },
    digestSub: { color: t.text3, fontSize: 10.5, marginTop: 3 },
    digestValue: {
      color: t.accent,
      fontSize: 11,
      fontFamily: fontFamily.mono,
      maxWidth: 170,
      textAlign: 'right',
    },
    quorumCard: {
      padding: spacing.md,
      borderRadius: radius.xl,
      backgroundColor: t.surface,
      borderColor: t.hairline,
      borderWidth: StyleSheet.hairlineWidth,
    },
    quorumSub: { color: t.text3, fontSize: 11.5, marginBottom: 10 },
    quorumRows: { gap: 6 },
    quorumRow: {
      flexDirection: 'row',
      alignItems: 'center',
      gap: 10,
      paddingVertical: 8,
      paddingHorizontal: 8,
      borderRadius: 10,
      backgroundColor: t.surface2,
    },
    quorumRowSelf: { backgroundColor: `${t.accent}10`, borderColor: `${t.accent}33`, borderWidth: StyleSheet.hairlineWidth },
    quorumIdBox: {
      width: 30,
      height: 24,
      borderRadius: 6,
      backgroundColor: t.surface,
      borderColor: t.hairline,
      borderWidth: StyleSheet.hairlineWidth,
      alignItems: 'center',
      justifyContent: 'center',
    },
    quorumId: { color: t.text2, fontSize: 10.5, fontWeight: '700', fontFamily: fontFamily.mono },
    quorumIdSelf: { color: t.accent },
    quorumLabel: { flex: 1, color: t.text2, fontSize: 12, fontWeight: '500' },
    quorumState: {
      flexDirection: 'row',
      alignItems: 'center',
      gap: 5,
      paddingHorizontal: 9,
      paddingVertical: 3,
      borderRadius: radius.pill,
      borderWidth: StyleSheet.hairlineWidth,
    },
    quorumStateDot: { width: 6, height: 6, borderRadius: 3 },
    quorumStateText: { fontSize: 10.5, fontWeight: '700', letterSpacing: 0.3 },
    sessionStrip: {
      marginTop: spacing.lg,
      flexDirection: 'row',
      alignItems: 'center',
      gap: 6,
      paddingHorizontal: 4,
    },
    sessionStripText: { flex: 1, color: t.text3, fontSize: 10.5 },
    sessionStripMono: { fontFamily: fontFamily.mono, color: t.text2 },
    footer: { flexDirection: 'row', gap: spacing.sm, alignItems: 'center' },
    rejectStd: { flex: 1 },
    rejectStrong: { flex: 1, borderColor: `${t.danger}66`, backgroundColor: `${t.danger}1a` },
    approve: { flex: 1.2 },
  });
}
