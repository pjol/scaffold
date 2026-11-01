"use client";

import { CHAT_QUICK_REACTIONS, systemEventText, type ChatMember, type ChatThreadMessage, chatFullTime, chatMessageTime } from "@scaffoldapp/shared";
import { Button, IconButton, Menu, MenuItem, MenuSeparator, Tooltip, cn } from "@scaffoldapp/ui-web";
import { Copy, Flag, MoreHorizontal, Pencil, Reply, SmilePlus, Trash2 } from "lucide-react";
import { memo } from "react";
import { AppAvatar } from "@/components/shell/AppAvatar";
import { Attachments } from "./Attachments";

/** How long after sending the UI offers Edit (the server's CHAT_EDIT_WINDOW decides). */
export const EDIT_WINDOW_MS = 5 * 60_000;

export interface MessageActions {
  onReply: (m: ChatThreadMessage) => void;
  onEdit: (m: ChatThreadMessage) => void;
  onDelete: (m: ChatThreadMessage) => void;
  onReport: (m: ChatThreadMessage) => void;
  onReact: (m: ChatThreadMessage, emoji: string) => void;
  onPickEmoji: (m: ChatThreadMessage) => void;
  onRetry: (m: ChatThreadMessage) => void;
  onDiscard: (m: ChatThreadMessage) => void;
  onOpenImage: (m: ChatThreadMessage, attachmentId: string) => void;
  onJumpTo: (messageId: string) => void;
}

export interface MessageItemProps {
  message: ChatThreadMessage;
  meId: string | undefined;
  group: boolean;
  /** Same sender as the message above: no avatar or name. */
  grouped: boolean;
  /** Status under your latest message: "Read", "Sent". */
  status?: string | undefined;
  highlighted: boolean;
  members: Map<string, ChatMember>;
  actions: MessageActions;
  canInteract: boolean;
}

function SystemLine({ text }: { text: string }) {
  return <p className="px-gutter py-2 text-center type-caption text-fg-muted lg:px-6">{text}</p>;
}

