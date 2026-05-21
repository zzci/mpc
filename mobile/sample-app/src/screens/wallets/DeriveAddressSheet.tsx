// Modal sheet that derives a new address for an existing wallet from the
// invariant group public key + chaincode. The actual derivation in this L3
// is a demo (see derive.ts) — the visible flow mirrors what a real
// bridge-backed derivation will look like: pick a chain, optionally tweak
// the path, see a preview, confirm to add it locally. No bridge call is
// made: this preserves the current sdk.ts pass-through and never invents
// a network round trip.

import React, { useEffect, useMemo, useState } from 'react';
import { Modal, Pressable, StyleSheet, Text, TextInput, View } from 'react-native';
import {
  ChainBadge,
  GhostButton,
  Icon,
  PrimaryButton,
  fontFamily,
  radius,
  spacing,
  useTheme,
} from '../../ui';
import type { ThemeTokens } from '../../ui';
import { useI18n } from '../../i18n';
import type { Wallet, WalletAddress } from '../../data';
import { derivePreview, normalizePath } from './derive';

interface ChainOption {
  readonly chain: string;
  readonly chainLabel: string;
}

export interface DeriveAddressSheetProps {
  readonly visible: boolean;
  readonly wallet: Wallet;
  readonly existing: ReadonlyArray<WalletAddress>;
  readonly onClose: () => void;
  readonly onAdd: (added: WalletAddress) => void;
}

export function DeriveAddressSheet({
  visible,
  wallet,
  existing,
  onClose,
  onAdd,
}: DeriveAddressSheetProps): React.JSX.Element {
  const t = useTheme();
  const { t: T } = useI18n();
  const s = useMemo(() => makeStyles(t), [t]);

  const chains = useMemo<ReadonlyArray<ChainOption>>(() => {
    const seen = new Map<string, string>();
    for (const a of wallet.addresses) {
      if (!seen.has(a.chain)) seen.set(a.chain, a.chainLabel);
    }
    return Array.from(seen.entries(), ([chain, chainLabel]) => ({ chain, chainLabel }));
  }, [wallet.addresses]);

  const [chainIdx, setChainIdx] = useState(0);
  const [path, setPath] = useState('');
  const [added, setAdded] = useState<WalletAddress | null>(null);

  // Reset transient state every time the sheet opens.
  useEffect(() => {
    if (visible) {
      setChainIdx(0);
      setPath('');
      setAdded(null);
    }
  }, [visible]);

  const chain = chains[chainIdx];
  const preview = useMemo(() => {
    if (!chain) return null;
    return derivePreview(wallet, chain.chain, chain.chainLabel, path);
  }, [wallet, chain, path]);

  const normalizedPath = normalizePath(path);
  const duplicate = useMemo(() => {
    if (!chain) return false;
    return existing.some((a) => a.chain === chain.chain && a.path === normalizedPath);
  }, [chain, existing, normalizedPath]);

  const handleAdd = (): void => {
    if (!chain || !preview || duplicate) return;
    const next: WalletAddress = {
      id: `derived_${Date.now().toString(36)}`,
      label: `${chain.chainLabel} · ${preview.path}`,
      chain: chain.chain,
      chainLabel: chain.chainLabel,
      path: preview.path,
      address: preview.address,
    };
    onAdd(next);
    setAdded(next);
  };

  const body = added ? (
    <AddedView address={added} onClose={onClose} />
  ) : (
    <ConfigureView
      chains={chains}
      chainIdx={chainIdx}
      onPickChain={setChainIdx}
      path={path}
      onChangePath={setPath}
      previewAddress={preview?.address ?? null}
      duplicate={duplicate}
      canAdd={Boolean(chain && !duplicate)}
      onAdd={handleAdd}
      onCancel={onClose}
    />
  );

  return (
    <Modal
      visible={visible}
      transparent
      animationType="fade"
      onRequestClose={onClose}
      accessibilityViewIsModal
    >
      <Pressable style={s.backdrop} accessibilityLabel={T.common.cancel} onPress={onClose} />
      <View style={s.sheetWrap} pointerEvents="box-none">
        <View style={s.sheet}>
          <View style={s.handle} />
          <View style={s.header}>
            <View style={s.headerText}>
              <Text style={s.title}>{added ? T.derive.addedTitle : T.derive.title}</Text>
              <Text style={s.subtitle}>{added ? T.derive.addedSub : T.derive.subtitle}</Text>
            </View>
            <Pressable
              onPress={onClose}
              accessibilityRole="button"
              accessibilityLabel={T.common.cancel}
              style={({ pressed }) => [s.closeBtn, pressed ? s.pressed : null]}
            >
              <Icon name="close" size={14} color={t.text2} />
            </Pressable>
          </View>
          {body}
        </View>
      </View>
    </Modal>
  );
}

