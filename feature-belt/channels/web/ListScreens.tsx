"use client";

import {
  CHANNELS_REMIND_OPTIONS,
  channelsAuthorName,
  channelsDisplayName,
  channelsMessagePath,
  channelsPlainText,
  channelsShortTime,
  channelsWhen,
  errorMessage,
  useActiveOrg,
  useChannelsBrowse,
  useChannelsLater,
  useChannelsReminders,
  useChannelsSaveLater,
  useChannelsSearch,
  useChannelsThreads,
  useCreateChannelsReminder,
  useDeleteChannelsReminder,
  useUpdateChannelsReminder,
  useDebouncedValue,
  useJoinChannel,
  type ChannelsBrowseChannel,
  type ChannelsMessage,
  type ChannelsRef,
} from "@scaffoldapp/shared";
import { Badge, Button, EmptyState, ErrorState, IconButton, InfoTip, Input, Menu, MenuItem, SegmentedControl, Select, SkeletonRow, Switch, ToggleChip, cn, toast } from "@scaffoldapp/ui-web";
import { AlarmClock, ArrowLeft, Bookmark, Check, Clock, Compass, MessagesSquare, Search, SearchX, Trash2 } from "lucide-react";
import Link from "next/link";
import { useRouter, useSearchParams } from "next/navigation";
import { useEffect, useState, type ReactNode } from "react";
import { LoadMore } from "@/components/shell/LoadMore";
import { ResourceMenu } from "@/components/resource/ResourceMenu";
import { AppAvatar } from "@/components/shell/AppAvatar";
import { PickTimeDialog } from "./ActionDialogs";
import { ChannelIcon } from "./ChannelScreen";

export function Header({ title, info, children }: { title: string; info?: string; children?: ReactNode }) {
  const router = useRouter();
  const { org } = useActiveOrg();
  return (
    <header className="flex min-h-topbar items-center gap-1 border-b border-border px-2 lg:px-4">
      <IconButton className="lg:hidden" variant="ghost" label="Back to channels" icon={<ArrowLeft />} onClick={() => router.push("/channels")} />
      <h1 className="flex min-w-0 flex-1 items-center gap-2 px-2 type-title-sm text-fg">
        <span className="truncate">{title}</span>
        {info && <InfoTip content={info} />}
      </h1>
      {children}
      <ResourceMenu feature="channels" {...(org ? { orgId: org.id } : {})} title={title} label={`${title} actions`} />
    </header>
  );
}

export function ListSkeleton() {
  return (
    <div className="flex flex-col gap-2 p-gutter lg:p-5" aria-hidden>
      {Array.from({ length: 5 }, (_, i) => (
        <SkeletonRow key={i} />
      ))}
    </div>
  );
}

/** A message in a list (threads, Later, search): where it is, who wrote it, what it says. */
export function MessageCard({ message: m, channel, href, meId, trailing }: { message: ChannelsMessage; channel: ChannelsRef; href: string; meId?: string; trailing?: ReactNode }) {
  return (
    <div className="flex items-start gap-3 rounded-lg border border-border bg-surface p-4">
      <AppAvatar name={channelsAuthorName(m)} src={m.sender?.avatar_url ?? null} size="sm" />
      <Link href={href} className="flex min-w-0 flex-1 flex-col gap-1 rounded-md focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-ring">
        <span className="flex flex-wrap items-baseline gap-x-2">
          <span className="type-label font-semibold text-fg">{channelsAuthorName(m, meId)}</span>
          <span className="flex items-center gap-1 type-caption text-fg-muted">
            <ChannelIcon channel={channel} className="size-3.5" />
            {channel.kind === "public" || channel.kind === "private" ? channel.name : channelsDisplayName(channel)}
          </span>
          <span className="type-caption text-fg-muted">{channelsShortTime(m.created_at)}</span>
        </span>
        {m.title !== null && <span className="type-body font-semibold text-fg">{m.title}</span>}
        {/* Plain text: the card is a link, and message links inside it would nest anchors. */}
        <span className="line-clamp-3 type-body text-fg">{channelsPlainText(m.body, m.mentions) || (m.attachments.length ? "Shared a file" : m.poll ? "Poll" : "")}</span>
      </Link>
      {trailing}
    </div>
  );
}