/** One message: bubble, quoted reply, files, reactions and its actions. */
export const MessageItem = memo(function MessageItem({ message: m, meId, group, grouped, status, highlighted, members, actions, canInteract }: MessageItemProps) {
  if (m.kind === "system") return <SystemLine text={systemEventText(m, meId)} />;
  const own = m.sender.id === meId;
  const pending = m.status === "sending";
  const failed = m.status === "failed";
  const live = !m.deleted && !m.status;
  const canEdit = own && live && Date.now() - Date.parse(m.created_at) < EDIT_WINDOW_MS && canInteract;
  const showAvatar = group && !own;

  return (
    <div id={`message-${m.id}`} className={cn("group flex gap-2 px-gutter lg:px-6", own && "flex-row-reverse", grouped ? "pt-1" : "pt-4")}>
      {showAvatar && (grouped ? <span aria-hidden className="w-8 shrink-0" /> : <AppAvatar name={m.sender.display_name} src={m.sender.avatar_url} size="sm" />)}
      <div className={cn("flex min-w-0 max-w-prose flex-col gap-1", own ? "items-end" : "items-start")}>
        {showAvatar && !grouped && <span className="px-3 type-caption text-fg-muted">{m.sender.display_name}</span>}
        <div className={cn("flex max-w-full items-center gap-1", own && "flex-row-reverse")}>
          <div
            className={cn(
              "flex min-w-0 flex-col gap-2 rounded-lg px-3 py-2 transition-shadow",
              m.deleted ? "border border-border text-fg-muted" : own ? "bg-primary text-primary-fg" : "bg-sunken text-fg dark:bg-surface-raised dark:ring-1 dark:ring-border",
              highlighted && "ring-2 ring-ring ring-offset-2 ring-offset-canvas",
              (pending || failed) && "opacity-70",
            )}
          >
            {m.deleted ? (
              <span className="type-body italic">Message deleted</span>
            ) : (
              <>
                {m.reply_to && (
                  <button
                    type="button"
                    onClick={() => m.reply_to && actions.onJumpTo(m.reply_to.id)}
                    className={cn("flex min-w-0 flex-col rounded-md px-3 py-1.5 text-left focus-visible:outline-2 focus-visible:outline-ring", own ? "bg-primary-fg/15" : "bg-surface")}
                  >
                    <span className="truncate type-caption font-semibold">{m.reply_to.sender.display_name}</span>
                    <span className={cn("line-clamp-2 type-label", m.reply_to.deleted && "italic")}>
                      {m.reply_to.deleted ? "Message deleted" : m.reply_to.body || (m.reply_to.has_attachments ? "Attachment" : "")}
                    </span>
                  </button>
                )}
                {m.attachments.length > 0 && <Attachments attachments={m.attachments} own={own} pending={pending} onOpenImage={(id) => actions.onOpenImage(m, id)} />}
                {m.body && <p className="whitespace-pre-wrap break-words type-body">{m.body}</p>}
              </>
            )}
            <span className={cn("flex items-center justify-end gap-1.5 type-caption", own && !m.deleted ? "opacity-80" : "text-fg-muted")}>
              {m.edited_at && !m.deleted && <span>Edited</span>}
              <Tooltip content={chatFullTime(m.created_at)}>
                <time dateTime={m.created_at} tabIndex={0} className="focus-visible:outline-2 focus-visible:outline-ring">
                  {chatMessageTime(m.created_at)}
                </time>
              </Tooltip>
            </span>
          </div>
          {live && canInteract && (
            <div className="flex shrink-0 items-center gap-0.5 opacity-0 transition-opacity group-focus-within:opacity-100 group-hover:opacity-100 pointer-coarse:opacity-100">
              <span className="pointer-coarse:hidden">
                <Menu side="top" align={own ? "end" : "start"} trigger={<IconButton size="sm" variant="ghost" label="React" icon={<SmilePlus />} />}>
                  {CHAT_QUICK_REACTIONS.map((e) => (
                    <MenuItem key={e} onSelect={() => actions.onReact(m, e)}>
                      <span className="type-body-lg" aria-label={`React with ${e}`}>
                        {e}
                      </span>
                    </MenuItem>
                  ))}
                  <MenuSeparator />
                  <MenuItem onSelect={() => actions.onPickEmoji(m)}>Other emoji</MenuItem>
                </Menu>
              </span>
              <span className="pointer-coarse:hidden">
                <IconButton size="sm" variant="ghost" label="Reply" icon={<Reply />} onClick={() => actions.onReply(m)} />
              </span>
              <Menu align={own ? "end" : "start"} trigger={<IconButton size="sm" variant="ghost" label="More actions" icon={<MoreHorizontal />} />}>
                <MenuItem icon={<Reply />} onSelect={() => actions.onReply(m)}>
                  Reply
                </MenuItem>
                <MenuItem icon={<SmilePlus />} onSelect={() => actions.onPickEmoji(m)}>
                  React
                </MenuItem>
                {m.body && (
                  <MenuItem icon={<Copy />} onSelect={() => void navigator.clipboard?.writeText(m.body)}>
                    Copy text
                  </MenuItem>
                )}
                {canEdit && (
                  <MenuItem icon={<Pencil />} onSelect={() => actions.onEdit(m)}>
                    Edit
                  </MenuItem>
                )}
                {!own && (
                  <MenuItem icon={<Flag />} onSelect={() => actions.onReport(m)}>
                    Report
                  </MenuItem>
                )}
                {own && (
                  <>
                    <MenuSeparator />
                    <MenuItem icon={<Trash2 />} destructive onSelect={() => actions.onDelete(m)}>
                      Delete for everyone
                    </MenuItem>
                  </>
                )}
              </Menu>
            </div>
          )}
        </div>
        {m.reactions.length > 0 && !m.deleted && (
          <ul className={cn("flex flex-wrap gap-1", own && "justify-end")} aria-label="Reactions">
            {m.reactions.map((r) => {
              const mine = !!meId && r.user_ids.includes(meId);
              const names = r.user_ids.map((id) => (id === meId ? "You" : (members.get(id)?.user.display_name ?? "Former member"))).join(", ");
              return (
                <li key={r.emoji}>
                  <Tooltip content={names}>
                    <button
                      type="button"
                      aria-pressed={mine}
                      aria-label={`${r.emoji} ${r.count}: ${names}`}
                      disabled={!canInteract}
                      onClick={() => actions.onReact(m, r.emoji)}
                      className={cn(
                        "flex h-control-sm items-center gap-1 rounded-pill border px-2 type-label transition-colors focus-visible:outline-2 focus-visible:outline-ring pointer-coarse:min-h-tap-sm",
                        mine ? "border-primary bg-primary-soft text-primary-strong" : "border-border bg-surface text-fg hover:bg-sunken",
                      )}
                    >
                      <span>{r.emoji}</span>
                      <span className="tabular-nums">{r.count}</span>
                    </button>
                  </Tooltip>
                </li>
              );
            })}
          </ul>
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
        ) : status ? (
          <span className="type-caption text-fg-muted">{status}</span>
        ) : null}
      </div>
    </div>
  );
});
