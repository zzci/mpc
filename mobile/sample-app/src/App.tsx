// Trine Signer sample app shell. Composes the two-theme dark visual system
// (midnight + onyx), the zh/en i18n provider, four bottom tabs (Inbox /
// Wallets / Audit / Settings), the multi-step onboarding flow, and a
// developer panel that keeps the raw SDK keygen / sign / reshare screens
// reachable for later L3s.

import React, { useEffect, useMemo, useState } from 'react';
import { View, Text, StyleSheet, StatusBar } from 'react-native';
import { newSDK, KEYSTORE_DIR } from './sdk';
import { BottomTabs, ThemeProvider, useTheme, spacing } from './ui';
import type { TabId, TabSpec, ThemeTokens } from './ui';
import { I18nProvider, useI18n } from './i18n';
import type { Strings } from './i18n';
import { InboxScreen, WalletsScreen, AuditScreen, SettingsScreen } from './screens/tabs';
import { OnboardingFlow } from './screens/onboarding';
import { SdkPanel } from './screens/SdkPanel';
import { SigningFlow } from './screens/signing';
import { WalletDetailScreen } from './screens/wallets';
import { ENVELOPES_NEEDING_SELF } from './data';
import type { SettingsAction, SigningEnvelope, Wallet } from './data';

type Route =
  | { readonly kind: 'tabs'; readonly tab: TabId }
  | { readonly kind: 'onboarding' }
  | { readonly kind: 'sdk' }
  | { readonly kind: 'signing'; readonly envelope: SigningEnvelope }
  | { readonly kind: 'walletDetail'; readonly wallet: Wallet };

export default function App(): React.JSX.Element {
  return (
    <I18nProvider initial="zh">
      <ThemeProvider initial="midnight">
        <AppShell />
      </ThemeProvider>
    </I18nProvider>
  );
}

function AppShell(): React.JSX.Element {
  const theme = useTheme();
  const { t: T } = useI18n();
  const s = useMemo(() => makeStyles(theme), [theme]);
  const [route, setRoute] = useState<Route>({ kind: 'tabs', tab: 'inbox' });
  const [sdkReady, setSdkReady] = useState(false);
  const [sdkError, setSdkError] = useState<string | null>(null);

  useEffect(() => {
    let cancelled = false;
    newSDK(KEYSTORE_DIR)
      .then(() => {
        if (!cancelled) setSdkReady(true);
      })
      .catch((err: unknown) => {
        if (!cancelled) setSdkError(getErrorMessage(err));
      });
    return () => {
      cancelled = true;
    };
  }, []);

  const banner = useMemo(() => bannerText(sdkReady, sdkError, T), [sdkReady, sdkError, T]);
  const tabs = useMemo<ReadonlyArray<TabSpec>>(
    () => [
      { id: 'inbox', label: T.tabs.inbox, icon: 'inbox' },
      { id: 'wallets', label: T.tabs.wallets, icon: 'wallet' },
      { id: 'audit', label: T.tabs.audit, icon: 'list' },
      { id: 'settings', label: T.tabs.settings, icon: 'cog' },
    ],
    [T],
  );

  const goTab = (tab: TabId): void => setRoute({ kind: 'tabs', tab });

  const onSettingsAction = (action: SettingsAction): void => {
    // Foundation handles navigation-bearing kinds only; backup / about
    // surfaces are deferred to later L3s and are silent no-ops here.
    if (action.kind === 'onboarding') {
      setRoute({ kind: 'onboarding' });
      return;
    }
    if (action.kind === 'keygen' || action.kind === 'reshare') {
      setRoute({ kind: 'sdk' });
    }
  };

  if (route.kind === 'onboarding') {
    return (
      <View style={s.root}>
        <StatusBar barStyle="light-content" backgroundColor={theme.bg} />
        <OnboardingFlow onExit={() => setRoute({ kind: 'tabs', tab: 'inbox' })} />
      </View>
    );
  }

  if (route.kind === 'sdk') {
    return (
      <View style={s.root}>
        <StatusBar barStyle="light-content" backgroundColor={theme.bg} />
        <SdkPanel onClose={() => setRoute({ kind: 'tabs', tab: 'settings' })} />
      </View>
    );
  }

  if (route.kind === 'signing') {
    return (
      <View style={s.root}>
        <StatusBar barStyle="light-content" backgroundColor={theme.bg} />
        <SigningFlow
          envelope={route.envelope}
          onExit={() => setRoute({ kind: 'tabs', tab: 'inbox' })}
        />
      </View>
    );
  }

  if (route.kind === 'walletDetail') {
    return (
      <View style={s.root}>
        <StatusBar barStyle="light-content" backgroundColor={theme.bg} />
        <WalletDetailScreen
          wallet={route.wallet}
          onBack={() => setRoute({ kind: 'tabs', tab: 'wallets' })}
          onReshare={() => setRoute({ kind: 'sdk' })}
        />
      </View>
    );
  }

  return (
    <View style={s.root}>
      <StatusBar barStyle="light-content" backgroundColor={theme.bg} />
      {banner ? (
        <View style={s.banner} accessibilityRole="alert">
          <Text style={s.bannerText}>{banner}</Text>
        </View>
      ) : null}

      <View style={s.body}>
        {route.tab === 'inbox' ? (
          <InboxScreen onOpenEnvelope={(env) => setRoute({ kind: 'signing', envelope: env })} />
        ) : null}
        {route.tab === 'wallets' ? (
          <WalletsScreen
            onOpenWallet={(wallet) => setRoute({ kind: 'walletDetail', wallet })}
            onStartKeygen={() => setRoute({ kind: 'sdk' })}
          />
        ) : null}
        {route.tab === 'audit' ? <AuditScreen /> : null}
        {route.tab === 'settings' ? <SettingsScreen onAction={onSettingsAction} /> : null}
      </View>

      <BottomTabs active={route.tab} onChange={goTab} inboxBadge={ENVELOPES_NEEDING_SELF} tabs={tabs} />
    </View>
  );
}

function bannerText(ready: boolean, err: string | null, T: Strings): string | null {
  if (err) return `${T.app.initFailed}: ${err}`;
  if (!ready) return T.app.initializing;
  return null;
}

function getErrorMessage(err: unknown): string {
  if (err instanceof Error) return err.message;
  return 'unknown error';
}

function makeStyles(t: ThemeTokens) {
  return StyleSheet.create({
    root: { flex: 1, backgroundColor: t.bg },
    body: { flex: 1 },
    banner: {
      paddingHorizontal: spacing.lg,
      paddingVertical: spacing.sm,
      backgroundColor: t.surface2,
      borderBottomColor: t.hairline,
      borderBottomWidth: StyleSheet.hairlineWidth,
    },
    bannerText: { color: t.text2, fontSize: 12, fontWeight: '600' },
  });
}
