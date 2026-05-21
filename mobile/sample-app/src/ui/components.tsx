import React, { useMemo } from 'react';
import {
  View,
  Text,
  Pressable,
  StyleSheet,
  ScrollView,
  SafeAreaView,
  StatusBar,
} from 'react-native';
import type { StyleProp, ViewStyle, TextStyle } from 'react-native';
import { radius, spacing, chainColors, fontFamily } from './tokens';
import type { ThemeTokens } from './tokens';
import { useTheme } from './theme-context';
import { Icon } from './Icon';
import type { IconName } from './Icon';

// Reusable primitives composing every Trine Signer screen. Each primitive
// reads the active theme via useTheme() so a runtime theme switch updates
// surface colors, accents and text contrast without a remount.

export interface ScreenProps {
  readonly children?: React.ReactNode;
  readonly scroll?: boolean;
  readonly footer?: React.ReactNode;
  readonly contentStyle?: StyleProp<ViewStyle>;
}

export function Screen({ children, scroll = true, footer, contentStyle }: ScreenProps): React.JSX.Element {
  const t = useTheme();
  const s = useMemo(() => screenStyles(t), [t]);
  const body = scroll ? (
    <ScrollView
      style={s.flex1}
      contentContainerStyle={[s.scrollContent, contentStyle]}
      showsVerticalScrollIndicator={false}
    >
      {children}
    </ScrollView>
  ) : (
    <View style={[s.flex1, contentStyle]}>{children}</View>
  );
  return (
    <SafeAreaView style={s.screen}>
      <StatusBar barStyle="light-content" backgroundColor={t.bg} />
      {body}
      {footer ? <View style={s.footer}>{footer}</View> : null}
    </SafeAreaView>
  );
}

export interface TopBarProps {
  readonly title?: string;
  readonly onBack?: () => void;
  readonly right?: React.ReactNode;
  readonly leftAccessory?: React.ReactNode;
  readonly backLabel?: string;
}

export function TopBar({ title, onBack, right, leftAccessory, backLabel }: TopBarProps): React.JSX.Element {
  const t = useTheme();
  const s = useMemo(() => topBarStyles(t), [t]);
  return (
    <View style={s.topBar}>
      {onBack ? (
        <Pressable
          onPress={onBack}
          accessibilityRole="button"
          accessibilityLabel={backLabel ?? 'Back'}
          style={({ pressed }) => [s.iconBtn, pressed ? s.pressed : null]}
        >
          <Icon name="chevronL" size={18} color={t.text} />
        </Pressable>
      ) : leftAccessory ? (
        <View style={s.topBarLeft}>{leftAccessory}</View>
      ) : (
        <View style={s.topBarSpacer} />
      )}
      <View style={s.topBarTitleBox}>
        {title ? <Text style={s.topBarTitle}>{title}</Text> : null}
      </View>
      <View style={s.topBarRight}>{right}</View>
    </View>
  );
}

export interface CardProps {
  readonly children?: React.ReactNode;
  readonly style?: StyleProp<ViewStyle>;
  readonly onPress?: () => void;
  readonly danger?: boolean;
}

export function Card({ children, style, onPress, danger }: CardProps): React.JSX.Element {
  const t = useTheme();
  const s = useMemo(() => cardStyles(t), [t]);
  const composed: StyleProp<ViewStyle> = [s.card, danger ? s.cardDanger : null, style];
  if (onPress) {
    return (
      <Pressable onPress={onPress} style={({ pressed }) => [composed, pressed ? s.cardPressed : null]}>
        {children}
      </Pressable>
    );
  }
  return <View style={composed}>{children}</View>;
}

export interface PrimaryButtonProps {
  readonly label: string;
  readonly onPress?: () => void;
  readonly disabled?: boolean;
  readonly style?: StyleProp<ViewStyle>;
}

export function PrimaryButton({ label, onPress, disabled, style }: PrimaryButtonProps): React.JSX.Element {
  const t = useTheme();
  const s = useMemo(() => buttonStyles(t), [t]);
  return (
    <Pressable
      accessibilityRole="button"
      onPress={onPress}
      disabled={disabled}
      style={({ pressed }) => [
        s.primaryBtn,
        disabled ? s.primaryBtnDisabled : null,
        pressed && !disabled ? s.primaryBtnPressed : null,
        style,
      ]}
    >
      <Text style={[s.primaryBtnText, disabled ? s.primaryBtnTextDisabled : null]}>{label}</Text>
    </Pressable>
  );
}

export interface GhostButtonProps {
  readonly label: string;
  readonly onPress?: () => void;
  readonly icon?: IconName;
  readonly style?: StyleProp<ViewStyle>;
}

export function GhostButton({ label, onPress, icon, style }: GhostButtonProps): React.JSX.Element {
  const t = useTheme();
  const s = useMemo(() => buttonStyles(t), [t]);
  return (
    <Pressable
      accessibilityRole="button"
      onPress={onPress}
      style={({ pressed }) => [s.ghostBtn, pressed ? s.ghostBtnPressed : null, style]}
    >
      {icon ? <Icon name={icon} size={14} color={t.text} style={s.ghostBtnIcon} /> : null}
      <Text style={s.ghostBtnText}>{label}</Text>
    </Pressable>
  );
}

