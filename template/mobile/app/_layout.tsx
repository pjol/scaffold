import { isAccountSuspended, useAuthSession, useConfig, useMe } from "@scaffoldapp/shared";
import { ThemeProvider, ToastHost, useAppFonts, useTheme } from "@scaffoldapp/ui-native";
import { DarkTheme, DefaultTheme, Stack, ThemeProvider as NavigationThemeProvider, router, useSegments, useUnstableGlobalHref, type Href } from "expo-router";
import * as SplashScreen from "expo-splash-screen";
import { StatusBar } from "expo-status-bar";
import * as SystemUI from "expo-system-ui";
import { useEffect, useMemo, useState } from "react";
import { Platform } from "react-native";
import { GestureHandlerRootView } from "react-native-gesture-handler";
import { SafeAreaProvider } from "react-native-safe-area-context";
import { BootError, BootLoading, ConfigMissing, useHideSplash } from "@/components/BootScreens";
import { UpdateRequired } from "@/components/UpdateRequired";
import { isConsentRoute, isPublicRoute } from "@/features/registry";
import { ApiLayer } from "@/lib/api";
import { AuthRoot } from "@/lib/auth";
import { API_URL } from "@/lib/env";
import { rememberRoute, restorePendingRoute, setGateOpen, takePendingRoute } from "@/lib/pending-route";
import { restorePreferences, usePreferences } from "@/lib/preferences";
import { useSignOut } from "@/lib/session";
import { requiredUpdate } from "@/lib/version";

void SplashScreen.preventAutoHideAsync();
SplashScreen.setOptions({ fade: true, duration: 250 });
void restorePreferences();
void restorePendingRoute();

/** Longest the splash waits for /v1/config before the app opens anyway (the update gate still applies when it arrives). */
const CONFIG_WAIT_MS = 3_000;

export default function RootLayout() {
  const [fontsLoaded, fontError] = useAppFonts();
  const prefs = usePreferences();
  // The native splash stays up until fonts and the saved appearance are ready (no flash).
  if (!(fontsLoaded || fontError) || !prefs.ready) return null;
  return (
    <GestureHandlerRootView style={{ flex: 1 }}>
      <SafeAreaProvider>
        <ThemeProvider paletteId={prefs.palette} mode={prefs.appearance}>
          <SystemChrome />
          {API_URL ? (
            <AuthRoot>
              <ApiLayer>
                <RootNavigator />
              </ApiLayer>
            </AuthRoot>
          ) : (
            <ConfigMissing kind="api" />
          )}
          <ToastHost />
        </ThemeProvider>
      </SafeAreaProvider>
    </GestureHandlerRootView>
  );
}

/** Status bar style and the root background (shows behind the keyboard and during transitions). */
function SystemChrome() {
  const theme = useTheme();
  useEffect(() => {
    void SystemUI.setBackgroundColorAsync(theme.colors.canvas).catch(() => undefined);
  }, [theme.colors.canvas]);
  return <StatusBar style={theme.mode === "dark" ? "light" : "dark"} />;
}

function useSettled(done: boolean, maxWaitMs: number): boolean {
  const [timedOut, setTimedOut] = useState(false);
  useEffect(() => {
    const t = setTimeout(() => setTimedOut(true), maxWaitMs);
    return () => clearTimeout(t);
  }, [maxWaitMs]);
  return done || timedOut;
}

/**
 * Three gates, in order:
 * 1. update: this build is older than `min_app_version` → unpassable "Update required";
 * 2. auth: signed out → only (auth), legal and +not-found exist;
 * 3. consent: signed in without the current Terms/Privacy → only the consent screen
 *    (blocked accounts get the blocked screen); `consentRoutes` stay reachable in both.
 * Core routes are declared here behind `Stack.Protected`; routes features add (any file
 * under app/ not listed) follow the same gates through the effect below, unless listed
 * in `publicRoutes` (src/features/registry.tsx). Where the user was headed when a gate
 * stopped them (a deep link, or a push) opens once every gate is passed.
 */
