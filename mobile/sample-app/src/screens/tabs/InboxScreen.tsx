import React, { useMemo, useState } from 'react';
import { View, Text, StyleSheet, Pressable } from 'react-native';
import {
  ChainBadge,
  Hairline,
  Icon,
  Pill,
  Screen,
  TrineMark,
  useTheme,
  spacing,
  radius,
  fontFamily,
} from '../../ui';
import type { ThemeTokens } from '../../ui';
import { useI18n } from '../../i18n';
import { COORD, ENVELOPES, MEMBER, WALLETS } from '../../data';
import type { SigningEnvelope } from '../../data';

// Inbox surface — list of pending signing envelopes. Foundation L3 renders
// the list and summary stats; later L3s add detail review behind onOpenEnvelope.

type Filter = 'all' | 'mine' | 'suspicious';

export interface InboxScreenProps {
  readonly onOpenEnvelope?: (env: SigningEnvelope) => void;
}

export function InboxScreen({ onOpenEnvelope }: InboxScreenProps): React.JSX.Element {
  const t = useTheme();
  const { t: T, tx } = useI18n();
  const s = useMemo(() => makeStyles(t), [t]);
  const [filter, setFilter] = useState<Filter>('all');

  const filtered = useMemo(() => filterEnvelopes(filter), [filter]);

  return (
    <Screen>
      <View style={s.appBar}>
        <View style={s.brand}>
          <TrineMark size={22} glow />
          <Text style={s.brandText}>{T.app.name}</Text>
        </View>
        <View style={s.appBarRight}>
          <CoordPillView />
        </View>
      </View>

      <View style={s.hero}>
        <View style={s.heroRow}>
          <Text style={s.heroCount}>{filtered.length}</Text>
          <Text style={s.heroLabel}>{T.inbox.pendingTitle}</Text>
        </View>
        <Text style={s.heroMeta}>
          {tx(T.inbox.meta, { memberId: MEMBER.memberId, relay: COORD.relayPeerID })}
        </Text>
      </View>

      <View style={s.filterRow}>
        <Pill
          label={tx(T.inbox.filterAll, { count: ENVELOPES.length })}
          active={filter === 'all'}
          onPress={() => setFilter('all')}
        />
        <Pill
          label={tx(T.inbox.filterMine, { count: filterEnvelopes('mine').length })}
          active={filter === 'mine'}
          onPress={() => setFilter('mine')}
        />
        <Pill
          label={tx(T.inbox.filterSuspicious, { count: filterEnvelopes('suspicious').length })}
          tone={filter === 'suspicious' ? 'danger' : 'default'}
          active={filter === 'suspicious'}
          onPress={() => setFilter('suspicious')}
        />
      </View>

      <View style={s.list}>
        {filtered.length === 0 ? <EmptyState /> : null}
        {filtered.map((env) => (
          <EnvelopeCard key={env.requestId} env={env} onPress={() => onOpenEnvelope?.(env)} />
        ))}
      </View>
    </Screen>
  );
}

function filterEnvelopes(filter: Filter): ReadonlyArray<SigningEnvelope> {
  if (filter === 'mine') {
    return ENVELOPES.filter((e) => e.decisions.find((d) => d.self)?.state === 'pending');
  }
  if (filter === 'suspicious') {
    return ENVELOPES.filter((e) => !e.crossCheckOk);
  }
  return ENVELOPES;
}

interface EnvelopeCardProps {
  readonly env: SigningEnvelope;
  readonly onPress?: () => void;
}

