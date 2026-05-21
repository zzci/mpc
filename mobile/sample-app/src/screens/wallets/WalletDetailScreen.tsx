// Wallet detail surface. Shows member roster with presence, threshold/party
// quorum, address list with chain badges and default markers, group key
// material (groupId / ecdsa pubkey / chaincode / xpub-ready), and the two
// product CTAs: derive a new address (in-app sheet, see DeriveAddressSheet)
// and reshare (preserves the existing SDK panel reshare surface so the
// bridge contract stays intact).

import React, { useMemo, useState } from 'react';
import { View, Text, StyleSheet, Pressable } from 'react-native';
import {
  Card,
  ChainBadge,
  GhostButton,
  Hairline,
  Icon,
  PrimaryButton,
  Screen,
  SectionLabel,
  TopBar,
  TrineMark,
  fontFamily,
  radius,
  spacing,
  useTheme,
} from '../../ui';
import type { ThemeTokens } from '../../ui';
import { useI18n } from '../../i18n';
import type { Strings } from '../../i18n';
import type { GroupMember, Wallet, WalletAddress } from '../../data';
import { DeriveAddressSheet } from './DeriveAddressSheet';

export interface WalletDetailScreenProps {
  readonly wallet: Wallet;
  readonly onBack: () => void;
  readonly onReshare: (wallet: Wallet) => void;
}

export function WalletDetailScreen({ wallet, onBack, onReshare }: WalletDetailScreenProps): React.JSX.Element {
  const t = useTheme();
  const { t: T, tx } = useI18n();
  const s = useMemo(() => makeStyles(t), [t]);
  const [extraAddresses, setExtraAddresses] = useState<ReadonlyArray<WalletAddress>>([]);
  const [deriveOpen, setDeriveOpen] = useState(false);

  const addresses = useMemo<ReadonlyArray<WalletAddress>>(
    () => [...wallet.addresses, ...extraAddresses],
    [wallet.addresses, extraAddresses],
  );
  const uniqueChains = useMemo(
    () => Array.from(new Set(addresses.map((a) => a.chainLabel))),
    [addresses],
  );
  const onlineCount = wallet.members.filter((m) => m.status === 'online').length;

  return (
    <Screen
      footer={
        <View style={s.footer}>
          <GhostButton
            label={T.walletDetail.reshareAction}
            icon="refresh"
            onPress={() => onReshare(wallet)}
            style={s.flex1}
          />
          <PrimaryButton
            label={T.walletDetail.deriveAction}
            onPress={() => setDeriveOpen(true)}
            style={s.flex12}
          />
        </View>
      }
    >
      <TopBar title={T.walletDetail.title} onBack={onBack} backLabel={T.common.back} />

      <View style={s.body}>
        <Card style={s.headCard}>
          <View style={s.headRow}>
            <View style={s.headIcon}>
              <TrineMark size={22} />
            </View>
            <View style={s.headText}>
              <Text style={s.moniker} numberOfLines={1}>
                {wallet.moniker}
              </Text>
              <Text style={s.groupId} numberOfLines={1}>
                {wallet.groupId}
              </Text>
            </View>
            <View style={s.threshold}>
              <Text style={s.thresholdText}>
                {wallet.threshold}-of-{wallet.parties}
              </Text>
            </View>
          </View>

          <View style={s.headStats}>
            <View style={s.headStatCell}>
              <Text style={s.headStatValue}>{addresses.length}</Text>
              <Text style={s.headStatLabel}>{T.wallets.addresses}</Text>
            </View>
            <View style={s.headStatDivider} />
            <View style={s.headStatCell}>
              <Text style={s.headStatValue}>{wallet.members.length}</Text>
              <Text style={s.headStatLabel}>{T.walletDetail.membersHeading.toLowerCase()}</Text>
            </View>
            <View style={s.headStatDivider} />
            <View style={[s.headStatCell, s.headStatXpub]}>
              <View
                style={[
                  s.xpubDot,
                  { backgroundColor: wallet.xpubReady ? t.ok : t.warn },
                ]}
              />
              <Text style={s.xpubLabel}>
                {wallet.xpubReady ? T.walletDetail.xpubReady : T.walletDetail.xpubPending}
              </Text>
            </View>
          </View>
        </Card>

        <SectionLabel>{T.walletDetail.thresholdHeading}</SectionLabel>
        <Card>
          <Text style={s.thresholdLine}>
            {tx(T.walletDetail.thresholdSub, {
              threshold: wallet.threshold,
              parties: wallet.parties,
            })}
          </Text>
        </Card>

        <SectionLabel>{T.walletDetail.membersHeading}</SectionLabel>
        <Card style={s.padCard}>
          <Text style={s.subHeading}>
            {tx(T.walletDetail.membersSub, {
              count: wallet.members.length,
              online: onlineCount,
            })}
          </Text>
          <View style={s.memberList}>
            {wallet.members.map((m, i) => (
              <View key={m.id}>
                <MemberRow member={m} T={T} />
                {i < wallet.members.length - 1 ? <Hairline /> : null}
              </View>
            ))}
          </View>
        </Card>

        <SectionLabel>{T.walletDetail.addressesHeading}</SectionLabel>
        <Card style={s.padCard}>
          <Text style={s.subHeading}>
            {tx(T.walletDetail.addressesSub, {
              count: addresses.length,
              chains: uniqueChains.length,
            })}
          </Text>
          <View style={s.addrList}>
            {addresses.map((a, i) => (
              <View key={a.id}>
                <AddressRow address={a} T={T} />
                {i < addresses.length - 1 ? <Hairline /> : null}
              </View>
            ))}
          </View>
        </Card>

        <SectionLabel>{T.walletDetail.keyMaterialHeading}</SectionLabel>
        <Card style={s.padCard}>
          <Text style={s.subHeading}>{T.walletDetail.keyMaterialSub}</Text>
          <View style={s.keyList}>
            <KeyValueRow
              label={T.walletDetail.groupIdLabel}
              value={wallet.groupId}
            />
            <Hairline />
            <KeyValueRow
              label={T.walletDetail.ecdsaPubkeyLabel}
              value={wallet.ecdsaPubkey}
            />
            <Hairline />
            <KeyValueRow
              label={T.walletDetail.chaincodeLabel}
              value={wallet.chaincode}
            />
          </View>
        </Card>

        <View style={s.actionList}>
          <ActionCard
            title={T.walletDetail.deriveAction}
            sub={T.walletDetail.deriveActionSub}
            icon="plus"
            onPress={() => setDeriveOpen(true)}
          />
          <ActionCard
            title={T.walletDetail.reshareAction}
            sub={T.walletDetail.reshareActionSub}
            icon="refresh"
            onPress={() => onReshare(wallet)}
          />
        </View>
      </View>

      <DeriveAddressSheet
        visible={deriveOpen}
        wallet={wallet}
        existing={addresses}
        onClose={() => setDeriveOpen(false)}
        onAdd={(addr) => setExtraAddresses((prev) => [...prev, addr])}
      />
    </Screen>
  );
}

