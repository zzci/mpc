// Multi-stage Trine-styled flow for restoring a share from a passphrase-
// wrapped backup blob. Stages: paste/select blob → enter passphrase → preview
// metadata → confirm → progress → result. The confirm step drives sdk.ts
// importShare via bridgeCall.ts, falling back to a structurally-equivalent
// demo response when the native body is a stub (B-005). The flow surfaces
// the same security facts as the export side and reports a passphrase- or
// blob-shape error if the bridge call rejects the input.

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
import { COORD, MEMBER } from '../../data';
import {
  BridgeCallError,
  callImportShare,
} from './bridgeCall';
import type { BridgeMode } from './bridgeCall';
import {
  ErrorStage,
  FactRow,
  PassphraseStage,
  PreviewRow,
  ProgressStage,
} from './BackupExportScreen';

type Stage =
  | { readonly kind: 'paste' }
  | { readonly kind: 'passphrase' }
  | { readonly kind: 'preview' }
  | { readonly kind: 'progress' }
  | { readonly kind: 'done'; readonly moniker: string; readonly mode: BridgeMode }
  | { readonly kind: 'error'; readonly errorKind: 'passphrase' | 'blob' | 'generic' };

const SAMPLE_BLOB =
  'bWNwYms6MTpUcmVhc3VyeSBtYWluOmFyZ29uMmlkOjE2fFRyZWFzdXJ5IG1haW4tc2hhcmUtZGVtby1ibG9iLXBheWxvYWQtYmFzZTY0LWVuY29kZWQtc2FtcGxl';

export interface BackupImportScreenProps {
  readonly onClose: () => void;
}

export function BackupImportScreen({ onClose }: BackupImportScreenProps): React.JSX.Element {
  const theme = useTheme();
  const { t: T } = useI18n();
  const s = useMemo(() => makeStyles(theme), [theme]);
  const [stage, setStage] = useState<Stage>({ kind: 'paste' });
  const [blob, setBlob] = useState('');
  const [passphrase, setPassphrase] = useState('');

  useEffect(() => {
    if (stage.kind !== 'progress') return;
    let cancelled = false;
    const exec = async (): Promise<void> => {
      try {
        const outcome = await callImportShare(blob, passphrase);
        if (cancelled) return;
        setStage({ kind: 'done', moniker: outcome.moniker, mode: outcome.mode });
      } catch (err: unknown) {
        if (cancelled) return;
        if (err instanceof BridgeCallError) {
          if (err.kind === 'badPassphrase') {
            setStage({ kind: 'error', errorKind: 'passphrase' });
          } else {
            setStage({ kind: 'error', errorKind: 'blob' });
          }
          return;
        }
        setStage({ kind: 'error', errorKind: 'generic' });
      }
    };
    void exec();
    return () => {
      cancelled = true;
    };
  }, [stage, blob, passphrase]);

  return (
    <Screen>
      <TopBar
        title={T.backup.importTitle}
        onBack={onClose}
        backLabel={T.common.back}
      />
      <View style={s.body}>
        <Text style={s.intro}>{T.backup.importIntro}</Text>

        {stage.kind === 'paste' ? (
          <PasteBlobStage
            blob={blob}
            onChangeBlob={setBlob}
            onNext={() => setStage({ kind: 'passphrase' })}
            onCancel={onClose}
            onFill={() => setBlob(SAMPLE_BLOB)}
          />
        ) : null}

        {stage.kind === 'passphrase' ? (
          <PassphraseStage
            mode="import"
            passphrase={passphrase}
            onChangePassphrase={setPassphrase}
            repeat=""
            onChangeRepeat={() => undefined}
            onBack={() => setStage({ kind: 'paste' })}
            onNext={() => setStage({ kind: 'preview' })}
          />
        ) : null}

        {stage.kind === 'preview' ? (
          <PreviewStage
            blob={blob}
            onBack={() => setStage({ kind: 'passphrase' })}
            onNext={() => setStage({ kind: 'progress' })}
          />
        ) : null}

        {stage.kind === 'progress' ? <ProgressStage mode="import" /> : null}

        {stage.kind === 'done' ? (
          <DoneStage
            moniker={stage.moniker}
            mode={stage.mode}
            onClose={onClose}
          />
        ) : null}

        {stage.kind === 'error' ? (
          <ErrorStage
            errorKind={stage.errorKind}
            onRetry={() => setStage(stage.errorKind === 'passphrase' ? { kind: 'passphrase' } : { kind: 'paste' })}
            onCancel={onClose}
          />
        ) : null}
      </View>
    </Screen>
  );
}

