// Staged MPC signing progress. After Approve the WYSIWYS detail hands the
// envelope here; the screen runs through five visual stages (prepare → r1
// commitments → r2 shares → r3 sign → combine → coord report) that mirror
// the high-level shape from docs/design/mcp/sdk.md §3. The animation is
// purely visual — no MPC machinery is exercised on this skeleton — but the
// stage labels and ordering are accurate for the design walkthrough.

import React, { useEffect, useMemo, useRef, useState } from 'react';
import { Animated, Easing, StyleSheet, Text, View } from 'react-native';
import {
  ChainBadge,
  GhostButton,
  Icon,
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
import type { SigningEnvelope } from '../../data';

const STAGE_KEYS = ['prepare', 'r1', 'r2', 'r3', 'combine', 'report'] as const;
type StageKey = (typeof STAGE_KEYS)[number];

const STAGE_DURATION_MS = 850;

export interface SignProgressScreenProps {
  readonly envelope: SigningEnvelope;
  readonly onComplete: (rsvBase64: string) => void;
  readonly onCancel: () => void;
}

export function SignProgressScreen({
  envelope,
  onComplete,
  onCancel,
}: SignProgressScreenProps): React.JSX.Element {
  const t = useTheme();
  const { t: T, tx } = useI18n();
  const s = useMemo(() => makeStyles(t), [t]);
  const [activeIdx, setActiveIdx] = useState(0);
  const wallet = WALLETS.find((w) => w.groupId === envelope.groupId);

  // Keep onComplete in a ref so a parent-induced re-render that produces a
  // fresh callback reference does not restart the staged timer mid-flight.
  const onCompleteRef = useRef(onComplete);
  onCompleteRef.current = onComplete;

  useEffect(() => {
    let cancelled = false;
    let timer: ReturnType<typeof setTimeout> | null = null;
    const tick = (idx: number): void => {
      if (cancelled) return;
      if (idx >= STAGE_KEYS.length) {
        onCompleteRef.current(synthesizeRsv(envelope));
        return;
      }
      setActiveIdx(idx);
      timer = setTimeout(() => tick(idx + 1), STAGE_DURATION_MS);
    };
    tick(0);
    return () => {
      cancelled = true;
      if (timer) clearTimeout(timer);
    };
  }, [envelope]);

  return (
    <Screen
      footer={
        <GhostButton label={T.signing.cancelFlow} onPress={onCancel} icon="close" />
      }
    >
      <TopBar title={T.signing.progressTitle} />

      <View style={s.bodyPad}>
        <View style={s.headerCard}>
          <View style={s.headRow}>
            <ChainBadge chain={envelope.chain} label={envelope.chainLabel} />
            <Text style={s.requestId} numberOfLines={1}>
              {envelope.requestId}
            </Text>
          </View>
          <Text style={s.title} numberOfLines={2}>
            {envelope.businessInfo.title}
          </Text>
          <Text style={s.subtitle}>
            {tx(T.signing.progressSub, {
              parties: wallet?.parties ?? envelope.decisions.length,
              digest: shortDigest(envelope.digest32),
            })}
          </Text>
        </View>

        <View style={s.stages}>
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
        </View>
      </View>
    </Screen>
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
  const t = useTheme();
  const s = useMemo(() => makeStyles(t), [t]);
  // Create the Animated.Value exactly once per StageRow instance.
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

  const headline = stageHeadline(stage, T);
  const sub = stageSub(stage, T);
  const badge = active
    ? T.signing.stageBadgeActive
    : done
      ? T.signing.stageBadgeDone
      : T.signing.stageBadgeWait;
  const dotState: 'active' | 'done' | 'wait' = active ? 'active' : done ? 'done' : 'wait';

  const dotStyle = [
    s.stageDot,
    dotState === 'active' ? s.stageDotActive : null,
    dotState === 'done' ? s.stageDotDone : null,
  ];

  const opacity = pulse.interpolate({ inputRange: [0, 1], outputRange: [0.25, 0.95] });

  return (
    <View style={s.stageRow}>
      <View style={s.stageRail}>
        <View style={dotStyle}>
          {dotState === 'active' ? (
            <Animated.View style={[s.stageDotPulse, { opacity }]} />
          ) : null}
          {dotState === 'done' ? <Icon name="check" size={12} color={t.bg} /> : null}
        </View>
        {!isLast ? (
          <View style={[s.stageConn, done ? s.stageConnDone : null]} />
        ) : null}
      </View>
      <View style={s.stageBody}>
        <View style={s.stageHeadRow}>
          <Text style={[s.stageHead, active ? s.stageHeadActive : null, done ? s.stageHeadDone : null]} numberOfLines={1}>
            {headline}
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

function stageHeadline(stage: StageKey, T: Strings): string {
  switch (stage) {
    case 'prepare':
      return T.signing.stagePrepare;
    case 'r1':
      return T.signing.stageRound1;
    case 'r2':
      return T.signing.stageRound2;
    case 'r3':
      return T.signing.stageRound3;
    case 'combine':
      return T.signing.stageCombine;
    case 'report':
      return T.signing.stageReport;
  }
}

function stageSub(stage: StageKey, T: Strings): string {
  switch (stage) {
    case 'prepare':
      return T.signing.stagePrepareSub;
    case 'r1':
      return T.signing.stageRound1Sub;
    case 'r2':
      return T.signing.stageRound2Sub;
    case 'r3':
      return T.signing.stageRound3Sub;
    case 'combine':
      return T.signing.stageCombineSub;
    case 'report':
      return T.signing.stageReportSub;
  }
}

function shortDigest(d: string): string {
  if (d.length <= 12) return d;
  return `${d.slice(0, 8)}…${d.slice(-4)}`;
}

function synthesizeRsv(env: SigningEnvelope): string {
  // 65-byte synthetic placeholder shaped like an RSV signature so the result
  // screen renders the visual format; never represents a real signature.
  const digest = env.digest32.replace(/^0x/, '').replace(/…/g, '').padEnd(64, '0').slice(0, 64);
  const r = digest;
  const sBytes = digest.split('').reverse().join('');
  const v = '1c';
  return `0x${r}${sBytes}${v}`.slice(0, 2 + 130);
}

function makeStyles(t: ThemeTokens) {
  return StyleSheet.create({
    bodyPad: { paddingHorizontal: spacing.lg, paddingBottom: spacing.lg, gap: spacing.lg },
    headerCard: {
      padding: spacing.lg,
      borderRadius: radius.xl,
      backgroundColor: t.surface,
      borderColor: t.hairline,
      borderWidth: StyleSheet.hairlineWidth,
      marginTop: spacing.sm,
    },
    headRow: { flexDirection: 'row', alignItems: 'center', gap: spacing.sm, marginBottom: 10 },
    requestId: { color: t.text3, fontSize: 11.5, fontFamily: fontFamily.mono, flexShrink: 1 },
    title: { color: t.text, fontSize: 16, fontWeight: '700', lineHeight: 22 },
    subtitle: { color: t.text3, fontSize: 11.5, marginTop: 6, lineHeight: 17 },
    stages: {
      padding: spacing.lg,
      borderRadius: radius.xl,
      backgroundColor: t.surface,
      borderColor: t.hairline,
      borderWidth: StyleSheet.hairlineWidth,
      gap: 0,
    },
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
    stageConn: {
      flex: 1,
      width: StyleSheet.hairlineWidth,
      backgroundColor: t.hairline,
      marginTop: 4,
    },
    stageConnDone: { backgroundColor: `${t.accent}88` },
    stageBody: { flex: 1, paddingBottom: spacing.md },
    stageHeadRow: { flexDirection: 'row', alignItems: 'center', gap: spacing.sm },
    stageHead: { color: t.text2, fontSize: 13, fontWeight: '600', flex: 1 },
    stageHeadActive: { color: t.text, fontWeight: '700' },
    stageHeadDone: { color: t.text2, fontWeight: '600' },
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
  });
}
