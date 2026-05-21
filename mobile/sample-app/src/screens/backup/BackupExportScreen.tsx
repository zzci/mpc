// Multi-stage Trine-styled flow for exporting a passphrase-wrapped share
// backup. Stages: pick share → set passphrase → confirm security facts →
// progress → result. The confirm step drives sdk.ts exportShare via
// bridgeCall.ts, falling back to a demo response when the native body is a
// stub (B-005). The visible chrome surfaces every security fact called out in
// the design handoff: Argon2id-wrapped KEK, no plaintext share ever leaves
// the keystore, device-bound identity, target group context.

import React, { useEffect, useMemo, useState } from 'react';
import {
  Pressable,
  StyleSheet,
  Text,
  TextInput,
  View,
} from 'react-native';
import {
  Card,
  GhostButton,
  Hairline,
  Icon,
  PrimaryButton,
  Screen,
  SectionLabel,
  TopBar,
  fontFamily,
  radius,
  spacing,
  useTheme,
} from '../../ui';
import type { ThemeTokens } from '../../ui';
import { useI18n } from '../../i18n';
import type { Strings } from '../../i18n';
import { COORD, MEMBER, WALLETS } from '../../data';
import type { Wallet } from '../../data';
import {
  BridgeCallError,
  callExportShare,
} from './bridgeCall';
import type { BridgeMode } from './bridgeCall';

type Stage =
  | { readonly kind: 'select' }
  | { readonly kind: 'passphrase' }
  | { readonly kind: 'confirm' }
  | { readonly kind: 'progress' }
  | { readonly kind: 'done'; readonly blobBase64: string; readonly mode: BridgeMode }
  | { readonly kind: 'error'; readonly errorKind: ErrorKind };

type ErrorKind = 'passphrase' | 'blob' | 'generic';

export interface BackupExportScreenProps {
  readonly onClose: () => void;
}

export function BackupExportScreen({ onClose }: BackupExportScreenProps): React.JSX.Element {
  const theme = useTheme();
  const { t: T } = useI18n();
  const s = useMemo(() => makeStyles(theme), [theme]);
  const [stage, setStage] = useState<Stage>({ kind: 'select' });
  const [walletIdx, setWalletIdx] = useState(0);
  const [passphrase, setPassphrase] = useState('');
  const [repeat, setRepeat] = useState('');
  const wallet = WALLETS[walletIdx] ?? WALLETS[0];

  // Drive the bridge call as soon as the progress stage is entered.
  useEffect(() => {
    if (stage.kind !== 'progress') return;
    let cancelled = false;
    const exec = async (): Promise<void> => {
      try {
        const outcome = await callExportShare(wallet.moniker, passphrase);
        if (cancelled) return;
        setStage({ kind: 'done', blobBase64: outcome.blobBase64, mode: outcome.mode });
      } catch (err: unknown) {
        if (cancelled) return;
        setStage({ kind: 'error', errorKind: errorKindOf(err) });
      }
    };
    void exec();
    return () => {
      cancelled = true;
    };
  }, [stage, wallet.moniker, passphrase]);

  const stageIndex = stageIdxFor(stage.kind);

  return (
    <Screen>
      <TopBar
        title={T.backup.exportTitle}
        onBack={onClose}
        backLabel={T.common.back}
      />
      <View style={s.body}>
        <Text style={s.intro}>{T.backup.exportIntro}</Text>
        <StageDots active={stageIndex} total={STAGE_COUNT} theme={theme} />

        {stage.kind === 'select' ? (
          <SelectShareStage
            walletIdx={walletIdx}
            onPick={setWalletIdx}
            onNext={() => setStage({ kind: 'passphrase' })}
            onCancel={onClose}
          />
        ) : null}

        {stage.kind === 'passphrase' ? (
          <PassphraseStage
            mode="export"
            passphrase={passphrase}
            onChangePassphrase={setPassphrase}
            repeat={repeat}
            onChangeRepeat={setRepeat}
            onBack={() => setStage({ kind: 'select' })}
            onNext={() => setStage({ kind: 'confirm' })}
          />
        ) : null}

        {stage.kind === 'confirm' ? (
          <ConfirmStage
            wallet={wallet}
            onBack={() => setStage({ kind: 'passphrase' })}
            onNext={() => setStage({ kind: 'progress' })}
          />
        ) : null}

        {stage.kind === 'progress' ? <ProgressStage mode="export" /> : null}

        {stage.kind === 'done' ? (
          <DoneStage
            wallet={wallet}
            blobBase64={stage.blobBase64}
            mode={stage.mode}
            onClose={onClose}
          />
        ) : null}

        {stage.kind === 'error' ? (
          <ErrorStage
            errorKind={stage.errorKind}
            onRetry={() => setStage({ kind: 'confirm' })}
            onCancel={onClose}
          />
        ) : null}
      </View>
    </Screen>
  );
}

