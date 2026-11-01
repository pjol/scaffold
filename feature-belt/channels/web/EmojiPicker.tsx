"use client";

import { CHANNELS_EMOJI, CHANNELS_QUICK_REACTIONS, searchChannelsEmoji } from "@scaffoldapp/shared";
import { Dialog, Input } from "@scaffoldapp/ui-web";
import { Search } from "lucide-react";
import { useEffect, useMemo, useState } from "react";

/** Pick an emoji by name (reactions, the composer). Shows frequent ones until you type. */
export function EmojiPickerDialog({ open, onOpenChange, onPick, title = "Add a reaction" }: { open: boolean; onOpenChange: (open: boolean) => void; onPick: (emoji: string, name: string) => void; title?: string }) {
  const [q, setQ] = useState("");
  useEffect(() => {
    if (open) setQ("");
  }, [open]);
  const list = useMemo(
    () => (q.trim() ? searchChannelsEmoji(q.trim(), 48) : Object.entries(CHANNELS_EMOJI).filter(([n], i, all) => all.findIndex(([, e]) => e === CHANNELS_EMOJI[n]) === i).slice(0, 96).map(([name, emoji]) => ({ name, emoji }))),
    [q],
  );
  return (
    <Dialog open={open} onOpenChange={onOpenChange} title={title} size="sm">
      <div className="flex flex-col gap-4">
        <Input autoFocus value={q} onChange={(e) => setQ(e.target.value)} placeholder="Search emoji" aria-label="Search emoji" leadingIcon={<Search />} />
        {!q.trim() && (
          <ul className="flex flex-wrap gap-1" aria-label="Quick reactions">
            {CHANNELS_QUICK_REACTIONS.map((e) => (
              <li key={e}>
                <button type="button" onClick={() => onPick(e, e)} aria-label={e} className="flex size-tap-sm items-center justify-center rounded-md type-title-sm transition-colors hover:bg-sunken focus-visible:outline-2 focus-visible:outline-ring">
                  {e}
                </button>
              </li>
            ))}
          </ul>
        )}
        {list.length === 0 ? (
          <p className="type-label text-fg-muted">No emoji match “{q.trim()}”.</p>
        ) : (
          <ul className="grid max-h-72 grid-cols-6 gap-1 overflow-y-auto sm:grid-cols-8" aria-label="Emoji">
            {list.map(({ name, emoji }) => (
              <li key={name}>
                <button
                  type="button"
                  onClick={() => onPick(emoji, name)}
                  title={`:${name}:`}
                  aria-label={name.replace(/_/g, " ")}
                  className="flex size-tap-sm w-full items-center justify-center rounded-md type-title-sm transition-colors hover:bg-sunken focus-visible:outline-2 focus-visible:outline-ring"
                >
                  {emoji}
                </button>
              </li>
            ))}
          </ul>
        )}
      </div>
    </Dialog>
  );
}
