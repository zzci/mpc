// Trine-styled lost-member resharing flow. Preserves the bridge call shape
// `reshare(ReshareConfig, callbacks)` and the callback contract from
// docs/design/mcp/sdk.md §7: zero or more onProgress events followed by
// exactly one of onResult / onError. Reshare rotates secret shares onto a
// new committee while keeping the group public key (and every derived
// address) invariant — the screen emphasises that invariant up-front and at
// the result stage. A five-stage visual progression runs in parallel with
// the real bridge call so the flow stays demonstrable while the gomobile
// native body is still a stub (B-005).

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
import { reshare } from '../sdk';
import type { GroupSummary, ReshareConfig, SdkError } from '../sdk';
import { WALLETS } from '../data';

type Stage =
  | { readonly kind: 'intro' }
  | { readonly kind: 'configure' }
  | { readonly kind: 'confirm' }
  | { readonly kind: 'running' }
  | { readonly kind: 'done'; readonly summary: GroupSummary; readonly mode: BridgeMode }
  | { readonly kind: 'error'; readonly code: string; readonly msg: string };

type BridgeMode = 'live' | 'demo';

const STAGE_KEYS = ['prepare', 'distribute', 'recombine', 'install', 'verify'] as const;
type StageKey = (typeof STAGE_KEYS)[number];

const STAGE_DURATION_MS = 800;

const PARTY_OPTIONS = [2, 3, 4, 5] as const;
const DEFAULT_OLD_THRESHOLD = 2;
const DEFAULT_NEW_THRESHOLD = 2;
const DEFAULT_NEW_PARTIES = 3;

export default function ReshareScreen(): React.JSX.Element {
  const theme = useTheme();
  const { t: T } = useI18n();
  const s = useMemo(() => makeStyles(theme), [theme]);
  const [stage, setStage] = useState<Stage>({ kind: 'intro' });
  const [oldThreshold, setOldThreshold] = useState<number>(DEFAULT_OLD_THRESHOLD);
  const [newThreshold, setNewThreshold] = useState<number>(DEFAULT_NEW_THRESHOLD);
  const [newParties, setNewParties] = useState<number>(DEFAULT_NEW_PARTIES);
  const [passphrase, setPassphrase] = useState<string>('demo');

  // Clamp thresholds when their respective party count shifts.
  useEffect(() => {
    if (newThreshold > newParties) setNewThreshold(newParties);
  }, [newParties, newThreshold]);

  const cfg = useMemo<ReshareConfig>(
    () => ({ oldThreshold, newThreshold, newParties, passphrase }),
    [oldThreshold, newThreshold, newParties, passphrase],
  );

  const stageIndex = stageIdxFor(stage.kind);
  const configReady =
    passphrase.length >= 4 &&
    oldThreshold >= 1 &&
    newThreshold >= 1 &&
    newThreshold <= newParties;

  return (
    <View style={s.body}>
      <Text style={s.intro}>{T.reshare.intro}</Text>
      <StageDots active={stageIndex} total={5} theme={theme} />

      {stage.kind === 'intro' ? (
        <IntroStage
          onNext={() => setStage({ kind: 'configure' })}
        />
      ) : null}

      {stage.kind === 'configure' ? (
        <ConfigureStage
          oldThreshold={oldThreshold}
          newThreshold={newThreshold}
          newParties={newParties}
          passphrase={passphrase}
          onOldThreshold={setOldThreshold}
          onNewThreshold={setNewThreshold}
          onNewParties={setNewParties}
          onPassphrase={setPassphrase}
          onBack={() => setStage({ kind: 'intro' })}
          onNext={() => setStage({ kind: 'confirm' })}
          ready={configReady}
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
          onClose={() => setStage({ kind: 'intro' })}
        />
      ) : null}

      {stage.kind === 'error' ? (
        <ErrorStage
          code={stage.code}
          msg={stage.msg}
          onRetry={() => setStage({ kind: 'configure' })}
          onCancel={() => setStage({ kind: 'intro' })}
        />
      ) : null}
    </View>
  );
}

function stageIdxFor(kind: Stage['kind']): number {
  switch (kind) {
    case 'intro':
      return 0;
    case 'configure':
      return 1;
    case 'confirm':
      return 2;
    case 'running':
      return 3;
    case 'done':
    case 'error':
      return 4;
  }
}

// ── Stage 1 (intro) ────────────────────────────────────────────────────────

interface IntroStageProps {
  readonly onNext: () => void;
}

