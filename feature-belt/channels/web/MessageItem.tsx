"use client";

import {
  CHANNELS_QUICK_REACTIONS,
  CHANNELS_REMIND_OPTIONS,
  activeCustomStatus,
  channelsFullTime,
  channelsReplyCount,
  channelsShortTime,
  channelsSystemText,
  channelsTime,
  presenceStatusText,
  useUserPresence,
  type ChannelsCachedMessage,
} from "@scaffoldapp/shared";
import { Badge, Button, IconButton, Tooltip, cn } from "@scaffoldapp/ui-web";
import { AlarmClock, Bookmark, BookmarkCheck, Bot, Copy, EyeOff, Flag, Forward, Link2, MailOpen, MessageSquareReply, Pencil, Pin, PinOff, SmilePlus, Trash2, UserPlus, LogIn, LogOut, Info, Webhook } from "lucide-react";
import { memo, type ReactNode } from "react";
import { ResourceMenu } from "@/components/resource/ResourceMenu";
import type { ResourceMenuAction } from "@/features/registry";
import { AppAvatar } from "@/components/shell/AppAvatar";
import { PresenceDot } from "@/features/presence";
import { Attachments } from "./Attachments";
import { EmojiGlyph } from "./emoji";
import { Markdown } from "./Markdown";
import { ForwardCard, LinkPreviews, PollCard } from "./MessageExtras";

export interface MessageActions {
  onReplyInThread: (m: ChannelsCachedMessage) => void;
  onEdit: (m: ChannelsCachedMessage) => void;
  onDelete: (m: ChannelsCachedMessage) => void;
  onReport: (m: ChannelsCachedMessage) => void;
  onReact: (m: ChannelsCachedMessage, emoji: string) => void;
  onPickEmoji: (m: ChannelsCachedMessage) => void;
  onTogglePin: (m: ChannelsCachedMessage) => void;
  onToggleSave: (m: ChannelsCachedMessage) => void;
  onCopyLink: (m: ChannelsCachedMessage) => void;
  onRetry: (m: ChannelsCachedMessage) => void;
  onDiscard: (m: ChannelsCachedMessage) => void;
  onOpenImage: (m: ChannelsCachedMessage, attachmentId: string) => void;
  onOpenThread: (rootId: string, messageId?: string) => void;
  /** `when` is a phrase the server reads ("in 20 minutes"), or "custom" to pick a time. */
  onRemind: (m: ChannelsCachedMessage, when: string) => void;
  onMarkUnread: (m: ChannelsCachedMessage) => void;
  onForward: (m: ChannelsCachedMessage) => void;
  onHidePreviews: (m: ChannelsCachedMessage) => void;
  onOpenProfile: (userId: string) => void;
}

export interface MessageItemProps {
  message: ChannelsCachedMessage;
  meId: string | undefined;
  orgId: string;
  /** Same author as the message above: no avatar or name. */
  grouped: boolean;
  highlighted: boolean;
  /** Names for reaction tooltips (people seen in this channel). */
  names: Map<string, string>;
  actions: MessageActions;
  /** Member of a channel that isn't archived. */
  canInteract: boolean;
  /** A channel or org admin: may delete others' messages. */
  canModerate: boolean;
  /** Shown inside a thread: no thread summary. */
  inThread?: boolean;
  /** Edit is offered this long after sending (0 = always). */
  editWindowMs: number;
  /** /collapse: link previews and media hidden in this conversation. */
  collapsed?: boolean;
}

const SYSTEM_ICON: Record<string, ReactNode> = {
  joined: <LogIn aria-hidden />,
  left: <LogOut aria-hidden />,
  added: <UserPlus aria-hidden />,
};

/** Name, presence and status emoji in a message header. */
function AuthorName({ m, onOpenProfile }: { m: ChannelsCachedMessage; onOpenProfile: (id: string) => void }) {
  const presence = useUserPresence(m.sender && !m.sender.deleted ? m.sender.id : null);
  const status = activeCustomStatus(presence);
  if (!m.sender) {
    return (
      <span className="flex items-center gap-1.5">
        <span className="type-label font-semibold text-fg">{m.bot_name}</span>
        <Badge size="sm" icon={m.integration ? <Webhook aria-hidden /> : undefined}>{m.integration ? "Integration" : "App"}</Badge>
      </span>
    );
  }
  const sender = m.sender;
  return (
    <span className="flex min-w-0 items-center gap-1.5">
      {sender.deleted ? (
        <span className="truncate type-label font-semibold text-fg-muted">{sender.display_name}</span>
      ) : (
        <button type="button" onClick={() => onOpenProfile(sender.id)} className="truncate type-label font-semibold text-fg underline-offset-2 hover:underline focus-visible:outline-2 focus-visible:outline-ring">
          {sender.display_name}
        </button>
      )}
      <PresenceDot presence={presence} />
      {status && (
        <Tooltip content={presenceStatusText(status)}>
          <span tabIndex={0} aria-label={presenceStatusText(status)} className="type-label focus-visible:outline-2 focus-visible:outline-ring">
            {status.emoji || "💬"}
          </span>
        </Tooltip>
      )}
    </span>
  );
}

