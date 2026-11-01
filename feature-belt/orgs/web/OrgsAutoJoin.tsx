"use client";

import { isApiError, useMe, useOrgsAutoJoin } from "@scaffoldapp/shared";
import { toast } from "@scaffoldapp/ui-web";
import { useEffect } from "react";

/** Identities already handled in this tab. Module state outlives remounts of the session mount; sessionStorage outlives reloads. */
const attempted = new Set<string>();
const STORAGE_KEY = "orgs.autoJoin";

function alreadyAttempted(key: string): boolean {
  if (attempted.has(key)) return true;
  try {
    return window.sessionStorage.getItem(STORAGE_KEY) === key;
  } catch {
    return false;
  }
}

function remember(key: string, on: boolean) {
  if (on) attempted.add(key);
  else attempted.delete(key);
  try {
    if (on) window.sessionStorage.setItem(STORAGE_KEY, key);
    else if (window.sessionStorage.getItem(STORAGE_KEY) === key) window.sessionStorage.removeItem(STORAGE_KEY);
  } catch {
    // Storage blocked: the module set still covers remounts.
  }
}

/**
 * Session mount: once the account is active and has accepted the Terms, joins every org
 * whose verified auto-join domain matches the verified email (never one the person left
 * or was removed from), then says so. Runs once per identity per tab, however often the
 * mount remounts or the page reloads. It is a background call, so failures stay silent: a
 * network or server error lets a later mount try again, a rate limit doesn't.
 */
export function OrgsAutoJoin() {
  const me = useMe();
  const { mutate } = useOrgsAutoJoin();
  const m = me.data;
  const key = m && m.status === "active" && !m.consent.required ? `${m.id}:${m.email}` : null;
  useEffect(() => {
    if (!key || alreadyAttempted(key)) return;
    remember(key, true);
    mutate(undefined, {
      onSuccess: (r) => {
        for (const org of r.joined) toast.success(`You joined ${org.name}`);
      },
      onError: (e) => {
        if (!(isApiError(e) && e.status === 429)) remember(key, false);
      },
    });
  }, [key, mutate]);
  return null;
}