function IntroStage({ onNext }: IntroStageProps): React.JSX.Element {
  const theme = useTheme();
  const { t: T } = useI18n();
  const s = useMemo(() => makeStyles(theme), [theme]);
  return (
    <View style={s.stageBody}>
      <SectionLabel>{T.reshare.stepIntro}</SectionLabel>
      <Text style={s.sectionSub}>{T.reshare.stepIntroSub}</Text>

      <Card style={s.invariantCard}>
        <View style={s.invariantHeader}>
          <Icon name="shield" size={16} color={theme.accent} />
          <Text style={s.invariantTitle}>{T.reshare.invariantTitle}</Text>
        </View>
        <Text style={s.invariantBody}>{T.reshare.invariantBody}</Text>
      </Card>

      <Card style={s.factsCard}>
        <FactRow icon="key" title={T.reshare.factInvariantPub} sub={T.reshare.factInvariantPubSub} />
        <Hairline />
        <FactRow icon="wallet" title={T.reshare.factInvariantAddr} sub={T.reshare.factInvariantAddrSub} />
        <Hairline />
        <FactRow icon="refresh" title={T.reshare.factRotate} sub={T.reshare.factRotateSub} />
        <Hairline />
        <FactRow icon="shield" title={T.reshare.factOldWipe} sub={T.reshare.factOldWipeSub} />
      </Card>

      <View style={s.footerRow}>
        <PrimaryButton label={T.reshare.ctaContinue} onPress={onNext} style={s.flex1} />
      </View>
    </View>
  );
}

// ── Stage 2 (configure) ────────────────────────────────────────────────────

interface ConfigureStageProps {
  readonly oldThreshold: number;
  readonly newThreshold: number;
  readonly newParties: number;
  readonly passphrase: string;
  readonly onOldThreshold: (n: number) => void;
  readonly onNewThreshold: (n: number) => void;
  readonly onNewParties: (n: number) => void;
  readonly onPassphrase: (p: string) => void;
  readonly onBack: () => void;
  readonly onNext: () => void;
  readonly ready: boolean;
}

function ConfigureStage({
  oldThreshold,
  newThreshold,
  newParties,
  passphrase,
  onOldThreshold,
  onNewThreshold,
  onNewParties,
  onPassphrase,
  onBack,
  onNext,
  ready,
}: ConfigureStageProps): React.JSX.Element {
  const theme = useTheme();
  const { t: T, tx } = useI18n();
  const s = useMemo(() => makeStyles(theme), [theme]);
  const oldThresholdOptions = useMemo(() => buildRange(1, 5), []);
  const newThresholdOptions = useMemo(() => buildRange(1, newParties), [newParties]);
  return (
    <View style={s.stageBody}>
      <SectionLabel>{T.reshare.stepConfigure}</SectionLabel>
      <Text style={s.sectionSub}>{T.reshare.stepConfigureSub}</Text>

      <Card style={s.padCard}>
        <Text style={s.fieldLabel}>{T.reshare.oldThresholdLabel}</Text>
        <Text style={s.fieldSub}>{T.reshare.oldThresholdSub}</Text>
        <ChipRow values={oldThresholdOptions} selected={oldThreshold} onSelect={onOldThreshold} />

        <View style={s.fieldGap} />
        <Text style={s.fieldLabel}>{T.reshare.newPartiesLabel}</Text>
        <Text style={s.fieldSub}>{T.reshare.newPartiesSub}</Text>
        <ChipRow
          values={PARTY_OPTIONS as ReadonlyArray<number>}
          selected={newParties}
          onSelect={onNewParties}
        />

        <View style={s.fieldGap} />
        <Text style={s.fieldLabel}>{T.reshare.newThresholdLabel}</Text>
        <Text style={s.fieldSub}>{T.reshare.newThresholdSub}</Text>
        <ChipRow values={newThresholdOptions} selected={newThreshold} onSelect={onNewThreshold} />

        <View style={s.quorumPreview}>
          <Icon name="shield" size={14} color={theme.accent} />
          <Text style={s.quorumPreviewText}>
            {tx(T.reshare.newQuorum, { threshold: newThreshold, parties: newParties })}
          </Text>
        </View>

        <View style={s.fieldGap} />
        <Text style={s.fieldLabel}>{T.reshare.passphraseLabel}</Text>
        <Text style={s.fieldSub}>{T.reshare.passphraseSub}</Text>
        <TextInput
          value={passphrase}
          onChangeText={onPassphrase}
          placeholder={T.reshare.passphrasePlaceholder}
          placeholderTextColor={theme.text3}
          secureTextEntry
          autoCapitalize="none"
          autoCorrect={false}
          spellCheck={false}
          style={s.textInput}
          accessibilityLabel={T.reshare.passphraseLabel}
        />
      </Card>

      <View style={s.footerRow}>
        <GhostButton label={T.common.back} onPress={onBack} style={s.flex1} />
        <PrimaryButton label={T.reshare.ctaContinue} onPress={onNext} disabled={!ready} style={s.flex12} />
      </View>
    </View>
  );
}

