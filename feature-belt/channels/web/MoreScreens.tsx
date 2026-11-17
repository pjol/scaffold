"use client";

import {
  channelsAuthorName,
  channelsDisplayName,
  channelsMessagePath,
  channelsPlainText,
  channelsShortTime,
  channelsWhen,
  decodeChannelsMentions,
  encodeChannelsMentions,
  errorMessage,
  useActiveOrg,
  useChannelsActivity,
  useChannelsDrafts,
  useChannelsMarkRead,
  useChannelsScheduledList,
  useChannelsSent,
  useChannelsUnreads,
  useDeleteChannelsDraft,
  useDeleteChannelsScheduled,
  useMarkChannelsActivityRead,
  useMe,
  useSendChannelsScheduledNow,
  useUpdateChannelsScheduled,
  type ChannelsActivityKind,
  type ChannelsRef,
  type ChannelsScheduled,
  type ChannelsUnreadChannel,
} from "@scaffoldapp/shared";
import { Badge, Button, Dialog, EmptyState, ErrorState, Field, IconButton, Menu, MenuItem, SegmentedControl, TextArea, cn, toast } from "@scaffoldapp/ui-web";
import { AtSign, CalendarClock, Check, Inbox, MessageSquareReply, Pencil, Send, SendHorizontal, SmilePlus, Trash2 } from "lucide-react";
import Link from "next/link";
import { useRouter, useSearchParams } from "next/navigation";
import { useEffect, useState } from "react";
import { LoadMore } from "@/components/shell/LoadMore";
import { PickTimeDialog } from "./ActionDialogs";
import { ChannelIcon } from "./ChannelScreen";
import { EmojiGlyph } from "./emoji";
import { Header, ListSkeleton, MessageCard } from "./ListScreens";
import { Markdown } from "./Markdown";

/** A conversation's name beside its icon: "general", or a DM's people. */
const placeName = (c: ChannelsRef) => (c.kind === "public" || c.kind === "private" ? c.name : channelsDisplayName(c));

const ACTIVITY_ICON = { mention: AtSign, reaction: SmilePlus, thread_reply: MessageSquareReply } as const;

/** /channels/activity: mentions of you, reactions to your messages, replies in threads you follow. */
export function ActivityScreen() {
  const { org } = useActiveOrg();
  const me = useMe();
  const [filter, setFilter] = useState<ChannelsActivityKind | "">("");
  const list = useChannelsActivity(org?.id, filter);
  const markRead = useMarkChannelsActivityRead(org?.id);
  // Opening Activity counts as seeing it (the unread marks stay until you leave).
  useEffect(() => {
    if (list.data) markRead();
  }, [list.data !== undefined, markRead]);
  return (
    <div className="flex min-h-0 flex-1 flex-col">
      <Header title="Activity" info="Mentions of you, reactions to your messages and replies in threads you follow, from the last 90 days." />
      <div className="border-b border-border px-gutter py-3 lg:px-5">
        <SegmentedControl<ChannelsActivityKind | "">
          aria-label="Show"
          value={filter}
          onValueChange={setFilter}
          fullWidth={false}
          options={[
            { value: "", label: "All" },
            { value: "mention", label: "Mentions" },
            { value: "reaction", label: "Reactions" },
            { value: "thread_reply", label: "Threads" },
          ]}
        />
      </div>
      <div className="min-h-0 flex-1 overflow-y-auto">
        {list.isPending ? (
          <ListSkeleton />
        ) : list.isError ? (
          <ErrorState compact title="Couldn't load activity" info={list.error.message} onRetry={() => void list.refetch()} retrying={list.isFetching} />
        ) : list.data.items.length === 0 ? (
          <EmptyState compact icon={<AtSign />} title="Nothing yet" />
        ) : (
          <>
            <ul className="flex flex-col gap-3 p-gutter lg:p-5" aria-label="Activity">
              {list.data.items.map((a) => {
                const Icon = ACTIVITY_ICON[a.kind];
                const actor = a.actor?.display_name ?? channelsAuthorName(a.message);
                const what = a.kind === "reaction" ? "reacted" : a.kind === "mention" ? "mentioned you" : "replied in a thread";
                return (
                  <li key={`${a.kind}-${a.message.id}-${a.actor?.id ?? ""}-${a.emoji}`} className="flex flex-col gap-1">
                    <span className="flex items-center gap-2 px-1 type-caption text-fg-muted">
                      <Icon aria-hidden className="size-3.5" />
                      <span className={cn(a.unread && "font-semibold text-fg")}>{actor}</span>
                      {what}
                      {a.kind === "reaction" && <EmojiGlyph value={a.emoji} />}
                      <span>· {channelsShortTime(a.at)}</span>
                      {a.unread && <Badge size="sm" tone="danger">New</Badge>}
                    </span>
                    <MessageCard message={a.message} channel={a.channel} href={channelsMessagePath(a.message)} meId={me.data?.id} />
                  </li>
                );
              })}
            </ul>
            <LoadMore hasNextPage={list.hasNextPage} isFetchingNextPage={list.isFetchingNextPage} fetchNextPage={list.fetchNextPage} />
          </>
        )}
      </div>
    </div>
  );
}