interface ConfigureViewProps {
  readonly chains: ReadonlyArray<ChainOption>;
  readonly chainIdx: number;
  readonly onPickChain: (idx: number) => void;
  readonly path: string;
  readonly onChangePath: (next: string) => void;
  readonly previewAddress: string | null;
  readonly duplicate: boolean;
  readonly canAdd: boolean;
  readonly onAdd: () => void;
  readonly onCancel: () => void;
}

function ConfigureView({
  chains,
  chainIdx,
  onPickChain,
  path,
  onChangePath,
  previewAddress,
  duplicate,
  canAdd,
  onAdd,
  onCancel,
}: ConfigureViewProps): React.JSX.Element {
  const t = useTheme();
  const { t: T } = useI18n();
  const s = useMemo(() => makeStyles(t), [t]);
  const chain = chains[chainIdx];
  return (
    <View style={s.section}>
      <Text style={s.sectionHeading}>{T.derive.chainSection}</Text>
      <View style={s.chainRow}>
        {chains.map((c, i) => {
          const on = i === chainIdx;
          return (
            <Pressable
              key={c.chain}
              onPress={() => onPickChain(i)}
              accessibilityRole="button"
              accessibilityState={{ selected: on }}
              accessibilityLabel={c.chainLabel}
              style={({ pressed }) => [
                s.chainPick,
                on ? s.chainPickOn : null,
                pressed ? s.pressed : null,
              ]}
            >
              <ChainBadge chain={c.chain} label={c.chainLabel} />
            </Pressable>
          );
        })}
      </View>

      <Text style={s.sectionHeading}>{T.derive.pathSection}</Text>
      <View style={s.pathBox}>
        <TextInput
          value={path}
          onChangeText={onChangePath}
          placeholder="m/0"
          placeholderTextColor={t.text3}
          autoCapitalize="none"
          autoCorrect={false}
          spellCheck={false}
          accessibilityLabel={T.derive.pathSection}
          style={s.pathInput}
        />
      </View>
      <Text style={s.pathSub}>{T.derive.pathSub}</Text>

      <Text style={s.sectionHeading}>{T.derive.previewSection}</Text>
      <View style={s.previewCard}>
        {chain && previewAddress ? (
          <>
            <View style={s.previewHead}>
              <ChainBadge chain={chain.chain} label={chain.chainLabel} />
              <Text style={s.previewPath}>{normalizePath(path)}</Text>
            </View>
            <Text style={s.previewAddress} numberOfLines={2} ellipsizeMode="middle">
              {previewAddress}
            </Text>
            <Text style={s.previewSub}>{T.derive.previewSub}</Text>
            <View style={s.demoNote}>
              <Icon name="info" size={11} color={t.text3} />
              <Text style={s.demoNoteText}>{T.derive.demoNote}</Text>
            </View>
          </>
        ) : (
          <Text style={s.previewPlaceholder}>{T.derive.previewPlaceholder}</Text>
        )}
      </View>

      {duplicate ? (
        <View style={s.duplicate} accessibilityRole="alert">
          <Icon name="warn" size={12} color={t.warn} />
          <Text style={s.duplicateText}>{T.derive.duplicateNote}</Text>
        </View>
      ) : null}

      <View style={s.footer}>
        <GhostButton label={T.common.cancel} onPress={onCancel} style={s.flex1} />
        <PrimaryButton
          label={T.derive.addAction}
          onPress={onAdd}
          disabled={!canAdd}
          style={s.flex12}
        />
      </View>
    </View>
  );
}

interface AddedViewProps {
  readonly address: WalletAddress;
  readonly onClose: () => void;
}

function AddedView({ address, onClose }: AddedViewProps): React.JSX.Element {
  const t = useTheme();
  const { t: T } = useI18n();
  const s = useMemo(() => makeStyles(t), [t]);
  return (
    <View style={s.section}>
      <View style={s.addedCard}>
        <View style={s.addedIcon}>
          <Icon name="check" size={18} color={t.ok} />
        </View>
        <View style={s.addedBody}>
          <View style={s.addedRow}>
            <ChainBadge chain={address.chain} label={address.chainLabel} />
            <Text style={s.addedPath}>{address.path}</Text>
          </View>
          <Text style={s.previewAddress} numberOfLines={2} ellipsizeMode="middle">
            {address.address}
          </Text>
        </View>
      </View>
      <View style={s.footer}>
        <PrimaryButton label={T.derive.closeAction} onPress={onClose} style={s.flex1} />
      </View>
    </View>
  );
}