// ── Stage 3 (confirm) ──────────────────────────────────────────────────────

interface ConfirmStageProps {
  readonly cfg: ReshareConfig;
  readonly onBack: () => void;
  readonly onStart: () => void;
}

function ConfirmStage({ cfg, onBack, onStart }: ConfirmStageProps): React.JSX.Element {
  const theme = useTheme();
  const { t: T, tx } = useI18n();
  const s = useMemo(() => makeStyles(theme), [theme]);
  // Use the first wallet as a default reference for "this is what's protected".
  const reference = WALLETS[0];
  const referencePartyCount = reference ? reference.parties : cfg.oldThreshold + 1;
  return (
    <View style={s.stageBody}>
      <SectionLabel>{T.reshare.summaryHeading}</SectionLabel>

      <Card style={s.diffCard}>
        <View style={s.diffRow}>
          <View style={s.diffSide}>
            <Text style={s.diffLabel}>{T.walletDetail.thresholdHeading}</Text>
            <Text style={s.diffValueDim}>
              {tx(T.reshare.oldQuorum, { threshold: cfg.oldThreshold, parties: referencePartyCount })}
            </Text>
          </View>
          <Icon name="chevronR" size={16} color={theme.text3} />
          <View style={s.diffSide}>
            <Text style={s.diffLabel}>{T.walletDetail.thresholdHeading}</Text>
            <Text style={s.diffValueAccent}>
              {tx(T.reshare.newQuorum, { threshold: cfg.newThreshold, parties: cfg.newParties })}
            </Text>
          </View>
        </View>
      </Card>

      <SectionLabel>{T.reshare.stepConfirm}</SectionLabel>
      <Text style={s.sectionSub}>{T.reshare.stepConfirmSub}</Text>

      <Card style={s.factsCard}>
        <FactRow icon="key" title={T.reshare.factInvariantPub} sub={T.reshare.factInvariantPubSub} />
        <Hairline />
        <FactRow icon="wallet" title={T.reshare.factInvariantAddr} sub={T.reshare.factInvariantAddrSub} />
        <Hairline />
        <FactRow icon="refresh" title={T.reshare.factRotate} sub={T.reshare.factRotateSub} />
        <Hairline />
        <FactRow icon="shield" title={T.reshare.factOldWipe} sub={T.reshare.factOldWipeSub} />
      </Card>

      <SectionLabel>{T.reshare.callShape}</SectionLabel>
      <Card style={s.callCard}>
        <Text style={s.callShape}>{T.reshare.callShapeValue}</Text>
      </Card>

      <View style={s.footerRow}>
        <GhostButton label={T.common.back} onPress={onBack} style={s.flex1} />
        <PrimaryButton label={T.reshare.ctaStart} onPress={onStart} style={s.flex12} />
      </View>
    </View>
  );
}

// ── Stage 4 (running) ──────────────────────────────────────────────────────

interface RunningStageProps {
  readonly cfg: ReshareConfig;
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

  const onDoneRef = useRef(onDone);
  const onErrorRef = useRef(onError);
  onDoneRef.current = onDone;
  onErrorRef.current = onError;
  // Pre-compute the launch-error template so the effect does not depend on
  // the i18n surface object (which would re-launch the bridge call if the
  // provider ever produced a fresh reference).
  const launchErrorTemplate = T.reshare.errorLaunch;
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
      onDoneRef.current(synthesizeSummary(cfg), 'demo');
    };

    reshare(cfg, {
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
      // surfaced before the promise rejected.
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
      <SectionLabel>{T.reshare.stepProgress}</SectionLabel>
      <Text style={s.sectionSub}>{T.reshare.stepProgressSub}</Text>

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
          {T.reshare.progressEventLabel} · {progressMsg}
        </Text>
      ) : null}

      <View style={s.footerRow}>
        <GhostButton label={T.reshare.ctaCancel} onPress={onCancel} icon="close" style={s.flex1} />
      </View>
    </View>
  );
}

// ── Stage 5 (done) ─────────────────────────────────────────────────────────