function UnreadGroup({ group, meId }: { group: ChannelsUnreadChannel; meId: string | undefined }) {
  const c = group.channel;
  const markRead = useChannelsMarkRead(c.id, c.org_id);
  const last = group.messages[group.messages.length - 1];
  return (
    <section className="flex flex-col gap-2 rounded-lg border border-border bg-surface p-4" aria-label={channelsDisplayName(c)}>
      <div className="flex items-center gap-2">
        <ChannelIcon channel={c} className="size-4 text-fg-muted" />
        <Link href={`/channels/${c.id}`} className="min-w-0 flex-1 truncate type-label font-semibold text-fg underline-offset-2 hover:underline focus-visible:outline-2 focus-visible:outline-ring">
          {channelsDisplayName(c)}
        </Link>
        {c.mentions > 0 && <Badge tone="danger" variant="solid">{c.mentions}</Badge>}
        <Button size="sm" variant="ghost" leftIcon={<Check />} onClick={() => last && markRead(last)}>
          Mark read
        </Button>
      </div>
      <ul className="flex flex-col gap-2">
        {group.messages.map((m) => (
          <li key={m.id} className="flex flex-col gap-0.5">
            <span className="flex items-baseline gap-2">
              <span className="type-label font-semibold text-fg">{channelsAuthorName(m, meId)}</span>
              <span className="type-caption text-fg-muted">{channelsShortTime(m.created_at)}</span>
            </span>
            {m.deleted ? <span className="type-body italic text-fg-muted">Message deleted</span> : <Markdown body={m.body || (m.attachments.length ? "📎" : " ")} mentions={m.mentions} meId={meId} />}
          </li>
        ))}
      </ul>
      {group.has_more && (
        <Link href={`/channels/${c.id}`} className="w-fit type-label font-semibold text-primary-strong underline-offset-2 hover:underline focus-visible:outline-2 focus-visible:outline-ring">
          See all unread
        </Link>
      )}
    </section>
  );
}

/** /channels/unreads: every conversation's unread messages on one page. */
export function UnreadsScreen() {
  const { org } = useActiveOrg();
  const me = useMe();
  const list = useChannelsUnreads(org?.id);
  return (
    <div className="flex min-h-0 flex-1 flex-col">
      <Header title="All unread" info="Conversations with unread messages, most recent first. Muted ones are left out." />
      <div className="min-h-0 flex-1 overflow-y-auto">
        {list.isPending ? (
          <ListSkeleton />
        ) : list.isError ? (
          <ErrorState compact title="Couldn't load unread messages" info={list.error.message} onRetry={() => void list.refetch()} retrying={list.isFetching} />
        ) : list.data.items.length === 0 ? (
          <EmptyState compact icon={<Inbox />} title="You're all caught up" />
        ) : (
          <div className="flex flex-col gap-3 p-gutter lg:p-5">
            {list.data.items.map((g) => (
              <UnreadGroup key={g.channel.id} group={g} meId={me.data?.id} />
            ))}
          </div>
        )}
      </div>
    </div>
  );
}

