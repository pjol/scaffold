import type { QueryClient } from "@tanstack/react-query";
import { isConsentRequired } from "../api/errors";
import { queryKeys } from "./keys";

/**
 * When any query or mutation fails with `403 consent_required`, refetch `me`
 * (whose `consent_required` then flips `useConsentGate().required`). Throttled
 * so a burst of failures refetches once. Installed by `ApiProvider`; returns
 * an uninstall function.
 */
export function installConsentRecovery(qc: QueryClient, options: { throttleMs?: number } = {}): () => void {
  const throttleMs = options.throttleMs ?? 2_000;
  let last = Number.NEGATIVE_INFINITY;
  const onError = (error: unknown) => {
    if (!isConsentRequired(error)) return;
    const now = Date.now();
    if (now - last < throttleMs) return;
    last = now;
    void qc.invalidateQueries({ queryKey: queryKeys.me() });
  };
  const offMutations = qc.getMutationCache().subscribe((event) => {
    if (event.type === "updated" && event.action.type === "error") onError(event.action.error);
  });
  const offQueries = qc.getQueryCache().subscribe((event) => {
    if (event.type === "updated" && event.action.type === "error") onError(event.action.error);
  });
  return () => {
    offMutations();
    offQueries();
  };
}
