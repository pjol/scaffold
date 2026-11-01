"use client";

import { useSyncExternalStore } from "react";

// Whether the command palette is open: the header button and the keyboard shortcut both
// toggle it, and the palette (a session mount) renders it.
let open = false;
const listeners = new Set<() => void>();

export function setSearchPaletteOpen(next: boolean): void {
  if (open === next) return;
  open = next;
  for (const l of listeners) l();
}

export function useSearchPaletteOpen(): boolean {
  return useSyncExternalStore(
    (l) => {
      listeners.add(l);
      return () => listeners.delete(l);
    },
    () => open,
    () => false,
  );
}
