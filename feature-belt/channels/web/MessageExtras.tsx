"use client";

import {
  channelsAuthorName,
  channelsDisplayName,
  channelsMessagePath,
  channelsShortTime,
  errorMessage,
  useChannelsCard,
  useChannelsClosePoll,
  useChannelsMessage,
  useChannelsVote,
  type ChannelsCachedMessage,
  type ChannelsLinkPreview,
} from "@scaffoldapp/shared";
import { Button, Checkbox, IconButton, Skeleton, cn, toast } from "@scaffoldapp/ui-web";
import { BarChart3, CalendarDays, FileText, Forward, GitPullRequest, Hash, Lock, MessageSquare, X, type LucideIcon } from "lucide-react";
import Link from "next/link";
import { Markdown } from "./Markdown";

const CARD_ICONS: Record<string, LucideIcon> = { hash: Hash, lock: Lock, "message-square": MessageSquare, "file-text": FileText, calendar: CalendarDays, "git-pull-request": GitPullRequest };

/** A link to the app, shown as a card when you may see what it points at. */
function InternalCard({ url }: { url: string }) {
  const card = useChannelsCard(url);
  if (card.isPending) return <Skeleton className="h-14 w-full max-w-md rounded-md" />;
  if (!card.data) return null;
  const c = card.data;
  const Icon = CARD_ICONS[c.icon] ?? FileText;
  return (
    <Link
      href={c.link}
      className="flex w-full max-w-md items-start gap-3 rounded-md border border-border bg-surface px-3 py-2 transition-colors hover:bg-sunken focus-visible:outline-2 focus-visible:outline-ring"
    >
      <Icon aria-hidden className="mt-0.5 size-4 shrink-0 text-fg-muted" />
      <span className="flex min-w-0 flex-1 flex-col">
        <span className="truncate type-label font-semibold text-fg">{c.title}</span>
        {c.context && <span className="truncate type-caption text-fg-muted">{c.context}</span>}
        {c.snippet && <span className="line-clamp-2 type-label text-fg-muted">{c.snippet}</span>}
      </span>
    </Link>
  );
}

function ExternalCard({ link }: { link: ChannelsLinkPreview }) {
  const ratio = link.image_width && link.image_height ? link.image_width / link.image_height : null;
  const wide = ratio !== null && ratio > 1.3;
  return (
    <a
      href={link.url}
      target="_blank"
      rel="noopener noreferrer nofollow"
      className={cn(
        "flex w-full max-w-md overflow-hidden rounded-md border border-border bg-surface transition-colors hover:bg-sunken focus-visible:outline-2 focus-visible:outline-ring",
        wide ? "flex-col" : "items-start gap-3 p-3",
      )}
    >
      {link.image_url && wide && <img src={link.image_url} alt="" loading="lazy" className="max-h-56 w-full object-cover" />}
      <span className={cn("flex min-w-0 flex-1 flex-col gap-0.5", wide && "p-3")}>
        <span className="flex min-w-0 items-center gap-1.5">
          {link.icon_url && <img src={link.icon_url} alt="" className="size-4 shrink-0 rounded-xs" loading="lazy" />}
          <span className="truncate type-caption text-fg-muted">{link.site_name}</span>
        </span>
        {link.title && <span className="line-clamp-2 type-label font-semibold text-primary-strong">{link.title}</span>}
        {link.description && <span className="line-clamp-3 type-label text-fg-muted">{link.description}</span>}
      </span>
      {link.image_url && !wide && <img src={link.image_url} alt="" loading="lazy" className="size-16 shrink-0 rounded-sm object-cover" />}
    </a>
  );
}

/** A message's link previews; the author can remove them. Hidden when the conversation is collapsed. */
export function LinkPreviews({ message, own, onRemove }: { message: ChannelsCachedMessage; own: boolean; onRemove: () => void }) {
  if (message.links_hidden || message.links.length === 0) return null;
  const external = message.links.filter((l) => l.kind === "external");
  const internal = message.links.filter((l) => l.kind === "internal");
  return (
    <div className="group/links relative flex flex-col gap-2">
      {internal.map((l) => (
        <InternalCard key={l.url} url={l.url} />
      ))}
      {external.map((l) => (
        <ExternalCard key={l.url} link={l} />
      ))}
      {own && external.length > 0 && (
        <span className="absolute left-[min(28rem,calc(100%-2rem))] top-1 hidden group-focus-within/links:block group-hover/links:block pointer-coarse:block">
          <IconButton size="sm" variant="ghost" label="Remove previews" icon={<X />} onClick={onRemove} />
        </span>
      )}
    </div>
  );
}

