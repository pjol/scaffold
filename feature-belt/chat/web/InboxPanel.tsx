"use client";

import { messagePreview, useChatInbox, useChatSearch, useDebouncedValue, useMe, type ChatConversation, type ChatSearchHit, chatInboxTime } from "@scaffoldapp/shared";
import { Badge, Button, EmptyState, ErrorState, IconButton, Input, Menu, MenuItem, SegmentedControl, SkeletonRow, cn } from "@scaffoldapp/ui-web";
import { BellOff, MessageSquarePlus, MessagesSquare, Search, SearchX, Users, X } from "lucide-react";
import Link from "next/link";
import { useState } from "react";
import { AppAvatar } from "@/components/shell/AppAvatar";
import { LoadMore } from "@/components/shell/LoadMore";
import { NewConversationDialog, type NewConversationMode } from "./NewConversationDialog";

type View = "inbox" | "archived";

/** The conversation list: inbox or archive, search across messages, and new conversations. */
export function InboxPanel({ activeId }: { activeId: string | null }) {
  const [view, setView] = useState<View>("inbox");
  const [searching, setSearching] = useState(false);
  const [query, setQuery] = useState("");
  const [creating, setCreating] = useState<NewConversationMode | null>(null);

  return (
    <div className="flex min-h-0 flex-1 flex-col">
      <div className="flex flex-col gap-3 border-b border-border px-gutter py-3 lg:px-4">
        <div className="flex min-h-control-lg items-center gap-2">
          <h1 className="min-w-0 flex-1 truncate type-title text-fg lg:type-title-sm">Chat</h1>
          <IconButton
            label={searching ? "Close search" : "Search messages"}
            icon={searching ? <X /> : <Search />}
            variant="ghost"
            onClick={() => {
              setSearching((s) => !s);
              setQuery("");
            }}
          />
          <Menu trigger={<IconButton label="New conversation" icon={<MessageSquarePlus />} variant="soft" />}>
            <MenuItem icon={<MessagesSquare />} onSelect={() => setCreating("direct")}>
              New message
            </MenuItem>
            <MenuItem icon={<Users />} onSelect={() => setCreating("group")}>
              New group
            </MenuItem>
          </Menu>
        </div>
        {searching ? (
          <Input autoFocus value={query} onChange={(e) => setQuery(e.target.value)} placeholder="Search messages" aria-label="Search messages" leadingIcon={<Search />} />
        ) : (
          <SegmentedControl<View>
            fullWidth
            aria-label="Show"
            value={view}
            onValueChange={setView}
            options={[
              { value: "inbox", label: "Inbox" },
              { value: "archived", label: "Archived" },
            ]}
          />
        )}
      </div>
      <div className="min-h-0 flex-1 overflow-y-auto">
        {searching ? <SearchResults query={query} /> : <ConversationList archived={view === "archived"} activeId={activeId} onNew={() => setCreating("direct")} />}
      </div>
      <NewConversationDialog mode={creating} onOpenChange={(open) => !open && setCreating(null)} />
    </div>
  );
}

function ListSkeleton() {
  return (
    <div className="flex flex-col gap-1 p-2" aria-hidden>
      {Array.from({ length: 6 }, (_, i) => (
        <SkeletonRow key={i} />
      ))}
    </div>
  );
}

function ConversationList({ archived, activeId, onNew }: { archived: boolean; activeId: string | null; onNew: () => void }) {
  const inbox = useChatInbox(archived);
  const me = useMe();
  if (inbox.isPending) return <ListSkeleton />;
  if (inbox.isError) return <ErrorState compact title="Couldn't load conversations" info={inbox.error.message} onRetry={() => void inbox.refetch()} retrying={inbox.isFetching} />;
  const rows = inbox.data.items;
  if (rows.length === 0) {
    return archived ? (
      <EmptyState compact icon={<MessagesSquare />} title="No archived conversations" />
    ) : (
      <EmptyState
        compact
        icon={<MessagesSquare />}
        title="No messages yet"
        action={
          <Button variant="secondary" size="auto" onClick={onNew}>
            Start a conversation
          </Button>
        }
      />
    );
  }
  return (
    <>
      <ul className="flex flex-col p-2" aria-label={archived ? "Archived conversations" : "Conversations"}>
        {rows.map((c) => (
          <li key={c.id}>
            <ConversationRow conversation={c} active={c.id === activeId} meId={me.data?.id} />
          </li>
        ))}
      </ul>
      <LoadMore hasNextPage={inbox.hasNextPage} isFetchingNextPage={inbox.isFetchingNextPage} fetchNextPage={inbox.fetchNextPage} />
    </>
  );
}