/** One message in a channel or thread: header, body, files, reactions, thread summary and its actions. */
export const MessageItem = memo(function MessageItem({ message: m, meId, orgId, grouped, highlighted, names, actions, canInteract, canModerate, inThread, editWindowMs, collapsed }: MessageItemProps) {
  if (m.kind === "system") {
    return (
      <div id={`message-${m.id}`} className="flex items-center gap-3 px-gutter py-1 lg:px-5">
        <span className="flex w-8 shrink-0 justify-center text-fg-subtle [&_svg]:size-4">{SYSTEM_ICON[m.event ?? ""] ?? <Info aria-hidden />}</span>
        <p className="min-w-0 flex-1 type-label text-fg-muted">
          {channelsSystemText(m, meId)}
          <time dateTime={m.created_at} className="ml-2 type-caption text-fg-subtle">
            {channelsTime(m.created_at)}
          </time>
        </p>
      </div>
    );
  }
  const own = !!m.sender && m.sender.id === meId;
  const pending = m.status === "sending";
  const failed = m.status === "failed";
  const live = !m.deleted && !m.status;
  const mentionsMe = live && !own && (m.mentions.some((u) => u.id === meId) || /<@(here|channel)>/.test(m.body));
  const canEdit = own && live && canInteract && m.kind === "text" && (editWindowMs <= 0 || Date.now() - Date.parse(m.created_at) < editWindowMs);
  const canDelete = live && canInteract && (own || canModerate);

  const menuActions: ResourceMenuAction[] = [
    ...(!inThread && !m.thread_root_id ? [{ id: "thread", label: "Reply in thread", icon: MessageSquareReply, onSelect: () => actions.onReplyInThread(m) }] : []),
    ...(canInteract ? [{ id: "react", label: "Add reaction", icon: SmilePlus, onSelect: () => actions.onPickEmoji(m) }] : []),
    { id: "link", label: "Copy link", icon: Link2, onSelect: () => actions.onCopyLink(m) },
    ...(m.body ? [{ id: "copy", label: "Copy text", icon: Copy, onSelect: () => void navigator.clipboard?.writeText(m.body) }] : []),
    {
      id: "remind",
      label: "Remind me about this",
      icon: AlarmClock,
      choices: [...CHANNELS_REMIND_OPTIONS.map((o) => ({ id: o.when, label: o.label, onSelect: () => actions.onRemind(m, o.when) })), { id: "custom", label: "Custom…", onSelect: () => actions.onRemind(m, "custom") }],
    },
    { id: "unread", label: "Mark unread", icon: MailOpen, onSelect: () => actions.onMarkUnread(m) },
    ...(canInteract ? [{ id: "forward", label: "Forward…", icon: Forward, onSelect: () => actions.onForward(m) }] : []),
    ...(own && m.links.some((l) => l.kind === "external") && !m.links_hidden ? [{ id: "previews", label: "Remove previews", icon: EyeOff, onSelect: () => actions.onHidePreviews(m) }] : []),
    ...(canInteract ? [{ id: "pin", label: m.pinned ? "Unpin from channel" : "Pin to channel", icon: m.pinned ? PinOff : Pin, onSelect: () => actions.onTogglePin(m) }] : []),
    { id: "save", label: m.saved ? "Remove from Later" : "Save for later", icon: m.saved ? BookmarkCheck : Bookmark, onSelect: () => actions.onToggleSave(m) },
    ...(canEdit ? [{ id: "edit", label: "Edit message", icon: Pencil, onSelect: () => actions.onEdit(m) }] : []),
    ...(!own && m.sender && !m.sender.deleted ? [{ id: "report", label: "Report message", icon: Flag, onSelect: () => actions.onReport(m) }] : []),
    ...(canDelete ? [{ id: "delete", label: "Delete message", icon: Trash2, destructive: true, onSelect: () => actions.onDelete(m) }] : []),
  ];

  return (
    <div
      id={`message-${m.id}`}
      className={cn(
        "group relative flex gap-3 px-gutter transition-colors hover:bg-sunken focus-within:bg-sunken lg:px-5",
        grouped ? "py-0.5" : "pb-1 pt-2",
        mentionsMe && "bg-warning-soft hover:bg-warning-soft",
        highlighted && "bg-primary-soft hover:bg-primary-soft",
      )}
    >
      <div className="w-8 shrink-0 pt-0.5">
        {grouped ? (
          <Tooltip content={channelsFullTime(m.created_at)}>
            <time dateTime={m.created_at} tabIndex={0} className="block pt-0.5 text-right type-caption text-fg-subtle opacity-0 group-hover:opacity-100 focus-visible:opacity-100">
              {channelsTime(m.created_at).replace(/\s?[AP]M$/i, "")}
            </time>
          </Tooltip>
        ) : m.sender ? (
          m.sender.deleted ? (
            <AppAvatar name={m.sender.display_name} src={m.sender.avatar_url} size="sm" />
          ) : (
            <button type="button" onClick={() => m.sender && actions.onOpenProfile(m.sender.id)} aria-label={`${m.sender.display_name}'s profile`} className="rounded-full focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-ring">
              <AppAvatar name={m.sender.display_name} src={m.sender.avatar_url} size="sm" />
            </button>
          )
        ) : (
          <span aria-hidden className="flex size-8 items-center justify-center rounded-md bg-primary-soft text-primary-strong [&_svg]:size-5">
            <Bot />
          </span>
        )}
      </div>
      <div className="flex min-w-0 flex-1 flex-col gap-1">
        {!grouped && (
          <div className="flex min-w-0 items-baseline gap-2">
            <AuthorName m={m} onOpenProfile={actions.onOpenProfile} />
            <Tooltip content={channelsFullTime(m.created_at)}>
              <time dateTime={m.created_at} tabIndex={0} className="shrink-0 type-caption text-fg-muted focus-visible:outline-2 focus-visible:outline-ring">
                {channelsShortTime(m.created_at) === channelsTime(m.created_at) ? channelsTime(m.created_at) : `${channelsShortTime(m.created_at)}, ${channelsTime(m.created_at)}`}
              </time>
            </Tooltip>
            {m.pinned && (
              <Tooltip content="Pinned">
                <Pin aria-label="Pinned" className="size-3.5 shrink-0 text-fg-subtle" />
              </Tooltip>
            )}
          </div>
        )}
        {m.thread_root_id && m.also_in_channel && !inThread && (
          <button type="button" onClick={() => m.thread_root_id && actions.onOpenThread(m.thread_root_id, m.id)} className="w-fit type-caption text-fg-muted underline-offset-2 hover:underline focus-visible:outline-2 focus-visible:outline-ring">
            Replied to a thread
          </button>
        )}
        {m.deleted ? (
          <p className="type-body italic text-fg-muted">{m.title !== null ? "Post deleted" : "Message deleted"}</p>
        ) : (
          <>
            {m.title !== null && <h3 className="type-title-sm text-fg">{m.title}</h3>}
            {m.tags.length > 0 && (
              <ul className="flex flex-wrap gap-1" aria-label="Tags">
                {m.tags.map((t) => (
                  <li key={t}>
                    <Badge tone="primary">{t}</Badge>
                  </li>
                ))}
              </ul>
            )}
            {m.poll ? (
              <PollCard message={m} meId={meId} canVote={canInteract} canClose={canInteract && (own || canModerate)} names={names} />
            ) : (
              m.body && (
                <div className={cn((pending || failed) && "opacity-70")}>
                  <Markdown body={m.body} mentions={m.mentions} meId={meId} />
                </div>
              )
            )}
            {m.edited_at && <span className="type-caption text-fg-subtle">(edited)</span>}
            {m.forward && <ForwardCard messageId={m.forward.message_id} meId={meId} />}
            {m.attachments.length > 0 &&
              (collapsed ? (
                <span className="type-caption text-fg-muted">{m.attachments.length === 1 ? "1 file (collapsed)" : `${m.attachments.length} files (collapsed)`}</span>
              ) : (
                <Attachments attachments={m.attachments} pending={pending} onOpenImage={(id) => actions.onOpenImage(m, id)} />
              ))}
            {!collapsed && <LinkPreviews message={m} own={own} onRemove={() => actions.onHidePreviews(m)} />}
          </>
        )}
        {m.reactions.length > 0 && !m.deleted && (
          <ul className="flex flex-wrap gap-1" aria-label="Reactions">
            {m.reactions.map((r) => {
              const mine = !!meId && r.user_ids.includes(meId);
              const who = r.user_ids.map((id) => (id === meId ? "You" : (names.get(id) ?? "Someone"))).join(", ");
              return (
                <li key={r.emoji}>
                  <Tooltip content={who}>
                    <button
                      type="button"
                      aria-pressed={mine}
                      aria-label={`${r.emoji} ${r.count}: ${who}`}
                      disabled={!canInteract}
                      onClick={() => actions.onReact(m, r.emoji)}
                      className={cn(
                        "flex h-control-sm items-center gap-1 rounded-pill border px-2 type-label transition-colors focus-visible:outline-2 focus-visible:outline-ring pointer-coarse:min-h-tap-sm",
                        mine ? "border-primary bg-primary-soft text-primary-strong" : "border-border bg-surface text-fg hover:bg-sunken",
                      )}
                    >
                      <EmojiGlyph value={r.emoji} />
                      <span className="tabular-nums">{r.count}</span>
                    </button>
                  </Tooltip>
                </li>
              );
            })}
            {canInteract && (
              <li>
                <IconButton size="sm" variant="ghost" label="Add reaction" icon={<SmilePlus />} onClick={() => actions.onPickEmoji(m)} />
              </li>
            )}
          </ul>
        )}
        {!inThread && !m.thread_root_id && m.reply_count > 0 && (
          <button
            type="button"
            onClick={() => actions.onOpenThread(m.id)}
            className="flex w-fit max-w-full items-center gap-2 rounded-md py-0.5 pr-2 transition-colors hover:bg-surface focus-visible:outline-2 focus-visible:outline-ring"
          >
            <span className="flex -space-x-1">
              {m.reply_users.slice(0, 3).map((u) => (
                <AppAvatar key={u.id} name={u.display_name} src={u.avatar_url} size="xs" ring />
              ))}
            </span>
            <span className="type-label font-semibold text-primary-strong">{channelsReplyCount(m.reply_count)}</span>
            {m.last_reply_at && <span className="hidden truncate type-caption text-fg-muted sm:inline">Last reply {channelsShortTime(m.last_reply_at)}</span>}
          </button>
        )}
        {failed ? (
          <span className="flex items-center gap-2 type-caption text-danger-strong" role="status">
            Not sent.
            <Button size="sm" variant="ghost" onClick={() => actions.onRetry(m)}>
              Retry
            </Button>
            <Button size="sm" variant="danger-ghost" onClick={() => actions.onDiscard(m)}>
              Delete
            </Button>
          </span>
        ) : pending ? (
          <span className="type-caption text-fg-muted" role="status">
            Sending…
          </span>
        ) : null}
      </div>
      {live && (
        <div className="absolute -top-4 right-4 z-10 hidden items-center gap-0.5 rounded-md border border-border bg-surface-raised p-0.5 shadow-md group-focus-within:flex group-hover:flex pointer-coarse:right-2 pointer-coarse:top-1 pointer-coarse:flex pointer-coarse:border-0 pointer-coarse:bg-transparent pointer-coarse:shadow-none">
          <span className="flex items-center gap-0.5 pointer-coarse:hidden">
            {canInteract &&
              CHANNELS_QUICK_REACTIONS.slice(0, 3).map((e) => (
                <button key={e} type="button" onClick={() => actions.onReact(m, e)} aria-label={`React with ${e}`} className="flex size-control-sm items-center justify-center rounded-sm type-body transition-colors hover:bg-sunken focus-visible:outline-2 focus-visible:outline-ring">
                  {e}
                </button>
              ))}
            {canInteract && <IconButton size="sm" variant="ghost" label="Add reaction" icon={<SmilePlus />} onClick={() => actions.onPickEmoji(m)} />}
            {!inThread && !m.thread_root_id && <IconButton size="sm" variant="ghost" label="Reply in thread" icon={<MessageSquareReply />} onClick={() => actions.onReplyInThread(m)} />}
            <IconButton size="sm" variant="ghost" label={m.saved ? "Remove from Later" : "Save for later"} icon={m.saved ? <BookmarkCheck /> : <Bookmark />} onClick={() => actions.onToggleSave(m)} />
          </span>
          <ResourceMenu feature="channels" kind="channels.message" id={m.id} orgId={orgId} title="Message" actions={menuActions} label="More message actions" />
        </div>
      )}
    </div>
  );
});
