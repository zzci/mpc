// Trine Signer design tokens. Two production-grade dark themes — midnight
// (teal accent over deep navy) and onyx (warm amber accent over near-black)
// — both adapted from the read-only handoff at
// design/tss-mpc/project/theme.jsx. The runtime theme is picked through the
// ThemeProvider in ui/theme-context.tsx so users can toggle at runtime.

export interface ThemeTokens {
  readonly name: ThemeName;
  readonly bg: string;
  readonly bgGrad: string;
  readonly surface: string;
  readonly surface2: string;
  readonly hairline: string;
  readonly text: string;
  readonly text2: string;
  readonly text3: string;
  readonly danger: string;
  readonly warn: string;
  readonly ok: string;
  readonly accent: string;
}

export const THEME_NAMES = ['midnight', 'onyx'] as const;
export type ThemeName = (typeof THEME_NAMES)[number];

const midnightAccent = '#5EEAD4';
const onyxAccent = '#F7C25A';

export const themes: Record<ThemeName, ThemeTokens> = {
  midnight: {
    name: 'midnight',
    bg: '#0A0F1C',
    bgGrad: '#0A0F1C',
    surface: '#121829',
    surface2: '#1A2236',
    hairline: 'rgba(255,255,255,0.07)',
    text: '#F5F7FA',
    text2: 'rgba(245,247,250,0.62)',
    text3: 'rgba(245,247,250,0.38)',
    danger: '#FF6E6E',
    warn: '#F7C25A',
    ok: midnightAccent,
    accent: midnightAccent,
  },
  onyx: {
    name: 'onyx',
    bg: '#06070A',
    bgGrad: '#06070A',
    surface: '#0F1116',
    surface2: '#171A22',
    hairline: 'rgba(255,255,255,0.06)',
    text: '#FFFFFF',
    text2: 'rgba(255,255,255,0.6)',
    text3: 'rgba(255,255,255,0.36)',
    danger: '#FF7E6E',
    warn: '#FFD27A',
    ok: onyxAccent,
    accent: onyxAccent,
  },
};

export const radius = {
  xs: 6,
  sm: 9,
  md: 12,
  lg: 14,
  xl: 18,
  pill: 99,
} as const;

export const spacing = {
  xs: 4,
  sm: 8,
  md: 12,
  lg: 16,
  xl: 22,
  xxl: 32,
} as const;

export const fontFamily = {
  text: undefined,
  mono: 'Menlo',
} as const;

export const chainColors: Record<string, string> = {
  'eip155:1': '#627EEA',
  'eip155:42161': '#28A0F0',
  tron: '#EE3F47',
};
