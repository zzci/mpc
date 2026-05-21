import React, { createContext, useContext, useMemo, useState } from 'react';
import { THEME_NAMES, themes } from './tokens';
import type { ThemeName, ThemeTokens } from './tokens';

export interface ThemeContextValue {
  readonly theme: ThemeTokens;
  readonly themeName: ThemeName;
  readonly setThemeName: (name: ThemeName) => void;
  readonly availableThemes: ReadonlyArray<ThemeName>;
}

const ThemeContext = createContext<ThemeContextValue | null>(null);

export interface ThemeProviderProps {
  readonly initial?: ThemeName;
  readonly children: React.ReactNode;
}

export function ThemeProvider({ initial = 'midnight', children }: ThemeProviderProps): React.JSX.Element {
  const [themeName, setThemeName] = useState<ThemeName>(initial);
  const value = useMemo<ThemeContextValue>(
    () => ({
      theme: themes[themeName],
      themeName,
      setThemeName,
      availableThemes: THEME_NAMES,
    }),
    [themeName],
  );
  return <ThemeContext.Provider value={value}>{children}</ThemeContext.Provider>;
}

export function useTheme(): ThemeTokens {
  const value = useContext(ThemeContext);
  if (!value) {
    throw new Error('useTheme must be used inside <ThemeProvider>');
  }
  return value.theme;
}

export function useThemeControls(): ThemeContextValue {
  const value = useContext(ThemeContext);
  if (!value) {
    throw new Error('useThemeControls must be used inside <ThemeProvider>');
  }
  return value;
}
