import React, { useMemo } from 'react';
import { View, Text, StyleSheet, Pressable } from 'react-native';
import {
  Hairline,
  Icon,
  Screen,
  TopBar,
  TrineMark,
  useTheme,
  spacing,
  radius,
  fontFamily,
} from '../../ui';
import type { ThemeTokens } from '../../ui';
import { useI18n } from '../../i18n';
import type { Strings } from '../../i18n';
import { WALLETS } from '../../data';
import type { GroupMember, Wallet } from '../../data';

export interface WalletsScreenProps {
  readonly onOpenWallet?: (wallet: Wallet) => void;
  readonly onStartKeygen?: () => void;
}

export function WalletsScreen({ onOpenWallet, onStartKeygen }: WalletsScreenProps): React.JSX.Element {
  const t = useTheme();
  const { t: T } = useI18n();
  const s = useMemo(() => makeStyles(t), [t]);
  return (
    <Screen>
      <TopBar
        title={T.wallets.title}
        right={
          <Pressable accessibilityLabel={T.common.add} style={s.iconBtn}>
            <Icon name="plus" size={18} color={t.text} />
          </Pressable>
        }
      />

      <View style={s.list}>
        {WALLETS.map((w) => (
          <WalletCard key={w.groupId} wallet={w} onPress={() => onOpenWallet?.(w)} />
        ))}

        <Pressable
          onPress={onStartKeygen}
          accessibilityRole="button"
          accessibilityLabel={T.wallets.newWallet}
          style={({ pressed }) => [s.cta, pressed ? s.ctaPressed : null]}
        >
          <View style={s.ctaIcon}>
            <Icon name="plus" size={18} color={t.accent} />
          </View>
          <View style={s.ctaText}>
            <Text style={s.ctaTitle}>{T.wallets.newWallet}</Text>
            <Text style={s.ctaSub}>{T.wallets.newWalletSub}</Text>
          </View>
          <Icon name="chevronR" size={13} color={t.text3} />
        </Pressable>
      </View>
    </Screen>
  );
}

interface WalletCardProps {
  readonly wallet: Wallet;
  readonly onPress?: () => void;
}

function WalletCard({ wallet, onPress }: WalletCardProps): React.JSX.Element {
  const t = useTheme();
  const { t: T } = useI18n();
  const s = useMemo(() => makeStyles(t), [t]);
  const uniqueChains = Array.from(new Set(wallet.addresses.map((a) => a.chainLabel)));
  return (
    <Pressable
      onPress={onPress}
      accessibilityRole="button"
      accessibilityLabel={wallet.moniker}
      style={({ pressed }) => [s.card, pressed ? s.cardPressed : null]}
    >
      <View style={s.head}>
        <View style={s.headIcon}>
          <TrineMark size={20} />
        </View>
        <View style={s.headText}>
          <Text style={s.moniker}>{wallet.moniker}</Text>
          <Text style={s.groupId}>{wallet.groupId}</Text>
        </View>
        <View style={s.threshold}>
          <Text style={s.thresholdText}>
            {wallet.threshold}-of-{wallet.parties}
          </Text>
        </View>
      </View>

      <View style={s.members}>
        {wallet.members.map((m) => (
          <View key={m.id} style={[s.member, m.self ? s.memberSelf : null]}>
            <View style={s.memberIdRow}>
              <Text style={[s.memberId, m.self ? s.memberIdSelf : null]}>{m.id}</Text>
              <View
                style={[
                  s.memberStatusDot,
                  m.status === 'online' ? s.statusOnline : null,
                  m.status === 'offline' ? s.statusOffline : null,
                  m.status === 'standby' ? s.statusStandby : null,
                ]}
              />
            </View>
            <Text style={s.memberLabel}>{memberLabel(m, T)}</Text>
          </View>
        ))}
      </View>

      <Hairline />

      <View style={s.summary}>
        <View>
          <Text style={s.summaryCount}>{wallet.addresses.length}</Text>
          <Text style={s.summaryLabel}>{T.wallets.addresses}</Text>
        </View>
        <View style={s.summaryDivider} />
        <View style={s.chainTagsWrap}>
          {uniqueChains.map((c) => (
            <View key={c} style={s.chainTag}>
              <Text style={s.chainTagText}>{c}</Text>
            </View>
          ))}
        </View>
        <Icon name="chevronR" size={13} color={t.text3} />
      </View>
    </Pressable>
  );
}

function memberLabel(m: GroupMember, T: Strings): string {
  if (m.self) return T.wallets.thisDevice;
  if (m.status === 'online') return T.wallets.online;
  if (m.status === 'offline') return T.wallets.offline;
  return T.wallets.standby;
}