function EnvelopeCard({ env, onPress }: EnvelopeCardProps): React.JSX.Element {
  const t = useTheme();
  const { t: T, tx } = useI18n();
  const s = useMemo(() => makeStyles(t), [t]);
  const wallet = WALLETS.find((w) => w.groupId === env.groupId);
  const myDecision = env.decisions.find((d) => d.self);
  const approvedCount = env.decisions.filter((d) => d.state === 'approved').length;
  const danger = !env.crossCheckOk;
  return (
    <Pressable
      onPress={onPress}
      accessibilityRole="button"
      accessibilityLabel={`${T.app.name} — ${env.requestId}`}
      style={({ pressed }) => [
        s.card,
        danger ? s.cardDanger : null,
        pressed ? s.cardPressed : null,
      ]}
    >
      <View style={s.cardHead}>
        <ChainBadge chain={env.chain} label={env.chainLabel} />
        {wallet ? <Text style={s.cardWallet}>{wallet.moniker}</Text> : null}
        <View style={s.spacer} />
        <View style={s.expiry}>
          <Icon name="clock" size={12} color={t.warn} />
          <Text style={s.expiryText}>{formatExpiry(env.expiresIn, T.inbox.expired)}</Text>
        </View>
      </View>

      <View style={s.titleRow}>
        <View style={s.titleLeft}>
          <Text style={s.title} numberOfLines={1}>
            {env.businessInfo.title}
          </Text>
          <Text style={s.subTitle} numberOfLines={1}>
            → {env.unsignedTxSummary.to}
            <Text style={[s.subTitleEm, danger ? s.subTitleDanger : null]}>
              {'  · '}
              {env.unsignedTxSummary.toLabel}
            </Text>
          </Text>
        </View>
        <View style={s.amountBox}>
          <Text style={s.amount}>{env.unsignedTxSummary.value}</Text>
          <Text style={s.amountFiat}>{env.unsignedTxSummary.valueFiat}</Text>
        </View>
      </View>

      {danger ? (
        <View style={s.banner}>
          <Icon name="warn" size={14} color={t.danger} />
          <Text style={s.bannerText}>{env.mismatchHint ?? T.inbox.mismatchFallback}</Text>
        </View>
      ) : null}

      <Hairline />

      <View style={s.footRow}>
        <View style={s.decisions}>
          <View style={s.decisionDots}>
            {env.decisions.map((d) => (
              <View
                key={d.id}
                style={[
                  s.decisionDot,
                  d.state === 'approved' ? s.decisionApproved : null,
                  d.state === 'rejected' ? s.decisionRejected : null,
                ]}
              >
                <Text
                  style={[
                    s.decisionDotText,
                    d.state === 'approved' ? s.decisionApprovedText : null,
                    d.state === 'rejected' ? s.decisionRejectedText : null,
                  ]}
                >
                  {d.id}
                </Text>
              </View>
            ))}
          </View>
          <Text style={s.tallyText}>
            {tx(T.inbox.approvedOf, {
              approved: approvedCount,
              threshold: wallet?.threshold ?? '?',
            })}
          </Text>
        </View>
        <View style={s.cta}>
          <Text style={[s.ctaText, myDecision?.state === 'pending' ? s.ctaTextActive : null]}>
            {myDecision?.state === 'pending' ? T.inbox.needsDecision : T.inbox.decided}
          </Text>
          <Icon
            name="chevronR"
            size={12}
            color={myDecision?.state === 'pending' ? t.accent : t.text3}
          />
        </View>
      </View>
    </Pressable>
  );
}

function CoordPillView(): React.JSX.Element {
  const t = useTheme();
  const s = useMemo(() => makeStyles(t), [t]);
  return (
    <View style={s.coordPill}>
      <View style={s.coordDot} />
      <Text style={s.coordText}>coord · {COORD.latencyMs}ms</Text>
    </View>
  );
}

function EmptyState(): React.JSX.Element {
  const t = useTheme();
  const { t: T } = useI18n();
  const s = useMemo(() => makeStyles(t), [t]);
  return (
    <View style={s.empty}>
      <View style={s.emptyIcon}>
        <Icon name="check" size={28} color={t.accent} />
      </View>
      <Text style={s.emptyTitle}>{T.inbox.emptyTitle}</Text>
      <Text style={s.emptySub}>{T.inbox.emptySub}</Text>
    </View>
  );
}

export function formatExpiry(seconds: number, expiredLabel: string): string {
  if (seconds <= 0) return expiredLabel;
  if (seconds < 60) return `${seconds}s`;
  const m = Math.floor(seconds / 60);
  const sec = seconds % 60;
  return `${m}m ${sec < 10 ? '0' : ''}${sec}s`;
}

