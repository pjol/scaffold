"use client";

import { DOCS_KEYBOARD_SHORTCUTS } from "@scaffoldapp/shared";
import { Dialog } from "@scaffoldapp/ui-web";
import { useMemo } from "react";

/** Every editor shortcut (⌘? or ? with blocks selected; "Keyboard shortcuts" in a block's menu). */
export function ShortcutsSheet({ open, onOpenChange }: { open: boolean; onOpenChange: (open: boolean) => void }) {
  const apple = typeof navigator !== "undefined" && /Mac|iPhone|iPad/.test(navigator.platform);
  const groups = useMemo(
    () => DOCS_KEYBOARD_SHORTCUTS.map((g) => ({ ...g, items: g.items.map(([k, v]) => [apple ? k : k.replace(/⌘/g, "Ctrl+").replace(/⌥/g, "Alt+").replace(/⇧/g, "Shift+"), v] as const) })),
    [apple],
  );
  return (
    <Dialog open={open} onOpenChange={onOpenChange} title="Keyboard shortcuts" size="lg">
      <div className="grid max-h-[60dvh] gap-6 overflow-y-auto sm:grid-cols-2">
        {groups.map((g) => (
          <section key={g.group} className="flex flex-col gap-2">
            <h3 className="type-label font-semibold text-fg">{g.group}</h3>
            <dl className="flex flex-col">
              {g.items.map(([keys, what]) => (
                <div key={keys + what} className="flex items-center justify-between gap-4 border-b border-border py-1.5 last:border-b-0">
                  <dt className="type-label text-fg-muted">{what}</dt>
                  <dd>
                    <kbd className="rounded-xs border border-border bg-sunken px-1.5 py-0.5 font-mono type-caption text-fg">{keys}</kbd>
                  </dd>
                </div>
              ))}
            </dl>
          </section>
        ))}
      </div>
    </Dialog>
  );
}
