import type { AssistantContextLaunch } from "@scaffoldapp/shared";
import { useSyncExternalStore } from "react";

/** The side panel's launch (a ⋮ menu's Summarise or Ask about this); null = closed. */
let current: { launch: AssistantContextLaunch; key: number } | null = null;
const listeners = new Set<() => void>();

/** Opens the assistant beside the page with a resource attached. */
export function openAssistantPanel(launch: AssistantContextLaunch | null): void {
  current = launch ? { launch, key: Date.now() } : null;
  for (const l of listeners) l();
}

export function useAssistantPanel(): { launch: AssistantContextLaunch; key: number } | null {
  return useSyncExternalStore(
    (l) => {
      listeners.add(l);
      return () => listeners.delete(l);
    },
    () => current,
    () => null,
  );
}