export interface PillProps {
  readonly label: string;
  readonly active?: boolean;
  readonly tone?: 'default' | 'accent' | 'danger' | 'warn';
  readonly onPress?: () => void;
}

export function Pill({ label, active, tone = 'default', onPress }: PillProps): React.JSX.Element {
  const t = useTheme();
  const s = useMemo(() => pillStyles(t), [t]);
  const palette = pillPalette(t, tone, active);
  const Inner = (
    <View style={[s.pill, { backgroundColor: palette.bg, borderColor: palette.border }]}>
      <Text style={[s.pillText, { color: palette.fg }]}>{label}</Text>
    </View>
  );
  if (onPress) {
    return (
      <Pressable onPress={onPress} hitSlop={6}>
        {Inner}
      </Pressable>
    );
  }
  return Inner;
}

function pillPalette(
  t: ThemeTokens,
  tone: 'default' | 'accent' | 'danger' | 'warn',
  active?: boolean,
): { bg: string; border: string; fg: string } {
  if (active || tone === 'accent') {
    return { bg: `${t.accent}1a`, border: `${t.accent}55`, fg: t.accent };
  }
  if (tone === 'danger') {
    return { bg: `${t.danger}1a`, border: `${t.danger}55`, fg: t.danger };
  }
  if (tone === 'warn') {
    return { bg: `${t.warn}1a`, border: `${t.warn}55`, fg: t.warn };
  }
  return { bg: t.surface, border: t.hairline, fg: t.text2 };
}

export interface ChainBadgeProps {
  readonly chain: string;
  readonly label: string;
}

export function ChainBadge({ chain, label }: ChainBadgeProps): React.JSX.Element {
  const t = useTheme();
  const s = useMemo(() => chainBadgeStyles(t), [t]);
  const c = chainColors[chain] ?? t.text2;
  return (
    <View style={[s.chainBadge, { backgroundColor: `${c}24`, borderColor: `${c}55` }]}>
      <View style={[s.chainDot, { backgroundColor: c }]} />
      <Text style={[s.chainText, { color: c }]}>{label}</Text>
    </View>
  );
}

export interface KVProps {
  readonly k: string;
  readonly v?: string;
  readonly sub?: string;
  readonly mono?: boolean;
  readonly accent?: boolean;
  readonly onPress?: () => void;
}

export function KV({ k, v, sub, mono, accent, onPress }: KVProps): React.JSX.Element {
  const t = useTheme();
  const s = useMemo(() => kvStyles(t), [t]);
  const valueStyle: StyleProp<TextStyle> = [
    s.kvVal,
    accent ? { color: t.accent } : null,
    mono ? s.mono : null,
  ];
  const Inner = (
    <View style={s.kvRow}>
      <View style={s.kvLeft}>
        <Text style={s.kvLabel}>{k}</Text>
        {sub ? <Text style={s.kvSub}>{sub}</Text> : null}
      </View>
      {v ? (
        <Text style={valueStyle} numberOfLines={1} ellipsizeMode="middle">
          {v}
        </Text>
      ) : null}
      {onPress ? <Icon name="chevronR" size={14} color={t.text3} style={s.kvChevron} /> : null}
    </View>
  );
  if (onPress) {
    return (
      <Pressable onPress={onPress} style={({ pressed }) => (pressed ? s.pressed : undefined)}>
        {Inner}
      </Pressable>
    );
  }
  return Inner;
}

export interface SectionLabelProps {
  readonly children: React.ReactNode;
}

export function SectionLabel({ children }: SectionLabelProps): React.JSX.Element {
  const t = useTheme();
  const s = useMemo(() => sectionStyles(t), [t]);
  return <Text style={s.sectionLabel}>{children}</Text>;
}

export function Hairline(): React.JSX.Element {
  const t = useTheme();
  return <View style={{ height: StyleSheet.hairlineWidth, backgroundColor: t.hairline }} />;
}

export interface BadgeProps {
  readonly count: number;
  readonly color?: string;
}

export function Badge({ count, color }: BadgeProps): React.JSX.Element | null {
  const t = useTheme();
  if (count <= 0) return null;
  const bg = color ?? t.warn;
  return (
    <View style={[badgeStyles.badge, { backgroundColor: bg }]}>
      <Text style={[badgeStyles.badgeText, { color: t.bg }]}>{count > 99 ? '99+' : String(count)}</Text>
    </View>
  );
}

const badgeStyles = StyleSheet.create({
  badge: {
    minWidth: 16,
    height: 16,
    paddingHorizontal: 4,
    borderRadius: 8,
    alignItems: 'center',
    justifyContent: 'center',
  },
  badgeText: { fontSize: 9.5, fontWeight: '700' },
});

