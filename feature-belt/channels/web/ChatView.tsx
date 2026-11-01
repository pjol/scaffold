"use client";

import {
  channelsDayLabel,
  channelsDisplayName,
  channelsGroupsWith,
  channelsSameDay,
  channelsTypingText,
  flattenChannelsPages,
  useChannelsMarkRead,
  useChannelsMessages,
  useChannelsTypingUsers,
  type ChannelsCachedMessage,
  type ChannelsConfig,
  type ChannelsDetail,
} from "@scaffoldapp/shared";
import { Button, EmptyState, ErrorState, Skeleton, Spinner, cn } from "@scaffoldapp/ui-web";
import { ArrowDown, Hash, Lock } from "lucide-react";
import { useRouter } from "next/navigation";
import { useCallback, useEffect, useLayoutEffect, useMemo, useRef, useState, type ReactNode } from "react";
import { Composer } from "./Composer";
import { MessageItem } from "./MessageItem";
import { useMessageActions } from "./useMessageActions";

export function messageNames(messages: readonly ChannelsCachedMessage[]): Map<string, string> {
  const names = new Map<string, string>();
  for (const m of messages) {
    if (m.sender) names.set(m.sender.id, m.sender.display_name);
    for (const u of m.mentions) names.set(u.id, u.display_name);
    for (const u of m.reply_users) names.set(u.id, u.display_name);
  }
  return names;
}

function DaySeparator({ iso }: { iso: string }) {
  return (
    <div className="flex items-center gap-3 px-gutter pt-4 lg:px-5" role="separator" aria-label={channelsDayLabel(iso)}>
      <span className="h-px flex-1 bg-border" />
      <span className="rounded-pill border border-border bg-surface px-3 py-0.5 type-caption font-semibold text-fg-muted">{channelsDayLabel(iso)}</span>
      <span className="h-px flex-1 bg-border" />
    </div>
  );
}

function ChannelIntro({ channel }: { channel: ChannelsDetail }) {
  const direct = channel.kind === "dm" || channel.kind === "group_dm";
  return (
    <div className="flex flex-col gap-2 px-gutter pb-2 pt-section lg:px-5">
      <span aria-hidden className="flex size-12 items-center justify-center rounded-lg bg-primary-soft text-primary-strong [&_svg]:size-6">
        {channel.kind === "private" ? <Lock /> : <Hash />}
      </span>
      <h2 className="type-title text-fg">{direct ? channel.name : channelsDisplayName(channel)}</h2>
      {channel.description && <p className="max-w-prose type-body text-fg-muted">{channel.description}</p>}
    </div>
  );
}

export interface ChatViewProps {
  channel: ChannelsDetail;
  config: ChannelsConfig;
  meId: string | undefined;
  around: string | null;
  onOpenThread: (rootId: string, messageId?: string) => void;
  /** Replaces the composer (join bar, archived notice). */
  composerSlot?: ReactNode;
}

