"use client";

import {
  chatDayLabel,
  errorMessage,
  flattenThread,
  groupsWith,
  isReadBy,
  refreshChatAfterBlock,
  sameDay,
  useArchiveConversation,
  useChatConversation,
  useChatConversationTopic,
  useChatMessages,
  useChatTypingUsers,
  useDeleteMessage,
  useIsBlocked,
  useLeaveConversation,
  useMarkRead,
  useMe,
  useMuteConversation,
  useQueryClient,
  useSendMessage,
  useToggleReaction,
  useUnblockUser,
  type ChatConversationDetail,
  type ChatThreadMessage,
  type ChatUser,
} from "@scaffoldapp/shared";
import { Button, ConfirmDialog, EmptyState, ErrorState, IconButton, Menu, MenuItem, MenuSeparator, PhotoLightbox, Skeleton, Spinner, cn, toast, type GalleryPhoto } from "@scaffoldapp/ui-web";
import { Archive, ArrowDown, ArrowLeft, Ban, Bell, BellOff, Flag, Inbox, Info, LogOut, MessagesSquare, MoreVertical, ShieldCheck, Trash2, UserRoundCheck } from "lucide-react";
import Link from "next/link";
import { useRouter, useSearchParams } from "next/navigation";
import { useCallback, useEffect, useLayoutEffect, useMemo, useRef, useState } from "react";
import { BlockDialog, ReportDialog } from "@/features/moderation";
import { Composer } from "./Composer";
import { ConversationInfoSheet } from "./ConversationInfoSheet";
import { EmojiDialog, MuteDialog } from "./dialogs";
import { ConversationAvatar } from "./InboxPanel";
import { MessageItem, type MessageActions } from "./MessageItem";

/** "Jordan is typing…", "Jordan and Sam are typing…", "3 people are typing…". */
function typingText(users: ChatUser[]): string {
  const [a, b] = users;
  if (!a) return "";
  if (!b) return `${a.display_name} is typing…`;
  if (users.length === 2) return `${a.display_name} and ${b.display_name} are typing…`;
  return `${users.length} people are typing…`;
}

function ThreadSkeleton() {
  return (
    <div className="flex min-h-0 flex-1 flex-col" aria-busy aria-label="Loading conversation">
      <div className="flex min-h-topbar items-center gap-3 border-b border-border px-4">
        <Skeleton shape="circle" width={32} height={32} />
        <Skeleton className="h-5 w-40" />
      </div>
      <div className="flex flex-1 flex-col justify-end gap-4 px-gutter py-4 lg:px-6">
        <Skeleton className="h-10 w-2/3 rounded-lg" />
        <Skeleton className="ml-auto h-10 w-1/2 rounded-lg" />
        <Skeleton className="h-16 w-3/5 rounded-lg" />
        <Skeleton className="ml-auto h-10 w-2/5 rounded-lg" />
      </div>
      <div className="border-t border-border px-gutter py-3 lg:px-6">
        <Skeleton className="h-control w-full rounded-md" />
      </div>
    </div>
  );
}

/** /chat/<id>: header, messages (both directions, jump to `?message=`), typing, composer. */
export function ThreadScreen({ id }: { id: string }) {
  const around = useSearchParams()?.get("message") ?? null;
  const me = useMe();
  const conversation = useChatConversation(id);
  useChatConversationTopic(id);

  if (conversation.isPending) return <ThreadSkeleton />;
  if (conversation.isError) {
    if (conversation.error.status === 404) {
      return (
        <div className="flex flex-1 items-center justify-center px-gutter py-section">
          <EmptyState
            icon={<MessagesSquare />}
            title="Conversation not found"
            action={
              <div className="flex flex-wrap justify-center gap-2">
                <Button asChild variant="secondary" size="auto">
                  <Link href="/chat">Back to chat</Link>
                </Button>
                {me.data?.admin_role && (
                  <Button asChild variant="ghost" size="auto" leftIcon={<ShieldCheck />}>
                    <Link href={`/admin/chat/${id}${around ? `?around=${around}` : ""}`}>Open staff view</Link>
                  </Button>
                )}
              </div>
            }
          />
        </div>
      );
    }
    return (
      <div className="flex flex-1 items-center justify-center px-gutter py-section">
        <ErrorState title="Couldn't load this conversation" info={conversation.error.message} onRetry={() => void conversation.refetch()} retrying={conversation.isFetching} />
      </div>
    );
  }
  return <Thread key={id} conversation={conversation.data} around={around} meId={me.data?.id} suspended={me.data?.status === "suspended"} />;
}

