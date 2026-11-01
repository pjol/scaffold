"use client";

import { useAuthSession, useQueryClient } from "@scaffoldapp/shared";
import { useRouter } from "next/navigation";
import { useCallback } from "react";

// Where an in-progress sign-out is heading, so the app gate's own "signed out → /sign-in"
// redirect doesn't race it to a different URL.
let pendingTarget: string | null = null;

/** The destination of a sign-out in progress, if any. */
export function signOutTarget(): string | null {
  return pendingTarget;
}

/** Signs out, clears every cached query and navigates to `to` (default `/sign-in`). */
export function useSignOut(): (to?: string) => Promise<void> {
  const auth = useAuthSession();
  const qc = useQueryClient();
  const router = useRouter();
  return useCallback(
    async (to = "/sign-in") => {
      pendingTarget = to;
      try {
        await auth.signOut();
        qc.clear();
        router.replace(to);
      } finally {
        setTimeout(() => {
          pendingTarget = null;
        }, 1000);
      }
    },
    [auth, qc, router],
  );
}