const STAGE_COUNT = 5;

function stageIdxFor(kind: Stage['kind']): number {
  switch (kind) {
    case 'select':
      return 0;
    case 'passphrase':
      return 1;
    case 'confirm':
      return 2;
    case 'progress':
      return 3;
    case 'done':
    case 'error':
      return 4;
  }
}

function errorKindOf(err: unknown): ErrorKind {
  if (err instanceof BridgeCallError) {
    if (err.kind === 'badPassphrase') return 'passphrase';
    return 'blob';
  }
  return 'generic';
}

// ── Stage 1 ────────────────────────────────────────────────────────────────

interface SelectShareStageProps {
  readonly walletIdx: number;
  readonly onPick: (idx: number) => void;
  readonly onNext: () => void;
  readonly onCancel: () => void;
}

function SelectShareStage({ walletIdx, onPick, onNext, onCancel }: SelectShareStageProps): React.JSX.Element {
  const theme = useTheme();
  const { t: T, tx } = useI18n();
  const s = useMemo(() => makeStyles(theme), [theme]);
  return (
    <View style={s.stageBody}>
      <SectionLabel>{T.backup.stepSelect}</SectionLabel>
      <Text style={s.sectionSub}>{T.backup.stepSelectSub}</Text>
      <Card style={s.tightCard}>
        {WALLETS.map((w, i) => {
          const on = i === walletIdx;
          return (
            <View key={w.groupId}>
              <Pressable
                accessibilityRole="button"
                accessibilityState={{ selected: on }}
                accessibilityLabel={w.moniker}
                onPress={() => onPick(i)}
                style={({ pressed }) => [s.shareRow, pressed ? s.pressed : null]}
              >
                <View style={[s.radio, on ? s.radioOn : null]}>
                  {on ? <View style={s.radioDot} /> : null}
                </View>
                <View style={s.shareText}>
                  <Text style={s.shareMoniker}>{w.moniker}</Text>
                  <Text style={s.shareSub}>
                    {tx(T.backup.walletMembers, {
                      threshold: w.threshold,
                      parties: w.parties,
                      members: w.members.length,
                    })}
                  </Text>
                  <Text style={s.shareGroupId}>{w.groupId}</Text>
                </View>
              </Pressable>
              {i < WALLETS.length - 1 ? <Hairline /> : null}
            </View>
          );
        })}
      </Card>

      <View style={s.footerRow}>
        <GhostButton label={T.common.cancel} onPress={onCancel} style={s.flex1} />
        <PrimaryButton
          label={T.backup.ctaContinue}
          onPress={onNext}
          style={s.flex12}
        />
      </View>
    </View>
  );
}

// ── Stage 2 ────────────────────────────────────────────────────────────────

interface PassphraseStageProps {
  readonly mode: 'export' | 'import';
  readonly passphrase: string;
  readonly onChangePassphrase: (next: string) => void;
  readonly repeat: string;
  readonly onChangeRepeat: (next: string) => void;
  readonly onBack: () => void;
  readonly onNext: () => void;
}

