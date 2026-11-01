"use client";

import {
  channelsDisplayName,
  channelsGroupsWith,
  channelsReplyCount,
  channelsTypingText,
  errorMessage,
  flattenChannelsPages,
  useChannelsActiveThread,
  useChannelsFollow,
  useChannelsMarkThreadRead,
  useChannelsThread,
  useChannelsTypingUsers,
  type ChannelsCachedMessage,
  type ChannelsConfig,
  type ChannelsDetail,
} from "@scaffoldapp/shared";
import { Button, EmptyState, ErrorState, IconButton, Skeleton, Spinner, cn, toast } from "@scaffoldapp/ui-web";
import { Bell, BellOff, MessagesSquare, X } from "lucide-react";
import { useEffect, useLayoutEffect, useMemo, useRef, useState } from "react";
import { messageNames } from "./ChatView";
import { Composer } from "./Composer";
import { MessageItem } from "./MessageItem";
import { useMessageActions } from "./useMessageActions";

export interface ThreadPaneProps {
  rootId: string;
  channel: ChannelsDetail;
  config: ChannelsConfig;
  meId: string | undefined;
  /** A reply to scroll to and highlight. */
  around: string | null;
  onClose: () => void;
  onOpenThread: (rootId: string, messageId?: string) => void;
}

