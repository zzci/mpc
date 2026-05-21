// Trine-styled t-of-n ECDSA keygen flow. Preserves the bridge call shape
// `keyGen(KeygenConfig, callbacks)` and the callback contract from
// docs/design/mcp/sdk.md §2: zero or more onProgress events followed by
// exactly one of onResult / onError as the terminal state. The screen runs
// a five-stage visual progression in parallel with the real bridge call so
// the flow stays demonstrable while the gomobile native body is still a
// stub (B-005). When the bridge returns successfully it overrides the
// synthesized GroupSummary; when it rejects we surface the SdkError.

import React, { useEffect, useMemo, useRef, useState } from 'react';
import {
  Animated,
  Easing,
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
  SectionLabel,
  fontFamily,
  radius,
  spacing,
  useTheme,
} from '../ui';
import type { ThemeTokens } from '../ui';
import { useI18n } from '../i18n';
import type { Strings } from '../i18n';
import { keyGen } from '../sdk';
import type { GroupSummary, KeygenConfig, SdkError } from '../sdk';

type Stage =
  | { readonly kind: 'configure' }
  | { readonly kind: 'confirm' }
  | { readonly kind: 'running' }
  | { readonly kind: 'done'; readonly summary: GroupSummary; readonly mode: BridgeMode }
  | { readonly kind: 'error'; readonly code: string; readonly msg: string };

type BridgeMode = 'live' | 'demo';

const STAGE_KEYS = ['handshake', 'commit', 'share', 'finalize', 'publish'] as const;
type StageKey = (typeof STAGE_KEYS)[number];

const STAGE_DURATION_MS = 800;

const PARTY_OPTIONS = [2, 3, 4, 5] as const;
const DEFAULT_THRESHOLD = 2;
const DEFAULT_PARTIES = 3;

export default function KeygenScreen(): React.JSX.Element {
  const theme = useTheme();
  const { t: T } = useI18n();
  const s = useMemo(() => makeStyles(theme), [theme]);
  const [stage, setStage] = useState<Stage>({ kind: 'configure' });
  const [parties, setParties] = useState<number>(DEFAULT_PARTIES);
  const [threshold, setThreshold] = useState<number>(DEFAULT_THRESHOLD);
  const [passphrase, setPassphrase] = useState<string>('demo');

  // Clamp threshold when parties shrink below it.
  useEffect(() => {
    if (threshold > parties) setThreshold(parties);
  }, [parties, threshold]);

  const cfg = useMemo<KeygenConfig>(
    () => ({ threshold, parties, passphrase }),
    [threshold, parties, passphrase],
  );

  const stageIndex = stageIdxFor(stage.kind);
  const ready = passphrase.length >= 4 && threshold >= 1 && threshold <= parties;

  return (
    <View style={s.body}>
      <Text style={s.intro}>{T.keygen.intro}</Text>
      <StageDots active={stageIndex} total={5} theme={theme} />

      {stage.kind === 'configure' ? (
        <ConfigureStage
          parties={parties}
          threshold={threshold}
          passphrase={passphrase}
          onParties={setParties}
          onThreshold={setThreshold}
          onPassphrase={setPassphrase}
          onNext={() => setStage({ kind: 'confirm' })}
          ready={ready}
        />
      ) : null}

      {stage.kind === 'confirm' ? (
        <ConfirmStage
          cfg={cfg}
          onBack={() => setStage({ kind: 'configure' })}
          onStart={() => setStage({ kind: 'running' })}
        />
      ) : null}

      {stage.kind === 'running' ? (
        <RunningStage
          cfg={cfg}
          onDone={(summary, mode) => {
            setPassphrase('');
            setStage({ kind: 'done', summary, mode });
          }}
          onError={(code, msg) => {
            setPassphrase('');
            setStage({ kind: 'error', code, msg });
          }}
          onCancel={() => setStage({ kind: 'configure' })}
        />
      ) : null}

      {stage.kind === 'done' ? (
        <DoneStage
          cfg={cfg}
          summary={stage.summary}
          mode={stage.mode}
          onClose={() => setStage({ kind: 'configure' })}
        />
      ) : null}

      {stage.kind === 'error' ? (
        <ErrorStage
          code={stage.code}
          msg={stage.msg}
          onRetry={() => setStage({ kind: 'configure' })}
          onCancel={() => setStage({ kind: 'configure' })}
        />
      ) : null}
    </View>
  );
}