export function PassphraseStage({
  mode,
  passphrase,
  onChangePassphrase,
  repeat,
  onChangeRepeat,
  onBack,
  onNext,
}: PassphraseStageProps): React.JSX.Element {
  const theme = useTheme();
  const { t: T } = useI18n();
  const s = useMemo(() => makeStyles(theme), [theme]);
  const tooShort = passphrase.length > 0 && passphrase.length < 8;
  const mismatch = mode === 'export' && repeat.length > 0 && repeat !== passphrase;
  const ready =
    mode === 'export'
      ? passphrase.length >= 8 && repeat === passphrase
      : passphrase.length >= 8;
  return (
    <View style={s.stageBody}>
      <SectionLabel>{T.backup.stepPassphrase}</SectionLabel>
      <Text style={s.sectionSub}>
        {mode === 'export' ? T.backup.stepPassphraseSub : T.backup.stepImportPassphraseSub}
      </Text>
      <Card style={s.padCard}>
        <Text style={s.fieldLabel}>{T.backup.passphraseLabel}</Text>
        <TextInput
          value={passphrase}
          onChangeText={onChangePassphrase}
          placeholder={T.backup.passphrasePlaceholder}
          placeholderTextColor={theme.text3}
          secureTextEntry
          autoCapitalize="none"
          autoCorrect={false}
          spellCheck={false}
          style={s.textInput}
          accessibilityLabel={T.backup.passphraseLabel}
        />
        {mode === 'export' ? (
          <>
            <View style={s.fieldGap} />
            <Text style={s.fieldLabel}>{T.backup.passphraseRepeat}</Text>
            <TextInput
              value={repeat}
              onChangeText={onChangeRepeat}
              placeholder={T.backup.passphrasePlaceholder}
              placeholderTextColor={theme.text3}
              secureTextEntry
              autoCapitalize="none"
              autoCorrect={false}
              spellCheck={false}
              style={s.textInput}
              accessibilityLabel={T.backup.passphraseRepeat}
            />
          </>
        ) : null}
        {tooShort ? <Text style={s.fieldErr}>{T.backup.passphraseTooShort}</Text> : null}
        {mismatch ? <Text style={s.fieldErr}>{T.backup.passphraseMismatch}</Text> : null}
      </Card>

      <View style={s.footerRow}>
        <GhostButton label={T.common.back} onPress={onBack} style={s.flex1} />
        <PrimaryButton
          label={T.backup.ctaContinue}
          onPress={onNext}
          disabled={!ready}
          style={s.flex12}
        />
      </View>
    </View>
  );
}

// ── Stage 3 ────────────────────────────────────────────────────────────────

interface ConfirmStageProps {
  readonly wallet: Wallet;
  readonly onBack: () => void;
  readonly onNext: () => void;
}

function ConfirmStage({ wallet, onBack, onNext }: ConfirmStageProps): React.JSX.Element {
  const theme = useTheme();
  const { t: T, tx } = useI18n();
  const s = useMemo(() => makeStyles(theme), [theme]);
  return (
    <View style={s.stageBody}>
      <SectionLabel>{T.backup.factsHeading}</SectionLabel>
      <Card style={s.factsCard}>
        <FactRow
          icon="shield"
          title={T.backup.factKek}
          sub={T.backup.factKekSub}
        />
        <Hairline />
        <FactRow
          icon="key"
          title={T.backup.factNoPlaintext}
          sub={T.backup.factNoPlaintextSub}
        />
        <Hairline />
        <FactRow
          icon="info"
          title={T.backup.factDevice}
          sub={tx(T.backup.factDeviceSub, { memberId: MEMBER.memberId, device: MEMBER.device })}
        />
        <Hairline />
        <FactRow
          icon="wallet"
          title={T.backup.factGroup}
          sub={tx(T.backup.factGroupSub, { moniker: wallet.moniker, groupId: wallet.groupId })}
        />
      </Card>

      <SectionLabel>{T.backup.callShape}</SectionLabel>
      <Card style={s.callCard}>
        <Text style={s.callShape}>{T.backup.callShapeExport}</Text>
        <Text style={s.callRelay}>
          relay {COORD.relayPeerID} · coord {COORD.tls}
        </Text>
      </Card>

      <View style={s.footerRow}>
        <GhostButton label={T.common.back} onPress={onBack} style={s.flex1} />
        <PrimaryButton
          label={T.backup.ctaExport}
          onPress={onNext}
          style={s.flex12}
        />
      </View>
    </View>
  );
}

