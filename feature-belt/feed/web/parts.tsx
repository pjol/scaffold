"use client";

import {
  attachmentExtension,
  attachmentName,
  FEED_REACTIONS,
  splitText,
  type FeedMedia,
  type FeedMention,
  type FeedReaction,
  type FeedReactionCounts,
} from "@scaffoldapp/shared";
import { Button, cn, IconButton, Menu, MenuItem, PhotoLightbox, type GalleryPhoto } from "@scaffoldapp/ui-web";
import { ChevronDown, FileText, Frown, Heart, Laugh, Play, Sparkles, ThumbsUp, type LucideIcon } from "lucide-react";
import Image from "next/image";
import { useState, type CSSProperties, type ReactNode } from "react";
import { isOptimisable } from "@/lib/images";

export const REACTION_META: Record<FeedReaction, { label: string; icon: LucideIcon }> = {
  like: { label: "Like", icon: ThumbsUp },
  love: { label: "Love", icon: Heart },
  laugh: { label: "Haha", icon: Laugh },
  wow: { label: "Wow", icon: Sparkles },
  sad: { label: "Sad", icon: Frown },
};

/** Post or comment text with @mentions and links. Keeps line breaks. */
export function RichText({ text, mentions, className }: { text: string; mentions: readonly FeedMention[]; className?: string }) {
  if (!text) return null;
  return (
    <p className={cn("whitespace-pre-wrap break-words type-body text-fg", className)}>
      {splitText(text, mentions).map((seg, i) =>
        seg.type === "mention" ? (
          <span key={i} className="font-semibold text-primary-strong">
            {seg.text}
          </span>
        ) : seg.type === "link" ? (
          <a key={i} href={seg.href} target="_blank" rel="noopener noreferrer nofollow ugc" className="text-primary-strong underline underline-offset-2 break-all">
            {seg.text}
          </a>
        ) : (
          <span key={i}>{seg.text}</span>
        ),
      )}
    </p>
  );
}

/** Reaction counts in a short row: the three most used icons and the total. */
export function ReactionSummary({ reactions, total }: { reactions: FeedReactionCounts; total: number }) {
  if (total <= 0) return null;
  const top = FEED_REACTIONS.filter((r) => (reactions[r] ?? 0) > 0)
    .sort((a, b) => (reactions[b] ?? 0) - (reactions[a] ?? 0))
    .slice(0, 3);
  const label = top.map((r) => `${reactions[r]} ${REACTION_META[r].label.toLowerCase()}`).join(", ");
  return (
    <span className="flex items-center gap-1.5 type-caption text-fg-muted" aria-label={label} title={label}>
      <span className="flex -space-x-1" aria-hidden>
        {top.map((r) => {
          const Icon = REACTION_META[r].icon;
          return (
            <span key={r} className="flex size-5 items-center justify-center rounded-pill border border-surface bg-primary-soft text-primary-strong">
              <Icon className="size-3" />
            </span>
          );
        })}
      </span>
      <span aria-hidden>{total}</span>
    </span>
  );
}

/**
 * The viewer's reaction: one button toggles Like (or clears the current reaction), the
 * chevron opens the full set.
 */
export function ReactButton({ mine, onReact, disabled, size = "sm" }: { mine: FeedReaction | null; onReact: (r: FeedReaction | null) => void; disabled?: boolean; size?: "sm" | "md" }) {
  const current = mine ? REACTION_META[mine] : REACTION_META.like;
  const Icon = current.icon;
  return (
    <span className="flex items-center">
      <Button
        variant="ghost"
        size={size}
        disabled={disabled}
        aria-pressed={!!mine}
        leftIcon={<Icon className={cn(mine && "fill-current")} />}
        className={cn("rounded-r-none", mine ? "text-primary-strong" : "text-fg-muted")}
        onClick={() => onReact(mine ? null : "like")}
      >
        {current.label}
      </Button>
      <Menu
        align="start"
        trigger={<IconButton label="Choose a reaction" icon={<ChevronDown />} variant="ghost" size={size} disabled={disabled} className="rounded-l-none text-fg-muted" />}
      >
        {FEED_REACTIONS.map((r) => {
          const M = REACTION_META[r];
          return (
            <MenuItem key={r} icon={<M.icon />} onSelect={() => onReact(r === mine ? null : r)}>
              {r === mine ? `Remove ${M.label.toLowerCase()}` : M.label}
            </MenuItem>
          );
        })}
      </Menu>
    </span>
  );
}

