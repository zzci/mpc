import React, { useMemo } from 'react';
import { View, Text, StyleSheet, Pressable } from 'react-native';
import { GhostButton, Icon, useTheme, spacing, radius, fontFamily } from '../../ui';
import type { ThemeTokens } from '../../ui';
import { useI18n } from '../../i18n';
import { InitShell } from './InitShell';

export interface InitCoordScreenProps {
  readonly onBack?: () => void;
  readonly onNext?: () => void;
}

export function InitCoordScreen({ onBack, onNext }: InitCoordScreenProps): React.JSX.Element {
  const t = useTheme();
  const { t: T } = useI18n();
  const s = useMemo(() => makeStyles(t), [t]);
  return (
    <InitShell
      step={2}
      total={5}
      title={T.onboarding.coordTitle}
      subtitle={T.onboarding.coordSubtitle}
      onBack={onBack}
    >
      <View style={s.viewfinder}>
        <View style={s.cameraField}>
          <Text style={s.cameraHint}>{T.onboarding.cameraHint}</Text>
        </View>
        {([
          { tl: true },
          { tr: true },
          { bl: true },
          { br: true },
        ] as ReadonlyArray<{ tl?: boolean; tr?: boolean; bl?: boolean; br?: boolean }>).map(
          (c, i) => (
            <View
              key={i}
              style={[
                s.corner,
                c.tl ? s.cornerTL : null,
                c.tr ? s.cornerTR : null,
                c.bl ? s.cornerBL : null,
                c.br ? s.cornerBR : null,
              ]}
            />
          ),
        )}
      </View>

      <View style={s.actionsRow}>
        <GhostButton label={T.onboarding.fromAlbum} icon="qr" style={s.flex} onPress={() => {}} />
        <Pressable
          accessibilityRole="button"
          accessibilityLabel={T.onboarding.enterManually}
          onPress={onNext}
          style={({ pressed }) => [s.manualBtn, pressed ? s.manualBtnPressed : null]}
        >
          <Text style={s.manualBtnText}>{T.onboarding.enterManually}</Text>
        </Pressable>
      </View>

      <View style={s.infoBox}>
        <Icon name="info" size={14} color={t.text3} />
        <Text style={s.infoText}>{T.onboarding.bootstrapNote}</Text>
      </View>
    </InitShell>
  );
}

function makeStyles(t: ThemeTokens) {
  return StyleSheet.create({
    viewfinder: {
      position: 'relative',
      width: '100%',
      aspectRatio: 1,
      borderRadius: 24,
      overflow: 'hidden',
      marginBottom: spacing.md,
      backgroundColor: '#000',
    },
    cameraField: {
      flex: 1,
      backgroundColor: '#0E1320',
      alignItems: 'center',
      justifyContent: 'center',
    },
    cameraHint: { color: t.text2, fontSize: 12, fontWeight: '600', letterSpacing: 0.5 },
    corner: { position: 'absolute', width: 34, height: 34, borderColor: '#fff' },
    cornerTL: { top: 18, left: 18, borderTopWidth: 2.5, borderLeftWidth: 2.5, borderTopLeftRadius: 10 },
    cornerTR: { top: 18, right: 18, borderTopWidth: 2.5, borderRightWidth: 2.5, borderTopRightRadius: 10 },
    cornerBL: { bottom: 18, left: 18, borderBottomWidth: 2.5, borderLeftWidth: 2.5, borderBottomLeftRadius: 10 },
    cornerBR: { bottom: 18, right: 18, borderBottomWidth: 2.5, borderRightWidth: 2.5, borderBottomRightRadius: 10 },
    actionsRow: { flexDirection: 'row', gap: 10 },
    flex: { flex: 1 },
    manualBtn: {
      flex: 1,
      height: 50,
      borderRadius: radius.lg,
      backgroundColor: t.surface,
      borderColor: t.hairline,
      borderWidth: StyleSheet.hairlineWidth,
      alignItems: 'center',
      justifyContent: 'center',
    },
    manualBtnPressed: { opacity: 0.7 },
    manualBtnText: { color: t.text, fontSize: 14, fontWeight: '600' },
    infoBox: {
      marginTop: 14,
      padding: 12,
      borderRadius: radius.md,
      backgroundColor: t.surface,
      borderColor: t.hairline,
      borderWidth: StyleSheet.hairlineWidth,
      flexDirection: 'row',
      alignItems: 'flex-start',
      gap: 8,
    },
    infoText: { flex: 1, color: t.text3, fontSize: 11, lineHeight: 16, fontFamily: fontFamily.text },
  });
}