/** A forwarded message: the original, loaded as you (gone or not yours to see: says so). */
export function ForwardCard({ messageId, meId }: { messageId: string | null; meId: string | undefined }) {
  const original = useChannelsMessage(messageId);
  const box = "flex w-full max-w-lg flex-col gap-1 rounded-md border border-border bg-surface px-3 py-2";
  if (!messageId || original.isError) {
    return (
      <div className={box}>
        <span className="flex items-center gap-1.5 type-caption text-fg-muted">
          <Forward aria-hidden className="size-3.5" />
          Forwarded message unavailable
        </span>
      </div>
    );
  }
  if (original.isPending) return <Skeleton className="h-16 w-full max-w-lg rounded-md" />;
  const { message: m, channel } = original.data;
  const where = channelsDisplayName(channel);
  return (
    <div className={box}>
      <span className="flex min-w-0 items-center gap-1.5 type-caption text-fg-muted">
        <Forward aria-hidden className="size-3.5 shrink-0" />
        <span className="truncate">
          {channelsAuthorName(m, meId)} · {where} · {channelsShortTime(m.created_at)}
        </span>
      </span>
      {m.deleted ? <p className="type-body italic text-fg-muted">Message deleted</p> : <Markdown body={m.body} mentions={m.mentions} meId={meId} className="max-h-60 overflow-hidden" />}
      {m.attachments.length > 0 && <span className="type-caption text-fg-muted">{m.attachments.length === 1 ? "1 file" : `${m.attachments.length} files`}</span>}
      <Link href={channelsMessagePath(m)} className="w-fit type-caption font-semibold text-primary-strong underline-offset-2 hover:underline focus-visible:outline-2 focus-visible:outline-ring">
        View message
      </Link>
    </div>
  );
}

/** A poll: vote (one or several), live counts, voters for named polls, close for the author. */
export function PollCard({ message, meId, canVote, canClose, names }: { message: ChannelsCachedMessage; meId: string | undefined; canVote: boolean; canClose: boolean; names: Map<string, string> }) {
  const poll = message.poll;
  const vote = useChannelsVote();
  const close = useChannelsClosePoll();
  if (!poll) return null;
  const mine = new Set(poll.my_votes ?? []);
  const total = poll.options.reduce((n, o) => n + o.count, 0);
  const open = !poll.closed && canVote && !message.status;
  const choose = (id: string) => {
    const next = poll.multiple ? (mine.has(id) ? [...mine].filter((x) => x !== id) : [...mine, id]) : mine.has(id) ? [] : [id];
    vote.mutate({ message, optionIds: next }, { onError: (e) => toast.error(errorMessage(e)) });
  };
  const voterNames = (ids: string[]) => ids.map((id) => (id === meId ? "You" : (names.get(id) ?? "Someone"))).join(", ");
  return (
    <div className="flex w-full max-w-md flex-col gap-3 rounded-md border border-border bg-surface p-3" role="group" aria-label={`Poll: ${message.body}`}>
      <div className="flex items-start gap-2">
        <BarChart3 aria-hidden className="mt-0.5 size-4 shrink-0 text-primary-strong" />
        <Markdown body={message.body} mentions={message.mentions} meId={meId} className="font-semibold" />
      </div>
      <ul className="flex flex-col gap-2">
        {poll.options.map((o) => {
          const pct = total ? Math.round((o.count / total) * 100) : 0;
          const chosen = mine.has(o.id);
          return (
            <li key={o.id} className="flex flex-col gap-1">
              <div className="flex items-center gap-2">
                {open ? (
                  poll.multiple ? (
                    <Checkbox checked={chosen} onCheckedChange={() => choose(o.id)} label={o.text} />
                  ) : (
                    <button
                      type="button"
                      role="radio"
                      aria-checked={chosen}
                      onClick={() => choose(o.id)}
                      className="flex min-h-tap-sm min-w-0 flex-1 items-center gap-2 rounded-sm text-left focus-visible:outline-2 focus-visible:outline-ring"
                    >
                      <span aria-hidden className={cn("flex size-4 shrink-0 items-center justify-center rounded-full border", chosen ? "border-primary" : "border-border-strong")}>
                        {chosen && <span className="size-2 rounded-full bg-primary" />}
                      </span>
                      <span className="min-w-0 flex-1 type-label text-fg">{o.text}</span>
                    </button>
                  )
                ) : (
                  <span className={cn("min-w-0 flex-1 type-label text-fg", chosen && "font-semibold")}>{o.text}</span>
                )}
                <span className="shrink-0 type-caption tabular-nums text-fg-muted">
                  {o.count} · {pct}%
                </span>
              </div>
              <span className="h-1.5 w-full overflow-hidden rounded-pill bg-sunken" aria-hidden>
                <span className={cn("block h-full rounded-pill transition-[width] motion-reduce:transition-none", chosen ? "bg-primary" : "bg-border-strong")} style={{ width: `${pct}%` }} />
              </span>
              {!poll.anonymous && o.voter_ids.length > 0 && <span className="truncate type-caption text-fg-subtle">{voterNames(o.voter_ids)}</span>}
            </li>
          );
        })}
      </ul>
      <div className="flex flex-wrap items-center gap-2">
        <span className="flex-1 type-caption text-fg-muted">
          {poll.voters === 1 ? "1 vote" : `${poll.voters} votes`}
          {poll.multiple ? " · Several choices" : ""}
          {poll.anonymous ? " · Anonymous" : ""}
          {poll.closed ? " · Closed" : ""}
        </span>
        {canClose && !poll.closed && (
          <Button size="sm" variant="ghost" loading={close.isPending} onClick={() => close.mutate(message, { onError: (e) => toast.error(errorMessage(e)) })}>
            Close poll
          </Button>
        )}
      </div>
    </div>
  );
}