/** /channels/threads: threads you follow, latest reply first, unread ones marked. */
export function ThreadsScreen() {
  const { org } = useActiveOrg();
  const threads = useChannelsThreads(org?.id);
  return (
    <div className="flex min-h-0 flex-1 flex-col">
      <Header title="Threads" info="Threads you started, replied in, were mentioned in or follow." />
      <div className="min-h-0 flex-1 overflow-y-auto">
        {threads.isPending ? (
          <ListSkeleton />
        ) : threads.isError ? (
          <ErrorState compact title="Couldn't load threads" info={threads.error.message} onRetry={() => void threads.refetch()} retrying={threads.isFetching} />
        ) : threads.data.items.length === 0 ? (
          <EmptyState compact icon={<MessagesSquare />} title="No threads yet" />
        ) : (
          <>
            <ul className="flex flex-col gap-3 p-gutter lg:p-5" aria-label="Threads">
              {threads.data.items.map((t) => (
                <li key={t.root.id} className="flex flex-col gap-2">
                  <MessageCard
                    message={t.root}
                    channel={t.channel}
                    href={`/channels/${t.channel.id}?thread=${t.root.id}`}
                    trailing={t.unread > 0 ? <Badge tone="danger" variant="solid" aria-label={`${t.unread} unread replies`}>{t.unread > 99 ? "99+" : t.unread}</Badge> : undefined}
                  />
                  {t.latest_replies.length > 0 && (
                    <ul className="ml-11 flex flex-col gap-1" aria-label="Latest replies">
                      {t.latest_replies.map((r) => (
                        <li key={r.id} className="flex min-w-0 items-baseline gap-2 type-label">
                          <span className="shrink-0 font-semibold text-fg">{channelsAuthorName(r)}</span>
                          <span className="min-w-0 truncate text-fg-muted">{r.deleted ? "Message deleted" : channelsPlainText(r.body, r.mentions)}</span>
                        </li>
                      ))}
                    </ul>
                  )}
                </li>
              ))}
            </ul>
            <LoadMore hasNextPage={threads.hasNextPage} isFetchingNextPage={threads.isFetchingNextPage} fetchNextPage={threads.fetchNextPage} />
          </>
        )}
      </div>
    </div>
  );
}

/** /channels/later: saved messages (open or done) and your reminders. */
export function LaterScreen() {
  const params = useSearchParams();
  const { org } = useActiveOrg();
  const [state, setState] = useState<"open" | "done" | "reminders">(params?.get("tab") === "reminders" ? "reminders" : "open");
  const later = useChannelsLater(org?.id, state === "done" ? "done" : "open");
  const save = useChannelsSaveLater(org?.id);
  const remind = useCreateChannelsReminder(org?.id);
  const updateReminder = useUpdateChannelsReminder(org?.id);
  return (
    <div className="flex min-h-0 flex-1 flex-col">
      <Header title="Later" info="Messages you saved and your reminders. Mark them done when you've dealt with them." />
      <div className="border-b border-border px-gutter py-3 lg:px-5">
        <SegmentedControl<"open" | "done" | "reminders">
          aria-label="Show"
          value={state}
          onValueChange={setState}
          fullWidth={false}
          options={[
            { value: "open", label: "In progress" },
            { value: "reminders", label: "Reminders" },
            { value: "done", label: "Done" },
          ]}
        />
      </div>
      <div className="min-h-0 flex-1 overflow-y-auto">
        {state === "reminders" ? (
          <RemindersList orgId={org?.id} />
        ) : later.isPending ? (
          <ListSkeleton />
        ) : later.isError ? (
          <ErrorState compact title="Couldn't load saved items" info={later.error.message} onRetry={() => void later.refetch()} retrying={later.isFetching} />
        ) : later.data.items.length === 0 ? (
          <EmptyState compact icon={<Bookmark />} title={state === "open" ? "Nothing saved" : "Nothing done yet"} />
        ) : (
          <>
            <ul className="flex flex-col gap-3 p-gutter lg:p-5" aria-label="Saved messages">
              {later.data.items.map((i) => (
                <li key={i.message.id} className="flex flex-col gap-1">
                  <MessageCard
                    message={i.message}
                    channel={i.channel}
                    href={channelsMessagePath(i.message)}
                    trailing={
                      <span className="flex shrink-0 gap-1">
                        {state === "open" && (
                          <Menu trigger={<IconButton size="sm" variant="ghost" label="Remind me" icon={<AlarmClock />} />}>
                            {CHANNELS_REMIND_OPTIONS.map((o) => (
                              <MenuItem
                                key={o.when}
                                onSelect={() => remind.mutate({ message_id: i.message.id, when: o.when }, { onSuccess: (r) => toast.success(`I'll remind you ${channelsWhen(r.remind_at)}.`), onError: (e) => toast.error(errorMessage(e)) })}
                              >
                                {o.label}
                              </MenuItem>
                            ))}
                          </Menu>
                        )}
                        {state === "open" && (
                          <IconButton size="sm" variant="ghost" label="Mark done" icon={<Check />} onClick={() => save.mutate({ message: i.message, saved: true, done: true }, { onError: (e) => toast.error(errorMessage(e)) })} />
                        )}
                        <IconButton size="sm" variant="ghost" label="Remove" icon={<Trash2 />} onClick={() => save.mutate({ message: i.message, saved: false }, { onError: (e) => toast.error(errorMessage(e)) })} />
                      </span>
                    }
                  />
                  {i.reminder && state === "open" && (
                    <span className="ml-11 flex items-center gap-2 type-caption text-fg-muted">
                      <AlarmClock aria-hidden className="size-3.5" />
                      {i.reminder.fired_at ? `Reminded ${channelsWhen(i.reminder.remind_at)}` : `Reminder ${channelsWhen(i.reminder.remind_at)}`}
                      <Button size="sm" variant="ghost" onClick={() => i.reminder && updateReminder.mutate({ id: i.reminder.id, done: true }, { onError: (e) => toast.error(errorMessage(e)) })}>
                        Cancel reminder
                      </Button>
                    </span>
                  )}
                </li>
              ))}
            </ul>
            <LoadMore hasNextPage={later.hasNextPage} isFetchingNextPage={later.isFetchingNextPage} fetchNextPage={later.fetchNextPage} />
          </>
        )}
      </div>
    </div>
  );
}

