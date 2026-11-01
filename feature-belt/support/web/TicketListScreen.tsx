"use client";

import { useMyTickets, useNow } from "@scaffoldapp/shared";
import { Button, Card, EmptyState, ErrorState, ListRow, SkeletonGroup, SkeletonRow, Stagger, StaggerItem } from "@scaffoldapp/ui-web";
import { MessagesSquare, Plus } from "lucide-react";
import Link from "next/link";
import { LoadMore } from "@/components/shell/LoadMore";
import { Screen } from "@/components/shell/PageHeader";
import { formatRelative } from "@/lib/format";
import { TicketStatusBadge } from "./components";
import { supportPaths } from "./registry";

/** Settings → Support requests: the caller's tickets, newest first. */
export function TicketListScreen() {
  const tickets = useMyTickets();
  const now = useNow(60_000);
  const rows = tickets.data?.items ?? [];
  const newButton = (
    <Button asChild size="auto" leftIcon={<Plus />}>
      <Link href={supportPaths.create}>New request</Link>
    </Button>
  );

  return (
    <Screen title="Support requests" info="Requests you've sent to support. Replies also arrive by email." action={rows.length > 0 ? newButton : undefined}>
      <Card padding="none" className="p-1">
        {tickets.isPending ? (
          <SkeletonGroup label="Loading requests" className="flex flex-col">
            <SkeletonRow />
            <SkeletonRow />
            <SkeletonRow />
          </SkeletonGroup>
        ) : tickets.isError ? (
          <ErrorState compact title="Couldn't load your requests" info={tickets.error.message} onRetry={() => void tickets.refetch()} retrying={tickets.isFetching} />
        ) : rows.length === 0 ? (
          <EmptyState compact icon={<MessagesSquare />} title="No requests yet" action={newButton} />
        ) : (
          <>
            <Stagger as="ul" className="flex flex-col" aria-label="Support requests">
              {rows.map((t) => (
                <StaggerItem as="li" key={t.id}>
                  <ListRow
                    asChild
                    title={t.subject}
                    trailing={
                      <span className="flex items-center gap-3">
                        <span className="hidden type-caption text-fg-muted sm:inline">{formatRelative(t.last_message_at, now)}</span>
                        <TicketStatusBadge status={t.status} />
                      </span>
                    }
                  >
                    <Link href={supportPaths.ticket(t.id)} />
                  </ListRow>
                </StaggerItem>
              ))}
            </Stagger>
            <LoadMore hasNextPage={tickets.hasNextPage} isFetchingNextPage={tickets.isFetchingNextPage} fetchNextPage={tickets.fetchNextPage} />
          </>
        )}
      </Card>
    </Screen>
  );
}
