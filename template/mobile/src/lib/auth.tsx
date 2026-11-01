import { ClerkProvider, useAuth, useUser } from "@clerk/expo";
import { passkeys } from "@clerk/expo/passkeys";
import { tokenCache } from "@clerk/expo/token-cache";
import { AuthSessionProvider, DevAuthProvider, type AuthSession, type DevAuthStorage, type DevIdentityLike } from "@scaffoldapp/shared";
import * as SecureStore from "expo-secure-store";
import { useEffect, useMemo, useRef, type ReactNode } from "react";
import { ConfigMissing } from "@/components/BootScreens";
import { CLERK_PUBLISHABLE_KEY, DEV_AUTH } from "./env";
import { DEVICE_KEYS } from "./storage";

const devStorage: DevAuthStorage = {
  get: () => SecureStore.getItemAsync(DEVICE_KEYS.devIdentity).catch(() => null),
  set: async (value) => {
    try {
      if (value === null) await SecureStore.deleteItemAsync(DEVICE_KEYS.devIdentity);
      else await SecureStore.setItemAsync(DEVICE_KEYS.devIdentity, value);
    } catch {
      // SecureStore unavailable: the dev session lives in memory only.
    }
  },
};

/**
 * Auth for the whole app; screens only call `useAuthSession()`.
 * - `EXPO_PUBLIC_CLERK_PUBLISHABLE_KEY` set → Clerk (SecureStore token cache, native passkeys).
 * - else `EXPO_PUBLIC_AUTH_DEV=true` in a non-production env → seeded dev identities.
 * - else a "Sign-in isn't configured" screen (a production build without its key).
 */
export function AuthRoot({ children }: { children: ReactNode }) {
  if (CLERK_PUBLISHABLE_KEY) {
    return (
      <ClerkProvider publishableKey={CLERK_PUBLISHABLE_KEY} tokenCache={tokenCache} __experimental_passkeys={passkeys}>
        <ClerkBridge>{children}</ClerkBridge>
      </ClerkProvider>
    );
  }
  if (DEV_AUTH) return <DevAuthProvider storage={devStorage}>{children}</DevAuthProvider>;
  return <ConfigMissing kind="auth" />;
}

/** Clerk's `useAuth` + `useUser` → the shared AuthSession. */
function ClerkBridge({ children }: { children: ReactNode }) {
  const { isLoaded, isSignedIn, userId, getToken, signOut } = useAuth();
  const { user } = useUser();
  // Clerk hands out new function identities on re-render; the API client is created once,
  // so it reads the latest through refs.
  const tokenRef = useRef(getToken);
  const signOutRef = useRef(signOut);
  useEffect(() => {
    tokenRef.current = getToken;
    signOutRef.current = signOut;
  }, [getToken, signOut]);

  const email = user?.primaryEmailAddress?.emailAddress ?? null;
  const name = user?.fullName ?? null;
  const imageUrl = user?.imageUrl ?? null;
  const session = useMemo<AuthSession>(
    () => ({
      mode: "clerk",
      isLoaded,
      isSignedIn: Boolean(isSignedIn),
      userId: userId ?? null,
      email,
      name,
      imageUrl,
      getToken: async (o) => (await tokenRef.current({ skipCache: o?.forceRefresh ?? false })) ?? null,
      signOut: async () => {
        await signOutRef.current();
      },
    }),
    [isLoaded, isSignedIn, userId, email, name, imageUrl],
  );
  return <AuthSessionProvider value={session}>{children}</AuthSessionProvider>;
}

/**
 * A dev identity for any email (`dev:<subject>:<email>`; the backend stores `dev_<subject>`).
 * The subject is derived from the whole address so two emails never collide.
 */
export function devIdentityForEmail(email: string): DevIdentityLike {
  const normalized = email.trim().toLowerCase();
  return { subject: `dev_${normalized.replace(/[^a-z0-9]+/g, "_")}`, email: normalized, name: normalized.split("@")[0] ?? normalized };
}