function stageIdxFor(kind: Stage['kind']): number {
  switch (kind) {
    case 'configure':
      return 0;
    case 'confirm':
      return 1;
    case 'running':
      return 2;
    case 'done':
    case 'error':
      return 4;
  }
}

// ── Stage 1 ────────────────────────────────────────────────────────────────

interface ConfigureStageProps {
  readonly parties: number;
  readonly threshold: number;
  readonly passphrase: string;
  readonly onParties: (n: number) => void;
  readonly onThreshold: (t: number) => void;
  readonly onPassphrase: (p: string) => void;
  readonly onNext: () => void;
  readonly ready: boolean;
}

function ConfigureStage({
  parties,
  threshold,
  passphrase,
  onParties,
  onThreshold,
  onPassphrase,
  onNext,
  ready,
}: ConfigureStageProps): React.JSX.Element {
  const theme = useTheme();
  const { t: T, tx } = useI18n();
  const s = useMemo(() => makeStyles(theme), [theme]);
  const thresholdOptions = useMemo(() => buildRange(1, parties), [parties]);
  return (
    <View style={s.stageBody}>
      <SectionLabel>{T.keygen.stepConfigure}</SectionLabel>
      <Text style={s.sectionSub}>{T.keygen.stepConfigureSub}</Text>

      <Card style={s.padCard}>
        <Text style={s.fieldLabel}>{T.keygen.partiesLabel}</Text>
        <Text style={s.fieldSub}>{T.keygen.partiesSub}</Text>
        <ChipRow
          values={PARTY_OPTIONS as ReadonlyArray<number>}
          selected={parties}
          onSelect={onParties}
        />

        <View style={s.fieldGap} />
        <Text style={s.fieldLabel}>{T.keygen.thresholdLabel}</Text>
        <Text style={s.fieldSub}>{T.keygen.thresholdSub}</Text>
        <ChipRow values={thresholdOptions} selected={threshold} onSelect={onThreshold} />

        <View style={s.quorumPreview}>
          <Icon name="shield" size={14} color={theme.accent} />
          <Text style={s.quorumPreviewText}>
            {tx(T.keygen.quorumPreview, { threshold, parties })}
          </Text>
        </View>

        <View style={s.fieldGap} />
        <Text style={s.fieldLabel}>{T.keygen.passphraseLabel}</Text>
        <Text style={s.fieldSub}>{T.keygen.passphraseSub}</Text>
        <TextInput
          value={passphrase}
          onChangeText={onPassphrase}
          placeholder={T.keygen.passphrasePlaceholder}
          placeholderTextColor={theme.text3}
          secureTextEntry
          autoCapitalize="none"
          autoCorrect={false}
          spellCheck={false}
          style={s.textInput}
          accessibilityLabel={T.keygen.passphraseLabel}
        />
      </Card>

      <View style={s.footerRow}>
        <PrimaryButton label={T.keygen.ctaContinue} onPress={onNext} disabled={!ready} style={s.flex1} />
      </View>
    </View>
  );
}

// ── Stage 2 ────────────────────────────────────────────────────────────────

interface ConfirmStageProps {
  readonly cfg: KeygenConfig;
  readonly onBack: () => void;
  readonly onStart: () => void;
}

