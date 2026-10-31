import { ApiProvider, bindAppStateLifecycle, clientPlatform, createApiClient, createQueryClient, createRealtimeClient, toWebSocketUrl, useAuthSession } from "@scaffoldapp/shared";
import { toast } from "@scaffoldapp/ui-native";
import { useEffect, useMemo, useRef, useState, type ReactNode } from "react";
import { AppState, Platform } from "react-native";
import { sessionMounts } from "@/features/registry";
import { API_URL } from "./env";
import { signOutEverywhere } from "./session";
import { appVersion } from "./version";

/**
 * API client, realtime socket and react-query for everything below, authenticated with
 * whatever backs `useAuthSession()`. The socket exists only while signed in, pauses in
 * the background and resumes (with refetch) on foreground. A different account never sees
 * the previous one's cache. Features' `sessionMounts` render here while signed in.
 */
export function ApiLayer({ children }: { children: ReactNode }) {
  const session = useAuthSession();
  // Assigned during render, not in an effect: children's first queries after a sign-in run
  // in their own effects (before ours) and must already use the new token getter.
  const tokenRef = useRef(session.getToken);
  tokenRef.current = session.getToken;
  const sessionRef = useRef(session);
  sessionRef.current = session;

  const [queryClient] = useState(() => createQueryClient());
  const [api] = useState(() => {
    let lastRejected = 0;
    return createApiClient({
      baseUrl: API_URL,
      platform: clientPlatform(Platform.OS),
      version: appVersion(),
      getToken: (o) => tokenRef.current(o),
      // The API refused a refreshed token: the session is over on the server side.
      onUnauthorized: () => {
        const now = Date.now();
        if (now - lastRejected < 5_000 || !sessionRef.current.isSignedIn) return;
        lastRejected = now;
        toast.error("Your session ended. Sign in again.");
        void signOutEverywhere({ session: sessionRef.current, queryClient });
      },
    });
  });

  const { isSignedIn, userId } = session;
  const realtime = useMemo(() => (isSignedIn && userId ? createRealtimeClient({ url: toWebSocketUrl(API_URL), getToken: (o) => tokenRef.current(o) }) : null), [isSignedIn, userId]);
  useEffect(() => bindAppStateLifecycle(AppState, { realtime }), [realtime]);

  const lastUser = useRef<string | null>(userId);
  useEffect(() => {
    if (lastUser.current !== null && lastUser.current !== userId) queryClient.clear();
    lastUser.current = userId;
  }, [userId, queryClient]);

  return (
    <ApiProvider api={api} realtime={realtime} queryClient={queryClient}>
      {children}
      {/* Features' session work (push lifecycle, badges): mounted once per signed-in session. */}
      {isSignedIn && userId ? sessionMounts.map((Mount, i) => <Mount key={`${userId}:${i}`} />) : null}
    </ApiProvider>
  );
}