function RemindersList({ orgId }: { orgId: string | undefined }) {
  const list = useChannelsReminders(orgId, "open");
  const update = useUpdateChannelsReminder(orgId);
  const remove = useDeleteChannelsReminder(orgId);
  const [snoozing, setSnoozing] = useState<string | null>(null);
  if (list.isPending) return <ListSkeleton />;
  if (list.isError) return <ErrorState compact title="Couldn't load reminders" info={list.error.message} onRetry={() => void list.refetch()} retrying={list.isFetching} />;
  if (list.data.items.length === 0) return <EmptyState compact icon={<AlarmClock />} title="No reminders" />;
  const err = (e: unknown) => toast.error(errorMessage(e));
  return (
    <>
      <ul className="flex flex-col gap-3 p-gutter lg:p-5" aria-label="Reminders">
        {list.data.items.map((r) => (
          <li key={r.id} className="flex flex-col gap-2 rounded-lg border border-border bg-surface p-4">
            <div className="flex items-start gap-3">
              <AlarmClock aria-hidden className={cn("mt-0.5 size-5 shrink-0", r.fired_at ? "text-danger-strong" : "text-fg-muted")} />
              <div className="flex min-w-0 flex-1 flex-col gap-1">
                <span className="type-label font-semibold text-fg">{r.fired_at ? `Due ${channelsWhen(r.remind_at)}` : channelsWhen(r.remind_at)}</span>
                {r.text && <span className="type-body text-fg">{r.text}</span>}
                {r.message && r.channel ? (
                  <Link href={channelsMessagePath(r.message)} className="line-clamp-2 type-label text-fg-muted underline-offset-2 hover:underline focus-visible:outline-2 focus-visible:outline-ring">
                    {channelsAuthorName(r.message)} in {channelsDisplayName(r.channel)}: {channelsPlainText(r.message.body, r.message.mentions) || "a message"}
                  </Link>
                ) : !r.text ? (
                  <span className="type-label text-fg-muted">A message you can't open any more</span>
                ) : null}
              </div>
              <span className="flex shrink-0 gap-1">
                <Menu trigger={<IconButton size="sm" variant="ghost" label="Snooze" icon={<Clock />} />}>
                  {CHANNELS_REMIND_OPTIONS.map((o) => (
                    <MenuItem key={o.when} onSelect={() => update.mutate({ id: r.id, when: o.when }, { onError: err })}>
                      {o.label}
                    </MenuItem>
                  ))}
                  <MenuItem onSelect={() => setSnoozing(r.id)}>Custom…</MenuItem>
                </Menu>
                <IconButton size="sm" variant="ghost" label="Mark done" icon={<Check />} onClick={() => update.mutate({ id: r.id, done: true }, { onError: err })} />
                <IconButton size="sm" variant="ghost" label="Delete reminder" icon={<Trash2 />} onClick={() => remove.mutate(r.id, { onError: err })} />
              </span>
            </div>
          </li>
        ))}
      </ul>
      <LoadMore hasNextPage={list.hasNextPage} isFetchingNextPage={list.isFetchingNextPage} fetchNextPage={list.fetchNextPage} />
      <PickTimeDialog
        open={snoozing !== null}
        onOpenChange={(o) => !o && setSnoozing(null)}
        title="Remind me at"
        confirmLabel="Save"
        onPick={(at) => (snoozing ? update.mutateAsync({ id: snoozing, at: at.toISOString() }).catch((e: unknown) => (err(e), Promise.reject(e))) : undefined)}
      />
    </>
  );
}