/** The thread beside (or over) the channel: the first message, replies, follow, and a reply box. */
export function ThreadPane({ rootId, channel, config, meId, around, onClose, onOpenThread }: ThreadPaneProps) {
  const thread = useChannelsThread(rootId, around && around !== rootId ? around : null);
  const replies = useMemo(() => flattenChannelsPages(thread.data), [thread.data]);
  const first = thread.data?.pages[0];
  const root = first?.root;
  const following = thread.data?.pages[0]?.following ?? false;
  const follow = useChannelsFollow(rootId);
  const markRead = useChannelsMarkThreadRead(following ? rootId : null, channel.org_id);
  const typing = useChannelsTypingUsers(channel.id, rootId);
  const [editing, setEditing] = useState<ChannelsCachedMessage | null>(null);
  const { actions, dialogs } = useMessageActions({ channel, onOpenThread, onEdit: setEditing });
  const names = useMemo(() => messageNames(root ? [root, ...replies] : replies), [root, replies]);
  useChannelsActiveThread(rootId);

  const scroller = useRef<HTMLDivElement>(null);
  const [highlight, setHighlight] = useState<string | null>(null);
  const placed = useRef(false);
  useLayoutEffect(() => {
    if (placed.current || !thread.data) return;
    placed.current = true;
    const target = around && around !== rootId ? document.getElementById(`thread-${around}`) : null;
    if (target) {
      target.scrollIntoView({ block: "center" });
      setHighlight(around);
    } else if (scroller.current) scroller.current.scrollTop = scroller.current.scrollHeight;
  }, [thread.data, around, rootId]);
  useEffect(() => {
    if (!highlight) return undefined;
    const t = setTimeout(() => setHighlight(null), 2500);
    return () => clearTimeout(t);
  }, [highlight]);
  // Follow new replies when already at the bottom.
  const lastId = replies[replies.length - 1]?.id;
  useEffect(() => {
    const el = scroller.current;
    if (el && el.scrollHeight - el.scrollTop - el.clientHeight < 160) el.scrollTop = el.scrollHeight;
  }, [lastId]);

  const latest = useMemo(() => [...replies].reverse().find((m) => !m.status), [replies]);
  useEffect(() => {
    if (!latest) return undefined;
    const run = () => {
      if (document.visibilityState === "visible") markRead(latest);
    };
    run();
    document.addEventListener("visibilitychange", run);
    return () => document.removeEventListener("visibilitychange", run);
  }, [latest, markRead]);

  const lastOwn = useMemo(() => [...replies].reverse().find((m) => m.sender?.id === meId && m.kind === "text" && !m.deleted && !m.status), [replies, meId]);
  const editWindowMs = config.edit_window_seconds * 1000;
  const { hasNextPage, isFetchingNextPage, fetchNextPage } = thread;

  return (
    <div className="flex min-h-0 flex-1 flex-col bg-surface">
      <header className="flex min-h-topbar items-center gap-2 border-b border-border px-gutter lg:px-4">
        <h2 className="type-title-sm text-fg">Thread</h2>
        <span className="min-w-0 flex-1 truncate type-label text-fg-muted">{channelsDisplayName(channel)}</span>
        {channel.member && root && (
          <Button
            size="sm"
            variant={following ? "ghost" : "secondary"}
            leftIcon={following ? <BellOff /> : <Bell />}
            onClick={() => follow.mutate(!following, { onError: (e) => toast.error(errorMessage(e)) })}
          >
            {following ? "Unfollow" : "Follow"}
          </Button>
        )}
        <IconButton variant="ghost" label="Close thread" icon={<X />} onClick={onClose} />
      </header>
      <div ref={scroller} className="min-h-0 flex-1 overflow-y-auto overscroll-contain pb-4" role="log" aria-label="Thread" aria-live="polite" aria-relevant="additions">
        {thread.isPending ? (
          <div className="flex flex-col gap-4 p-gutter" aria-hidden>
            <Skeleton className="h-16 w-full" />
            <Skeleton className="h-10 w-3/4" />
            <Skeleton className="h-10 w-2/3" />
          </div>
        ) : thread.isError ? (
          thread.error.status === 404 ? (
            <EmptyState compact icon={<MessagesSquare />} title="Thread not found" />
          ) : (
            <ErrorState compact title="Couldn't load this thread" info={thread.error.message} onRetry={() => void thread.refetch()} retrying={thread.isFetching} />
          )
        ) : root ? (
          <>
            <div className="pt-3">
              <MessageItem
                message={root}
                meId={meId}
                orgId={channel.org_id}
                grouped={false}
                highlighted={highlight === root.id}
                names={names}
                actions={actions}
                canInteract={channel.permissions.post}
                canModerate={channel.permissions.edit}
                inThread
                editWindowMs={editWindowMs}
              />
            </div>
            <div className="flex items-center gap-3 px-gutter py-2 lg:px-5" role="separator">
              <span className="shrink-0 type-caption font-semibold text-fg-muted">{root.reply_count > 0 ? channelsReplyCount(root.reply_count) : "No replies yet"}</span>
              <span className="h-px flex-1 bg-border" />
            </div>
            {hasNextPage && (
              <div className="flex justify-center py-1">
                <Button size="sm" variant="ghost" loading={isFetchingNextPage} onClick={() => void fetchNextPage()}>
                  Earlier replies
                </Button>
              </div>
            )}
            {replies.map((m, i) => (
              <div key={m.client_id && m.sender?.id === meId ? `c-${m.client_id}` : m.id} id={`thread-${m.id}`}>
                <MessageItem
                  message={m}
                  meId={meId}
                  orgId={channel.org_id}
                  grouped={channelsGroupsWith(replies[i - 1], m)}
                  highlighted={highlight === m.id}
                  names={names}
                  actions={actions}
                  canInteract={channel.permissions.post}
                  canModerate={channel.permissions.edit}
                  inThread
                  editWindowMs={editWindowMs}
                />
              </div>
            ))}
            {thread.isFetchingPreviousPage && (
              <div className="flex justify-center py-3">
                <Spinner size={18} label="Loading newer replies" />
              </div>
            )}
          </>
        ) : null}
      </div>
      <p className={cn("min-h-6 px-gutter type-caption text-fg-muted lg:px-5", typing.length === 0 && "invisible")} aria-live="polite">
        {channelsTypingText(typing)}
      </p>
      {root && !root.deleted && channel.permissions.post && (
        <Composer
          channel={channel}
          config={config}
          threadRootId={rootId}
          editing={editing}
          onCancelEdit={() => setEditing(null)}
          onEditLast={lastOwn && (editWindowMs === 0 || Date.now() - Date.parse(lastOwn.created_at) < editWindowMs) ? () => setEditing(lastOwn) : undefined}
          onSent={() => requestAnimationFrame(() => scroller.current && (scroller.current.scrollTop = scroller.current.scrollHeight))}
          autoFocus
        />
      )}
      {dialogs}
    </div>
  );
}