function MediaTile({ media, onOpen, className, style, more }: { media: FeedMedia; onOpen: () => void; className?: string; style?: CSSProperties; more?: number }) {
  if (media.kind === "video") {
    return (
      <div className={cn("relative overflow-hidden rounded-md bg-inverse", className)} style={style}>
        <video src={media.url} controls preload="metadata" playsInline className="size-full object-contain" aria-label="Video" />
      </div>
    );
  }
  return (
    <button type="button" onClick={onOpen} style={style} className={cn("group relative overflow-hidden rounded-md bg-sunken focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-ring", className)} aria-label={more ? `Open photo, ${more} more` : "Open photo"}>
      <Image src={media.url} alt="" fill sizes="(min-width: 768px) 640px, 100vw" className="object-cover transition-opacity group-hover:opacity-90 motion-reduce:transition-none" unoptimized={!isOptimisable(media.url)} />
      {more ? <span className="absolute inset-0 flex items-center justify-center bg-overlay type-title text-fg-inverse">+{more}</span> : null}
    </button>
  );
}

/** Photos and videos in a grid sized by count (1, 2, 3, 4+), photos open in a lightbox; documents below as file rows. */
export function MediaGrid({ media }: { media: readonly FeedMedia[] }) {
  const visual = media.filter((m) => m.kind !== "file");
  const files = media.filter((m) => m.kind === "file");
  const photos: GalleryPhoto[] = visual.filter((m) => m.kind === "image").map((m) => ({ id: m.id, url: m.url }));
  const [open, setOpen] = useState(false);
  const [index, setIndex] = useState(0);
  const openAt = (m: FeedMedia) => {
    const i = photos.findIndex((p) => p.id === m.id);
    if (i < 0) return;
    setIndex(i);
    setOpen(true);
  };
  const shown = visual.slice(0, 4);
  const extra = visual.length - shown.length;
  const single = shown.length === 1 ? shown[0] : undefined;
  // One photo keeps its shape within 4:5 … 1.91:1 (video defaults to 16:9).
  const ratio = single?.width && single.height ? Math.min(Math.max(single.width / single.height, 0.8), 1.91) : single?.kind === "video" ? 16 / 9 : 4 / 3;

  return (
    <div className="flex flex-col gap-2">
      {single ? (
        <MediaTile media={single} onOpen={() => openAt(single)} className="w-full" style={{ aspectRatio: ratio }} />
      ) : shown.length > 0 ? (
        <div className={cn("grid aspect-4/3 gap-1 overflow-hidden rounded-md", shown.length === 2 ? "grid-cols-2" : "grid-cols-2 grid-rows-2")}>
          {shown.map((m, i) => (
            <MediaTile
              key={m.id}
              media={m}
              onOpen={() => openAt(m)}
              more={i === shown.length - 1 && extra > 0 ? extra : undefined}
              className={cn("h-full", shown.length === 3 && i === 0 && "row-span-2")}
            />
          ))}
        </div>
      ) : null}
      {files.length > 0 ? (
        <ul className="flex flex-col gap-2" aria-label="Attachments">
          {files.map((f) => (
            <li key={f.id}>
              <FileRow media={f} />
            </li>
          ))}
        </ul>
      ) : null}
      {photos.length > 0 ? <PhotoLightbox photos={photos} open={open} onOpenChange={setOpen} index={index} onIndexChange={setIndex} /> : null}
    </div>
  );
}

/** A document attachment: icon, name, type; opens in a new tab. */
export function FileRow({ media, trailing }: { media: { name: string | null; key: string; url?: string }; trailing?: ReactNode }) {
  const name = attachmentName(media);
  return (
    <div className="flex min-h-row items-center gap-3 rounded-md border border-border bg-surface px-3">
      <span className="flex size-9 shrink-0 items-center justify-center rounded-sm bg-primary-soft text-primary-strong" aria-hidden>
        <FileText className="size-4" />
      </span>
      {media.url ? (
        <a href={media.url} target="_blank" rel="noopener noreferrer" className="min-w-0 flex-1 truncate type-label text-fg underline-offset-2 hover:underline">
          {name}
        </a>
      ) : (
        <span className="min-w-0 flex-1 truncate type-label text-fg">{name}</span>
      )}
      <span className="type-caption text-fg-muted">{attachmentExtension(name)}</span>
      {trailing}
    </div>
  );
}

/** A video poster for composer previews. */
export function VideoBadge() {
  return (
    <span className="absolute inset-0 flex items-center justify-center bg-overlay text-fg-inverse" aria-hidden>
      <Play className="size-6 fill-current" />
    </span>
  );
}