// ── Stage 4 ────────────────────────────────────────────────────────────────

interface ProgressStageProps {
  readonly mode: 'export' | 'import';
}

export function ProgressStage({ mode }: ProgressStageProps): React.JSX.Element {
  const theme = useTheme();
  const { t: T } = useI18n();
  const s = useMemo(() => makeStyles(theme), [theme]);
  const steps =
    mode === 'export'
      ? [
          { title: T.backup.progressDeriveKek, sub: T.backup.progressDeriveKekSub },
          { title: T.backup.progressWrap, sub: T.backup.progressWrapSub },
          { title: T.backup.progressEmit, sub: T.backup.progressEmitSub },
        ]
      : [
          { title: T.backup.progressDeriveKek, sub: T.backup.progressDeriveKekSub },
          { title: T.backup.progressUnwrap, sub: T.backup.progressUnwrapSub },
          { title: T.backup.progressInstall, sub: T.backup.progressInstallSub },
        ];
  return (
    <View style={s.stageBody}>
      <SectionLabel>{T.backup.stepProgress}</SectionLabel>
      <Text style={s.sectionSub}>{T.backup.stepProgressSub}</Text>
      <Card style={s.progressCard}>
        {steps.map((step, i) => (
          <View key={step.title}>
            <View style={s.progressRow}>
              <View style={[s.progressDot, i === 0 ? s.progressDotActive : null]}>
                <Text style={s.progressDotText}>{i + 1}</Text>
              </View>
              <View style={s.progressText}>
                <Text style={s.progressTitle}>{step.title}</Text>
                <Text style={s.progressSub}>{step.sub}</Text>
              </View>
              <View style={s.progressBadge}>
                <Text style={s.progressBadgeText}>
                  {i === 0 ? T.backup.progressActive : T.backup.progressWait}
                </Text>
              </View>
            </View>
            {i < steps.length - 1 ? <Hairline /> : null}
          </View>
        ))}
      </Card>
      <Text style={s.intro}>{T.common.pleaseWait}</Text>
    </View>
  );
}

// ── Stage 5 (Export done) ──────────────────────────────────────────────────

interface DoneStageProps {
  readonly wallet: Wallet;
  readonly blobBase64: string;
  readonly mode: BridgeMode;
  readonly onClose: () => void;
}

function DoneStage({ wallet, blobBase64, mode, onClose }: DoneStageProps): React.JSX.Element {
  const theme = useTheme();
  const { t: T } = useI18n();
  const s = useMemo(() => makeStyles(theme), [theme]);
  const [copied, setCopied] = useState(false);
  const blobSize = `${blobBase64.length}b`;
  return (
    <View style={s.stageBody}>
      <View style={s.doneHero}>
        <View style={s.doneIcon}>
          <Icon name="check" size={20} color={theme.ok} />
        </View>
        <View style={s.doneText}>
          <Text style={s.doneTitle}>{T.backup.resultExportTitle}</Text>
          <Text style={s.doneSub}>{T.backup.resultExportSub}</Text>
        </View>
      </View>

      <View style={modeBannerStyle(mode, theme)} accessibilityRole="text">
        <Icon name="info" size={12} color={modeBannerColor(mode, theme)} />
        <Text style={[s.bannerText, { color: modeBannerColor(mode, theme) }]}>
          {mode === 'live' ? T.backup.liveBanner : T.backup.demoBanner}
        </Text>
      </View>

      <SectionLabel>{T.backup.artifactHeading}</SectionLabel>
      <Card style={s.padCard}>
        <Text style={s.artifactSub}>{T.backup.artifactSub}</Text>
        <View style={s.artifactBox}>
          <Text style={s.artifactBlob} selectable numberOfLines={6} ellipsizeMode="tail">
            {blobBase64}
          </Text>
        </View>
        <View style={s.previewGrid}>
          <PreviewRow label={T.backup.previewMoniker} value={wallet.moniker} />
          <Hairline />
          <PreviewRow label={T.backup.previewSize} value={blobSize} />
          <Hairline />
          <PreviewRow label={T.backup.previewWrap} value={T.backup.previewWrapValue} />
          <Hairline />
          <PreviewRow label={T.backup.previewCreated} value={T.common.justNow} />
        </View>
        <View style={s.artifactCopyRow}>
          <GhostButton
            label={copied ? T.backup.artifactCopied : T.backup.artifactCopy}
            icon="copy"
            onPress={() => setCopied(true)}
            style={s.flex1}
          />
        </View>
      </Card>

      <View style={s.footerRow}>
        <PrimaryButton
          label={T.backup.ctaCloseExport}
          onPress={onClose}
          style={s.flex1}
        />
      </View>
    </View>
  );
}