interface MemberRowProps {
  readonly member: GroupMember;
  readonly T: Strings;
}

function MemberRow({ member, T }: MemberRowProps): React.JSX.Element {
  const t = useTheme();
  const { tx } = useI18n();
  const s = useMemo(() => makeStyles(t), [t]);
  const presenceColor =
    member.status === 'online' ? t.ok : member.status === 'offline' ? t.danger : t.text3;
  const presenceLabel = presenceLabelFor(member, T);
  return (
    <View style={s.memberRow}>
      <View style={[s.memberAvatar, { borderColor: `${presenceColor}55` }]}>
        <Text style={[s.memberAvatarText, member.self ? { color: t.accent } : null]}>
          {member.id}
        </Text>
      </View>
      <View style={s.memberInfo}>
        <View style={s.memberInfoRow}>
          <Text style={s.memberLabel} numberOfLines={1}>
            {member.label}
          </Text>
          {member.self ? (
            <View style={s.selfBadge}>
              <Text style={s.selfBadgeText}>{T.walletDetail.memberSelfBadge}</Text>
            </View>
          ) : null}
        </View>
        <Text style={s.memberSub} numberOfLines={1}>
          {tx(T.walletDetail.memberLastSeen, { when: member.last })}
        </Text>
      </View>
      <View style={[s.presencePill, { borderColor: `${presenceColor}55`, backgroundColor: `${presenceColor}1a` }]}>
        <View style={[s.presenceDot, { backgroundColor: presenceColor }]} />
        <Text style={[s.presenceText, { color: presenceColor }]}>{presenceLabel}</Text>
      </View>
    </View>
  );
}

function presenceLabelFor(member: GroupMember, T: Strings): string {
  if (member.status === 'online') return T.wallets.online;
  if (member.status === 'offline') return T.wallets.offline;
  return T.wallets.standby;
}

interface AddressRowProps {
  readonly address: WalletAddress;
  readonly T: Strings;
}