function ScheduledRow({ item, orgId }: { item: ChannelsScheduled; orgId: string | undefined }) {
  const update = useUpdateChannelsScheduled(orgId);
  const remove = useDeleteChannelsScheduled(orgId);
  const sendNow = useSendChannelsScheduledNow(orgId);
  const [editing, setEditing] = useState(false);
  const [rescheduling, setRescheduling] = useState(false);
  const [text, setText] = useState("");
  const err = (e: unknown) => toast.error(errorMessage(e));
  const decoded = decodeChannelsMentions(item.body, item.mentions);
  return (
    <li className="flex flex-col gap-2 rounded-lg border border-border bg-surface p-4">
      <div className="flex items-start gap-3">
        <CalendarClock aria-hidden className={cn("mt-0.5 size-5 shrink-0", item.status === "failed" ? "text-danger-strong" : "text-fg-muted")} />
        <div className="flex min-w-0 flex-1 flex-col gap-1">
          <span className="flex flex-wrap items-center gap-2 type-caption text-fg-muted">
            <ChannelIcon channel={item.channel} className="size-3.5" />
            {placeName(item.channel)}
            {item.thread_root_id && " · in a thread"}
            <span>· {item.status === "failed" ? "Not sent" : `Sends ${channelsWhen(item.send_at)}`}</span>
          </span>
          {item.body ? <Markdown body={item.body} mentions={item.mentions} /> : <span className="type-label text-fg-muted">{item.attachments.length} files</span>}
          {item.status === "failed" && <span className="type-label text-danger-strong">{item.error}</span>}
        </div>
        <span className="flex shrink-0 gap-1">
          <IconButton size="sm" variant="ghost" label="Send now" icon={<Send />} loading={sendNow.isPending} onClick={() => sendNow.mutate(item.id, { onSuccess: () => toast.success("Sent."), onError: err })} />
          <Menu trigger={<IconButton size="sm" variant="ghost" label="Reschedule" icon={<CalendarClock />} />}>
            <MenuItem onSelect={() => update.mutate({ id: item.id, when: "in 1 hour" }, { onError: err })}>In 1 hour</MenuItem>
            <MenuItem onSelect={() => update.mutate({ id: item.id, when: "tomorrow at 9am" }, { onError: err })}>Tomorrow at 9:00 AM</MenuItem>
            <MenuItem onSelect={() => setRescheduling(true)}>Custom time…</MenuItem>
          </Menu>
          <IconButton
            size="sm"
            variant="ghost"
            label="Edit"
            icon={<Pencil />}
            onClick={() => {
              setText(decoded.text);
              setEditing(true);
            }}
          />
          <IconButton size="sm" variant="ghost" label="Delete" icon={<Trash2 />} onClick={() => remove.mutate(item.id, { onSuccess: () => toast.success("Scheduled message deleted."), onError: err })} />
        </span>
      </div>
      <PickTimeDialog open={rescheduling} onOpenChange={setRescheduling} title="Reschedule" confirmLabel="Reschedule" onPick={(at) => update.mutateAsync({ id: item.id, send_at: at.toISOString() }).catch((e: unknown) => (err(e), Promise.reject(e)))} />
      <Dialog
        open={editing}
        onOpenChange={setEditing}
        title="Edit scheduled message"
        footer={
          <Button
            loading={update.isPending}
            disabled={!text.trim() && item.attachments.length === 0}
            onClick={() =>
              update.mutate(
                { id: item.id, body: encodeChannelsMentions(text.trim(), decoded.picked), ...(item.status === "failed" ? { when: "in 1 minute" } : {}) },
                { onSuccess: () => setEditing(false), onError: err },
              )
            }
          >
            Save
          </Button>
        }
      >
        <Field label="Message">
          <TextArea autoFocus value={text} onChange={(e) => setText(e.target.value)} minRows={3} maxRows={10} />
        </Field>
      </Dialog>
    </li>
  );
}

