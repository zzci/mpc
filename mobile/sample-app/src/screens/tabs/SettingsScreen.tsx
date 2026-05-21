import React, { useMemo } from 'react';
import { View, Text, StyleSheet, Pressable } from 'react-native';
import {
  Card,
  Hairline,
  KV,
  Screen,
  TopBar,
  useTheme,
  useThemeControls,
  spacing,
  radius,
  fontFamily,
} from '../../ui';
import type { ThemeName, ThemeTokens } from '../../ui';
import { useI18n } from '../../i18n';
import type { Locale, Strings } from '../../i18n';
import { COORD, MEMBER, SETTINGS, WALLETS } from '../../data';
import type { SettingsAction, SettingsRow, SettingsRowKey } from '../../data';

export interface SettingsScreenProps {
  readonly onAction?: (action: SettingsAction) => void;
}

export function SettingsScreen({ onAction }: SettingsScreenProps): React.JSX.Element {
  const t = useTheme();
  const { t: T, tx, locale, setLocale, locales } = useI18n();
  const { themeName, setThemeName, availableThemes } = useThemeControls();
  const s = useMemo(() => makeStyles(t), [t]);
  const totalParties = WALLETS.reduce((sum, w) => sum + w.parties, 0);
  return (
    <Screen>
      <TopBar title={T.settings.title} />
      <View style={s.body}>
        <Card style={s.identity}>
          <View style={s.identityAvatar}>
            <Text style={s.identityAvatarText}>{MEMBER.memberId}</Text>
          </View>
          <View style={s.identityText}>
            <Text style={s.identityDevice}>{MEMBER.device}</Text>
            <Text style={s.identitySub}>
              {tx(T.settings.memberOf, { count: WALLETS.length })}
              {' · '}
              {tx(T.settings.totalParties, { count: totalParties })}
            </Text>
          </View>
          <View style={s.identityCoord}>
            <View style={s.coordDot} />
            <Text style={s.coordText}>coord · {COORD.latencyMs}ms</Text>
          </View>
        </Card>

        <View style={s.section}>
          <Text style={s.sectionHeading}>{T.settings.sections.appearance.toUpperCase()}</Text>
          <Card style={s.sectionCard}>
            <SegmentRow
              label={T.settings.languageLabel}
              value={locale}
              options={locales.map((l) => ({ id: l, label: localeLabel(l, T) }))}
              onChange={setLocale}
            />
            <Hairline />
            <SegmentRow
              label={T.settings.themeLabel}
              value={themeName}
              options={availableThemes.map((n) => ({ id: n, label: themeLabel(n, T) }))}
              onChange={setThemeName}
            />
          </Card>
        </View>

        {SETTINGS.map((section) => (
          <View key={section.heading} style={s.section}>
            <Text style={s.sectionHeading}>{T.settings.sections[section.heading].toUpperCase()}</Text>
            <Card style={s.sectionCard}>
              {section.rows.map((row, i) => (
                <View key={row.key}>
                  <Row row={row} onAction={onAction} />
                  {i < section.rows.length - 1 ? <Hairline /> : null}
                </View>
              ))}
            </Card>
          </View>
        ))}
      </View>
    </Screen>
  );
}

interface RowProps {
  readonly row: SettingsRow;
  readonly onAction?: (action: SettingsAction) => void;
}

function Row({ row, onAction }: RowProps): React.JSX.Element {
  const { t: T, tx } = useI18n();
  const rows = T.settings.rows;
  const meta = rowMeta(row.key, rows, tx);
  const handler = row.action && onAction ? () => onAction(row.action!) : undefined;
  const value = row.value ?? meta.fixedValue;
  return (
    <KV
      k={meta.label}
      v={value}
      sub={meta.sub}
      mono={row.mono}
      accent={row.accent}
      onPress={handler}
    />
  );
}

interface RowMeta {
  readonly label: string;
  readonly sub?: string;
  readonly fixedValue?: string;
}

function rowMeta(
  key: SettingsRowKey,
  rows: Strings['settings']['rows'],
  tx: (template: string, params?: Readonly<Record<string, string | number>>) => string,
): RowMeta {
  switch (key) {
    case 'memberId':
      return { label: rows.memberId };
    case 'device':
      return { label: rows.device };
    case 'identityPub':
      return { label: rows.identityPub, sub: rows.identityPubSub };
    case 'coordEndpoint':
      return {
        label: rows.coordEndpoint,
        sub: tx(rows.coordSub, { tls: COORD.tls, when: COORD.lastHeartbeat }),
      };
    case 'relayPeer':
      return { label: rows.relayPeer };
    case 'dispatch':
      return { label: rows.dispatch, sub: rows.dispatchSub, fixedValue: rows.dispatchValue };
    case 'exportShare':
      return { label: rows.exportShare, sub: rows.exportShareSub };
    case 'importShare':
      return { label: rows.importShare };
    case 'changeKek':
      return { label: rows.changeKek };
    case 'reshare':
      return { label: rows.reshare, sub: rows.reshareSub };
    case 'keygen':
      return { label: rows.keygen, sub: rows.keygenSub };
    case 'faceId':
      return { label: rows.faceId, fixedValue: rows.faceIdValue };
    case 'zeroize':
      return { label: rows.zeroize, fixedValue: rows.zeroizeValue };
    case 'strictWysiwys':
      return {
        label: rows.strictWysiwys,
        fixedValue: rows.strictWysiwysValue,
        sub: rows.strictWysiwysSub,
      };
    case 'rerunOnboarding':
      return { label: rows.rerunOnboarding, sub: rows.rerunOnboardingSub };
    case 'attestLog':
      return { label: rows.attestLog };
    case 'diagnostics':
      return { label: rows.diagnostics };
    case 'about':
      return { label: rows.about, fixedValue: rows.aboutValue };
  }
}