// ── Error stage (shared with import) ───────────────────────────────────────

interface ErrorStageProps {
  readonly errorKind: ErrorKind;
  readonly onRetry: () => void;
  readonly onCancel: () => void;
}

export function ErrorStage({ errorKind, onRetry, onCancel }: ErrorStageProps): React.JSX.Element {
  const theme = useTheme();
  const { t: T } = useI18n();
  const s = useMemo(() => makeStyles(theme), [theme]);
  const message = errorMessage(errorKind, T);
  return (
    <View style={s.stageBody}>
      <View style={s.errorHero}>
        <View style={s.errorIcon}>
          <Icon name="warn" size={18} color={theme.danger} />
        </View>
        <View style={s.doneText}>
          <Text style={s.doneTitle}>{T.backup.errorTitle}</Text>
          <Text style={s.doneSub}>{message}</Text>
        </View>
      </View>
      <View style={s.footerRow}>
        <GhostButton label={T.common.cancel} onPress={onCancel} style={s.flex1} />
        <PrimaryButton label={T.backup.ctaRetry} onPress={onRetry} style={s.flex12} />
      </View>
    </View>
  );
}

function errorMessage(kind: ErrorKind, T: Strings): string {
  if (kind === 'passphrase') return T.backup.errorPassphrase;
  if (kind === 'blob') return T.backup.errorBlob;
  return T.backup.errorGeneric;
}

// ── Helpers ────────────────────────────────────────────────────────────────

interface FactRowProps {
  readonly icon: 'shield' | 'key' | 'info' | 'wallet';
  readonly title: string;
  readonly sub: string;
}

export function FactRow({ icon, title, sub }: FactRowProps): React.JSX.Element {
  const theme = useTheme();
  const s = useMemo(() => makeStyles(theme), [theme]);
  return (
    <View style={s.factRow}>
      <View style={s.factIcon}>
        <Icon name={icon} size={14} color={theme.accent} />
      </View>
      <View style={s.factText}>
        <Text style={s.factTitle}>{title}</Text>
        <Text style={s.factSub}>{sub}</Text>
      </View>
    </View>
  );
}

interface PreviewRowProps {
  readonly label: string;
  readonly value: string;
}

export function PreviewRow({ label, value }: PreviewRowProps): React.JSX.Element {
  const theme = useTheme();
  const s = useMemo(() => makeStyles(theme), [theme]);
  return (
    <View style={s.previewRow}>
      <Text style={s.previewLabel}>{label}</Text>
      <Text style={s.previewValue} numberOfLines={1} ellipsizeMode="middle">
        {value}
      </Text>
    </View>
  );
}

interface StageDotsProps {
  readonly active: number;
  readonly total: number;
  readonly theme: ThemeTokens;
}

function StageDots({ active, total, theme }: StageDotsProps): React.JSX.Element {
  const dots = Array.from({ length: total }, (_v, i) => i);
  return (
    <View style={dotsStyle.row}>
      {dots.map((i) => (
        <View
          key={i}
          style={[
            dotsStyle.dot,
            { backgroundColor: i <= active ? theme.accent : theme.surface2 },
            i === active ? dotsStyle.dotActive : null,
          ]}
        />
      ))}
    </View>
  );
}