/** /channels/drafts: unsent drafts, scheduled messages and what you sent. */
export function DraftsScreen() {
  const params = useSearchParams();
  const router = useRouter();
  const { org } = useActiveOrg();
  const me = useMe();
  const initial = params?.get("tab");
  const [tab, setTab] = useState<"drafts" | "scheduled" | "sent">(initial === "scheduled" || initial === "sent" ? initial : "drafts");
  const drafts = useChannelsDrafts(tab === "drafts" ? org?.id : null);
  const scheduled = useChannelsScheduledList(tab === "scheduled" ? org?.id : null);
  const sent = useChannelsSent(tab === "sent" ? org?.id : null);
  const removeDraft = useDeleteChannelsDraft(org?.id);
  return (
    <div className="flex min-h-0 flex-1 flex-col">
      <Header title="Drafts & sent" />
      <div className="border-b border-border px-gutter py-3 lg:px-5">
        <SegmentedControl<"drafts" | "scheduled" | "sent">
          aria-label="Show"
          value={tab}
          onValueChange={(v) => {
            setTab(v);
            router.replace(`/channels/drafts?tab=${v}`);
          }}
          fullWidth={false}
          options={[
            { value: "drafts", label: "Drafts" },
            { value: "scheduled", label: "Scheduled" },
            { value: "sent", label: "Sent" },
          ]}
        />
      </div>
      <div className="min-h-0 flex-1 overflow-y-auto">
        {tab === "drafts" ? (
          drafts.isPending ? (
            <ListSkeleton />
          ) : drafts.isError ? (
            <ErrorState compact title="Couldn't load drafts" info={drafts.error.message} onRetry={() => void drafts.refetch()} retrying={drafts.isFetching} />
          ) : drafts.data.items.length === 0 ? (
            <EmptyState compact icon={<Pencil />} title="No drafts" />
          ) : (
            <>
              <ul className="flex flex-col gap-3 p-gutter lg:p-5" aria-label="Drafts">
                {drafts.data.items.map((d) => (
                  <li key={d.id} className="flex items-start gap-3 rounded-lg border border-border bg-surface p-4">
                    <Link
                      href={d.thread_root_id ? `/channels/${d.channel.id}?thread=${d.thread_root_id}` : `/channels/${d.channel.id}`}
                      className="flex min-w-0 flex-1 flex-col gap-1 rounded-md focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-ring"
                    >
                      <span className="flex items-center gap-2 type-caption text-fg-muted">
                        <ChannelIcon channel={d.channel} className="size-3.5" />
                        {placeName(d.channel)}
                        {d.thread_root_id && " · in a thread"}
                        <span>· {channelsShortTime(d.updated_at)}</span>
                      </span>
                      <span className="line-clamp-3 type-body text-fg">{channelsPlainText(d.body, d.mentions)}</span>
                    </Link>
                    <IconButton size="sm" variant="ghost" label="Delete draft" icon={<Trash2 />} onClick={() => removeDraft.mutate(d, { onError: (e) => toast.error(errorMessage(e)) })} />
                  </li>
                ))}
              </ul>
              <LoadMore hasNextPage={drafts.hasNextPage} isFetchingNextPage={drafts.isFetchingNextPage} fetchNextPage={drafts.fetchNextPage} />
            </>
          )
        ) : tab === "scheduled" ? (
          scheduled.isPending ? (
            <ListSkeleton />
          ) : scheduled.isError ? (
            <ErrorState compact title="Couldn't load scheduled messages" info={scheduled.error.message} onRetry={() => void scheduled.refetch()} retrying={scheduled.isFetching} />
          ) : scheduled.data.items.length === 0 ? (
            <EmptyState compact icon={<CalendarClock />} title="Nothing scheduled" />
          ) : (
            <>
              <ul className="flex flex-col gap-3 p-gutter lg:p-5" aria-label="Scheduled messages">
                {scheduled.data.items.map((s) => (
                  <ScheduledRow key={s.id} item={s} orgId={org?.id} />
                ))}
              </ul>
              <LoadMore hasNextPage={scheduled.hasNextPage} isFetchingNextPage={scheduled.isFetchingNextPage} fetchNextPage={scheduled.fetchNextPage} />
            </>
          )
        ) : sent.isPending ? (
          <ListSkeleton />
        ) : sent.isError ? (
          <ErrorState compact title="Couldn't load sent messages" info={sent.error.message} onRetry={() => void sent.refetch()} retrying={sent.isFetching} />
        ) : sent.data.items.length === 0 ? (
          <EmptyState compact icon={<SendHorizontal />} title="Nothing sent yet" />
        ) : (
          <>
            <ul className="flex flex-col gap-3 p-gutter lg:p-5" aria-label="Sent messages">
              {sent.data.items.map((i) => (
                <li key={i.message.id}>
                  <MessageCard message={i.message} channel={i.channel} href={channelsMessagePath(i.message)} meId={me.data?.id} />
                </li>
              ))}
            </ul>
            <LoadMore hasNextPage={sent.hasNextPage} isFetchingNextPage={sent.isFetchingNextPage} fetchNextPage={sent.fetchNextPage} />
          </>
        )}
      </div>
    </div>
  );
}