function localeLabel(l: Locale, T: Strings): string {
  if (l === 'en') return T.settings.languageEn;
  return T.settings.languageZh;
}

function themeLabel(name: ThemeName, T: Strings): string {
  if (name === 'midnight') return T.settings.themeMidnight;
  return T.settings.themeOnyx;
}

interface SegmentOption<T extends string> {
  readonly id: T;
  readonly label: string;
}

interface SegmentRowProps<T extends string> {
  readonly label: string;
  readonly value: T;
  readonly options: ReadonlyArray<SegmentOption<T>>;
  readonly onChange: (next: T) => void;
}

function SegmentRow<T extends string>({ label, value, options, onChange }: SegmentRowProps<T>): React.JSX.Element {
  const t = useTheme();
  const s = useMemo(() => segmentStyles(t), [t]);
  return (
    <View style={s.row}>
      <Text style={s.label}>{label}</Text>
      <View style={s.group}>
        {options.map((opt) => {
          const on = opt.id === value;
          return (
            <Pressable
              key={opt.id}
              onPress={() => onChange(opt.id)}
              accessibilityRole="button"
              accessibilityState={{ selected: on }}
              accessibilityLabel={opt.label}
              style={({ pressed }) => [
                s.option,
                on ? s.optionOn : null,
                pressed ? s.pressed : null,
              ]}
            >
              <Text style={[s.optionLabel, on ? s.optionLabelOn : null]}>{opt.label}</Text>
            </Pressable>
          );
        })}
      </View>
    </View>
  );
}

function makeStyles(t: ThemeTokens) {
  return StyleSheet.create({
    body: { paddingHorizontal: spacing.lg, paddingTop: spacing.xs },
    identity: {
      flexDirection: 'row',
      alignItems: 'center',
      gap: 14,
      padding: spacing.lg,
      marginBottom: 18,
    },
    identityAvatar: {
      width: 50,
      height: 50,
      borderRadius: 14,
      backgroundColor: t.accent,
      alignItems: 'center',
      justifyContent: 'center',
    },
    identityAvatarText: {
      color: t.bg,
      fontSize: 18,
      fontWeight: '700',
      letterSpacing: -0.3,
      fontFamily: fontFamily.mono,
    },
    identityText: { flex: 1, minWidth: 0 },
    identityDevice: { color: t.text, fontSize: 15, fontWeight: '700' },
    identitySub: { color: t.text3, fontSize: 11.5, marginTop: 3 },
    identityCoord: {
      flexDirection: 'row',
      alignItems: 'center',
      gap: 6,
      paddingHorizontal: 10,
      paddingVertical: 4,
      borderRadius: radius.pill,
      backgroundColor: t.surface2,
      borderColor: t.hairline,
      borderWidth: StyleSheet.hairlineWidth,
    },
    coordDot: { width: 7, height: 7, borderRadius: 4, backgroundColor: t.accent },
    coordText: { color: t.text2, fontSize: 11, fontWeight: '500' },
    section: { marginBottom: 16 },
    sectionHeading: {
      color: t.text2,
      fontSize: 11.5,
      fontWeight: '700',
      letterSpacing: 0.6,
      marginBottom: 8,
      paddingHorizontal: 4,
    },
    sectionCard: { padding: 0, paddingHorizontal: spacing.md },
  });
}

function segmentStyles(t: ThemeTokens) {
  return StyleSheet.create({
    row: {
      flexDirection: 'row',
      alignItems: 'center',
      paddingVertical: 12,
      gap: spacing.md,
    },
    label: { flex: 1, color: t.text, fontSize: 13.5, fontWeight: '500' },
    group: {
      flexDirection: 'row',
      backgroundColor: t.surface2,
      borderRadius: 10,
      padding: 3,
      borderWidth: StyleSheet.hairlineWidth,
      borderColor: t.hairline,
    },
    option: {
      paddingHorizontal: 12,
      paddingVertical: 6,
      borderRadius: 7,
    },
    optionOn: { backgroundColor: t.surface },
    pressed: { opacity: 0.7 },
    optionLabel: { color: t.text2, fontSize: 12, fontWeight: '600', letterSpacing: 0.2 },
    optionLabelOn: { color: t.accent },
  });
}