function RootNavigator() {
  const theme = useTheme();
  const c = theme.colors;
  const session = useAuthSession();
  const config = useConfig();
  const configSettled = useSettled(!config.isPending, CONFIG_WAIT_MS);
  const minimum = requiredUpdate(config.data);
  const signedIn = session.isSignedIn;
  const me = useMe({ enabled: signedIn });
  const consented = Boolean(me.data && !me.data.consent.required);
  // The API refuses banned/deleted accounts outright: support and sign-out only.
  const blocked = signedIn && !me.data && isAccountSuspended(me.error);
  const accountReady = !signedIn || Boolean(me.data) || blocked;
  const segments = useSegments();
  const signOut = useSignOut();

  const bootReady = session.isLoaded && configSettled;
  useHideSplash(bootReady && accountReady);

  const href = useUnstableGlobalHref();
  const isPublic = isPublicRoute(segments);
  const first = segments[0];
  const routeKey = segments.join("/");
  const open = signedIn && consented;
  useEffect(() => {
    setGateOpen(bootReady && accountReady && open);
  }, [bootReady, accountReady, open]);
  useEffect(() => {
    if (!bootReady || !accountReady) return;
    if (!signedIn && !isPublic) {
      rememberRoute(href);
      router.replace("/sign-in");
    } else if (blocked && first !== "blocked" && !isConsentRoute(segments)) {
      router.replace("/blocked");
    } else if (signedIn && !blocked && !consented && !isConsentRoute(segments)) {
      rememberRoute(href);
      router.replace("/consent");
    } else if (open && first !== "(auth)" && first !== "consent") {
      // Past every gate and off the gate screens: resume where the user was headed.
      const target = takePendingRoute();
      if (target && target.split("?")[0] !== href.split("?")[0]) router.push(target as Href);
    }
  }, [bootReady, accountReady, signedIn, consented, blocked, open, isPublic, first, href, routeKey]);

  const navTheme = useMemo(() => {
    const base = theme.mode === "dark" ? DarkTheme : DefaultTheme;
    return { ...base, colors: { ...base.colors, primary: c.primary, background: c.canvas, card: c.canvas, text: c.text, border: c.border, notification: c.danger } };
  }, [theme.mode, c]);

  if (!bootReady) return null;
  if (signedIn && !me.data && !blocked) {
    return me.isError ? <BootError error={me.error} onRetry={() => void me.refetch()} retrying={me.isRefetching} onSignOut={() => void signOut()} /> : <BootLoading />;
  }

  const modal = Platform.OS === "ios" ? ({ presentation: "modal" } as const) : ({ animation: "slide_from_bottom" } as const);
  return (
    <NavigationThemeProvider value={navTheme}>
      <Stack
        screenOptions={{
          headerShown: true,
          headerStyle: { backgroundColor: c.canvas },
          headerTintColor: c.text,
          headerTitleStyle: { fontFamily: theme.font("body", 600), color: c.text },
          headerShadowVisible: false,
          headerBackButtonDisplayMode: "minimal",
          contentStyle: { backgroundColor: c.canvas },
        }}
      >
        <Stack.Protected guard={signedIn && consented}>
          <Stack.Screen name="(tabs)" options={{ headerShown: false, title: "Home" }} />
          <Stack.Screen name="settings/profile" options={{ title: "Profile" }} />
          <Stack.Screen name="settings/appearance" options={{ title: "Appearance" }} />
          <Stack.Screen name="settings/account" options={{ title: "Account" }} />
          <Stack.Screen name="support" options={{ title: "Support" }} />
        </Stack.Protected>
        <Stack.Protected guard={signedIn && !consented && !blocked}>
          <Stack.Screen name="consent" options={{ headerShown: false, gestureEnabled: false }} />
        </Stack.Protected>
        <Stack.Protected guard={blocked}>
          <Stack.Screen name="blocked" options={{ headerShown: false, gestureEnabled: false }} />
        </Stack.Protected>
        <Stack.Protected guard={!signedIn}>
          <Stack.Screen name="(auth)" options={{ headerShown: false }} />
        </Stack.Protected>
        <Stack.Screen name="legal/[doc]" options={{ ...modal, title: "" }} />
        <Stack.Screen name="+not-found" options={{ title: "Not found" }} />
      </Stack>
      {minimum ? <UpdateRequired minimum={minimum} /> : null}
    </NavigationThemeProvider>
  );
}
