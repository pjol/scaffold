import { useSyncExternalStore } from "react";

/** Which presence dialog is open. The account menu closes before a dialog opens, so the state lives here. */
export type PresenceDialog = "status" | "dnd" | null;

let open: PresenceDialog = null;
const listeners = new Set<() => void>();

export function openPresenceDialog(dialog: PresenceDialog): void {
  open = dialog;
  for (const l of listeners) l();
}

function subscribe(l: () => void): () => void {
  listeners.add(l);
  return () => listeners.delete(l);
}

export function usePresenceDialog(): PresenceDialog {
  return useSyncExternalStore(
    subscribe,
    () => open,
    () => null,
  );
}