interface DoneStageProps {
  readonly cfg: ReshareConfig;
  readonly summary: GroupSummary;
  readonly mode: BridgeMode;
  readonly onClose: () => void;
}

function DoneStage({ cfg, summary, mode, onClose }: DoneStageProps): React.JSX.Element {
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
          <Text style={s.doneTitle}>{T.reshare.resultTitle}</Text>
          <Text style={s.doneSub}>{T.reshare.resultSub}</Text>
        </View>
      </View>

      <View style={modeBannerStyle(mode, theme)}>
        <Icon name="info" size={12} color={modeBannerColor(mode, theme)} />
        <Text style={[s.bannerText, { color: modeBannerColor(mode, theme) }]}>
          {mode === 'live' ? T.reshare.bannerLive : T.reshare.bannerDemo}
        </Text>
      </View>

      <Card style={s.padCard}>
        <ResultRow label={T.reshare.resultGroupPubKey} value={summary.groupPubKey} mono />
        <Hairline />
        <ResultRow label={T.reshare.resultOldThreshold} value={String(cfg.oldThreshold)} />
        <Hairline />
        <ResultRow label={T.reshare.resultNewThreshold} value={String(summary.threshold)} />
        <Hairline />
        <ResultRow label={T.reshare.resultNewParties} value={String(summary.parties)} />
      </Card>

      <View style={s.footerRow}>
        <PrimaryButton label={T.reshare.ctaClose} onPress={onClose} style={s.flex1} />
      </View>
    </View>
  );
}

// ── Error ──────────────────────────────────────────────────────────────────

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
          <Text style={s.doneTitle}>{T.reshare.errorTitle}</Text>
          <Text style={s.doneSub}>
            {code} · {msg}
          </Text>
        </View>
      </View>
      <View style={s.footerRow}>
        <GhostButton label={T.reshare.ctaCancel} onPress={onCancel} style={s.flex1} />
        <PrimaryButton label={T.reshare.ctaRetry} onPress={onRetry} style={s.flex12} />
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
    ? T.reshare.stageActive
    : done
      ? T.reshare.stageDone
      : T.reshare.stageWait;
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
    case 'prepare':
      return T.reshare.stagePrepare;
    case 'distribute':
      return T.reshare.stageDistribute;
    case 'recombine':
      return T.reshare.stageRecombine;
    case 'install':
      return T.reshare.stageInstall;
    case 'verify':
      return T.reshare.stageVerify;
  }
}

function stageSub(stage: StageKey, T: Strings): string {
  switch (stage) {
    case 'prepare':
      return T.reshare.stagePrepareSub;
    case 'distribute':
      return T.reshare.stageDistributeSub;
    case 'recombine':
      return T.reshare.stageRecombineSub;
    case 'install':
      return T.reshare.stageInstallSub;
    case 'verify':
      return T.reshare.stageVerifySub;
  }
}

interface FactRowProps {
  readonly icon: 'shield' | 'key' | 'info' | 'wallet' | 'refresh';
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

function synthesizeSummary(cfg: ReshareConfig): GroupSummary {
  const monikers = Array.from({ length: cfg.newParties }, (_v, i) => `m${i}`);
  // Stable deterministic synthetic group pubkey shape — 33-byte compressed
  // secp256k1 hex prefix. Deliberately *not* derived from any real wallet
  // pubkey: a reshare demo must not surface a value that could be mistaken
  // for the rotated key of an existing wallet.
  const seed = `reshare-demo:${cfg.oldThreshold}->${cfg.newThreshold}-of-${cfg.newParties}`;
  const hexBody = repeatToLen(toHexHash(seed), 64);
  return {
    threshold: cfg.newThreshold,
    parties: cfg.newParties,
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

    invariantCard: { padding: spacing.md, gap: 8, backgroundColor: `${t.accent}10`, borderColor: `${t.accent}33` },
    invariantHeader: { flexDirection: 'row', alignItems: 'center', gap: 8 },
    invariantTitle: { color: t.text, fontSize: 14, fontWeight: '700' },
    invariantBody: { color: t.text2, fontSize: 12.5, lineHeight: 18 },

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

    diffCard: { padding: spacing.md, gap: 8 },
    diffRow: { flexDirection: 'row', alignItems: 'center', gap: spacing.md },
    diffSide: { flex: 1, gap: 4 },
    diffLabel: { color: t.text3, fontSize: 11, letterSpacing: 0.4, textTransform: 'uppercase' },
    diffValueDim: { color: t.text2, fontSize: 13, fontWeight: '600' },
    diffValueAccent: { color: t.accent, fontSize: 13, fontWeight: '700' },

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
