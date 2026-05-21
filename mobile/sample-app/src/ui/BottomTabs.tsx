import React, { useMemo } from 'react';
import { View, Text, Pressable, StyleSheet } from 'react-native';
import { spacing, radius } from './tokens';
import type { ThemeTokens } from './tokens';
import { useTheme } from './theme-context';
import { Icon } from './Icon';
import type { IconName } from './Icon';
import { Badge } from './components';

export type TabId = 'inbox' | 'wallets' | 'audit' | 'settings';

export interface TabSpec {
  readonly id: TabId;
  readonly label: string;
  readonly icon: IconName;
}

export interface BottomTabsProps {
  readonly active: TabId;
  readonly onChange: (id: TabId) => void;
  readonly inboxBadge?: number;
  readonly tabs: ReadonlyArray<TabSpec>;
}

export function BottomTabs({ active, onChange, inboxBadge = 0, tabs }: BottomTabsProps): React.JSX.Element {
  const t = useTheme();
  const s = useMemo(() => makeStyles(t), [t]);
  return (
    <View style={s.wrap}>
      <View style={s.bar}>
        {tabs.map((tab) => {
          const on = active === tab.id;
          return (
            <Pressable
              key={tab.id}
              onPress={() => onChange(tab.id)}
              accessibilityRole="tab"
              accessibilityState={{ selected: on }}
              accessibilityLabel={tab.label}
              style={s.item}
            >
              <View style={s.iconWrap}>
                <Icon name={tab.icon} size={20} color={on ? t.accent : t.text2} />
                {tab.id === 'inbox' && inboxBadge > 0 ? (
                  <View style={s.badgeAbs}>
                    <Badge count={inboxBadge} />
                  </View>
                ) : null}
              </View>
              <Text style={[s.label, on ? s.labelOn : null]}>{tab.label}</Text>
            </Pressable>
          );
        })}
      </View>
    </View>
  );
}

function makeStyles(t: ThemeTokens) {
  return StyleSheet.create({
    wrap: {
      paddingHorizontal: spacing.md,
      paddingBottom: 18,
      paddingTop: 6,
      backgroundColor: t.bg,
    },
    bar: {
      flexDirection: 'row',
      backgroundColor: t.surface,
      borderRadius: radius.xl,
      borderColor: t.hairline,
      borderWidth: StyleSheet.hairlineWidth,
      paddingVertical: 8,
      paddingHorizontal: 4,
    },
    item: {
      flex: 1,
      alignItems: 'center',
      justifyContent: 'center',
      paddingVertical: 6,
      gap: 2,
    },
    iconWrap: { position: 'relative' },
    badgeAbs: { position: 'absolute', top: -6, right: -10 },
    label: { color: t.text2, fontSize: 10.5, fontWeight: '500', letterSpacing: 0.4 },
    labelOn: { color: t.accent, fontWeight: '700' },
  });
}
