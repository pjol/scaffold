"use client";

import { assistantConversationTitle, useAssistantConversations } from "@scaffoldapp/shared";
import { Button, EmptyState, ErrorState, Skeleton, SkeletonGroup, cn } from "@scaffoldapp/ui-web";
import { MessageSquarePlus, MessagesSquare } from "lucide-react";
import Link from "next/link";
import { LoadMore } from "@/components/shell/LoadMore";
import { formatRelative } from "@/lib/format";

/** The user's conversations, most recently active first; the active one is highlighted. */
export function ConversationList({ activeId, className }: { activeId?: string | undefined; className?: string }) {
  const list = useAssistantConversations({ limit: 30 });
  const rows = list.data?.items ?? [];
  return (
    <nav aria-label="Conversations" className={cn("flex min-h-0 flex-col gap-3", className)}>
      <div className="flex min-h-control-lg items-center gap-3">
        <h1 className="flex-1 type-title text-fg">Assistant</h1>
        <Button asChild size="sm" variant="secondary" leftIcon={<MessageSquarePlus />}>
          <Link href="/assistant/new">New</Link>
        </Button>
      </div>
      {list.isPending ? (
        <SkeletonGroup className="flex flex-col gap-1">
          {Array.from({ length: 6 }, (_, i) => (
            <Skeleton key={i} className="h-row" />
          ))}
        </SkeletonGroup>
      ) : list.isError ? (
        <ErrorState compact title="Couldn't load conversations" onRetry={() => void list.refetch()} retrying={list.isFetching} />
      ) : rows.length === 0 ? (
        <EmptyState compact icon={<MessagesSquare />} title="No conversations yet" />
      ) : (
        <ul className="-mx-2 flex min-h-0 flex-col gap-0.5 overflow-y-auto px-2 pb-2">
          {rows.map((c) => {
            const active = c.id === activeId;
            return (
              <li key={c.id}>
                <Link
                  href={`/assistant/${c.id}`}
                  aria-current={active ? "page" : undefined}
                  className={cn(
                    "flex min-h-row items-center gap-3 rounded-md px-3 py-2 transition-colors pointer-coarse:min-h-tap",
                    "focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-ring",
                    active ? "bg-primary-soft text-fg" : "text-fg hover:bg-sunken",
                  )}
                >
                  <span className="min-w-0 flex-1 truncate type-label font-semibold">{assistantConversationTitle(c)}</span>
                  {c.running ? <span aria-label="Replying" className="size-2 shrink-0 rounded-pill bg-primary" /> : null}
                  <span className="shrink-0 type-caption text-fg-muted">{formatRelative(c.last_message_at)}</span>
                </Link>
              </li>
            );
          })}
          <LoadMore hasNextPage={list.hasNextPage} isFetchingNextPage={list.isFetchingNextPage} fetchNextPage={list.fetchNextPage} />
        </ul>
      )}
    </nav>
  );
}
