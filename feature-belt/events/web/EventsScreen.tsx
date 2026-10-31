"use client";

import { formatEventRange, useEvents, type EventItem, type EventListParams } from "@scaffoldapp/shared";
import { Button, Card, EmptyState, ErrorState, SegmentedControl, Skeleton, SkeletonGroup, Stagger, StaggerItem, Tab, Tabs, TabsList } from "@scaffoldapp/ui-web";
import { CalendarDays, MapPin, Plus, SearchX, Video } from "lucide-react";
import Link from "next/link";
import { LoadMore } from "@/components/shell/LoadMore";
import { PageHeader } from "@/components/shell/PageHeader";
import { useUrlState } from "@/lib/url-state";
import { DateBadge, EventBadges, goingText } from "./parts";

type When = "upcoming" | "past";
type Filter = "all" | "hosting" | "attending";

const FILTERS: { value: Filter; label: string }[] = [
  { value: "all", label: "All" },
  { value: "hosting", label: "Hosting" },
  { value: "attending", label: "Going" },
];

function EventCard({ event }: { event: EventItem }) {
  return (
    <Card interactive asChild padding="compact">
      <Link href={`/events/${event.id}`} className="flex items-start gap-4">
        <DateBadge iso={event.starts_at} timeZone={event.timezone} />
        <span className="flex min-w-0 flex-1 flex-col gap-1">
          <span className="truncate type-title-sm text-fg">{event.title}</span>
          <span className="type-label text-fg-muted">{formatEventRange(event.starts_at, event.ends_at, event.timezone)}</span>
          {event.location_text ? (
            <span className="flex min-w-0 items-center gap-1.5 type-label text-fg-muted">
              <MapPin className="size-4 shrink-0" aria-hidden />
              <span className="truncate">{event.location_text}</span>
            </span>
          ) : event.online_url ? (
            <span className="flex items-center gap-1.5 type-label text-fg-muted">
              <Video className="size-4 shrink-0" aria-hidden />
              Online
            </span>
          ) : null}
          <span className="flex flex-wrap items-center gap-2 pt-1">
            {event.status === "scheduled" ? <span className="type-caption text-fg-muted">{goingText(event)}</span> : null}
            <EventBadges event={event} />
          </span>
        </span>
      </Link>
    </Card>
  );
}

function ListSkeleton() {
  return (
    <SkeletonGroup label="Loading events" className="flex flex-col gap-4">
      {[0, 1, 2].map((i) => (
        <div key={i} className="flex items-start gap-4 rounded-lg border border-border p-4">
          <Skeleton className="size-14" />
          <div className="flex flex-1 flex-col gap-2">
            <Skeleton shape="text" className="h-5 w-2/3" />
            <Skeleton shape="text" className="h-4 w-1/2" />
            <Skeleton shape="text" className="h-4 w-1/3" />
          </div>
        </div>
      ))}
    </SkeletonGroup>
  );
}

/** Events: upcoming (soonest first) or past, filtered to all, hosting or going. Tab and filter live in the URL. */
export function EventsScreen() {
  const [when, setWhen] = useUrlState<When>("when", "upcoming");
  const [filter, setFilter] = useUrlState<Filter>("filter", "all");
  const params: EventListParams = { when, filter, limit: 20 };
  const events = useEvents(params);
  const rows = events.data?.items ?? [];

  const emptyTitle =
    filter === "hosting"
      ? when === "upcoming"
        ? "You're not hosting anything coming up"
        : "You haven't hosted an event yet"
      : filter === "attending"
        ? when === "upcoming"
          ? "Nothing on your calendar"
          : "No past events you went to"
        : when === "upcoming"
          ? "No upcoming events"
          : "No past events";

  return (
    <div className="mx-auto flex w-full max-w-content flex-col gap-section px-gutter py-section">
      <PageHeader
        title="Events"
        action={
          <Button asChild size="auto" leftIcon={<Plus />}>
            <Link href="/events/new">New event</Link>
          </Button>
        }
      />
      <div className="flex flex-col gap-4">
        <div className="flex flex-col gap-3 sm:flex-row sm:items-center sm:justify-between">
          <Tabs value={when} onValueChange={(v) => setWhen(v as When)}>
            <TabsList aria-label="When">
              <Tab value="upcoming">Upcoming</Tab>
              <Tab value="past">Past</Tab>
            </TabsList>
          </Tabs>
          <SegmentedControl<Filter> options={FILTERS} value={filter} onValueChange={(v) => setFilter(v)} fullWidth={false} className="self-start" />
        </div>
        {events.isPending ? (
          <ListSkeleton />
        ) : events.isError ? (
          <Card>
            <ErrorState compact title="Couldn't load events" info={events.error.message} onRetry={() => void events.refetch()} retrying={events.isFetching} />
          </Card>
        ) : rows.length === 0 ? (
          <Card>
            <EmptyState
              compact
              icon={filter === "all" ? <CalendarDays /> : <SearchX />}
              title={emptyTitle}
              action={
                when === "upcoming" ? (
                  <Button asChild variant="secondary" size="auto" leftIcon={<Plus />}>
                    <Link href="/events/new">Create an event</Link>
                  </Button>
                ) : undefined
              }
            />
          </Card>
        ) : (
          <>
            <Stagger as="ul" className="flex flex-col gap-4" aria-label={when === "upcoming" ? "Upcoming events" : "Past events"}>
              {rows.map((e) => (
                <StaggerItem as="li" key={e.id}>
                  <EventCard event={e} />
                </StaggerItem>
              ))}
            </Stagger>
            <LoadMore hasNextPage={events.hasNextPage} isFetchingNextPage={events.isFetchingNextPage} fetchNextPage={events.fetchNextPage} />
          </>
        )}
      </div>
    </div>
  );
}