function makeStyles(t: ThemeTokens) {
  return StyleSheet.create({
    iconBtn: {
      width: 36,
      height: 36,
      borderRadius: 18,
      backgroundColor: t.surface,
      borderColor: t.hairline,
      borderWidth: StyleSheet.hairlineWidth,
      alignItems: 'center',
      justifyContent: 'center',
    },
    list: { paddingHorizontal: spacing.lg, paddingTop: spacing.sm, gap: 12 },
    card: {
      padding: spacing.lg,
      borderRadius: radius.xl,
      backgroundColor: t.surface,
      borderColor: t.hairline,
      borderWidth: StyleSheet.hairlineWidth,
    },
    cardPressed: { opacity: 0.85 },
    head: { flexDirection: 'row', alignItems: 'center', gap: spacing.sm, marginBottom: 14 },
    headIcon: {
      width: 36,
      height: 36,
      borderRadius: 11,
      backgroundColor: `${t.accent}14`,
      borderColor: `${t.accent}33`,
      borderWidth: StyleSheet.hairlineWidth,
      alignItems: 'center',
      justifyContent: 'center',
    },
    headText: { flex: 1, minWidth: 0 },
    moniker: { color: t.text, fontSize: 15, fontWeight: '700' },
    groupId: { color: t.text3, fontSize: 11, marginTop: 2, fontFamily: fontFamily.mono },
    threshold: {
      backgroundColor: `${t.accent}1a`,
      paddingHorizontal: 9,
      paddingVertical: 4,
      borderRadius: 7,
    },
    thresholdText: { color: t.accent, fontSize: 11, fontWeight: '700', letterSpacing: 0.4 },
    members: { flexDirection: 'row', gap: 5, marginBottom: 12 },
    member: {
      flex: 1,
      paddingVertical: 7,
      paddingHorizontal: 4,
      borderRadius: 8,
      backgroundColor: t.surface2,
      borderColor: t.hairline,
      borderWidth: StyleSheet.hairlineWidth,
      alignItems: 'center',
      gap: 3,
    },
    memberSelf: { backgroundColor: `${t.accent}10`, borderColor: `${t.accent}33` },
    memberIdRow: { flexDirection: 'row', alignItems: 'center', gap: 4 },
    memberId: { color: t.text2, fontSize: 11, fontWeight: '700', fontFamily: fontFamily.mono },
    memberIdSelf: { color: t.accent },
    memberStatusDot: { width: 6, height: 6, borderRadius: 3, backgroundColor: t.text3 },
    statusOnline: { backgroundColor: t.accent },
    statusOffline: { backgroundColor: t.danger },
    statusStandby: { backgroundColor: t.text3 },
    memberLabel: { color: t.text3, fontSize: 9.5 },
    summary: { flexDirection: 'row', alignItems: 'center', gap: spacing.sm, paddingTop: 10 },
    summaryCount: { color: t.text, fontSize: 18, fontWeight: '700' },
    summaryLabel: { color: t.text3, fontSize: 10, letterSpacing: 0.3, marginTop: 1 },
    summaryDivider: { width: StyleSheet.hairlineWidth, height: 24, backgroundColor: t.hairline },
    chainTagsWrap: { flex: 1, flexDirection: 'row', flexWrap: 'wrap', gap: 5 },
    chainTag: {
      paddingHorizontal: 7,
      paddingVertical: 2,
      borderRadius: 5,
      backgroundColor: t.surface,
      borderColor: t.hairline,
      borderWidth: StyleSheet.hairlineWidth,
    },
    chainTagText: { color: t.text2, fontSize: 10, fontWeight: '600', letterSpacing: 0.2 },
    cta: {
      padding: spacing.md,
      borderRadius: radius.xl,
      backgroundColor: t.surface,
      borderColor: t.hairline,
      borderWidth: StyleSheet.hairlineWidth,
      borderStyle: 'dashed',
      flexDirection: 'row',
      alignItems: 'center',
      gap: spacing.sm,
    },
    ctaPressed: { opacity: 0.85 },
    ctaIcon: {
      width: 36,
      height: 36,
      borderRadius: 11,
      backgroundColor: t.surface2,
      alignItems: 'center',
      justifyContent: 'center',
    },
    ctaText: { flex: 1 },
    ctaTitle: { color: t.text, fontSize: 14, fontWeight: '700' },
    ctaSub: { color: t.text3, fontSize: 11, marginTop: 2 },
  });
}