function ConfirmStage({ cfg, onBack, onStart }: ConfirmStageProps): React.JSX.Element {
  const theme = useTheme();
  const { t: T, tx } = useI18n();
  const s = useMemo(() => makeStyles(theme), [theme]);
  return (
    <View style={s.stageBody}>
      <SectionLabel>{T.keygen.stepConfirm}</SectionLabel>
      <Text style={s.sectionSub}>{T.keygen.stepConfirmSub}</Text>

      <Card style={s.summaryCard}>
        <View style={s.summaryHeader}>
          <Icon name="shield" size={14} color={theme.accent} />
          <Text style={s.summaryText}>
            {tx(T.keygen.quorumPreview, { threshold: cfg.threshold, parties: cfg.parties })}
          </Text>
        </View>
      </Card>

      <SectionLabel>{T.keygen.stepConfirm}</SectionLabel>
      <Card style={s.factsCard}>
        <FactRow icon="info" title={T.keygen.factCallback} sub={T.keygen.factCallbackSub} />
        <Hairline />
        <FactRow icon="key" title={T.keygen.factGroupPub} sub={T.keygen.factGroupPubSub} />
        <Hairline />
        <FactRow icon="shield" title={T.keygen.factShareLocal} sub={T.keygen.factShareLocalSub} />
        <Hairline />
        <FactRow icon="wallet" title={T.keygen.factCommittee} sub={T.keygen.factCommitteeSub} />
      </Card>

      <SectionLabel>{T.keygen.callShape}</SectionLabel>
      <Card style={s.callCard}>
        <Text style={s.callShape}>{T.keygen.callShapeValue}</Text>
      </Card>

      <View style={s.footerRow}>
        <GhostButton label={T.common.back} onPress={onBack} style={s.flex1} />
        <PrimaryButton label={T.keygen.ctaStart} onPress={onStart} style={s.flex12} />
      </View>
    </View>
  );
}

// ── Stage 3 ────────────────────────────────────────────────────────────────

interface RunningStageProps {
  readonly cfg: KeygenConfig;
  readonly onDone: (summary: GroupSummary, mode: BridgeMode) => void;
  readonly onError: (code: string, msg: string) => void;
  readonly onCancel: () => void;
}

function RunningStage({ cfg, onDone, onError, onCancel }: RunningStageProps): React.JSX.Element {
  const theme = useTheme();
  const { t: T, tx } = useI18n();
  const s = useMemo(() => makeStyles(theme), [theme]);
  const [activeIdx, setActiveIdx] = useState(0);
  const [progressMsg, setProgressMsg] = useState<string | null>(null);

  // Keep terminal callbacks current without restarting the timer.
  const onDoneRef = useRef(onDone);
  const onErrorRef = useRef(onError);
  onDoneRef.current = onDone;
  onErrorRef.current = onError;
  // Pre-compute the launch-error template so the effect does not depend on
  // the i18n surface object (which would re-launch the bridge call if the
  // provider ever produced a fresh reference).
  const launchErrorTemplate = T.keygen.errorLaunch;
  const launchErrorFmt = useRef(
    (detail: string) => tx(launchErrorTemplate, { detail }),
  );
  launchErrorFmt.current = (detail: string) => tx(launchErrorTemplate, { detail });

  useEffect(() => {
    let cancelled = false;
    let timer: ReturnType<typeof setTimeout> | null = null;
    let bridgeResult:
      | { kind: 'result'; summary: GroupSummary }
      | { kind: 'error'; code: string; msg: string }
      | null = null;

    // Start the staged visual timer.
    const tick = (idx: number): void => {
      if (cancelled) return;
      if (idx >= STAGE_KEYS.length) {
        finalize();
        return;
      }
      setActiveIdx(idx);
      timer = setTimeout(() => tick(idx + 1), STAGE_DURATION_MS);
    };

    const finalize = (): void => {
      if (cancelled) return;
      if (bridgeResult && bridgeResult.kind === 'result') {
        onDoneRef.current(bridgeResult.summary, 'live');
        return;
      }
      if (bridgeResult && bridgeResult.kind === 'error') {
        onErrorRef.current(bridgeResult.code, bridgeResult.msg);
        return;
      }
      // Bridge stub did not surface a terminal callback — synthesize a demo
      // GroupSummary so the visual flow still reaches a result state while
      // preserving the GroupSummary shape from the SDK.
      onDoneRef.current(synthesizeSummary(cfg), 'demo');
    };

    // Launch the real bridge call. Callbacks override the simulated flow.
    keyGen(cfg, {
      onProgress: (msg) => {
        if (cancelled) return;
        setProgressMsg(msg);
      },
      onResult: (summary) => {
        if (cancelled) return;
        bridgeResult = { kind: 'result', summary };
      },
      onError: (e: SdkError) => {
        if (cancelled) return;
        bridgeResult = { kind: 'error', code: e.code, msg: e.msg };
      },
    }).catch((err: unknown) => {
      if (cancelled) return;
      // Skip overwriting a real SdkError that the callback path may have
      // surfaced before the promise rejected (synchronous onError → reject).
      if (bridgeResult) return;
      bridgeResult = {
        kind: 'error',
        code: 'INTERNAL',
        msg: launchErrorFmt.current(errorDetail(err)),
      };
    });

    tick(0);
    return () => {
      cancelled = true;
      if (timer) clearTimeout(timer);
    };
  }, [cfg]);

  return (
    <View style={s.stageBody}>
      <SectionLabel>{T.keygen.stepProgress}</SectionLabel>
      <Text style={s.sectionSub}>{T.keygen.stepProgressSub}</Text>

      <Card style={s.stagesCard}>
        {STAGE_KEYS.map((key, idx) => (
          <StageRow
            key={key}
            stage={key}
            active={idx === activeIdx}
            done={idx < activeIdx}
            isLast={idx === STAGE_KEYS.length - 1}
            T={T}
          />
        ))}
      </Card>

      {progressMsg ? (
        <Text
          style={s.progressHint}
          numberOfLines={2}
          accessibilityLiveRegion="polite"
        >
          {T.keygen.progressEventLabel} · {progressMsg}
        </Text>
      ) : null}

      <View style={s.footerRow}>
        <GhostButton label={T.keygen.ctaCancel} onPress={onCancel} icon="close" style={s.flex1} />
      </View>
    </View>
  );
}

