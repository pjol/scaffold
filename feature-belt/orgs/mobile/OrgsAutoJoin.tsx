import { isApiError, useMe, useOrgsAutoJoin } from "@scaffoldapp/shared";
import { toast } from "@scaffoldapp/ui-native";
import { useEffect } from "react";

/** Identities already handled since launch. Module state outlives remounts of the session mount. */
const attempted = new Set<string>();

/**
 * Session mount: once the account is active and has accepted the Terms, joins every org
 * whose verified auto-join domain matches the verified email (never one the person left
 * or was removed from), then says so. Runs once per identity per launch, however often
 * the mount remounts. It is a background call, so failures stay silent: a network or
 * server error lets a later mount try again, a rate limit doesn't.
 */
export function OrgsAutoJoin() {
  const me = useMe();
  const { mutate } = useOrgsAutoJoin();
  const m = me.data;
  const key = m && m.status === "active" && !m.consent.required ? `${m.id}:${m.email}` : null;
  useEffect(() => {
    if (!key || attempted.has(key)) return;
    attempted.add(key);
    mutate(undefined, {
      onSuccess: (r) => {
        for (const org of r.joined) toast.success(`You joined ${org.name}`);
      },
      onError: (e) => {
        if (!(isApiError(e) && e.status === 429)) attempted.delete(key);
      },
    });
  }, [key, mutate]);
  return null;
}