function makeStyles(t: ThemeTokens) {
  return StyleSheet.create({
    appBar: {
      paddingTop: spacing.xl,
      paddingHorizontal: spacing.lg,
      flexDirection: 'row',
      alignItems: 'center',
      justifyContent: 'space-between',
    },
    brand: { flexDirection: 'row', alignItems: 'center', gap: spacing.sm },
    brandText: { color: t.text, fontSize: 17, fontWeight: '700', letterSpacing: 0.3 },
    appBarRight: { flexDirection: 'row', alignItems: 'center', gap: spacing.sm },
    hero: { paddingHorizontal: spacing.lg, paddingTop: spacing.md },
    heroRow: { flexDirection: 'row', alignItems: 'baseline', gap: spacing.sm },
    heroCount: { color: t.text, fontSize: 42, fontWeight: '700', letterSpacing: -1 },
    heroLabel: { color: t.text2, fontSize: 15, fontWeight: '500' },
    heroMeta: { color: t.text3, fontSize: 12, marginTop: 4 },
    filterRow: {
      paddingHorizontal: spacing.lg,
      paddingTop: spacing.lg,
      flexDirection: 'row',
      gap: spacing.sm,
      flexWrap: 'wrap',
    },
    list: { paddingHorizontal: spacing.lg, paddingTop: spacing.md, gap: 10 },
    card: {
      padding: spacing.md,
      borderRadius: radius.xl,
      backgroundColor: t.surface,
      borderColor: t.hairline,
      borderWidth: StyleSheet.hairlineWidth,
    },
    cardDanger: { borderColor: `${t.danger}55`, backgroundColor: `${t.danger}0d` },
    cardPressed: { opacity: 0.85 },
    cardHead: {
      flexDirection: 'row',
      alignItems: 'center',
      gap: spacing.sm,
      marginBottom: 10,
    },
    cardWallet: { color: t.text3, fontSize: 11 },
    spacer: { flex: 1 },
    expiry: { flexDirection: 'row', alignItems: 'center', gap: 4 },
    expiryText: { color: t.warn, fontSize: 11, fontWeight: '700' },
    titleRow: {
      flexDirection: 'row',
      alignItems: 'flex-start',
      gap: spacing.sm,
      marginBottom: 10,
    },
    titleLeft: { flex: 1, minWidth: 0 },
    title: { color: t.text, fontSize: 15, fontWeight: '700', letterSpacing: 0.1, marginBottom: 4 },
    subTitle: { color: t.text3, fontSize: 11.5, fontFamily: fontFamily.mono },
    subTitleEm: { color: t.text3 },
    subTitleDanger: { color: t.danger },
    amountBox: { alignItems: 'flex-end' },
    amount: { color: t.text, fontSize: 15, fontWeight: '700', letterSpacing: -0.2 },
    amountFiat: { color: t.text3, fontSize: 11 },
    banner: {
      flexDirection: 'row',
      alignItems: 'center',
      gap: 6,
      paddingVertical: 7,
      paddingHorizontal: 10,
      borderRadius: 9,
      backgroundColor: `${t.danger}1a`,
      borderColor: `${t.danger}55`,
      borderWidth: StyleSheet.hairlineWidth,
      marginBottom: 10,
    },
    bannerText: { color: t.danger, fontSize: 11.5, fontWeight: '600' },
    footRow: {
      flexDirection: 'row',
      alignItems: 'center',
      justifyContent: 'space-between',
      paddingTop: 10,
    },
    decisions: { flexDirection: 'row', alignItems: 'center', gap: 8 },
    decisionDots: { flexDirection: 'row', gap: 3 },
    decisionDot: {
      width: 18,
      height: 18,
      borderRadius: 5,
      backgroundColor: t.surface2,
      borderColor: t.hairline,
      borderWidth: StyleSheet.hairlineWidth,
      alignItems: 'center',
      justifyContent: 'center',
    },
    decisionApproved: { backgroundColor: `${t.accent}30`, borderColor: `${t.accent}66` },
    decisionRejected: { backgroundColor: `${t.danger}30`, borderColor: `${t.danger}66` },
    decisionDotText: { color: t.text3, fontSize: 8.5, fontWeight: '700' },
    decisionApprovedText: { color: t.accent },
    decisionRejectedText: { color: t.danger },
    tallyText: { color: t.text3, fontSize: 11 },
    cta: { flexDirection: 'row', alignItems: 'center', gap: 5 },
    ctaText: { color: t.text3, fontSize: 11.5, fontWeight: '600' },
    ctaTextActive: { color: t.accent },
    coordPill: {
      flexDirection: 'row',
      alignItems: 'center',
      gap: 6,
      paddingHorizontal: 10,
      paddingVertical: 4,
      borderRadius: radius.pill,
      backgroundColor: t.surface,
      borderColor: t.hairline,
      borderWidth: StyleSheet.hairlineWidth,
    },
    coordDot: { width: 7, height: 7, borderRadius: 4, backgroundColor: t.accent },
    coordText: { color: t.text2, fontSize: 11, fontWeight: '500', letterSpacing: 0.3 },
    empty: { alignItems: 'center', paddingVertical: 48, paddingHorizontal: spacing.lg },
    emptyIcon: {
      width: 60,
      height: 60,
      borderRadius: 30,
      backgroundColor: `${t.accent}1a`,
      borderColor: `${t.accent}55`,
      borderWidth: StyleSheet.hairlineWidth,
      alignItems: 'center',
      justifyContent: 'center',
      marginBottom: 14,
    },
    emptyTitle: { color: t.text, fontSize: 15, fontWeight: '600' },
    emptySub: {
      color: t.text2,
      fontSize: 12,
      marginTop: 6,
      textAlign: 'center',
      lineHeight: 18,
    },
  });
}