// ── Stage 4 (done) ─────────────────────────────────────────────────────────

interface DoneStageProps {
  readonly cfg: KeygenConfig;
  readonly summary: GroupSummary;
  readonly mode: BridgeMode;
  readonly onClose: () => void;
}

function DoneStage({ cfg, summary, mode, onClose }: DoneStageProps): React.JSX.Element {
  const theme = useTheme();
  const { t: T, tx } = useI18n();
  const s = useMemo(() => makeStyles(theme), [theme]);
  return (
    <View style={s.stageBody}>
      <View style={s.doneHero}>
        <View style={s.doneIcon}>
          <Icon name="check" size={20} color={theme.ok} />
        </View>
        <View style={s.doneText}>
          <Text style={s.doneTitle}>{T.keygen.resultTitle}</Text>
          <Text style={s.doneSub}>{T.keygen.resultSub}</Text>
        </View>
      </View>

      <View style={modeBannerStyle(mode, theme)}>
        <Icon name="info" size={12} color={modeBannerColor(mode, theme)} />
        <Text style={[s.bannerText, { color: modeBannerColor(mode, theme) }]}>
          {mode === 'live' ? T.keygen.bannerLive : T.keygen.bannerDemo}
        </Text>
      </View>

      <Card style={s.padCard}>
        <ResultRow label={T.keygen.resultGroupPubKey} value={summary.groupPubKey} mono />
        <Hairline />
        <ResultRow label={T.keygen.resultThreshold} value={String(summary.threshold)} />
        <Hairline />
        <ResultRow label={T.keygen.resultParties} value={String(summary.parties)} />
        <Hairline />
        <ResultRow
          label={T.keygen.resultMonikers}
          value={
            summary.monikers.length > 0
              ? summary.monikers.join(', ')
              : tx(T.keygen.resultMonikersFallback, { parties: cfg.parties })
          }
        />
      </Card>

      <View style={s.footerRow}>
        <PrimaryButton label={T.keygen.ctaClose} onPress={onClose} style={s.flex1} />
      </View>
    </View>
  );
}

// ── Stage 5 (error) ────────────────────────────────────────────────────────

interface ErrorStageProps {
  readonly code: string;
  readonly msg: string;
  readonly onRetry: () => void;
  readonly onCancel: () => void;
}