// ── Stage 1 ────────────────────────────────────────────────────────────────

interface PasteBlobStageProps {
  readonly blob: string;
  readonly onChangeBlob: (next: string) => void;
  readonly onNext: () => void;
  readonly onCancel: () => void;
  readonly onFill: () => void;
}

function PasteBlobStage({ blob, onChangeBlob, onNext, onCancel, onFill }: PasteBlobStageProps): React.JSX.Element {
  const theme = useTheme();
  const { t: T } = useI18n();
  const s = useMemo(() => makeStyles(theme), [theme]);
  const ready = blob.trim().length > 16;
  return (
    <View style={s.stageBody}>
      <SectionLabel>{T.backup.stepPaste}</SectionLabel>
      <Text style={s.sectionSub}>{T.backup.stepPasteSub}</Text>
      <Card style={s.padCard}>
        <Text style={s.fieldLabel}>{T.backup.blobInputLabel}</Text>
        <TextInput
          value={blob}
          onChangeText={onChangeBlob}
          placeholder={T.backup.blobInputPlaceholder}
          placeholderTextColor={theme.text3}
          autoCapitalize="none"
          autoCorrect={false}
          spellCheck={false}
          multiline
          textAlignVertical="top"
          style={s.blobInput}
          accessibilityLabel={T.backup.blobInputLabel}
        />
        <View style={s.blobActions}>
          <GhostButton label={T.backup.blobFromClipboard} icon="copy" onPress={onFill} style={s.flex1} />
          <GhostButton label={T.backup.blobFromFile} icon="link" onPress={onFill} style={s.flex1} />
        </View>
      </Card>

      <View style={s.footerRow}>
        <GhostButton label={T.common.cancel} onPress={onCancel} style={s.flex1} />
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

// ── Stage 3 — Preview metadata ─────────────────────────────────────────────

interface PreviewStageProps {
  readonly blob: string;
  readonly onBack: () => void;
  readonly onNext: () => void;
}

function PreviewStage({ blob, onBack, onNext }: PreviewStageProps): React.JSX.Element {
  const theme = useTheme();
  const { t: T, tx } = useI18n();
  const s = useMemo(() => makeStyles(theme), [theme]);
  const blobSize = `${blob.trim().length}b`;
  const previewMoniker = peekMoniker(blob) ?? 'recovered-share';
  return (
    <View style={s.stageBody}>
      <SectionLabel>{T.backup.stepPreview}</SectionLabel>
      <Text style={s.sectionSub}>{T.backup.stepPreviewSub}</Text>

      <Card style={s.padCard}>
        <PreviewRow label={T.backup.previewMoniker} value={previewMoniker} />
        <Hairline />
        <PreviewRow label={T.backup.previewSize} value={blobSize} />
        <Hairline />
        <PreviewRow label={T.backup.previewWrap} value={T.backup.previewWrapValue} />
      </Card>

      <SectionLabel>{T.backup.factsHeading}</SectionLabel>
      <Card style={s.factsCard}>
        <FactRow icon="shield" title={T.backup.factKek} sub={T.backup.factKekSub} />
        <Hairline />
        <FactRow icon="key" title={T.backup.factNoPlaintext} sub={T.backup.factNoPlaintextSub} />
        <Hairline />
        <FactRow
          icon="info"
          title={T.backup.factDevice}
          sub={tx(T.backup.factDeviceSub, { memberId: MEMBER.memberId, device: MEMBER.device })}
        />
      </Card>

      <SectionLabel>{T.backup.callShape}</SectionLabel>
      <Card style={s.callCard}>
        <Text style={s.callShape}>{T.backup.callShapeImport}</Text>
        <Text style={s.callRelay}>
          relay {COORD.relayPeerID} · coord {COORD.tls}
        </Text>
      </Card>

      <View style={s.footerRow}>
        <GhostButton label={T.common.back} onPress={onBack} style={s.flex1} />
        <PrimaryButton
          label={T.backup.ctaImport}
          onPress={onNext}
          style={s.flex12}
        />
      </View>
    </View>
  );
}

// ── Stage 5 — Done ─────────────────────────────────────────────────────────

interface DoneStageProps {
  readonly moniker: string;
  readonly mode: BridgeMode;
  readonly onClose: () => void;
}

function DoneStage({ moniker, mode, onClose }: DoneStageProps): React.JSX.Element {
  const theme = useTheme();
  const { t: T } = useI18n();
  const s = useMemo(() => makeStyles(theme), [theme]);
  return (
    <View style={s.stageBody}>
      <View style={s.doneHero}>
        <View style={s.doneIcon}>
          <Icon name="check" size={20} color={theme.ok} />
        </View>
        <View style={s.doneText}>
          <Text style={s.doneTitle}>{T.backup.resultImportTitle}</Text>
          <Text style={s.doneSub}>{T.backup.resultImportSub}</Text>
        </View>
      </View>

      <View style={modeBannerStyle(mode, theme)}>
        <Icon name="info" size={12} color={modeBannerColor(mode, theme)} />
        <Text style={[s.bannerText, { color: modeBannerColor(mode, theme) }]}>
          {mode === 'live' ? T.backup.liveBanner : T.backup.demoBanner}
        </Text>
      </View>

      <SectionLabel>{T.backup.previewMoniker}</SectionLabel>
      <Card style={s.padCard}>
        <Pressable accessibilityRole="text">
          <Text style={s.monikerOut}>{moniker}</Text>
        </Pressable>
      </Card>

      <View style={s.footerRow}>
        <PrimaryButton label={T.backup.ctaCloseImport} onPress={onClose} style={s.flex1} />
      </View>
    </View>
  );
}

// ── Helpers ────────────────────────────────────────────────────────────────

function peekMoniker(blob: string): string | null {
  if (!blob) return null;
  const trimmed = blob.trim();
  const atob = (globalThis as { atob?: (s: string) => string }).atob;
  if (typeof atob !== 'function') return null;
  try {
    const decoded = atob(trimmed);
    const i = decoded.indexOf('mcpbk:1:');
    if (i !== 0) return null;
    const parts = decoded.split(':');
    return parts[2] ?? null;
  } catch {
    return null;
  }
}

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
    intro: { color: t.text2, fontSize: 12.5, lineHeight: 18, marginTop: spacing.sm },
    stageBody: { gap: spacing.sm, marginTop: spacing.md },
    padCard: { padding: spacing.md, gap: spacing.sm },
    factsCard: { padding: 0, paddingHorizontal: spacing.md },
    callCard: { padding: spacing.md, gap: 4 },
    callShape: { color: t.text, fontSize: 12, fontFamily: fontFamily.mono },
    callRelay: { color: t.text3, fontSize: 11 },
    sectionSub: { color: t.text3, fontSize: 11.5, lineHeight: 16, paddingHorizontal: 2 },
    fieldLabel: { color: t.text2, fontSize: 11.5, letterSpacing: 0.4, fontWeight: '600' },
    blobInput: {
      color: t.text,
      fontSize: 12,
      fontFamily: fontFamily.mono,
      paddingHorizontal: 12,
      paddingVertical: 10,
      borderRadius: radius.lg,
      backgroundColor: t.surface2,
      borderColor: t.hairline,
      borderWidth: StyleSheet.hairlineWidth,
      marginTop: 6,
      minHeight: 100,
    },
    blobActions: { flexDirection: 'row', gap: spacing.sm, marginTop: spacing.sm },
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
    monikerOut: { color: t.text, fontSize: 13.5, fontFamily: fontFamily.mono },
    footerRow: { flexDirection: 'row', gap: spacing.sm, marginTop: spacing.md },
    flex1: { flex: 1 },
    flex12: { flex: 1.2 },
  });
}