function Thread({ conversation: c, around, meId, suspended }: { conversation: ChatConversationDetail; around: string | null; meId: string | undefined; suspended: boolean }) {
  const router = useRouter();
  const qc = useQueryClient();
  const thread = useChatMessages(c.id, around);
  const messages = useMemo(() => flattenThread(thread.data), [thread.data]);
  const members = useMemo(() => new Map(c.members.map((m) => [m.user.id, m])), [c.members]);
  const typing = useChatTypingUsers(c.id);
  const markRead = useMarkRead(c.id);
  const { mutate: reactMutate } = useToggleReaction(c.id);
  const remove = useDeleteMessage(c.id);
  const sender = useSendMessage(c.id);
  const mute = useMuteConversation(c.id);
  const archive = useArchiveConversation(c.id);
  const leave = useLeaveConversation();
  const unblock = useUnblockUser();
  const group = c.kind === "group";
  const peer = c.peer;
  const blockedByMe = useIsBlocked(peer?.id);

  const [replyTo, setReplyTo] = useState<ChatThreadMessage | null>(null);
  const [editing, setEditing] = useState<ChatThreadMessage | null>(null);
  const [reporting, setReporting] = useState<ChatThreadMessage | null>(null);
  const [deleting, setDeleting] = useState<ChatThreadMessage | null>(null);
  const [emojiFor, setEmojiFor] = useState<ChatThreadMessage | null>(null);
  const [lightbox, setLightbox] = useState<{ photos: GalleryPhoto[]; index: number } | null>(null);
  const [infoOpen, setInfoOpen] = useState(false);
  const [muteOpen, setMuteOpen] = useState(false);
  const [leaveOpen, setLeaveOpen] = useState(false);
  const [reportPerson, setReportPerson] = useState(false);
  const [blockOpen, setBlockOpen] = useState(false);
  const [highlight, setHighlight] = useState<string | null>(null);

  // ── scrolling ──
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

  const flash = useCallback((messageId: string) => {
    const el = document.getElementById(`message-${messageId}`);
    if (!el) return false;
    el.scrollIntoView({ block: "center" });
    setHighlight(messageId);
    return true;
  }, []);
  useEffect(() => {
    if (!highlight) return undefined;
    const t = setTimeout(() => setHighlight(null), 2500);
    return () => clearTimeout(t);
  }, [highlight]);

  // First position: the newest message, or the message jumped to.
  const positioned = useRef<string | null>(null);
  useLayoutEffect(() => {
    const key = around ?? "latest";
    if (positioned.current === key || messages.length === 0) return;
    positioned.current = key;
    if (around && flash(around)) return;
    scrollToBottom();
  }, [around, messages.length, flash, scrollToBottom]);

  // Keep the reading position when older messages load above; follow new ones at the bottom.
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
  // Images and video load after layout: stay at the bottom if we were there.
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

  // Paging sentinels.
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

  // Read state: the newest message counts as read once it's on screen.
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

  const lastOwn = useMemo(() => [...messages].reverse().find((m) => m.sender.id === meId && m.kind === "text" && !m.deleted && !m.status), [messages, meId]);
  const canInteract = !suspended && !(c.kind === "direct" && (c.blocked || !!peer?.deleted));

  const jumpTo = useCallback(
    (messageId: string) => {
      if (!flash(messageId)) router.replace(`/chat/${c.id}?message=${messageId}`);
    },
    [flash, router, c.id],
  );

  const actions: MessageActions = useMemo(
    () => ({
      onReply: (m) => {
        setEditing(null);
        setReplyTo(m);
      },
      onEdit: (m) => {
        setReplyTo(null);
        setEditing(m);
      },
      onDelete: setDeleting,
      onReport: setReporting,
      onReact: (m, emoji) => reactMutate({ message: m, emoji }, { onError: (err) => toast.error(errorMessage(err)) }),
      onPickEmoji: setEmojiFor,
      onRetry: sender.retry,
      onDiscard: sender.discard,
      onOpenImage: (m, attachmentId) => {
        const photos = m.attachments.filter((a) => a.kind === "image").map((a) => ({ id: a.id, url: a.url, caption: a.file_name || null, created_at: m.created_at }));
        setLightbox({ photos, index: Math.max(0, photos.findIndex((p) => p.id === attachmentId)) });
      },
      onJumpTo: jumpTo,
    }),
    [reactMutate, sender.retry, sender.discard, jumpTo],
  );

  const goLatest = () => {
    if (around || hasPreviousPage) router.replace(`/chat/${c.id}`);
    else scrollToBottom(true);
  };

  const composerNotice = suspended
    ? "Your account is suspended. You can read but not send."
    : c.kind === "direct" && peer?.deleted
      ? "This account was deleted."
      : c.kind === "direct" && c.blocked
        ? blockedByMe
          ? `You blocked ${peer?.display_name ?? "this person"}.`
          : "You can't reply to this conversation."
        : null;

  return (
    <div className="flex min-h-0 flex-1 flex-col">
      <header className="flex min-h-topbar items-center gap-1 border-b border-border px-2 lg:px-4">
        <IconButton className="lg:hidden" variant="ghost" label="Back to conversations" icon={<ArrowLeft />} onClick={() => router.push("/chat")} />
        <button
          type="button"
          onClick={() => setInfoOpen(true)}
          className="flex min-h-tap-sm min-w-0 flex-1 items-center gap-3 rounded-md px-2 py-1 text-left transition-colors hover:bg-sunken focus-visible:outline-2 focus-visible:outline-ring"
        >
          <ConversationAvatar conversation={c} size="sm" />
          <span className="min-w-0 truncate type-title-sm text-fg">{c.title}</span>
          {c.muted && <BellOff aria-label="Muted" className="size-4 shrink-0 text-fg-subtle" />}
        </button>
        <Menu trigger={<IconButton variant="ghost" label="Conversation options" icon={<MoreVertical />} />}>
          <MenuItem icon={<Info />} onSelect={() => setInfoOpen(true)}>
            Details
          </MenuItem>
          <MenuItem icon={c.muted ? <Bell /> : <BellOff />} onSelect={() => (c.muted ? mute.mutate(null) : setMuteOpen(true))}>
            {c.muted ? "Unmute" : "Mute"}
          </MenuItem>
          <MenuItem icon={c.archived ? <Inbox /> : <Archive />} onSelect={() => archive.mutate(!c.archived, { onSuccess: () => toast.success(c.archived ? "Moved to your inbox." : "Archived.") })}>
            {c.archived ? "Move to inbox" : "Archive"}
          </MenuItem>
          {peer && !peer.deleted && (
            <>
              <MenuSeparator />
              <MenuItem icon={<Flag />} onSelect={() => setReportPerson(true)}>
                Report {peer.display_name}
              </MenuItem>
              {blockedByMe ? (
                <MenuItem icon={<UserRoundCheck />} onSelect={() => unblock.mutate(peer.id, { onSuccess: () => void refreshChatAfterBlock(qc) })}>
                  Unblock
                </MenuItem>
              ) : (
                <MenuItem icon={<Ban />} destructive onSelect={() => setBlockOpen(true)}>
                  Block
                </MenuItem>
              )}
            </>
          )}
          <MenuSeparator />
          <MenuItem icon={group ? <LogOut /> : <Trash2 />} destructive onSelect={() => setLeaveOpen(true)}>
            {group ? "Leave group" : "Delete conversation"}
          </MenuItem>
        </Menu>
      </header>

      <div className="relative flex min-h-0 flex-1 flex-col">
        <div ref={scroller} onScroll={onScroll} className="min-h-0 flex-1 overflow-y-auto overscroll-contain" role="log" aria-label="Messages" aria-live="polite" aria-relevant="additions">
          <div ref={content} className="flex min-h-full flex-col justify-end pb-4">
            <div ref={topRef} aria-hidden className="h-px" />
            {isFetchingNextPage && (
              <div className="flex justify-center py-3">
                <Spinner size={18} label="Loading older messages" />
              </div>
            )}
            {thread.isPending ? (
              <div className="flex flex-col gap-4 px-gutter py-4 lg:px-6" aria-hidden>
                <Skeleton className="h-10 w-2/3 rounded-lg" />
                <Skeleton className="ml-auto h-10 w-1/2 rounded-lg" />
                <Skeleton className="h-16 w-3/5 rounded-lg" />
              </div>
            ) : thread.isError ? (
              <ErrorState compact title="Couldn't load messages" info={thread.error.message} onRetry={() => void thread.refetch()} retrying={thread.isFetching} />
            ) : messages.length === 0 ? (
              <EmptyState compact icon={<MessagesSquare />} title="No messages yet" />
            ) : (
              messages.map((m, i) => {
                const before = messages[i - 1];
                const newDay = !before || !sameDay(before.created_at, m.created_at);
                return (
                  <div key={m.client_id && m.sender.id === meId ? `c-${m.client_id}` : m.id}>
                    {newDay && (
                      <div className="flex items-center gap-3 px-gutter pt-6 lg:px-6" role="separator" aria-label={chatDayLabel(m.created_at)}>
                        <span className="h-px flex-1 bg-border" />
                        <span className="type-caption font-semibold text-fg-muted">{chatDayLabel(m.created_at)}</span>
                        <span className="h-px flex-1 bg-border" />
                      </div>
                    )}
                    <MessageItem
                      message={m}
                      meId={meId}
                      group={group}
                      grouped={!newDay && groupsWith(before, m)}
                      status={m.id === lastOwn?.id ? (isReadBy(c.others_read, m) ? "Read" : "Sent") : undefined}
                      highlighted={highlight === m.id}
                      members={members}
                      actions={actions}
                      canInteract={canInteract}
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

      <p className={cn("min-h-6 px-gutter type-caption text-fg-muted lg:px-6", typing.length === 0 && "invisible")} aria-live="polite">
        {typingText(typing)}
      </p>
      {composerNotice ? (
        <div className="flex min-h-tap-xl items-center justify-center gap-3 border-t border-border bg-surface px-gutter py-3 lg:px-6">
          <span className="type-label text-fg-muted">{composerNotice}</span>
          {blockedByMe && peer && (
            <Button size="sm" variant="secondary" loading={unblock.isPending} onClick={() => unblock.mutate(peer.id, { onSuccess: () => void refreshChatAfterBlock(qc) })}>
              Unblock
            </Button>
          )}
        </div>
      ) : (
        <Composer
          conversationId={c.id}
          replyTo={replyTo}
          onCancelReply={() => setReplyTo(null)}
          editing={editing}
          onCancelEdit={() => setEditing(null)}
          onSent={() => {
            atBottomRef.current = true;
            if (around || hasPreviousPage) router.replace(`/chat/${c.id}`);
            else requestAnimationFrame(() => scrollToBottom());
          }}
        />
      )}

      <ConversationInfoSheet conversation={c} open={infoOpen} onOpenChange={setInfoOpen} meId={meId} />
      <MuteDialog open={muteOpen} onOpenChange={setMuteOpen} onMute={(v) => mute.mutate(v, { onError: (err) => toast.error(errorMessage(err)) })} />
      <EmojiDialog
        open={emojiFor !== null}
        onOpenChange={(open) => !open && setEmojiFor(null)}
        onPick={(emoji) => {
          if (emojiFor) actions.onReact(emojiFor, emoji);
          setEmojiFor(null);
        }}
      />
      <ConfirmDialog
        open={deleting !== null}
        onOpenChange={(open) => !open && setDeleting(null)}
        title="Delete this message for everyone?"
        confirmLabel="Delete"
        tone="danger"
        onConfirm={() => (deleting ? remove.mutateAsync(deleting.id) : undefined)}
      />
      <ConfirmDialog
        open={leaveOpen}
        onOpenChange={setLeaveOpen}
        title={group ? `Leave ${c.title}?` : "Delete this conversation?"}
        confirmLabel={group ? "Leave group" : "Delete"}
        tone="danger"
        onConfirm={async () => {
          await leave.mutateAsync(c.id);
          router.push("/chat");
        }}
      >
        {group ? "You stop getting its messages. Someone in the group can add you back." : `Its messages are removed for you. ${peer?.display_name ?? "The other person"} still sees them.`}
      </ConfirmDialog>
      {reporting && (
        <ReportDialog
          open
          onOpenChange={(open) => !open && setReporting(null)}
          targetKind="chat.message"
          targetId={reporting.id}
          noun="message"
          author={reporting.sender.deleted ? null : { id: reporting.sender.id, name: reporting.sender.display_name }}
          onReported={(r) => r.blocked && void refreshChatAfterBlock(qc)}
        />
      )}
      {peer && !peer.deleted && (
        <>
          <ReportDialog open={reportPerson} onOpenChange={setReportPerson} targetKind="user" targetId={peer.id} author={{ id: peer.id, name: peer.display_name }} onReported={(r) => r.blocked && void refreshChatAfterBlock(qc)} />
          <BlockDialog open={blockOpen} onOpenChange={setBlockOpen} userId={peer.id} name={peer.display_name} onBlocked={() => void refreshChatAfterBlock(qc)} />
        </>
      )}
      {lightbox && <PhotoLightbox photos={lightbox.photos} open onOpenChange={(open) => !open && setLightbox(null)} defaultIndex={lightbox.index} />}
    </div>
  );
}
