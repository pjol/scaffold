"use client";

import {
  channelsAuthorName,
  channelsDisplayName,
  channelsMessagePath,
  channelsPlainText,
  channelsShortTime,
  errorMessage,
  useActiveOrg,
  useChannelsBrowse,
  useChannelsLater,
  useChannelsSaveLater,
  useChannelsSearch,
  useChannelsThreads,
  useDebouncedValue,
  useJoinChannel,
  type ChannelsBrowseChannel,
  type ChannelsMessage,
  type ChannelsRef,
} from "@scaffoldapp/shared";
import { Badge, Button, EmptyState, ErrorState, IconButton, InfoTip, Input, SegmentedControl, SkeletonRow, Switch, toast } from "@scaffoldapp/ui-web";
import { ArrowLeft, Bookmark, Check, Compass, MessagesSquare, Search, SearchX, Trash2 } from "lucide-react";
import Link from "next/link";
import { useRouter, useSearchParams } from "next/navigation";
import { useEffect, useState, type ReactNode } from "react";
import { LoadMore } from "@/components/shell/LoadMore";
import { ResourceMenu } from "@/components/resource/ResourceMenu";
import { AppAvatar } from "@/components/shell/AppAvatar";
import { ChannelIcon } from "./ChannelScreen";
import { Markdown } from "./Markdown";

function Header({ title, info, children }: { title: string; info?: string; children?: ReactNode }) {
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

function ListSkeleton() {
  return (
    <div className="flex flex-col gap-2 p-gutter lg:p-5" aria-hidden>
      {Array.from({ length: 5 }, (_, i) => (
        <SkeletonRow key={i} />
      ))}
    </div>
  );
}

/** A message in a list (threads, Later, search): where it is, who wrote it, what it says. */
function MessageCard({ message: m, channel, href, meId, trailing }: { message: ChannelsMessage; channel: ChannelsRef; href: string; meId?: string; trailing?: ReactNode }) {
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
        <span className="line-clamp-3">
          <Markdown body={m.body || " "} mentions={m.mentions} meId={meId} />
        </span>
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

/** /channels/later: saved messages, open or done. */
export function LaterScreen() {
  const { org } = useActiveOrg();
  const [state, setState] = useState<"open" | "done">("open");
  const later = useChannelsLater(org?.id, state);
  const save = useChannelsSaveLater(org?.id);
  return (
    <div className="flex min-h-0 flex-1 flex-col">
      <Header title="Later" info="Messages you saved. Mark them done when you've dealt with them." />
      <div className="border-b border-border px-gutter py-3 lg:px-5">
        <SegmentedControl<"open" | "done">
          aria-label="Show"
          value={state}
          onValueChange={setState}
          fullWidth={false}
          options={[
            { value: "open", label: "In progress" },
            { value: "done", label: "Done" },
          ]}
        />
      </div>
      <div className="min-h-0 flex-1 overflow-y-auto">
        {later.isPending ? (
          <ListSkeleton />
        ) : later.isError ? (
          <ErrorState compact title="Couldn't load saved items" info={later.error.message} onRetry={() => void later.refetch()} retrying={later.isFetching} />
        ) : later.data.items.length === 0 ? (
          <EmptyState compact icon={<Bookmark />} title={state === "open" ? "Nothing saved" : "Nothing done yet"} />
        ) : (
          <>
            <ul className="flex flex-col gap-3 p-gutter lg:p-5" aria-label="Saved messages">
              {later.data.items.map((i) => (
                <li key={i.message.id}>
                  <MessageCard
                    message={i.message}
                    channel={i.channel}
                    href={channelsMessagePath(i.message)}
                    trailing={
                      <span className="flex shrink-0 gap-1">
                        {state === "open" && (
                          <IconButton size="sm" variant="ghost" label="Mark done" icon={<Check />} onClick={() => save.mutate({ message: i.message, saved: true, done: true }, { onError: (e) => toast.error(errorMessage(e)) })} />
                        )}
                        <IconButton size="sm" variant="ghost" label="Remove" icon={<Trash2 />} onClick={() => save.mutate({ message: i.message, saved: false }, { onError: (e) => toast.error(errorMessage(e)) })} />
                      </span>
                    }
                  />
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

/** /channels/search?q=: messages, with in:#channel, from:@person, has:file, is:thread. */
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
  return (
    <div className="flex min-h-0 flex-1 flex-col">
      <Header title="Search" />
      <div className="border-b border-border px-gutter py-3 lg:px-5">
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