/** Avatar for a conversation: the other person, the group picture, or a group icon. */
export function ConversationAvatar({ conversation, size = "md" }: { conversation: Pick<ChatConversation, "kind" | "title" | "avatar_url">; size?: "sm" | "md" | "lg" }) {
  if (conversation.kind === "group" && !conversation.avatar_url) {
    const box = size === "lg" ? "size-14" : size === "sm" ? "size-8" : "size-10";
    return (
      <span aria-hidden className={cn("flex shrink-0 items-center justify-center rounded-pill bg-primary-soft text-primary-strong [&_svg]:size-5", box)}>
        <Users />
      </span>
    );
  }
  return <AppAvatar name={conversation.title} src={conversation.avatar_url} size={size} />;
}

function ConversationRow({ conversation: c, active, meId }: { conversation: ChatConversation; active: boolean; meId: string | undefined }) {
  const unread = c.unread > 0;
  const preview = messagePreview(c.last_message, meId, c.kind === "group");
  return (
    <Link
      href={`/chat/${c.id}`}
      aria-current={active ? "page" : undefined}
      className={cn(
        "flex min-h-tap-xl items-center gap-3 rounded-md px-3 py-2 transition-colors hover:bg-sunken focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-ring",
        active && "bg-sunken",
      )}
    >
      <ConversationAvatar conversation={c} />
      <span className="flex min-w-0 flex-1 flex-col">
        <span className="flex items-center gap-2">
          <span className={cn("min-w-0 flex-1 truncate type-body text-fg", unread && "font-semibold")}>{c.title}</span>
          <span className={cn("shrink-0 type-caption", unread ? "text-primary-strong" : "text-fg-muted")}>{chatInboxTime(c.last_message_at)}</span>
        </span>
        <span className="flex items-center gap-2">
          <span className={cn("min-w-0 flex-1 truncate type-label", unread ? "text-fg" : "text-fg-muted", c.last_message?.deleted && "italic")}>{preview || " "}</span>
          {c.muted && <BellOff aria-label="Muted" className="size-4 shrink-0 text-fg-subtle" />}
          {unread && (
            <Badge tone="primary" variant="solid" aria-label={`${c.unread} unread`}>
              {c.unread > 99 ? "99+" : c.unread}
            </Badge>
          )}
        </span>
      </span>
    </Link>
  );
}

function SearchResults({ query }: { query: string }) {
  const q = useDebouncedValue(query.trim(), 300);
  const results = useChatSearch(q);
  if (q.length < 2) return <EmptyState compact icon={<Search />} title="Search your messages" />;
  if (results.isPending) return <ListSkeleton />;
  if (results.isError) return <ErrorState compact title="Couldn't search" info={results.error.message} onRetry={() => void results.refetch()} retrying={results.isFetching} />;
  if (results.data.items.length === 0) return <EmptyState compact icon={<SearchX />} title="No matching messages" />;
  return (
    <>
      <ul className="flex flex-col p-2" aria-label="Search results">
        {results.data.items.map((hit) => (
          <li key={hit.message.id}>
            <SearchRow hit={hit} />
          </li>
        ))}
      </ul>
      <LoadMore hasNextPage={results.hasNextPage} isFetchingNextPage={results.isFetchingNextPage} fetchNextPage={results.fetchNextPage} />
    </>
  );
}

function SearchRow({ hit }: { hit: ChatSearchHit }) {
  return (
    <Link
      href={`/chat/${hit.conversation.id}?message=${hit.message.id}`}
      className="flex min-h-tap-xl items-center gap-3 rounded-md px-3 py-2 transition-colors hover:bg-sunken focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-ring"
    >
      <AppAvatar name={hit.message.sender.display_name} src={hit.message.sender.avatar_url} size="md" />
      <span className="flex min-w-0 flex-1 flex-col">
        <span className="flex items-center gap-2">
          <span className="min-w-0 flex-1 truncate type-body font-semibold text-fg">{hit.conversation.title}</span>
          <span className="shrink-0 type-caption text-fg-muted">{chatInboxTime(hit.message.created_at)}</span>
        </span>
        <span className="truncate type-label text-fg-muted">
          {hit.message.sender.display_name}: {hit.message.body}
        </span>
      </span>
    </Link>
  );
}