function ErrorStage({ code, msg, onRetry, onCancel }: ErrorStageProps): React.JSX.Element {
  const theme = useTheme();
  const { t: T } = useI18n();
  const s = useMemo(() => makeStyles(theme), [theme]);
  return (
    <View style={s.stageBody}>
      <View style={s.errorHero}>
        <View style={s.errorIcon}>
          <Icon name="warn" size={18} color={theme.danger} />
        </View>
        <View style={s.doneText}>
          <Text style={s.doneTitle}>{T.keygen.errorTitle}</Text>
          <Text style={s.doneSub}>
            {code} · {msg}
          </Text>
        </View>
      </View>
      <View style={s.footerRow}>
        <GhostButton label={T.keygen.ctaCancel} onPress={onCancel} style={s.flex1} />
        <PrimaryButton label={T.keygen.ctaRetry} onPress={onRetry} style={s.flex12} />
      </View>
    </View>
  );
}

// ── Helpers ────────────────────────────────────────────────────────────────

interface ChipRowProps {
  readonly values: ReadonlyArray<number>;
  readonly selected: number;
  readonly onSelect: (n: number) => void;
}

function ChipRow({ values, selected, onSelect }: ChipRowProps): React.JSX.Element {
  const theme = useTheme();
  const s = useMemo(() => makeStyles(theme), [theme]);
  return (
    <View style={s.chipRow}>
      {values.map((v) => {
        const on = v === selected;
        return (
          <Pressable
            key={v}
            onPress={() => onSelect(v)}
            accessibilityRole="button"
            accessibilityLabel={String(v)}
            accessibilityState={{ selected: on }}
            style={({ pressed }) => [
              s.chip,
              on ? s.chipOn : null,
              pressed ? s.chipPressed : null,
            ]}
          >
            <Text style={[s.chipText, on ? s.chipTextOn : null]}>{v}</Text>
          </Pressable>
        );
      })}
    </View>
  );
}

interface StageRowProps {
  readonly stage: StageKey;
  readonly active: boolean;
  readonly done: boolean;
  readonly isLast: boolean;
  readonly T: Strings;
}

function StageRow({ stage, active, done, isLast, T }: StageRowProps): React.JSX.Element {
  const theme = useTheme();
  const s = useMemo(() => makeStyles(theme), [theme]);
  const [pulse] = useState(() => new Animated.Value(0));
  useEffect(() => {
    if (!active) {
      pulse.setValue(0);
      return;
    }
    const loop = Animated.loop(
      Animated.sequence([
        Animated.timing(pulse, {
          toValue: 1,
          duration: 700,
          easing: Easing.inOut(Easing.ease),
          useNativeDriver: true,
        }),
        Animated.timing(pulse, {
          toValue: 0,
          duration: 700,
          easing: Easing.inOut(Easing.ease),
          useNativeDriver: true,
        }),
      ]),
    );
    loop.start();
    return () => loop.stop();
  }, [active, pulse]);

  const head = stageHead(stage, T);
  const sub = stageSub(stage, T);
  const badge = active
    ? T.keygen.stageActive
    : done
      ? T.keygen.stageDone
      : T.keygen.stageWait;
  const dotState: 'active' | 'done' | 'wait' = active ? 'active' : done ? 'done' : 'wait';
  const opacity = pulse.interpolate({ inputRange: [0, 1], outputRange: [0.25, 0.95] });

  return (
    <View style={s.stageRow}>
      <View style={s.stageRail}>
        <View
          style={[
            s.stageDot,
            dotState === 'active' ? s.stageDotActive : null,
            dotState === 'done' ? s.stageDotDone : null,
          ]}
        >
          {dotState === 'active' ? <Animated.View style={[s.stageDotPulse, { opacity }]} /> : null}
          {dotState === 'done' ? <Icon name="check" size={12} color={theme.bg} /> : null}
        </View>
        {!isLast ? <View style={[s.stageConn, done ? s.stageConnDone : null]} /> : null}
      </View>
      <View style={s.stageContent}>
        <View style={s.stageHeadRow}>
          <Text style={[s.stageHead, active ? s.stageHeadActive : null]} numberOfLines={1}>
            {head}
          </Text>
          <View
            style={[
              s.stageBadge,
              dotState === 'active' ? s.stageBadgeActiveBg : null,
              dotState === 'done' ? s.stageBadgeDoneBg : null,
            ]}
          >
            <Text
              style={[
                s.stageBadgeText,
                dotState === 'active' ? s.stageBadgeActiveTx : null,
                dotState === 'done' ? s.stageBadgeDoneTx : null,
              ]}
            >
              {badge}
            </Text>
          </View>
        </View>
        <Text style={s.stageSub} numberOfLines={2}>
          {sub}
        </Text>
      </View>
    </View>
  );
}

