import React, { useMemo } from 'react';
import { View, Text, Pressable, StyleSheet, ScrollView } from 'react-native';
import { Icon, PrimaryButton, useTheme, spacing } from '../../ui';
import type { ThemeTokens } from '../../ui';
import { useI18n } from '../../i18n';

// Shared step shell for every onboarding screen. Matches the design handoff
// layout (design/tss-mpc/project/screens-init.jsx InitShell): header with
// step indicator, scrollable body, sticky primary footer.

export interface InitShellProps {
  readonly step: number;
  readonly total: number;
  readonly title: string;
  readonly subtitle?: string;
  readonly onBack?: () => void;
  readonly primaryLabel?: string;
  readonly onPrimary?: () => void;
  readonly primaryDisabled?: boolean;
  readonly primaryLoading?: boolean;
  readonly children?: React.ReactNode;
}

export function InitShell(props: InitShellProps): React.JSX.Element {
  const { step, total, title, subtitle, onBack, primaryLabel, onPrimary, primaryDisabled, primaryLoading } = props;
  const theme = useTheme();
  const { t: T, tx } = useI18n();
  const s = useMemo(() => makeStyles(theme), [theme]);
  return (
    <View style={s.root}>
      <View style={s.header}>
        {onBack ? (
          <Pressable
            accessibilityRole="button"
            accessibilityLabel={T.common.back}
            onPress={onBack}
            style={({ pressed }) => [s.backBtn, pressed ? s.backBtnPressed : null]}
          >
            <Icon name="chevronL" size={18} color={theme.text} />
          </Pressable>
        ) : (
          <View style={s.backPlaceholder} />
        )}
        <Text style={s.stepLabel}>{tx(T.common.stepOf, { step, total })}</Text>
        <View style={s.backPlaceholder} />
      </View>

      <View style={s.progress}>
        {Array.from({ length: total }).map((_, i) => {
          const done = i < step - 1;
          const active = i === step - 1;
          return (
            <View key={i} style={s.progressTrack}>
              <View
                style={[
                  s.progressFill,
                  { width: done || active ? '100%' : '0%' },
                  active ? s.progressActive : null,
                ]}
              />
            </View>
          );
        })}
      </View>

      <View style={s.titleBlock}>
        <Text style={s.title}>{title}</Text>
        {subtitle ? <Text style={s.subtitle}>{subtitle}</Text> : null}
      </View>

      <ScrollView style={s.body} contentContainerStyle={s.bodyContent} showsVerticalScrollIndicator={false}>
        {props.children}
      </ScrollView>

      {onPrimary ? (
        <View style={s.footer}>
          <PrimaryButton
            label={primaryLoading ? T.common.pleaseWait : primaryLabel ?? T.common.continue}
            onPress={onPrimary}
            disabled={primaryDisabled || primaryLoading}
          />
        </View>
      ) : null}
    </View>
  );
}

function makeStyles(t: ThemeTokens) {
  return StyleSheet.create({
    root: { flex: 1, backgroundColor: t.bg },
    header: {
      flexDirection: 'row',
      alignItems: 'center',
      justifyContent: 'space-between',
      paddingTop: spacing.xl,
      paddingHorizontal: spacing.lg,
      paddingBottom: spacing.xs,
    },
    backBtn: {
      width: 36,
      height: 36,
      borderRadius: 18,
      backgroundColor: t.surface,
      alignItems: 'center',
      justifyContent: 'center',
      borderWidth: StyleSheet.hairlineWidth,
      borderColor: t.hairline,
    },
    backBtnPressed: { opacity: 0.7 },
    backPlaceholder: { width: 36 },
    stepLabel: { color: t.text2, fontSize: 12, fontWeight: '700', letterSpacing: 0.4 },
    progress: { flexDirection: 'row', gap: 5, paddingHorizontal: spacing.xl, paddingTop: 4 },
    progressTrack: {
      flex: 1,
      height: 3,
      borderRadius: 2,
      backgroundColor: t.surface2,
      overflow: 'hidden',
    },
    progressFill: { height: '100%', backgroundColor: t.accent },
    progressActive: { backgroundColor: t.accent },
    titleBlock: { paddingHorizontal: spacing.xl, paddingTop: spacing.lg },
    title: { color: t.text, fontSize: 24, fontWeight: '700', letterSpacing: -0.4, lineHeight: 30 },
    subtitle: { color: t.text2, fontSize: 13.5, lineHeight: 21, marginTop: 8 },
    body: { flex: 1, marginTop: spacing.md },
    bodyContent: { paddingHorizontal: spacing.xl, paddingBottom: 24 },
    footer: {
      paddingHorizontal: spacing.xl,
      paddingTop: spacing.md,
      paddingBottom: spacing.xl,
      borderTopColor: t.hairline,
      borderTopWidth: StyleSheet.hairlineWidth,
    },
  });
}
