import React from 'react';
import { View, StyleSheet } from 'react-native';
import { useTheme } from './theme-context';

// View-only Trine mark: an equilateral triangle outline with three nodes,
// approximating design/tss-mpc/project/theme.jsx TrineMark using only RN
// primitives (no SVG dependency).

export interface TrineMarkProps {
  readonly size?: number;
  readonly color?: string;
  readonly glow?: boolean;
}

export function TrineMark({ size = 28, color, glow = false }: TrineMarkProps): React.JSX.Element {
  const t = useTheme();
  const resolved = color ?? t.accent;
  const node = Math.max(4, Math.round(size * 0.18));
  const triH = size * 0.86;
  const triW = size;
  return (
    <View style={[styles.box, { width: size, height: size }]}>
      <View
        style={[
          styles.triangle,
          {
            borderLeftWidth: triW / 2,
            borderRightWidth: triW / 2,
            borderBottomWidth: triH,
            borderBottomColor: glow ? `${resolved}22` : 'transparent',
          },
        ]}
      />
      <View
        style={[
          styles.triangleOutline,
          {
            borderLeftWidth: triW / 2,
            borderRightWidth: triW / 2,
            borderBottomWidth: triH,
            borderBottomColor: resolved,
            opacity: 0.9,
            transform: [{ scale: 0.92 }],
          },
        ]}
      />
      <View
        style={[
          styles.node,
          { width: node, height: node, borderRadius: node / 2, backgroundColor: resolved, top: 0, left: size / 2 - node / 2 },
        ]}
      />
      <View
        style={[
          styles.node,
          {
            width: node,
            height: node,
            borderRadius: node / 2,
            backgroundColor: resolved,
            bottom: 0,
            left: -node / 2 + size * 0.04,
          },
        ]}
      />
      <View
        style={[
          styles.node,
          {
            width: node,
            height: node,
            borderRadius: node / 2,
            backgroundColor: resolved,
            bottom: 0,
            right: -node / 2 + size * 0.04,
          },
        ]}
      />
    </View>
  );
}

const styles = StyleSheet.create({
  box: {
    position: 'relative',
    alignItems: 'center',
    justifyContent: 'center',
  },
  triangle: {
    position: 'absolute',
    width: 0,
    height: 0,
    borderStyle: 'solid',
    borderLeftColor: 'transparent',
    borderRightColor: 'transparent',
  },
  triangleOutline: {
    position: 'absolute',
    width: 0,
    height: 0,
    borderStyle: 'solid',
    borderLeftColor: 'transparent',
    borderRightColor: 'transparent',
    opacity: 0.6,
  },
  node: {
    position: 'absolute',
  },
});
