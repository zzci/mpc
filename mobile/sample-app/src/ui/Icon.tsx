import React from 'react';
import { Text, StyleSheet } from 'react-native';
import type { TextStyle } from 'react-native';

// Lightweight glyph icon set. The design handoff uses SVG line icons; this
// sample-app stays dependency-free, so each design icon maps to a small
// unicode glyph. Replace with an SVG library in a later L3 if pixel parity
// is required.

export type IconName =
  | 'chevronR'
  | 'chevronL'
  | 'chevronD'
  | 'close'
  | 'plus'
  | 'copy'
  | 'shield'
  | 'check'
  | 'warn'
  | 'info'
  | 'clock'
  | 'inbox'
  | 'wallet'
  | 'list'
  | 'cog'
  | 'qr'
  | 'refresh'
  | 'key'
  | 'link'
  | 'more';

const glyphs: Record<IconName, string> = {
  chevronR: '›',
  chevronL: '‹',
  chevronD: '˅',
  close: '✕',
  plus: '＋',
  copy: '⧉',
  shield: '⛨',
  check: '✓',
  warn: '⚠',
  info: 'ⓘ',
  clock: '⧖',
  inbox: '▤',
  wallet: '▣',
  list: '☰',
  cog: '⚙',
  qr: '▦',
  refresh: '↻',
  key: '⚷',
  link: '⧉',
  more: '⋯',
};

export interface IconProps {
  readonly name: IconName;
  readonly size?: number;
  readonly color?: string;
  readonly style?: TextStyle;
}

export function Icon({ name, size = 16, color, style }: IconProps): React.JSX.Element {
  const fallback = color ?? '#FFFFFF';
  return (
    <Text style={[styles.base, { fontSize: size, color: fallback, lineHeight: size + 2 }, style]}>
      {glyphs[name]}
    </Text>
  );
}

const styles = StyleSheet.create({
  base: {
    textAlign: 'center',
  },
});