const SEARCH_FILTERS: { token: string; label: string }[] = [
  { token: "has:file", label: "Files" },
  { token: "has:link", label: "Links" },
  { token: "has:pin", label: "Pinned" },
  { token: "has:reaction", label: "Reactions" },
  { token: "is:thread", label: "In threads" },
  { token: "is:saved", label: "Saved" },
  { token: "to:me", label: "To me" },
  { token: "from:me", label: "From me" },
];

const DATE_FILTERS = [
  { value: "", label: "Any time" },
  { value: "during:today", label: "Today" },
  { value: "during:yesterday", label: "Yesterday" },
  { value: "during:week", label: "This week" },
  { value: "during:month", label: "This month" },
  { value: "during:year", label: "This year" },
];

function toggleToken(q: string, token: string): string {
  const parts = q.split(/\s+/).filter(Boolean);
  return (parts.includes(token) ? parts.filter((p) => p !== token) : [...parts, token]).join(" ");
}

/** /channels/search?q=: messages, with filters as chips (they edit the query's operators). */
export function SearchScreen() {
  const router = useRouter();
  const params = useSearchParams();
  const { org } = useActiveOrg();
  const [q, setQ] = useState(params?.get("q") ?? "");
  const query = useDebouncedValue(q.trim(), 300);
  useEffect(() => {
    router.replace(query ? `/channels/search?q=${encodeURIComponent(query)}` : "/channels/search");
  }, [query, router]);
  const results = useChannelsSearch(org?.id, query);
  const parts = q.split(/\s+/).filter(Boolean);
  const date = DATE_FILTERS.find((d) => d.value && parts.includes(d.value))?.value ?? "";
  return (
    <div className="flex min-h-0 flex-1 flex-col">
      <Header title="Search" info="Filters: in:#channel, from:@name, with:@name, to:me, has:file|link|pin|reaction, is:thread|saved, before: after: on: (2026-11-02, today, yesterday), during:week|month|year." />
      <div className="flex flex-col gap-3 border-b border-border px-gutter py-3 lg:px-5">
        <Input
          autoFocus
          value={q}
          onChange={(e) => setQ(e.target.value)}
          placeholder="Search messages"
          aria-label="Search messages"
          leadingIcon={<Search />}
          wrapperClassName="max-w-form"
          trailing={<span className="hidden pr-2 type-caption text-fg-subtle sm:inline">in:#channel from:@name</span>}
        />
        <div className="flex gap-2 overflow-x-auto pb-1" role="group" aria-label="Filters">
          <Select
            value={date || "any"}
            onValueChange={(v) => {
              const rest = parts.filter((p) => !DATE_FILTERS.some((d) => d.value === p)).join(" ");
              setQ(v === "any" ? rest : `${rest} ${v}`.trim());
            }}
            options={DATE_FILTERS.map((d) => ({ value: d.value || "any", label: d.label }))}
            aria-label="When"
            className="w-36 shrink-0"
          />
          {SEARCH_FILTERS.map((f) => (
            <ToggleChip key={f.token} size="sm" pressed={parts.includes(f.token)} onPressedChange={() => setQ((v) => toggleToken(v, f.token))} className="shrink-0">
              {f.label}
            </ToggleChip>
          ))}
        </div>
      </div>
      <div className="min-h-0 flex-1 overflow-y-auto">
        {query.length < 2 ? (
          <EmptyState compact icon={<Search />} title="Search messages and files" />
        ) : results.isPending ? (
          <ListSkeleton />
        ) : results.isError ? (
          <ErrorState compact title="Couldn't search" info={results.error.message} onRetry={() => void results.refetch()} retrying={results.isFetching} />
        ) : results.data.items.length === 0 ? (
          <EmptyState compact icon={<SearchX />} title="No matching messages" />
        ) : (
          <>
            <ul className="flex flex-col gap-3 p-gutter lg:p-5" aria-label="Results">
              {results.data.items.map((h) => (
                <li key={h.message.id}>
                  <MessageCard message={h.message} channel={h.channel} href={channelsMessagePath(h.message)} />
                </li>
              ))}
            </ul>
            <LoadMore hasNextPage={results.hasNextPage} isFetchingNextPage={results.isFetchingNextPage} fetchNextPage={results.fetchNextPage} />
          </>
        )}
      </div>
    </div>
  );
}

function BrowseRow({ c }: { c: ChannelsBrowseChannel }) {
  const router = useRouter();
  const join = useJoinChannel(c.id);
  return (
    <li className="flex min-h-tap-xl items-center gap-3 rounded-lg border border-border bg-surface px-4 py-3">
      <ChannelIcon channel={c} className="size-5 text-fg-muted" />
      <Link href={`/channels/${c.id}`} className="flex min-w-0 flex-1 flex-col rounded-md focus-visible:outline-2 focus-visible:outline-ring">
        <span className="flex items-center gap-2">
          <span className="truncate type-body font-semibold text-fg">{c.name}</span>
          {c.joined && <Badge size="sm" tone="success">Joined</Badge>}
          {c.is_default && <Badge size="sm">Default</Badge>}
        </span>
        <span className="truncate type-label text-fg-muted">{[`${c.member_count} ${c.member_count === 1 ? "member" : "members"}`, c.topic].filter(Boolean).join(" · ")}</span>
      </Link>
      {!c.joined && !c.archived && (
        <Button size="sm" variant="secondary" loading={join.isPending} onClick={() => join.mutate(undefined, { onSuccess: () => router.push(`/channels/${c.id}`), onError: (e) => toast.error(errorMessage(e)) })}>
          Join
        </Button>
      )}
    </li>
  );
}

/** /channels/browse: public channels (and private ones you're in), joined or not; archived on request. */
export function BrowseScreen() {
  const { org } = useActiveOrg();
  const [q, setQ] = useState("");
  const [archived, setArchived] = useState(false);
  const query = useDebouncedValue(q, 250);
  const list = useChannelsBrowse(org?.id, query, archived);
  return (
    <div className="flex min-h-0 flex-1 flex-col">
      <Header title="Browse channels" />
      <div className="flex flex-wrap items-center gap-4 border-b border-border px-gutter py-3 lg:px-5">
        <Input value={q} onChange={(e) => setQ(e.target.value)} placeholder="Search channels" aria-label="Search channels" leadingIcon={<Search />} wrapperClassName="min-w-0 flex-1 max-w-form" />
        <Switch checked={archived} onCheckedChange={setArchived} label="Archived" />
      </div>
      <div className="min-h-0 flex-1 overflow-y-auto">
        {list.isPending ? (
          <ListSkeleton />
        ) : list.isError ? (
          <ErrorState compact title="Couldn't load channels" info={list.error.message} onRetry={() => void list.refetch()} retrying={list.isFetching} />
        ) : list.data.items.length === 0 ? (
          <EmptyState compact icon={<Compass />} title={q ? "No channels match" : archived ? "No archived channels" : "No channels yet"} />
        ) : (
          <>
            <ul className="flex flex-col gap-2 p-gutter lg:p-5" aria-label="Channels">
              {list.data.items.map((c) => (
                <BrowseRow key={c.id} c={c} />
              ))}
            </ul>
            <LoadMore hasNextPage={list.hasNextPage} isFetchingNextPage={list.isFetchingNextPage} fetchNextPage={list.fetchNextPage} />
          </>
        )}
      </div>
    </div>
  );
}
