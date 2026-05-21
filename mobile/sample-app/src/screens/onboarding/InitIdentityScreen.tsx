import React, { useMemo, useState } from 'react';
import { View, Text, TextInput, StyleSheet } from 'react-native';
import { Card, Icon, SectionLabel, useTheme, spacing, radius, fontFamily } from '../../ui';
import type { ThemeTokens } from '../../ui';
import { useI18n } from '../../i18n';
import { InitShell } from './InitShell';

export interface InitIdentityScreenProps {
  readonly onBack?: () => void;
  readonly onNext?: () => void;
}

export function InitIdentityScreen({ onBack, onNext }: InitIdentityScreenProps): React.JSX.Element {
  const t = useTheme();
  const { t: T } = useI18n();
  const s = useMemo(() => makeStyles(t), [t]);
  const [pass, setPass] = useState('');
  const [pass2, setPass2] = useState('');
  const passOk = pass.length >= 8 && pass === pass2;
  const strength = Math.min(4, Math.floor(pass.length / 4));

  return (
    <InitShell
      step={1}
      total={5}
      title={T.onboarding.identityTitle}
      subtitle={T.onboarding.identitySubtitle}
      onBack={onBack}
      primaryLabel={T.onboarding.passphrasePrimary}
      primaryDisabled={!passOk}
      onPrimary={onNext}
    >
      <View style={s.note}>
        <View style={s.noteHead}>
          <Icon name="shield" size={14} color={t.accent} />
          <Text style={s.noteTitle}>{T.onboarding.passphraseNoteTitle}</Text>
        </View>
        <Text style={s.noteBody}>
          {T.onboarding.passphraseNoteBody}{' '}
          <Text style={s.noteBold}>{T.onboarding.passphraseNoteBold}</Text>
        </Text>
      </View>

      <SectionLabel>{T.onboarding.passphraseLabel}</SectionLabel>
      <View style={s.field}>
        <TextInput
          value={pass}
          onChangeText={setPass}
          placeholder={T.onboarding.passphrasePlaceholder}
          placeholderTextColor={t.text3}
          secureTextEntry
          style={s.input}
        />
        <View style={s.strengthRow}>
          {[0, 1, 2, 3].map((i) => (
            <View key={i} style={[s.strengthBar, i < strength ? s.strengthBarOn : null]} />
          ))}
        </View>
      </View>
      <View style={[s.field, pass2 && pass !== pass2 ? s.fieldError : null]}>
        <TextInput
          value={pass2}
          onChangeText={setPass2}
          placeholder={T.onboarding.passphraseRepeat}
          placeholderTextColor={t.text3}
          secureTextEntry
          style={s.input}
        />
        {pass2 && pass !== pass2 ? (
          <Text style={s.fieldErrorText}>{T.onboarding.passphraseMismatch}</Text>
        ) : null}
      </View>

      <SectionLabel>{T.onboarding.biometrics}</SectionLabel>
      <Card style={s.bio}>
        <View style={s.bioIcon}>
          <Icon name="shield" size={18} color={t.accent} />
        </View>
        <View style={s.bioText}>
          <Text style={s.bioTitle}>{T.onboarding.faceIdUnlock}</Text>
          <Text style={s.bioSub}>{T.onboarding.faceIdSub}</Text>
        </View>
        <View style={s.toggleOn}>
          <View style={s.toggleKnob} />
        </View>
      </Card>
    </InitShell>
  );
}

function makeStyles(t: ThemeTokens) {
  return StyleSheet.create({
    note: {
      padding: 12,
      borderRadius: radius.md,
      backgroundColor: `${t.accent}1a`,
      borderColor: `${t.accent}55`,
      borderWidth: StyleSheet.hairlineWidth,
      marginBottom: spacing.lg,
    },
    noteHead: { flexDirection: 'row', alignItems: 'center', gap: 6, marginBottom: 6 },
    noteTitle: { color: t.accent, fontSize: 12, fontWeight: '700', letterSpacing: 0.3 },
    noteBody: { color: t.text2, fontSize: 11.5, lineHeight: 18 },
    noteBold: { color: t.text, fontWeight: '700' },
    field: {
      padding: 14,
      borderRadius: radius.lg,
      backgroundColor: t.surface,
      borderColor: t.hairline,
      borderWidth: StyleSheet.hairlineWidth,
      marginBottom: spacing.sm,
    },
    fieldError: { borderColor: `${t.danger}66` },
    input: { color: t.text, fontSize: 15, fontFamily: fontFamily.mono, padding: 0 },
    strengthRow: { flexDirection: 'row', gap: 4, marginTop: 10 },
    strengthBar: { flex: 1, height: 3, borderRadius: 2, backgroundColor: t.surface2 },
    strengthBarOn: { backgroundColor: t.accent },
    fieldErrorText: { color: t.danger, fontSize: 10.5, marginTop: 6 },
    bio: { flexDirection: 'row', alignItems: 'center', gap: 12 },
    bioIcon: {
      width: 36,
      height: 36,
      borderRadius: 11,
      backgroundColor: `${t.accent}1a`,
      borderColor: `${t.accent}55`,
      borderWidth: StyleSheet.hairlineWidth,
      alignItems: 'center',
      justifyContent: 'center',
    },
    bioText: { flex: 1 },
    bioTitle: { color: t.text, fontSize: 13.5, fontWeight: '700' },
    bioSub: { color: t.text3, fontSize: 11, marginTop: 2 },
    toggleOn: {
      width: 36,
      height: 22,
      borderRadius: 11,
      backgroundColor: t.accent,
      justifyContent: 'center',
      alignItems: 'flex-end',
      paddingHorizontal: 2,
    },
    toggleKnob: { width: 18, height: 18, borderRadius: 9, backgroundColor: '#fff' },
  });
}
