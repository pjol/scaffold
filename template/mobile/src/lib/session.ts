import { useAuthSession, useQueryClient, type AuthSession } from "@scaffoldapp/shared";
import type { QueryClient } from "@tanstack/react-query";
import { useCallback } from "react";
import { clearUserStorage } from "./storage";

type SignOutHook = () => void | Promise<void>;

const hooks = new Set<SignOutHook>();
const HOOK_TIMEOUT_MS = 5_000;

/**
 * Run `fn` before every sign-out and account deletion, while the session token still
 * works (the notifications feature unregisters the device's push token here). Call it at
 * module scope or in an effect; returns an unregister function.
 */
export function registerSignOutHook(fn: SignOutHook): () => void {
  hooks.add(fn);
  return () => {
    hooks.delete(fn);
  };
}

/**
 * The one sign-out path: feature hooks (each capped at 5 s), end the auth session, drop
 * every cached query and wipe per-user storage, so the next account on this device starts
 * clean. Used by Settings, account deletion and a rejected session.
 */
export async function signOutEverywhere({ session, queryClient }: { session: Pick<AuthSession, "signOut">; queryClient: QueryClient }): Promise<void> {
  await Promise.allSettled(
    [...hooks].map((hook) => Promise.race([Promise.resolve().then(hook), new Promise<void>((resolve) => setTimeout(resolve, HOOK_TIMEOUT_MS))])),
  );
  try {
    await session.signOut();
  } finally {
    await queryClient.cancelQueries();
    queryClient.clear();
    await clearUserStorage();
  }
}

/** `signOutEverywhere` bound to the current session and query client. */
export function useSignOut(): () => Promise<void> {
  const session = useAuthSession();
  const queryClient = useQueryClient();
  return useCallback(() => signOutEverywhere({ session, queryClient }), [session, queryClient]);
}
