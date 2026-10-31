import { errorMessage, isNetworkError } from "@scaffoldapp/shared";
import { Button, EmptyState, ErrorState, Screen, Spinner, useTheme } from "@scaffoldapp/ui-native";
import * as SplashScreen from "expo-splash-screen";
import { LogOut, Settings2 } from "lucide-react-native";
import { useEffect } from "react";
import { View } from "react-native";

/** Hide the native splash once `ready` (idempotent). */
export function useHideSplash(ready: boolean): void {
  useEffect(() => {
    if (ready) void SplashScreen.hideAsync().catch(() => undefined);
  }, [ready]);
}

/** The account is loading after sign-in. The splash covers the first 1.5 s so fast loads don't flash a spinner. */
export function BootLoading() {
  const theme = useTheme();
  useEffect(() => {
    const t = setTimeout(() => void SplashScreen.hideAsync().catch(() => undefined), 1_500);
    return () => clearTimeout(t);
  }, []);
  return (
    <View style={{ flex: 1, alignItems: "center", justifyContent: "center", backgroundColor: theme.colors.canvas }}>
      <Spinner color={theme.colors.textMuted} label="Loading" />
    </View>
  );
}

/** The account couldn't load (API down, token refused, email unverified). */
export function BootError({ error, onRetry, retrying, onSignOut }: { error: unknown; onRetry: () => void; retrying: boolean; onSignOut: () => void }) {
  useHideSplash(true);
  return (
    <Screen centered gap={useTheme().space[4]}>
      <ErrorState title={isNetworkError(error) ? "Can't reach the server" : "Couldn't load your account"} info={errorMessage(error)} onRetry={onRetry} retrying={retrying} />
      <View style={{ alignItems: "center" }}>
        <Button variant="ghost" leftIcon={LogOut} onPress={onSignOut}>
          Sign out
        </Button>
      </View>
    </Screen>
  );
}

const MISSING = {
  api: { title: "The API address isn't set", info: "Set EXPO_PUBLIC_API_URL in mobile/.env (dev-up writes it), then restart Metro. Release builds take it from their EAS environment." },
  auth: {
    title: "Sign-in isn't configured",
    info: "Set EXPO_PUBLIC_CLERK_PUBLISHABLE_KEY. For local development, EXPO_PUBLIC_AUTH_DEV=true with EXPO_PUBLIC_APP_ENV=development turns on the dev sign-in (dev-up sets both).",
  },
} as const;

/** A build without the configuration it needs. Says which variable to set. */
export function ConfigMissing({ kind }: { kind: keyof typeof MISSING }) {
  useHideSplash(true);
  return (
    <Screen centered>
      <EmptyState icon={Settings2} title={MISSING[kind].title} info={MISSING[kind].info} />
    </Screen>
  );
}
