"use client";

import { ClerkProvider, useAuth, useUser } from "@clerk/nextjs";
import { AuthSessionProvider, DevAuthProvider, type AuthSession, type DevAuthStorage } from "@scaffoldapp/shared/auth";
import { useColorMode, useLatest, usePalette } from "@scaffoldapp/ui-web";
import { useMemo, type ReactNode } from "react";
import { DEV_IDENTITY_STORAGE_KEY } from "@/lib/app";
import { clerkAppearance } from "@/lib/clerk-appearance";
import { AUTH_MODE } from "@/lib/env";

/** Persists the chosen dev identity in localStorage (module-level, so it is referentially stable). */
const devStorage: DevAuthStorage = {
  get() {
    try {
      return window.localStorage.getItem(DEV_IDENTITY_STORAGE_KEY);
    } catch {
      return null;
    }
  },
  set(value) {
    try {
      if (value === null) window.localStorage.removeItem(DEV_IDENTITY_STORAGE_KEY);
      else window.localStorage.setItem(DEV_IDENTITY_STORAGE_KEY, value);
    } catch {
      // Private mode: the identity lasts for this page only.
    }
  },
};

/** Adapts Clerk's hooks into the shared AuthSession. Screens only call `useAuthSession()`. */
function ClerkBridge({ children }: { children: ReactNode }) {
  const { isLoaded, isSignedIn, userId, getToken, signOut } = useAuth();
  const { user } = useUser();
  const tokenRef = useLatest(getToken);
  const signOutRef = useLatest(signOut);
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
      getToken: (opts) => tokenRef.current({ skipCache: opts?.forceRefresh ?? false }),
      signOut: async () => {
        await signOutRef.current();
      },
    }),
    [isLoaded, isSignedIn, userId, email, name, imageUrl, tokenRef, signOutRef],
  );
  return <AuthSessionProvider value={session}>{children}</AuthSessionProvider>;
}

function ClerkAuth({ children }: { children: ReactNode }) {
  const { palette } = usePalette();
  const { mode } = useColorMode();
  return (
    <ClerkProvider appearance={clerkAppearance(palette, mode)} signInUrl="/sign-in" signUpUrl="/sign-in" afterSignOutUrl="/sign-in">
      <ClerkBridge>{children}</ClerkBridge>
    </ClerkProvider>
  );
}

/** Neither Clerk nor dev sign-in is configured: everyone is signed out and /sign-in says so. */
const NO_AUTH: AuthSession = {
  mode: "clerk",
  isLoaded: true,
  isSignedIn: false,
  userId: null,
  email: null,
  name: null,
  imageUrl: null,
  getToken: async () => null,
  signOut: async () => undefined,
};

/**
 * The app's auth root (see `AUTH_MODE`): Clerk + ClerkBridge whenever a publishable key
 * is set; otherwise the dev shim when NEXT_PUBLIC_AUTH_DEV is on in a non-production
 * build; otherwise nothing (everyone is signed out). Clerk is never mounted without a key.
 */
export function AuthProvider({ children }: { children: ReactNode }) {
  if (AUTH_MODE === "clerk") return <ClerkAuth>{children}</ClerkAuth>;
  if (AUTH_MODE === "dev") return <DevAuthProvider storage={devStorage}>{children}</DevAuthProvider>;
  return <AuthSessionProvider value={NO_AUTH}>{children}</AuthSessionProvider>;
}
