"use client";

import { CHANNELS_EMOJI, CHANNELS_QUICK_REACTIONS, errorMessage, searchChannelsEmoji, useActiveOrg, useChannelsWorkspace, useUpdateChannelsUserPrefs } from "@scaffoldapp/shared";
import { Dialog, Input, Tooltip, cn, toast } from "@scaffoldapp/ui-web";
import { Search } from "lucide-react";
import { useEffect, useMemo, useState } from "react";
import { EmojiGlyph, SKIN_TONE_SWATCHES, useWorkspaceEmoji, withSkinTone } from "./emoji";

const cell = "flex size-tap-sm w-full items-center justify-center rounded-md type-title-sm transition-colors hover:bg-sunken focus-visible:outline-2 focus-visible:outline-ring";

/**
 * Pick an emoji (reactions, the composer): your frequent ones, the workspace's custom emoji
 * and the built-in set, with your skin tone. `onPick` gets unicode or `:name:` for custom ones.
 */
export function EmojiPickerDialog({ open, onOpenChange, onPick, title = "Add a reaction" }: { open: boolean; onOpenChange: (open: boolean) => void; onPick: (emoji: string, name: string) => void; title?: string }) {
  const [q, setQ] = useState("");
  const { list: custom, frequent } = useWorkspaceEmoji();
  const { org } = useActiveOrg();
  const ws = useChannelsWorkspace(org?.id);
  const tone = ws.data?.prefs.skin_tone ?? 0;
  const setPrefs = useUpdateChannelsUserPrefs(org?.id);
  useEffect(() => {
    if (open) setQ("");
  }, [open]);
  const query = q.trim().toLowerCase().replace(/^:|:$/g, "");
  const builtin = useMemo(
    () =>
      query
        ? searchChannelsEmoji(query, 48)
        : Object.entries(CHANNELS_EMOJI)
            .filter(([n], i, all) => all.findIndex(([, e]) => e === CHANNELS_EMOJI[n]) === i)
            .slice(0, 96)
            .map(([name, emoji]) => ({ name, emoji })),
    [query],
  );
  const customHits = useMemo(() => (query ? custom.filter((e) => e.name.includes(query)) : custom).slice(0, 64), [custom, query]);
  const recent = useMemo(() => {
    const seen = new Set<string>();
    return [...frequent, ...CHANNELS_QUICK_REACTIONS].filter((e) => (seen.has(e) ? false : (seen.add(e), true))).slice(0, 16);
  }, [frequent]);
  const pick = (value: string, name: string) => onPick(value.startsWith(":") ? value : withSkinTone(value, tone), name);

  return (
    <Dialog open={open} onOpenChange={onOpenChange} title={title} size="sm">
      <div className="flex flex-col gap-4">
        <div className="flex items-center gap-2">
          <Input autoFocus value={q} onChange={(e) => setQ(e.target.value)} placeholder="Search emoji" aria-label="Search emoji" leadingIcon={<Search />} wrapperClassName="flex-1" />
          <div role="radiogroup" aria-label="Skin tone" className="flex shrink-0">
            {SKIN_TONE_SWATCHES.map((s, i) => (
              <Tooltip key={s} content={i === 0 ? "Default skin tone" : `Skin tone ${i}`}>
                <button
                  type="button"
                  role="radio"
                  aria-checked={tone === i}
                  aria-label={i === 0 ? "Default skin tone" : `Skin tone ${i}`}
                  onClick={() => setPrefs.mutate({ skin_tone: i }, { onError: (e) => toast.error(errorMessage(e)) })}
                  className={cn("flex size-7 items-center justify-center rounded-sm type-label focus-visible:outline-2 focus-visible:outline-ring", tone === i ? "bg-primary-soft" : "hover:bg-sunken", i > 2 && "hidden sm:flex")}
                >
                  {s}
                </button>
              </Tooltip>
            ))}
          </div>
        </div>
        {!query && recent.length > 0 && (
          <section className="flex flex-col gap-2">
            <h3 className="type-caption font-semibold text-fg-muted">Frequently used</h3>
            <ul className="grid grid-cols-6 gap-1 sm:grid-cols-8">
              {recent.map((e) => (
                <li key={e}>
                  <button type="button" onClick={() => pick(e, e)} aria-label={e.startsWith(":") ? e.slice(1, -1) : e} className={cell}>
                    <EmojiGlyph value={e.startsWith(":") ? e : withSkinTone(e, tone)} />
                  </button>
                </li>
              ))}
            </ul>
          </section>
        )}
        <div className="flex max-h-72 flex-col gap-4 overflow-y-auto">
          {customHits.length > 0 && (
            <section className="flex flex-col gap-2">
              <h3 className="type-caption font-semibold text-fg-muted">{org?.name ?? "Workspace"}</h3>
              <ul className="grid grid-cols-6 gap-1 sm:grid-cols-8" aria-label="Custom emoji">
                {customHits.map((e) => (
                  <li key={e.id}>
                    <button type="button" onClick={() => pick(`:${e.name}:`, e.name)} title={`:${e.name}:`} aria-label={e.name.replace(/[_-]/g, " ")} className={cell}>
                      <EmojiGlyph value={`:${e.name}:`} />
                    </button>
                  </li>
                ))}
              </ul>
            </section>
          )}
          {builtin.length === 0 && customHits.length === 0 ? (
            <p className="type-label text-fg-muted">No emoji match “{q.trim()}”.</p>
          ) : (
            builtin.length > 0 && (
              <ul className="grid grid-cols-6 gap-1 sm:grid-cols-8" aria-label="Emoji">
                {builtin.map(({ name, emoji }) => (
                  <li key={name}>
                    <button type="button" onClick={() => pick(emoji, name)} title={`:${name}:`} aria-label={name.replace(/_/g, " ")} className={cell}>
                      {withSkinTone(emoji, tone)}
                    </button>
                  </li>
                ))}
              </ul>
            )
          )}
        </div>
      </div>
    </Dialog>
  );
}