function AddressRow({ address, T }: AddressRowProps): React.JSX.Element {
  const t = useTheme();
  const s = useMemo(() => makeStyles(t), [t]);
  return (
    <View style={s.addrRow}>
      <View style={s.addrHead}>
        <Text style={s.addrLabel} numberOfLines={1}>
          {address.label}
        </Text>
        {address.isDefault ? (
          <View style={s.defaultBadge}>
            <Text style={s.defaultBadgeText}>{T.walletDetail.defaultBadge}</Text>
          </View>
        ) : null}
      </View>
      <View style={s.addrChainRow}>
        <ChainBadge chain={address.chain} label={address.chainLabel} />
        <Text style={s.addrPath} numberOfLines={1}>
          {T.walletDetail.pathLabel} · {address.path}
        </Text>
      </View>
      <Text style={s.addrValue} numberOfLines={1} ellipsizeMode="middle">
        {address.address}
      </Text>
    </View>
  );
}

interface KeyValueRowProps {
  readonly label: string;
  readonly value: string;
}

function KeyValueRow({ label, value }: KeyValueRowProps): React.JSX.Element {
  const t = useTheme();
  const s = useMemo(() => makeStyles(t), [t]);
  return (
    <View style={s.kvRow}>
      <Text style={s.kvLabel}>{label}</Text>
      <Text style={s.kvValue} numberOfLines={1} ellipsizeMode="middle">
        {value}
      </Text>
    </View>
  );
}

interface ActionCardProps {
  readonly title: string;
  readonly sub: string;
  readonly icon: 'plus' | 'refresh';
  readonly onPress: () => void;
}

function ActionCard({ title, sub, icon, onPress }: ActionCardProps): React.JSX.Element {
  const t = useTheme();
  const s = useMemo(() => makeStyles(t), [t]);
  return (
    <Pressable
      onPress={onPress}
      accessibilityRole="button"
      accessibilityLabel={title}
      style={({ pressed }) => [s.actionCard, pressed ? s.actionCardPressed : null]}
    >
      <View style={s.actionIcon}>
        <Icon name={icon} size={16} color={t.accent} />
      </View>
      <View style={s.actionText}>
        <Text style={s.actionTitle}>{title}</Text>
        <Text style={s.actionSub}>{sub}</Text>
      </View>
      <Icon name="chevronR" size={13} color={t.text3} />
    </Pressable>
  );
}