const dotsStyle = StyleSheet.create({
  row: { flexDirection: 'row', gap: 6, marginVertical: 12, paddingHorizontal: 2 },
  dot: { width: 22, height: 4, borderRadius: 2 },
  dotActive: { width: 36 },
});

function modeBannerColor(mode: BridgeMode, theme: ThemeTokens): string {
  return mode === 'live' ? theme.accent : theme.warn;
}

function modeBannerStyle(mode: BridgeMode, theme: ThemeTokens) {
  return {
    flexDirection: 'row' as const,
    alignItems: 'center' as const,
    gap: 8,
    paddingHorizontal: spacing.md,
    paddingVertical: 8,
    borderRadius: radius.md,
    marginVertical: spacing.sm,
    backgroundColor: `${modeBannerColor(mode, theme)}1a`,
    borderColor: `${modeBannerColor(mode, theme)}55`,
    borderWidth: StyleSheet.hairlineWidth,
  };
}

function makeStyles(t: ThemeTokens) {
  return StyleSheet.create({
    body: { paddingHorizontal: spacing.lg, paddingBottom: spacing.lg },
    intro: { color: t.text2, fontSize: 12.5, lineHeight: 18 },
    stageBody: { gap: spacing.sm, marginTop: spacing.sm },
    tightCard: { padding: 0, paddingHorizontal: spacing.md },
    padCard: { padding: spacing.md, gap: spacing.sm },
    factsCard: { padding: 0, paddingHorizontal: spacing.md },
    callCard: { padding: spacing.md, gap: 4 },
    callShape: { color: t.text, fontSize: 12, fontFamily: fontFamily.mono },
    callRelay: { color: t.text3, fontSize: 11 },
    sectionSub: { color: t.text3, fontSize: 11.5, lineHeight: 16, paddingHorizontal: 2 },
    shareRow: { flexDirection: 'row', alignItems: 'center', gap: spacing.sm, paddingVertical: 12 },
    pressed: { opacity: 0.7 },
    radio: {
      width: 18,
      height: 18,
      borderRadius: 9,
      borderColor: t.hairline,
      borderWidth: StyleSheet.hairlineWidth,
      alignItems: 'center',
      justifyContent: 'center',
      backgroundColor: t.surface2,
    },
    radioOn: { borderColor: `${t.accent}88`, backgroundColor: `${t.accent}1a` },
    radioDot: { width: 8, height: 8, borderRadius: 4, backgroundColor: t.accent },
    shareText: { flex: 1, minWidth: 0, gap: 2 },
    shareMoniker: { color: t.text, fontSize: 14, fontWeight: '700' },
    shareSub: { color: t.text2, fontSize: 11.5 },
    shareGroupId: { color: t.text3, fontSize: 10.5, fontFamily: fontFamily.mono },
    fieldLabel: { color: t.text2, fontSize: 11.5, letterSpacing: 0.4, fontWeight: '600' },
    fieldGap: { height: spacing.sm },
    textInput: {
      color: t.text,
      fontSize: 14,
      paddingHorizontal: 12,
      paddingVertical: 10,
      borderRadius: radius.lg,
      backgroundColor: t.surface2,
      borderColor: t.hairline,
      borderWidth: StyleSheet.hairlineWidth,
      marginTop: 6,
    },
    fieldErr: { color: t.danger, fontSize: 11.5, marginTop: spacing.xs },
    factRow: { flexDirection: 'row', alignItems: 'flex-start', gap: spacing.sm, paddingVertical: 12 },
    factIcon: {
      width: 32,
      height: 32,
      borderRadius: 10,
      backgroundColor: `${t.accent}14`,
      borderColor: `${t.accent}33`,
      borderWidth: StyleSheet.hairlineWidth,
      alignItems: 'center',
      justifyContent: 'center',
    },
    factText: { flex: 1, minWidth: 0, gap: 3 },
    factTitle: { color: t.text, fontSize: 13.5, fontWeight: '700' },
    factSub: { color: t.text3, fontSize: 11.5, lineHeight: 16 },
    progressCard: { padding: 0, paddingHorizontal: spacing.md },
    progressRow: { flexDirection: 'row', alignItems: 'center', gap: spacing.sm, paddingVertical: 12 },
    progressDot: {
      width: 26,
      height: 26,
      borderRadius: 13,
      backgroundColor: t.surface2,
      borderColor: t.hairline,
      borderWidth: StyleSheet.hairlineWidth,
      alignItems: 'center',
      justifyContent: 'center',
    },
    progressDotActive: { backgroundColor: `${t.accent}1a`, borderColor: `${t.accent}66` },
    progressDotText: {
      color: t.text2,
      fontSize: 11,
      fontWeight: '700',
      fontFamily: fontFamily.mono,
    },
    progressText: { flex: 1, minWidth: 0, gap: 2 },
    progressTitle: { color: t.text, fontSize: 13, fontWeight: '600' },
    progressSub: { color: t.text3, fontSize: 11 },
    progressBadge: {
      paddingHorizontal: 8,
      paddingVertical: 3,
      borderRadius: radius.pill,
      backgroundColor: t.surface2,
      borderColor: t.hairline,
      borderWidth: StyleSheet.hairlineWidth,
    },
    progressBadgeText: { color: t.text2, fontSize: 10, fontWeight: '700', letterSpacing: 0.4 },
    doneHero: {
      flexDirection: 'row',
      alignItems: 'center',
      gap: spacing.sm,
      padding: spacing.md,
      borderRadius: radius.xl,
      backgroundColor: `${t.ok}10`,
      borderColor: `${t.ok}55`,
      borderWidth: StyleSheet.hairlineWidth,
    },
    doneIcon: {
      width: 38,
      height: 38,
      borderRadius: 12,
      backgroundColor: `${t.ok}1a`,
      borderColor: `${t.ok}55`,
      borderWidth: StyleSheet.hairlineWidth,
      alignItems: 'center',
      justifyContent: 'center',
    },
    doneText: { flex: 1, gap: 3 },
    doneTitle: { color: t.text, fontSize: 15, fontWeight: '700' },
    doneSub: { color: t.text2, fontSize: 12, lineHeight: 17 },
    bannerText: { fontSize: 11.5, fontWeight: '600' },
    artifactSub: { color: t.text3, fontSize: 11 },
    artifactBox: {
      padding: spacing.md,
      borderRadius: radius.lg,
      backgroundColor: t.surface2,
      borderColor: t.hairline,
      borderWidth: StyleSheet.hairlineWidth,
    },
    artifactBlob: {
      color: t.text,
      fontSize: 11.5,
      fontFamily: fontFamily.mono,
      lineHeight: 17,
    },
    previewGrid: { paddingHorizontal: 2 },
    previewRow: { flexDirection: 'row', alignItems: 'center', paddingVertical: 10, gap: spacing.md },
    previewLabel: { color: t.text3, fontSize: 11.5, flex: 1 },
    previewValue: { color: t.text, fontSize: 12, fontFamily: fontFamily.mono, textAlign: 'right', maxWidth: 200 },
    artifactCopyRow: { flexDirection: 'row', marginTop: spacing.xs },
    errorHero: {
      flexDirection: 'row',
      alignItems: 'center',
      gap: spacing.sm,
      padding: spacing.md,
      borderRadius: radius.xl,
      backgroundColor: `${t.danger}10`,
      borderColor: `${t.danger}55`,
      borderWidth: StyleSheet.hairlineWidth,
    },
    errorIcon: {
      width: 38,
      height: 38,
      borderRadius: 12,
      backgroundColor: `${t.danger}1a`,
      borderColor: `${t.danger}55`,
      borderWidth: StyleSheet.hairlineWidth,
      alignItems: 'center',
      justifyContent: 'center',
    },
    footerRow: { flexDirection: 'row', gap: spacing.sm, marginTop: spacing.md },
    flex1: { flex: 1 },
    flex12: { flex: 1.2 },
  });
}
