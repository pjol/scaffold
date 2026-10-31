"use client";

import { errorMessage, useInbox, useMarkNotificationsRead, useNow, useUnreadNotificationCount, type NotificationItem } from "@scaffoldapp/shared";
import { Button, Card, EmptyState, ErrorState, SegmentedControl, Skeleton, SkeletonGroup, Stagger, StaggerItem, cn, toast } from "@scaffoldapp/ui-web";
import { Bell, BellOff, CheckCheck, Settings } from "lucide-react";
import Link from "next/link";
import { useRouter } from "next/navigation";
import { LoadMore } from "@/components/shell/LoadMore";
import { Screen } from "@/components/shell/PageHeader";
import { formatDateTime, formatRelative } from "@/lib/format";
import { useUrlState } from "@/lib/url-state";
import { safeAppPath } from "./toasts";

type Filter = "all" | "unread";

function Row({ item, now, onOpen }: { item: NotificationItem; now: number; onOpen: (item: NotificationItem) => void }) {
  const unread = !item.read_at;
  return (
    <button
      type="button"
      onClick={() => onOpen(item)}
      className="flex min-h-row w-full items-start gap-3 rounded-md px-4 py-3 text-left transition-colors hover:bg-sunken"
    >
      <span className={cn("mt-2 size-2 shrink-0 rounded-pill", unread ? "bg-primary" : "bg-transparent")} aria-hidden />
      <span className="flex min-w-0 flex-1 flex-col gap-1">
        <span className="flex items-baseline gap-3">
          <span className={cn("min-w-0 flex-1 type-body text-fg", unread && "font-semibold")}>
            {unread && <span className="sr-only">Unread: </span>}
            {item.title}
          </span>
          <time dateTime={item.created_at} title={formatDateTime(item.created_at)} className="shrink-0 type-caption text-fg-muted">
            {formatRelative(item.created_at, now)}
          </time>
        </span>
        {item.body && <span className="line-clamp-3 type-label whitespace-pre-line text-fg-muted">{item.body}</span>}
      </span>
    </button>
  );
}

function InboxSkeleton() {
  return (
    <SkeletonGroup label="Loading notifications" className="flex flex-col">
      {Array.from({ length: 5 }, (_, i) => (
        <div key={i} className="flex items-start gap-3 px-4 py-3">
          <Skeleton className="mt-2 size-2 rounded-pill" />
          <div className="flex flex-1 flex-col gap-2">
            <Skeleton shape="text" className="h-5 w-2/3" />
            <Skeleton shape="text" className="h-4 w-full" />
          </div>
        </div>
      ))}
    </SkeletonGroup>
  );
}

/** /notifications: the inbox, newest first; All or Unread (in the URL); opening a row marks it read and follows its link. */
export function InboxScreen() {
  const router = useRouter();
  const [filter, setFilter] = useUrlState<Filter>("filter", "all");
  const inbox = useInbox(filter === "unread");
  const unread = useUnreadNotificationCount() ?? 0;
  const markRead = useMarkNotificationsRead();
  const now = useNow(60_000);
  const rows = inbox.data?.items ?? [];

  const open = (item: NotificationItem) => {
    if (!item.read_at) markRead.mutate({ ids: [item.id] });
    const path = safeAppPath(item.link);
    if (path) router.push(path);
  };

  return (
    <div className="mx-auto w-full max-w-content px-gutter py-section">
      <Screen
        title="Inbox"
        className="max-w-prose"
        action={
          <Button
            variant="ghost"
            size="sm"
            leftIcon={<CheckCheck />}
            disabled={unread === 0 || markRead.isPending}
            onClick={() =>
              markRead.mutate({ all: true }, { onError: (e) => toast.error(errorMessage(e, "Couldn't mark them read. Try again.")) })
            }
          >
            Mark all read
          </Button>
        }
      >
        <div className="flex flex-col gap-4">
          <SegmentedControl<Filter>
            aria-label="Show"
            value={filter}
            onValueChange={setFilter}
            className="self-start"
            options={[
              { value: "all", label: "All" },
              { value: "unread", label: unread > 0 ? `Unread (${unread})` : "Unread" },
            ]}
          />
          <Card padding="none" className="p-1">
            {inbox.isPending ? (
              <InboxSkeleton />
            ) : inbox.isError ? (
              <ErrorState compact title="Couldn't load notifications" info={inbox.error.message} onRetry={() => void inbox.refetch()} retrying={inbox.isFetching} />
            ) : rows.length === 0 ? (
              <EmptyState
                compact
                icon={filter === "unread" ? <BellOff /> : <Bell />}
                title={filter === "unread" ? "Nothing unread" : "No notifications yet"}
                action={
                  <Button asChild variant="secondary" size="auto" leftIcon={<Settings />}>
                    <Link href="/settings/notifications">Notification settings</Link>
                  </Button>
                }
              />
            ) : (
              <>
                <Stagger as="ul" className="flex flex-col" aria-label="Notifications">
                  {rows.map((item) => (
                    <StaggerItem as="li" key={item.id}>
                      <Row item={item} now={now} onOpen={open} />
                    </StaggerItem>
                  ))}
                </Stagger>
                <LoadMore hasNextPage={inbox.hasNextPage} isFetchingNextPage={inbox.isFetchingNextPage} fetchNextPage={inbox.fetchNextPage} />
              </>
            )}
          </Card>
        </div>
      </Screen>
    </div>
  );
}
