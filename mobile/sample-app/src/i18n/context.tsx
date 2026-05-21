import React, { createContext, useContext, useMemo, useState } from 'react';
import type { Locale, Strings } from './strings';
import { LOCALES, STRINGS, format } from './strings';

export interface I18nContextValue {
  readonly locale: Locale;
  readonly t: Strings;
  readonly setLocale: (next: Locale) => void;
  readonly tx: (template: string, params?: Readonly<Record<string, string | number>>) => string;
  readonly locales: ReadonlyArray<Locale>;
}

const I18nContext = createContext<I18nContextValue | null>(null);

export interface I18nProviderProps {
  readonly initial?: Locale;
  readonly children: React.ReactNode;
}

export function I18nProvider({ initial = 'zh', children }: I18nProviderProps): React.JSX.Element {
  const [locale, setLocale] = useState<Locale>(initial);
  const value = useMemo<I18nContextValue>(
    () => ({
      locale,
      t: STRINGS[locale],
      setLocale,
      tx: (template, params) => (params ? format(template, params) : template),
      locales: LOCALES,
    }),
    [locale],
  );
  return <I18nContext.Provider value={value}>{children}</I18nContext.Provider>;
}

export function useI18n(): I18nContextValue {
  const value = useContext(I18nContext);
  if (!value) {
    throw new Error('useI18n must be used inside <I18nProvider>');
  }
  return value;
}
