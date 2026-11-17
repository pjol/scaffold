"use client";

import { Dialog } from "@scaffoldapp/ui-web";
import { shortcutLabel } from "../media";

const SHORTCUTS: { keys: () => string; what: string }[] = [
  { keys: () => shortcutLabel("D"), what: "Turn the microphone on or off" },
  { keys: () => shortcutLabel("E"), what: "Turn the camera on or off" },
  { keys: () => shortcutLabel("H", true), what: "Raise or lower your hand" },
  { keys: () => shortcutLabel("C", true), what: "Open or close chat" },
  { keys: () => shortcutLabel("P", true), what: "Open or close people" },
  { keys: () => shortcutLabel("A", true), what: "Open or close polls and Q&A" },
  { keys: () => shortcutLabel("K", true), what: "Turn captions on or off" },
  { keys: () => shortcutLabel("F", true), what: "Full screen" },
  { keys: () => "?", what: "Show these shortcuts" },
];

/** The keyboard shortcuts in a call. */
export function ShortcutsDialog({ open, onOpenChange }: { open: boolean; onOpenChange: (o: boolean) => void }) {
  return (
    <Dialog open={open} onOpenChange={onOpenChange} title="Keyboard shortcuts">
      <dl className="flex flex-col divide-y divide-border">
        {SHORTCUTS.map((s) => (
          <div key={s.what} className="flex items-center gap-4 py-2">
            <dt className="flex-1 type-body text-fg">{s.what}</dt>
            <dd>
              <kbd className="rounded-sm border border-border bg-sunken px-2 py-1 type-label font-semibold text-fg">{s.keys()}</kbd>
            </dd>
          </div>
        ))}
      </dl>
    </Dialog>
  );
}