/** A channel's timeline (both directions, jump to `?message=`, a "New" divider), typing and composer. */
export function ChatView({ channel, config, meId, around, onOpenThread, composerSlot }: ChatViewProps) {
  const router = useRouter();
  const thread = useChannelsMessages(channel.id, around);
  const messages = useMemo(() => flattenChannelsPages(thread.data), [thread.data]);
  const names = useMemo(() => messageNames(messages), [messages]);
  const typing = useChannelsTypingUsers(channel.id);
  const markRead = useChannelsMarkRead(channel.member ? channel.id : null, channel.org_id);
  const [editing, setEditing] = useState<ChannelsCachedMessage | null>(null);
  const { actions, dialogs } = useMessageActions({ channel, onOpenThread, onEdit: setEditing });
  const [highlight, setHighlight] = useState<string | null>(null);
  // The read pointer when the channel opened: the "New" divider stays put while you read.
  const readAtOpen = useRef(channel.last_read_at);

  const scroller = useRef<HTMLDivElement>(null);
  const content = useRef<HTMLDivElement>(null);
  const atBottomRef = useRef(true);
  const [atBottom, setAtBottom] = useState(true);
  const onScroll = () => {
    const el = scroller.current;
    if (!el) return;
    const bottom = el.scrollHeight - el.scrollTop - el.clientHeight < 80;
    atBottomRef.current = bottom;
    setAtBottom(bottom);
  };
  const scrollToBottom = useCallback((smooth = false) => {
    const el = scroller.current;
    const reduce = typeof window.matchMedia === "function" && window.matchMedia("(prefers-reduced-motion: reduce)").matches;
    if (el) el.scrollTo({ top: el.scrollHeight, behavior: smooth && !reduce ? "smooth" : "auto" });
  }, []);
  const flash = useCallback((id: string) => {
    const el = document.getElementById(`message-${id}`);
    if (!el) return false;
    el.scrollIntoView({ block: "center" });
    setHighlight(id);
    return true;
  }, []);
  useEffect(() => {
    if (!highlight) return undefined;
    const t = setTimeout(() => setHighlight(null), 2500);
    return () => clearTimeout(t);
  }, [highlight]);

  const positioned = useRef<string | null>(null);
  useLayoutEffect(() => {
    const key = around ?? "latest";
    if (positioned.current === key || messages.length === 0) return;
    positioned.current = key;
    if (around && flash(around)) return;
    scrollToBottom();
  }, [around, messages.length, flash, scrollToBottom]);

  const prev = useRef<{ first?: string; last?: string; height: number }>({ height: 0 });
  useLayoutEffect(() => {
    const el = scroller.current;
    if (!el) return;
    const first = messages[0]?.id;
    const last = messages[messages.length - 1]?.id;
    const p = prev.current;
    if (p.first && first !== p.first && last === p.last) el.scrollTop += el.scrollHeight - p.height;
    else if (last !== p.last && p.last && atBottomRef.current) el.scrollTop = el.scrollHeight;
    prev.current = { first, last, height: el.scrollHeight };
  }, [messages]);
  useEffect(() => {
    const el = content.current;
    if (!el || typeof ResizeObserver === "undefined") return undefined;
    const ro = new ResizeObserver(() => {
      if (atBottomRef.current && !around) scrollToBottom();
      if (scroller.current) prev.current.height = scroller.current.scrollHeight;
    });
    ro.observe(el);
    return () => ro.disconnect();
  }, [around, scrollToBottom]);

  const topRef = useRef<HTMLDivElement>(null);
  const bottomRef = useRef<HTMLDivElement>(null);
  const { hasNextPage, hasPreviousPage, isFetchingNextPage, isFetchingPreviousPage, fetchNextPage, fetchPreviousPage } = thread;
  useEffect(() => {
    const root = scroller.current;
    if (!root || typeof IntersectionObserver === "undefined") return undefined;
    const io = new IntersectionObserver(
      (entries) => {
        for (const e of entries) {
          if (!e.isIntersecting) continue;
          if (e.target === topRef.current && hasNextPage && !isFetchingNextPage) void fetchNextPage();
          if (e.target === bottomRef.current && hasPreviousPage && !isFetchingPreviousPage) void fetchPreviousPage();
        }
      },
      { root, rootMargin: "320px 0px" },
    );
    if (topRef.current) io.observe(topRef.current);
    if (bottomRef.current) io.observe(bottomRef.current);
    return () => io.disconnect();
  }, [hasNextPage, hasPreviousPage, isFetchingNextPage, isFetchingPreviousPage, fetchNextPage, fetchPreviousPage, messages.length]);

  const latest = useMemo(() => [...messages].reverse().find((m) => !m.status), [messages]);
  useEffect(() => {
    if (!latest || !atBottom || hasPreviousPage) return undefined;
    const run = () => {
      if (document.visibilityState === "visible") markRead(latest);
    };
    run();
    document.addEventListener("visibilitychange", run);
    window.addEventListener("focus", run);
    return () => {
      document.removeEventListener("visibilitychange", run);
      window.removeEventListener("focus", run);
    };
  }, [latest, atBottom, hasPreviousPage, markRead]);

  const firstUnread = useMemo(() => {
    const at = readAtOpen.current;
    if (!at) return null;
    return messages.find((m) => !m.status && m.kind !== "system" && m.sender?.id !== meId && Date.parse(m.created_at) > Date.parse(at))?.id ?? null;
  }, [messages, meId]);

  const lastOwn = useMemo(() => [...messages].reverse().find((m) => m.sender?.id === meId && m.kind === "text" && !m.deleted && !m.status), [messages, meId]);
  const canInteract = channel.permissions.post;
  const editWindowMs = config.edit_window_seconds * 1000;

  const goLatest = () => {
    if (around || hasPreviousPage) router.replace(`/channels/${channel.id}`);
    else scrollToBottom(true);
  };

  return (
    <div className="flex min-h-0 flex-1 flex-col">
      <div className="relative flex min-h-0 flex-1 flex-col">
        <div ref={scroller} onScroll={onScroll} className="min-h-0 flex-1 overflow-y-auto overscroll-contain" role="log" aria-label="Messages" aria-live="polite" aria-relevant="additions">
          <div ref={content} className="flex min-h-full flex-col justify-end pb-4">
            <div ref={topRef} aria-hidden className="h-px" />
            {!thread.isPending && !hasNextPage && !thread.isError && <ChannelIntro channel={channel} />}
            {isFetchingNextPage && (
              <div className="flex justify-center py-3">
                <Spinner size={18} label="Loading older messages" />
              </div>
            )}
            {thread.isPending ? (
              <div className="flex flex-col gap-5 px-gutter py-4 lg:px-5" aria-hidden>
                {[0, 1, 2].map((i) => (
                  <div key={i} className="flex gap-3">
                    <Skeleton shape="circle" width={32} height={32} />
                    <div className="flex flex-1 flex-col gap-2">
                      <Skeleton className="h-4 w-40" />
                      <Skeleton className={cn("h-4", i === 1 ? "w-1/2" : "w-3/4")} />
                    </div>
                  </div>
                ))}
              </div>
            ) : thread.isError ? (
              <ErrorState compact title="Couldn't load messages" info={thread.error.message} onRetry={() => void thread.refetch()} retrying={thread.isFetching} />
            ) : messages.length === 0 ? (
              <EmptyState compact icon={<Hash />} title="No messages yet" />
            ) : (
              messages.map((m, i) => {
                const before = messages[i - 1];
                const newDay = !before || !channelsSameDay(before.created_at, m.created_at);
                const divider = m.id === firstUnread;
                return (
                  <div key={m.client_id && m.sender?.id === meId ? `c-${m.client_id}` : m.id}>
                    {newDay && <DaySeparator iso={m.created_at} />}
                    {divider && (
                      <div className="flex items-center gap-3 px-gutter pt-2 lg:px-5" role="separator" aria-label="New messages">
                        <span className="h-px flex-1 bg-danger" />
                        <span className="type-caption font-semibold text-danger-strong">New</span>
                      </div>
                    )}
                    <MessageItem
                      message={m}
                      meId={meId}
                      orgId={channel.org_id}
                      grouped={!newDay && !divider && channelsGroupsWith(before, m)}
                      highlighted={highlight === m.id}
                      names={names}
                      actions={actions}
                      canInteract={canInteract}
                      canModerate={channel.permissions.edit}
                      editWindowMs={editWindowMs}
                    />
                  </div>
                );
              })
            )}
            {isFetchingPreviousPage && (
              <div className="flex justify-center py-3">
                <Spinner size={18} label="Loading newer messages" />
              </div>
            )}
            <div ref={bottomRef} aria-hidden className="h-px" />
          </div>
        </div>
        {(!atBottom || hasPreviousPage) && messages.length > 0 && (
          <div className="pointer-events-none absolute inset-x-0 bottom-4 flex justify-center">
            <Button className="pointer-events-auto shadow-md" size="sm" variant="secondary" leftIcon={<ArrowDown />} onClick={goLatest}>
              Latest messages
            </Button>
          </div>
        )}
      </div>
      <p className={cn("min-h-6 px-gutter type-caption text-fg-muted lg:px-5", typing.length === 0 && "invisible")} aria-live="polite">
        {channelsTypingText(typing)}
      </p>
      {composerSlot ?? (
        <Composer
          channel={channel}
          config={config}
          editing={editing}
          onCancelEdit={() => setEditing(null)}
          onEditLast={lastOwn && (config.edit_window_seconds === 0 || Date.now() - Date.parse(lastOwn.created_at) < config.edit_window_seconds * 1000) ? () => setEditing(lastOwn) : undefined}
          onSent={() => {
            atBottomRef.current = true;
            if (around || hasPreviousPage) router.replace(`/channels/${channel.id}`);
            else requestAnimationFrame(() => scrollToBottom());
          }}
          autoFocus
        />
      )}
      {dialogs}
    </div>
  );
}