function makeStyles(t: ThemeTokens) {
  return StyleSheet.create({
    body: { paddingHorizontal: spacing.lg, paddingBottom: spacing.lg },
    headCard: { padding: spacing.lg, gap: spacing.md },
    headRow: { flexDirection: 'row', alignItems: 'center', gap: spacing.sm },
    headIcon: {
      width: 40,
      height: 40,
      borderRadius: 12,
      backgroundColor: `${t.accent}14`,
      borderColor: `${t.accent}33`,
      borderWidth: StyleSheet.hairlineWidth,
      alignItems: 'center',
      justifyContent: 'center',
    },
    headText: { flex: 1, minWidth: 0 },
    moniker: { color: t.text, fontSize: 16, fontWeight: '700' },
    groupId: { color: t.text3, fontSize: 11.5, fontFamily: fontFamily.mono, marginTop: 2 },
    threshold: {
      backgroundColor: `${t.accent}1a`,
      paddingHorizontal: 10,
      paddingVertical: 5,
      borderRadius: 8,
    },
    thresholdText: { color: t.accent, fontSize: 11.5, fontWeight: '700', letterSpacing: 0.4 },
    headStats: {
      flexDirection: 'row',
      alignItems: 'center',
      paddingTop: spacing.sm,
      borderTopColor: t.hairline,
      borderTopWidth: StyleSheet.hairlineWidth,
    },
    headStatCell: { flex: 1, alignItems: 'flex-start', paddingHorizontal: 4 },
    headStatXpub: { flexDirection: 'row', alignItems: 'center', gap: 6 },
    headStatValue: { color: t.text, fontSize: 18, fontWeight: '700' },
    headStatLabel: { color: t.text3, fontSize: 10.5, marginTop: 2, letterSpacing: 0.3 },
    headStatDivider: { width: StyleSheet.hairlineWidth, height: 24, backgroundColor: t.hairline, marginHorizontal: 4 },
    xpubDot: { width: 8, height: 8, borderRadius: 4 },
    xpubLabel: { color: t.text2, fontSize: 11, fontWeight: '600', letterSpacing: 0.2 },
    thresholdLine: { color: t.text, fontSize: 13, lineHeight: 18, paddingVertical: 4 },
    padCard: { padding: spacing.md, gap: spacing.sm },
    subHeading: { color: t.text3, fontSize: 11.5, lineHeight: 16, paddingHorizontal: 2 },
    memberList: { gap: 0 },
    memberRow: {
      flexDirection: 'row',
      alignItems: 'center',
      gap: spacing.sm,
      paddingVertical: 10,
    },
    memberAvatar: {
      width: 36,
      height: 36,
      borderRadius: 10,
      backgroundColor: t.surface2,
      borderWidth: StyleSheet.hairlineWidth,
      alignItems: 'center',
      justifyContent: 'center',
    },
    memberAvatarText: {
      color: t.text2,
      fontSize: 12,
      fontWeight: '700',
      fontFamily: fontFamily.mono,
    },
    memberInfo: { flex: 1, minWidth: 0, gap: 2 },
    memberInfoRow: { flexDirection: 'row', alignItems: 'center', gap: 6 },
    memberLabel: { color: t.text, fontSize: 13, fontWeight: '600' },
    memberSub: { color: t.text3, fontSize: 11 },
    selfBadge: {
      paddingHorizontal: 7,
      paddingVertical: 2,
      borderRadius: 6,
      backgroundColor: `${t.accent}1a`,
      borderColor: `${t.accent}55`,
      borderWidth: StyleSheet.hairlineWidth,
    },
    selfBadgeText: { color: t.accent, fontSize: 9.5, fontWeight: '700', letterSpacing: 0.3 },
    presencePill: {
      flexDirection: 'row',
      alignItems: 'center',
      gap: 5,
      paddingHorizontal: 9,
      paddingVertical: 3,
      borderRadius: radius.pill,
      borderWidth: StyleSheet.hairlineWidth,
    },
    presenceDot: { width: 6, height: 6, borderRadius: 3 },
    presenceText: { fontSize: 10.5, fontWeight: '700', letterSpacing: 0.3 },
    addrList: { gap: 0 },
    addrRow: { paddingVertical: 12, gap: 6 },
    addrHead: { flexDirection: 'row', alignItems: 'center', gap: 6 },
    addrLabel: { color: t.text, fontSize: 13.5, fontWeight: '600', flex: 1 },
    defaultBadge: {
      paddingHorizontal: 7,
      paddingVertical: 2,
      borderRadius: 6,
      backgroundColor: `${t.ok}1a`,
      borderColor: `${t.ok}55`,
      borderWidth: StyleSheet.hairlineWidth,
    },
    defaultBadgeText: { color: t.ok, fontSize: 9.5, fontWeight: '700', letterSpacing: 0.3 },
    addrChainRow: { flexDirection: 'row', alignItems: 'center', gap: 8, flexWrap: 'wrap' },
    addrPath: { color: t.text3, fontSize: 11, fontFamily: fontFamily.mono },
    addrValue: { color: t.text2, fontSize: 12, fontFamily: fontFamily.mono },
    keyList: { gap: 0 },
    kvRow: {
      flexDirection: 'row',
      alignItems: 'center',
      paddingVertical: 12,
      gap: spacing.md,
    },
    kvLabel: { color: t.text3, fontSize: 11.5, letterSpacing: 0.3, width: 110 },
    kvValue: { color: t.text, fontSize: 12, fontFamily: fontFamily.mono, flex: 1, textAlign: 'right' },
    actionList: { gap: spacing.sm, marginTop: spacing.lg },
    actionCard: {
      flexDirection: 'row',
      alignItems: 'center',
      gap: spacing.sm,
      padding: spacing.md,
      borderRadius: radius.xl,
      backgroundColor: t.surface,
      borderColor: t.hairline,
      borderWidth: StyleSheet.hairlineWidth,
    },
    actionCardPressed: { opacity: 0.85 },
    actionIcon: {
      width: 36,
      height: 36,
      borderRadius: 11,
      backgroundColor: `${t.accent}14`,
      borderColor: `${t.accent}33`,
      borderWidth: StyleSheet.hairlineWidth,
      alignItems: 'center',
      justifyContent: 'center',
    },
    actionText: { flex: 1, minWidth: 0 },
    actionTitle: { color: t.text, fontSize: 14, fontWeight: '700' },
    actionSub: { color: t.text3, fontSize: 11, marginTop: 2 },
    footer: { flexDirection: 'row', gap: spacing.sm, alignItems: 'center' },
    flex1: { flex: 1 },
    flex12: { flex: 1.2 },
  });
}
