"use client";

import { useChannelsEmoji, type ChannelsCustomEmoji } from "@scaffoldapp/shared";
import { cn } from "@scaffoldapp/ui-web";
import { createContext, useContext, useMemo, type ReactNode } from "react";

interface EmojiState {
  byName: Map<string, ChannelsCustomEmoji>;
  list: ChannelsCustomEmoji[];
  frequent: string[];
  canAdd: boolean;
}

const EmojiContext = createContext<EmojiState>({ byName: new Map(), list: [], frequent: [], canAdd: false });

/** Makes the workspace's custom emoji available to messages, reactions and pickers below it. */
export function ChannelsEmojiProvider({ orgId, children }: { orgId: string | null | undefined; children: ReactNode }) {
  const emoji = useChannelsEmoji(orgId);
  const value = useMemo<EmojiState>(() => {
    const list = emoji.data?.items ?? [];
    return { byName: new Map(list.map((e) => [e.name, e])), list, frequent: emoji.data?.frequent ?? [], canAdd: emoji.data?.can_add ?? false };
  }, [emoji.data]);
  return <EmojiContext.Provider value={value}>{children}</EmojiContext.Provider>;
}

export function useWorkspaceEmoji(): EmojiState {
  return useContext(EmojiContext);
}

/** A custom emoji image, or its :name: when the workspace doesn't have it (any more). */
export function CustomEmoji({ name, className }: { name: string; className?: string }) {
  const { byName } = useWorkspaceEmoji();
  const e = byName.get(name);
  if (!e) return <span className={className}>:{name}:</span>;
  // Signed, short-lived URLs: plain <img>.
  return <img src={e.url} alt={`:${name}:`} title={`:${name}:`} className={cn("inline-block size-[1.25em] align-text-bottom object-contain", className)} />;
}

/** A reaction or picker glyph: unicode, or `:name:` for custom emoji. */
export function EmojiGlyph({ value, className }: { value: string; className?: string }): ReactNode {
  const m = /^:([a-z0-9_+-]{2,40}):$/.exec(value);
  if (m?.[1]) return <CustomEmoji name={m[1]} className={className} />;
  return <span className={className}>{value}</span>;
}

/** Skin tone modifiers (1–5 = light → dark), applied to emoji that take one. */
const TONES = ["", "\u{1F3FB}", "\u{1F3FC}", "\u{1F3FD}", "\u{1F3FE}", "\u{1F3FF}"];
const TONABLE = /^(\p{Emoji_Modifier_Base})(️)?$/u;

export function withSkinTone(emoji: string, tone: number): string {
  if (!tone || !TONES[tone]) return emoji;
  const m = TONABLE.exec(emoji);
  return m ? `${m[1]}${TONES[tone]}` : emoji;
}

export const SKIN_TONE_SWATCHES = ["✋", "✋🏻", "✋🏼", "✋🏽", "✋🏾", "✋🏿"];
