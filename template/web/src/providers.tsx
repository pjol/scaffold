"use client";

import { ApiProvider, bindVisibilityLifecycle, createQueryClient, queryKeys, useAuthSession } from "@scaffoldapp/shared";
import type { QueryClient } from "@tanstack/react-query";
import { Toaster, UIProvider, useLatest } from "@scaffoldapp/ui-web";
import { Fragment, useEffect, useMemo, useRef, useState, type ReactNode } from "react";
import { AuthProvider } from "@/components/auth/AuthProvider";
import { sessionMounts } from "@/features/registry";
import { createWebApiClient, createWebRealtimeClient } from "@/lib/api";

/**
 * Forget everything cached for the previous identity (public config stays). `reset`
 * rather than `remove`, so mounted screens refetch as the new identity instead of
 * holding a detached query.
 */
function clearUserData(qc: QueryClient) {
  const keep = JSON.stringify(queryKeys.config());
  void qc.resetQueries({ predicate: (q) => JSON.stringify(q.queryKey) !== keep });
}

/**
 * API client + realtime + react-query, authenticated through `useAuthSession()` (Clerk
 * bridge or dev shim). One API client for the app's lifetime; one realtime connection per
 * signed-in identity; the cache is cleared when the identity changes.
 */
function DataProviders({ children }: { children: ReactNode }) {
  const auth = useAuthSession();
  const tokenRef = useLatest(auth.getToken);
  // Until the auth provider restores the session (Clerk boot, dev identity from storage),
  // requests wait for it briefly instead of going out unauthenticated and caching a 401.
  const [authReady] = useState(() => {
    let resolve!: () => void;
    const promise = new Promise<void>((r) => (resolve = r));
    return { promise, resolve };
  });
  useEffect(() => {
    if (auth.isLoaded) authReady.resolve();
  }, [auth.isLoaded, authReady]);

  const [queryClient] = useState(() => createQueryClient());
  const [api] = useState(() =>
    createWebApiClient(async (o) => {
      await Promise.race([authReady.promise, new Promise((r) => setTimeout(r, 2500))]);
      return tokenRef.current(o);
    }),
  );

  const userKey = auth.isLoaded && auth.isSignedIn ? auth.userId : null;
  const realtime = useMemo(
    () => (userKey ? createWebRealtimeClient((o) => tokenRef.current(o)) : null),
    // A new identity gets a new socket (fresh token, fresh user topic).
    // eslint-disable-next-line react-hooks/exhaustive-deps
    [userKey],
  );

  // Signing out or switching identity must never show the previous user's data. (Signing
  // in from signed out clears nothing: queries started for the new identity must survive.)
  const previousUser = useRef<string | null>(userKey);
  useEffect(() => {
    if (previousUser.current && previousUser.current !== userKey) clearUserData(queryClient);
    previousUser.current = userKey;
  }, [userKey, queryClient]);

  // Background tabs throttle timers; reconnect the socket when the tab is visible again.
  useEffect(() => bindVisibilityLifecycle(document, { realtime }), [realtime]);

  return (
    <ApiProvider api={api} realtime={realtime} queryClient={queryClient}>
      {children}
      {/* Registry `sessionMounts`: once per signed-in identity (keyed, so they remount on a switch). */}
      {userKey ? (
        <Fragment key={userKey}>
          {sessionMounts.map((Mount, i) => (
            <Mount key={i} />
          ))}
        </Fragment>
      ) : null}
    </ApiProvider>
  );
}

/** Client providers: motion + tooltips, auth, data, toasts. */
export function Providers({ children }: { children: ReactNode }) {
  return (
    <UIProvider>
      <AuthProvider>
        <DataProviders>{children}</DataProviders>
      </AuthProvider>
      <Toaster />
    </UIProvider>
  );
}