function screenStyles(t: ThemeTokens) {
  return StyleSheet.create({
    flex1: { flex: 1 },
    screen: { flex: 1, backgroundColor: t.bg },
    scrollContent: { paddingBottom: 120 },
    footer: {
      paddingHorizontal: spacing.xl,
      paddingTop: spacing.md,
      paddingBottom: spacing.xl,
      backgroundColor: t.bg,
      borderTopColor: t.hairline,
      borderTopWidth: StyleSheet.hairlineWidth,
    },
  });
}

function topBarStyles(t: ThemeTokens) {
  return StyleSheet.create({
    topBar: {
      paddingTop: spacing.lg,
      paddingHorizontal: spacing.lg,
      paddingBottom: spacing.sm,
      flexDirection: 'row',
      alignItems: 'center',
      justifyContent: 'space-between',
      minHeight: 52,
    },
    topBarLeft: { flexDirection: 'row', alignItems: 'center', gap: spacing.sm },
    topBarSpacer: { width: 36 },
    topBarTitleBox: { flex: 1, alignItems: 'center', paddingHorizontal: spacing.sm },
    topBarTitle: { color: t.text, fontSize: 16, fontWeight: '600', letterSpacing: 0.3 },
    topBarRight: { minWidth: 36, alignItems: 'flex-end' },
    iconBtn: {
      width: 36,
      height: 36,
      borderRadius: 18,
      backgroundColor: t.surface,
      borderColor: t.hairline,
      borderWidth: StyleSheet.hairlineWidth,
      alignItems: 'center',
      justifyContent: 'center',
    },
    pressed: { opacity: 0.7 },
  });
}

function cardStyles(t: ThemeTokens) {
  return StyleSheet.create({
    card: {
      backgroundColor: t.surface,
      borderRadius: radius.xl,
      borderColor: t.hairline,
      borderWidth: StyleSheet.hairlineWidth,
      padding: spacing.md,
    },
    cardDanger: { borderColor: `${t.danger}55` },
    cardPressed: { opacity: 0.85 },
  });
}

function buttonStyles(t: ThemeTokens) {
  return StyleSheet.create({
    primaryBtn: {
      height: 54,
      borderRadius: radius.lg,
      backgroundColor: t.accent,
      alignItems: 'center',
      justifyContent: 'center',
      paddingHorizontal: spacing.lg,
    },
    primaryBtnPressed: { opacity: 0.92 },
    primaryBtnDisabled: { backgroundColor: t.surface2 },
    primaryBtnText: { color: t.bg, fontSize: 16, fontWeight: '700', letterSpacing: 0.3 },
    primaryBtnTextDisabled: { color: t.text3 },
    ghostBtn: {
      height: 50,
      borderRadius: radius.lg,
      backgroundColor: t.surface,
      borderColor: t.hairline,
      borderWidth: StyleSheet.hairlineWidth,
      flexDirection: 'row',
      alignItems: 'center',
      justifyContent: 'center',
      paddingHorizontal: spacing.lg,
      gap: spacing.sm,
    },
    ghostBtnPressed: { opacity: 0.7 },
    ghostBtnIcon: { marginRight: 2 },
    ghostBtnText: { color: t.text, fontSize: 15, fontWeight: '600', letterSpacing: 0.2 },
  });
}

function pillStyles(_t: ThemeTokens) {
  return StyleSheet.create({
    pill: {
      paddingHorizontal: 12,
      paddingVertical: 6,
      borderRadius: radius.pill,
      borderWidth: StyleSheet.hairlineWidth,
    },
    pillText: { fontSize: 11.5, fontWeight: '600', letterSpacing: 0.3 },
  });
}

function chainBadgeStyles(_t: ThemeTokens) {
  return StyleSheet.create({
    chainBadge: {
      flexDirection: 'row',
      alignItems: 'center',
      paddingHorizontal: 9,
      paddingVertical: 3,
      borderRadius: radius.xs,
      borderWidth: StyleSheet.hairlineWidth,
      gap: 5,
      alignSelf: 'flex-start',
    },
    chainDot: { width: 6, height: 6, borderRadius: 3 },
    chainText: { fontSize: 11, fontWeight: '700', letterSpacing: 0.4 },
  });
}

function kvStyles(t: ThemeTokens) {
  return StyleSheet.create({
    kvRow: { flexDirection: 'row', alignItems: 'center', paddingVertical: 12, gap: spacing.md },
    kvLeft: { flex: 1 },
    kvLabel: { color: t.text, fontSize: 13.5, fontWeight: '500' },
    kvSub: { color: t.text3, fontSize: 10.5, marginTop: 3, lineHeight: 14 },
    kvVal: { color: t.text3, fontSize: 12, maxWidth: 180, textAlign: 'right' },
    kvChevron: { marginLeft: 4 },
    mono: { fontFamily: fontFamily.mono },
    pressed: { opacity: 0.7 },
  });
}

function sectionStyles(t: ThemeTokens) {
  return StyleSheet.create({
    sectionLabel: {
      color: t.text2,
      fontSize: 11.5,
      fontWeight: '700',
      letterSpacing: 0.6,
      textTransform: 'uppercase',
      marginBottom: 8,
      marginTop: 18,
      paddingHorizontal: 4,
    },
  });
}