function makeStyles(t: ThemeTokens) {
  return StyleSheet.create({
    backdrop: { ...StyleSheet.absoluteFillObject, backgroundColor: 'rgba(0,0,0,0.55)' },
    sheetWrap: { flex: 1, justifyContent: 'flex-end' },
    sheet: {
      backgroundColor: t.surface,
      borderTopLeftRadius: 22,
      borderTopRightRadius: 22,
      paddingHorizontal: spacing.lg,
      paddingTop: spacing.sm,
      paddingBottom: spacing.xl,
      gap: spacing.sm,
      borderTopColor: t.hairline,
      borderTopWidth: StyleSheet.hairlineWidth,
    },
    handle: {
      alignSelf: 'center',
      width: 40,
      height: 4,
      borderRadius: 2,
      backgroundColor: t.surface2,
      marginBottom: spacing.sm,
    },
    header: { flexDirection: 'row', alignItems: 'flex-start', gap: spacing.sm },
    headerText: { flex: 1, gap: 4 },
    title: { color: t.text, fontSize: 17, fontWeight: '700' },
    subtitle: { color: t.text3, fontSize: 12, lineHeight: 17 },
    closeBtn: {
      width: 32,
      height: 32,
      borderRadius: 16,
      backgroundColor: t.surface2,
      borderColor: t.hairline,
      borderWidth: StyleSheet.hairlineWidth,
      alignItems: 'center',
      justifyContent: 'center',
    },
    pressed: { opacity: 0.7 },
    section: { gap: spacing.sm },
    sectionHeading: {
      color: t.text2,
      fontSize: 11,
      fontWeight: '700',
      letterSpacing: 0.5,
      textTransform: 'uppercase',
      marginTop: spacing.sm,
      paddingHorizontal: 2,
    },
    chainRow: { flexDirection: 'row', gap: 8, flexWrap: 'wrap' },
    chainPick: {
      paddingVertical: 8,
      paddingHorizontal: 10,
      borderRadius: 12,
      backgroundColor: t.surface2,
      borderColor: t.hairline,
      borderWidth: StyleSheet.hairlineWidth,
    },
    chainPickOn: { borderColor: `${t.accent}66`, backgroundColor: `${t.accent}1a` },
    pathBox: {
      backgroundColor: t.surface2,
      borderRadius: radius.lg,
      borderColor: t.hairline,
      borderWidth: StyleSheet.hairlineWidth,
      paddingHorizontal: spacing.md,
      paddingVertical: 4,
    },
    pathInput: {
      color: t.text,
      fontSize: 14,
      fontFamily: fontFamily.mono,
      paddingVertical: 10,
    },
    pathSub: { color: t.text3, fontSize: 11, marginTop: -2, paddingHorizontal: 2 },
    previewCard: {
      padding: spacing.md,
      borderRadius: radius.xl,
      backgroundColor: t.surface2,
      borderColor: t.hairline,
      borderWidth: StyleSheet.hairlineWidth,
      gap: spacing.sm,
    },
    previewHead: { flexDirection: 'row', alignItems: 'center', gap: 8 },
    previewPath: { color: t.text3, fontSize: 11, fontFamily: fontFamily.mono },
    previewAddress: { color: t.text, fontSize: 12.5, fontFamily: fontFamily.mono, lineHeight: 18 },
    previewSub: { color: t.text3, fontSize: 10.5 },
    previewPlaceholder: { color: t.text3, fontSize: 12, fontStyle: 'italic' },
    demoNote: { flexDirection: 'row', alignItems: 'center', gap: 6, marginTop: 2 },
    demoNoteText: { color: t.text3, fontSize: 10.5 },
    duplicate: {
      flexDirection: 'row',
      alignItems: 'center',
      gap: 8,
      paddingHorizontal: spacing.md,
      paddingVertical: 8,
      borderRadius: radius.lg,
      backgroundColor: `${t.warn}1a`,
      borderColor: `${t.warn}55`,
      borderWidth: StyleSheet.hairlineWidth,
    },
    duplicateText: { color: t.warn, fontSize: 11.5, fontWeight: '600' },
    footer: { flexDirection: 'row', gap: spacing.sm, marginTop: spacing.sm },
    flex1: { flex: 1 },
    flex12: { flex: 1.2 },
    addedCard: {
      flexDirection: 'row',
      gap: spacing.sm,
      padding: spacing.md,
      borderRadius: radius.xl,
      backgroundColor: `${t.ok}10`,
      borderColor: `${t.ok}55`,
      borderWidth: StyleSheet.hairlineWidth,
      marginTop: spacing.sm,
    },
    addedIcon: {
      width: 36,
      height: 36,
      borderRadius: 11,
      backgroundColor: `${t.ok}1a`,
      borderColor: `${t.ok}55`,
      borderWidth: StyleSheet.hairlineWidth,
      alignItems: 'center',
      justifyContent: 'center',
    },
    addedBody: { flex: 1, minWidth: 0, gap: 6 },
    addedRow: { flexDirection: 'row', alignItems: 'center', gap: 8 },
    addedPath: { color: t.text3, fontSize: 11, fontFamily: fontFamily.mono },
  });
}
