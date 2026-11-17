import { callsSecondsLeft, useNow } from "@scaffoldapp/shared";

/** Seconds until `iso`, re-rendering every second; null without an end time. */
export function useCountdown(iso: string | null | undefined): number | null {
  const now = useNow(1000, !!iso);
  return callsSecondsLeft(iso, now);
}
