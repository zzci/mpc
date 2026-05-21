import React, { useEffect, useMemo, useState } from 'react';
import { View, Text, StyleSheet } from 'react-native';
import { Card, Icon, useTheme, spacing, radius, fontFamily } from '../../ui';
import type { ThemeTokens } from '../../ui';
import { useI18n } from '../../i18n';
import type { Strings } from '../../i18n';
import { InitShell } from './InitShell';

export interface InitRegisterScreenProps {
  readonly onNext?: () => void;
}

const STAGE_MS: ReadonlyArray<number> = [700, 900, 1100, 800];

interface Stage {
  readonly label: string;
  readonly sub: string;
}

function buildStages(T: Strings): ReadonlyArray<Stage> {
  return [
    { label: T.onboarding.stageDerive, sub: T.onboarding.stageDeriveSub },
    { label: T.onboarding.stagePost, sub: T.onboarding.stagePostSub },
    { label: T.onboarding.stageVerify, sub: T.onboarding.stageVerifySub },
    { label: T.onboarding.stageAssign, sub: T.onboarding.stageAssignSub },
  ];
}

export function InitRegisterScreen({ onNext }: InitRegisterScreenProps): React.JSX.Element {
  const t = useTheme();
  const { t: T } = useI18n();
  const s = useMemo(() => makeStyles(t), [t]);
  const stages = useMemo(() => buildStages(T), [T]);
  const [stage, setStage] = useState(0);

  useEffect(() => {
    if (stage < stages.length) {
      const ms = STAGE_MS[stage] ?? 800;
      const handle = setTimeout(() => setStage((curr) => curr + 1), ms);
      return () => clearTimeout(handle);
    }
    const handle = setTimeout(() => onNext?.(), 600);
    return () => clearTimeout(handle);
  }, [stage, stages.length, onNext]);

  return (
    <InitShell
      step={4}
      total={5}
      title={T.onboarding.registerTitle}
      subtitle={T.onboarding.registerSubtitle}
    >
      <Card style={s.preview}>
        <Text style={s.previewHeading}>{T.onboarding.httpHeading}</Text>
        <View style={s.previewBody}>
          <Text style={s.code}>
            <Text style={s.codeVerb}>POST </Text>
            {`https://coord.zzci.io`}
            <Text style={s.codePath}>/v1/members/enroll</Text>
          </Text>
          <Text style={s.codeMuted}>Authorization: Bearer btk_2J9XaQ…wKf3</Text>
          <Text style={s.codeMuted}>X-Member-Ts: 1747843200000</Text>
          <Text style={s.codeMuted}>X-Member-Sig: 0x8a4f…32 (self-sign)</Text>
          <Text style={s.codeJson}>{'{'}</Text>
          <Text style={s.codeJson}>{'  "identityPub": "0x04a2c9f1…8e3b",'}</Text>
          <Text style={s.codeJson}>{'  "deviceLabel": "iPhone 15 Pro",'}</Text>
          <Text style={s.codeJson}>{'  "groupHint": "grp_5a8e7b3c"'}</Text>
          <Text style={s.codeJson}>{'}'}</Text>
        </View>
      </Card>

      <View style={s.stages}>
        {stages.map((st, i) => {
          const done = i < stage;
          const active = i === stage;
          return (
            <View
              key={st.label}
              style={[
                s.stage,
                active ? s.stageActive : null,
                !done && !active ? s.stagePending : null,
              ]}
            >
              <View style={[s.stageNum, done ? s.stageNumDone : null]}>
                {done ? (
                  <Icon name="check" size={12} color={t.bg} />
                ) : (
                  <Text style={s.stageNumText}>{i + 1}</Text>
                )}
              </View>
              <View style={s.stageText}>
                <Text style={[s.stageLabel, !done && !active ? s.stageLabelPending : null]}>
                  {st.label}
                </Text>
                <Text style={s.stageSub} numberOfLines={1}>
                  {st.sub}
                </Text>
              </View>
              {active ? <View style={s.spinner} /> : null}
            </View>
          );
        })}
      </View>
    </InitShell>
  );
}

function makeStyles(t: ThemeTokens) {
  return StyleSheet.create({
    preview: { padding: spacing.md, marginBottom: spacing.lg },
    previewHeading: {
      color: t.text3,
      fontSize: 10.5,
      fontWeight: '700',
      letterSpacing: 0.6,
      marginBottom: 8,
    },
    previewBody: { gap: 2 },
    code: { color: t.text, fontSize: 11.5, fontFamily: fontFamily.mono },
    codeVerb: { color: t.accent },
    codePath: { color: t.text },
    codeMuted: { color: t.text3, fontSize: 11.5, fontFamily: fontFamily.mono },
    codeJson: { color: t.text2, fontSize: 11.5, fontFamily: fontFamily.mono },
    stages: { gap: 8 },
    stage: {
      flexDirection: 'row',
      alignItems: 'center',
      gap: 12,
      padding: 11,
      borderRadius: radius.md,
      backgroundColor: t.surface,
      borderColor: t.hairline,
      borderWidth: StyleSheet.hairlineWidth,
    },
    stageActive: { backgroundColor: `${t.accent}1a`, borderColor: `${t.accent}55` },
    stagePending: { opacity: 0.5 },
    stageNum: {
      width: 26,
      height: 26,
      borderRadius: 13,
      backgroundColor: `${t.accent}22`,
      alignItems: 'center',
      justifyContent: 'center',
      borderWidth: StyleSheet.hairlineWidth,
      borderColor: t.hairline,
    },
    stageNumDone: { backgroundColor: t.accent, borderColor: t.accent },
    stageNumText: { color: t.accent, fontSize: 11, fontWeight: '700' },
    stageText: { flex: 1 },
    stageLabel: { color: t.text, fontSize: 12.5, fontWeight: '700' },
    stageLabelPending: { color: t.text3 },
    stageSub: { color: t.text3, fontSize: 10, marginTop: 2, fontFamily: fontFamily.mono },
    spinner: {
      width: 14,
      height: 14,
      borderRadius: 7,
      borderWidth: 2,
      borderColor: `${t.accent}33`,
      borderTopColor: t.accent,
    },
  });
}