function stageHead(stage: StageKey, T: Strings): string {
  switch (stage) {
    case 'handshake':
      return T.keygen.stageHandshake;
    case 'commit':
      return T.keygen.stageCommit;
    case 'share':
      return T.keygen.stageShare;
    case 'finalize':
      return T.keygen.stageFinalize;
    case 'publish':
      return T.keygen.stagePublish;
  }
}

function stageSub(stage: StageKey, T: Strings): string {
  switch (stage) {
    case 'handshake':
      return T.keygen.stageHandshakeSub;
    case 'commit':
      return T.keygen.stageCommitSub;
    case 'share':
      return T.keygen.stageShareSub;
    case 'finalize':
      return T.keygen.stageFinalizeSub;
    case 'publish':
      return T.keygen.stagePublishSub;
  }
}

interface FactRowProps {
  readonly icon: 'shield' | 'key' | 'info' | 'wallet';
  readonly title: string;
  readonly sub: string;
}

function FactRow({ icon, title, sub }: FactRowProps): React.JSX.Element {
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

interface ResultRowProps {
  readonly label: string;
  readonly value: string;
  readonly mono?: boolean;
}

function ResultRow({ label, value, mono }: ResultRowProps): React.JSX.Element {
  const theme = useTheme();
  const s = useMemo(() => makeStyles(theme), [theme]);
  return (
    <View style={s.resultRow}>
      <Text style={s.resultLabel}>{label}</Text>
      <Text
        style={[s.resultValue, mono ? s.resultValueMono : null]}
        numberOfLines={1}
        ellipsizeMode="middle"
      >
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

function buildRange(from: number, to: number): ReadonlyArray<number> {
  const out: number[] = [];
  for (let i = from; i <= to; i++) out.push(i);
  return out;
}

function errorDetail(err: unknown): string {
  if (err instanceof Error) return err.message;
  if (typeof err === 'string') return err;
  return 'unknown';
}

function synthesizeSummary(cfg: KeygenConfig): GroupSummary {
  const monikers = Array.from({ length: cfg.parties }, (_v, i) => `m${i}`);
  // Stable deterministic synthetic group pubkey shape — 33-byte compressed
  // secp256k1 hex prefix. Never represents a real key.
  const seed = `${cfg.threshold}-of-${cfg.parties}`;
  const hexBody = repeatToLen(toHexHash(seed), 64);
  return {
    threshold: cfg.threshold,
    parties: cfg.parties,
    monikers,
    groupPubKey: `02${hexBody}`,
  };
}

function repeatToLen(seed: string, total: number): string {
  let out = '';
  while (out.length < total) out += seed;
  return out.slice(0, total);
}

function toHexHash(value: string): string {
  let h = 0;
  for (let i = 0; i < value.length; i++) h = (h * 31 + value.charCodeAt(i)) >>> 0;
  return h.toString(16).padStart(8, '0');
}

function makeStyles(t: ThemeTokens) {
  return StyleSheet.create({
    body: { paddingHorizontal: 0, paddingBottom: spacing.lg },
    intro: { color: t.text2, fontSize: 12.5, lineHeight: 18 },
    stageBody: { gap: spacing.sm, marginTop: spacing.sm },
    sectionSub: { color: t.text3, fontSize: 11.5, lineHeight: 16, paddingHorizontal: 2 },
    padCard: { padding: spacing.md, gap: spacing.xs },
    factsCard: { padding: 0, paddingHorizontal: spacing.md },
    callCard: { padding: spacing.md, gap: 4 },
    summaryCard: { padding: spacing.md, gap: 4 },
    summaryHeader: { flexDirection: 'row', alignItems: 'center', gap: 8 },
    summaryText: { color: t.text, fontSize: 14, fontWeight: '700' },
    callShape: { color: t.text, fontSize: 12, fontFamily: fontFamily.mono },

    fieldLabel: { color: t.text2, fontSize: 11.5, letterSpacing: 0.4, fontWeight: '700' },
    fieldSub: { color: t.text3, fontSize: 11, lineHeight: 15 },
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

    chipRow: { flexDirection: 'row', gap: spacing.sm, marginTop: 6, flexWrap: 'wrap' },
    chip: {
      minWidth: 44,
      paddingHorizontal: 14,
      paddingVertical: 8,
      borderRadius: radius.pill,
      backgroundColor: t.surface2,
      borderColor: t.hairline,
      borderWidth: StyleSheet.hairlineWidth,
      alignItems: 'center',
    },
    chipOn: { backgroundColor: `${t.accent}1a`, borderColor: `${t.accent}66` },
    chipPressed: { opacity: 0.85 },
    chipText: { color: t.text2, fontSize: 13, fontWeight: '700' },
    chipTextOn: { color: t.accent },

    quorumPreview: {
      flexDirection: 'row',
      alignItems: 'center',
      gap: 8,
      paddingHorizontal: spacing.md,
      paddingVertical: 8,
      borderRadius: radius.md,
      backgroundColor: `${t.accent}1a`,
      borderColor: `${t.accent}55`,
      borderWidth: StyleSheet.hairlineWidth,
      marginTop: 8,
    },
    quorumPreviewText: { color: t.accent, fontSize: 12, fontWeight: '700' },

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

    stagesCard: { padding: spacing.lg, borderRadius: radius.xl, gap: 0 },
    stageRow: { flexDirection: 'row', alignItems: 'stretch', gap: spacing.md },
    stageRail: { width: 28, alignItems: 'center' },
    stageDot: {
      width: 22,
      height: 22,
      borderRadius: 11,
      backgroundColor: t.surface2,
      borderColor: t.hairline,
      borderWidth: StyleSheet.hairlineWidth,
      alignItems: 'center',
      justifyContent: 'center',
      marginTop: 2,
      overflow: 'hidden',
    },
    stageDotActive: { backgroundColor: `${t.accent}22`, borderColor: `${t.accent}88` },
    stageDotDone: { backgroundColor: t.accent, borderColor: t.accent },
    stageDotPulse: {
      position: 'absolute',
      width: 22,
      height: 22,
      borderRadius: 11,
      backgroundColor: t.accent,
    },
    stageConn: { flex: 1, width: StyleSheet.hairlineWidth, backgroundColor: t.hairline, marginTop: 4 },
    stageConnDone: { backgroundColor: `${t.accent}88` },
    stageContent: { flex: 1, paddingBottom: spacing.md },
    stageHeadRow: { flexDirection: 'row', alignItems: 'center', gap: spacing.sm },
    stageHead: { color: t.text2, fontSize: 13, fontWeight: '600', flex: 1 },
    stageHeadActive: { color: t.text, fontWeight: '700' },
    stageBadge: {
      paddingHorizontal: 8,
      paddingVertical: 2,
      borderRadius: radius.pill,
      backgroundColor: t.surface2,
      borderColor: t.hairline,
      borderWidth: StyleSheet.hairlineWidth,
    },
    stageBadgeActiveBg: { backgroundColor: `${t.accent}1a`, borderColor: `${t.accent}55` },
    stageBadgeDoneBg: { backgroundColor: `${t.ok}1a`, borderColor: `${t.ok}55` },
    stageBadgeText: { color: t.text3, fontSize: 9.5, fontWeight: '700', letterSpacing: 0.4 },
    stageBadgeActiveTx: { color: t.accent },
    stageBadgeDoneTx: { color: t.ok },
    stageSub: { color: t.text3, fontSize: 11, marginTop: 4, lineHeight: 15 },
    progressHint: {
      color: t.text3,
      fontSize: 11,
      fontFamily: fontFamily.mono,
      paddingHorizontal: 2,
    },

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

    resultRow: {
      flexDirection: 'row',
      alignItems: 'center',
      paddingVertical: 10,
      gap: spacing.md,
    },
    resultLabel: { color: t.text3, fontSize: 11.5, flex: 1 },
    resultValue: { color: t.text, fontSize: 12.5, fontWeight: '600', textAlign: 'right', maxWidth: 220 },
    resultValueMono: { fontFamily: fontFamily.mono, fontWeight: '500' },

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
