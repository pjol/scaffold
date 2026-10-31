"use client";

import { createContext, createElement, useCallback, useContext, useEffect, useMemo, useState, type ReactNode } from "react";

/**
 * One auth surface for every app, whatever backs it.
 *
 * Production: each app wraps Clerk (`@clerk/nextjs` / `@clerk/expo`) in a small bridge
 * that provides this context from Clerk's `useAuth()` + `useUser()`. Local development
 * without Clerk keys: `DevAuthProvider` signs in as a seeded identity and mints
 * `dev:<subject>:<email>` tokens, which the backend accepts only with
 * APP_ENV=development and AUTH_DEV_BYPASS=true.
 *
 * Screens only ever call `useAuthSession()`.
 */
export interface AuthSession {
  mode: "clerk" | "dev";
  isLoaded: boolean;
  isSignedIn: boolean;
  /** Auth-provider user id (Clerk user id, or the dev identity's subject). Not the API user id. */
  userId: string | null;
  email: string | null;
  name: string | null;
  imageUrl: string | null;
  getToken: (opts?: { forceRefresh?: boolean }) => Promise<string | null>;
  signOut: () => Promise<void>;
}

const AuthSessionContext = createContext<AuthSession | null>(null);

export interface AuthSessionProviderProps {
  value: AuthSession;
  children?: ReactNode;
}

/** Provides an {@link AuthSession}. The Clerk bridge in each app renders this. */
export function AuthSessionProvider({ value, children }: AuthSessionProviderProps) {
  return createElement(AuthSessionContext.Provider, { value }, children);
}

/** The current auth session. Throws outside a provider (a wiring bug). */
export function useAuthSession(): AuthSession {
  const s = useContext(AuthSessionContext);
  if (!s) throw new Error("useAuthSession must be used inside an auth provider (Clerk bridge or DevAuthProvider).");
  return s;
}

/** A seeded identity the dev sign-in picker can offer (see `DEV_IDENTITIES`). */
export interface DevIdentityLike {
  subject: string;
  email: string;
  name: string;
}

/** Persists the chosen identity (localStorage on web, SecureStore on native). */
export interface DevAuthStorage {
  get(): Promise<string | null> | string | null;
  set(value: string | null): Promise<void> | void;
}

interface DevAuthControls {
  signInAs: (identity: DevIdentityLike) => Promise<void>;
}

const DevAuthControlsContext = createContext<DevAuthControls | null>(null);

/** Dev-only sign-in control; null when the dev shim isn't active. */
export function useDevAuth(): DevAuthControls | null {
  return useContext(DevAuthControlsContext);
}

export interface DevAuthProviderProps {
  storage: DevAuthStorage;
  children?: ReactNode;
}

/** Provides an AuthSession backed by a locally chosen dev identity. */
export function DevAuthProvider({ storage, children }: DevAuthProviderProps) {
  const [identity, setIdentity] = useState<DevIdentityLike | null>(null);
  const [isLoaded, setLoaded] = useState(false);

  useEffect(() => {
    let alive = true;
    void Promise.resolve(storage.get())
      .then((raw) => {
        if (!alive || !raw) return;
        try {
          const parsed = JSON.parse(raw) as Partial<DevIdentityLike>;
          if (parsed.subject && parsed.email) setIdentity({ subject: parsed.subject, email: parsed.email, name: parsed.name ?? "" });
        } catch {
          // Corrupt value: treat as signed out.
        }
      })
      .finally(() => {
        if (alive) setLoaded(true);
      });
    return () => {
      alive = false;
    };
  }, [storage]);

  const signInAs = useCallback(
    async (next: DevIdentityLike) => {
      const value: DevIdentityLike = { subject: next.subject, email: next.email, name: next.name };
      await storage.set(JSON.stringify(value));
      setIdentity(value);
    },
    [storage],
  );

  const signOut = useCallback(async () => {
    await storage.set(null);
    setIdentity(null);
  }, [storage]);

  const session = useMemo<AuthSession>(
    () => ({
      mode: "dev",
      isLoaded,
      isSignedIn: identity !== null,
      userId: identity?.subject ?? null,
      email: identity?.email ?? null,
      name: identity?.name ?? null,
      imageUrl: null,
      getToken: async () => (identity ? `dev:${identity.subject}:${identity.email}` : null),
      signOut,
    }),
    [identity, isLoaded, signOut],
  );

  const controls = useMemo(() => ({ signInAs }), [signInAs]);

  return createElement(DevAuthControlsContext.Provider, { value: controls }, createElement(AuthSessionContext.Provider, { value: session }, children));
}
