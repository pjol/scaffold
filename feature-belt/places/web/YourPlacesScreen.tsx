"use client";

import { errorMessage, useFavoritePlaces, useMyPlaces } from "@scaffoldapp/shared";
import { Button, Card, EmptyState, ErrorState, SkeletonGroup, SkeletonRow, Tab, TabPanel, Tabs, TabsList } from "@scaffoldapp/ui-web";
import { ChevronLeft, Heart, MapPinPlus, Plus } from "lucide-react";
import Link from "next/link";
import { LoadMore } from "@/components/shell/LoadMore";
import { PageHeader } from "@/components/shell/PageHeader";
import { useUrlState } from "@/lib/url-state";
import { PlaceRow, PlaceStatusBadge, PlaceThumb, useCategoryLabel } from "./parts";

function ListSkeleton() {
  return (
    <SkeletonGroup label="Loading places" className="flex flex-col">
      <SkeletonRow />
      <SkeletonRow />
      <SkeletonRow />
    </SkeletonGroup>
  );
}

function Saved() {
  const favs = useFavoritePlaces();
  const rows = favs.data?.items ?? [];
  if (favs.isPending) return <ListSkeleton />;
  if (favs.isError) return <ErrorState compact title="Couldn't load saved places" info={errorMessage(favs.error)} onRetry={() => void favs.refetch()} retrying={favs.isFetching} />;
  if (rows.length === 0) {
    return (
      <EmptyState
        compact
        icon={<Heart />}
        title="No saved places yet"
        action={
          <Button asChild variant="secondary" size="auto">
            <Link href="/places">Browse places</Link>
          </Button>
        }
      />
    );
  }
  return (
    <>
      <ul aria-label="Saved places" className="flex flex-col gap-1">
        {rows.map((p) => (
          <PlaceRow key={p.id} place={p} />
        ))}
      </ul>
      <LoadMore hasNextPage={favs.hasNextPage} isFetchingNextPage={favs.isFetchingNextPage} fetchNextPage={favs.fetchNextPage} />
    </>
  );
}

function Mine() {
  const mine = useMyPlaces();
  const label = useCategoryLabel();
  const rows = mine.data?.items ?? [];
  if (mine.isPending) return <ListSkeleton />;
  if (mine.isError) return <ErrorState compact title="Couldn't load your places" info={errorMessage(mine.error)} onRetry={() => void mine.refetch()} retrying={mine.isFetching} />;
  if (rows.length === 0) {
    return (
      <EmptyState
        compact
        icon={<MapPinPlus />}
        title="You haven't added a place"
        action={
          <Button asChild variant="secondary" size="auto" leftIcon={<Plus />}>
            <Link href="/places/new">Add a place</Link>
          </Button>
        }
      />
    );
  }
  return (
    <>
      <ul aria-label="Your places" className="flex flex-col gap-1">
        {rows.map((p) => (
          <li key={p.id} className="flex items-center gap-3 rounded-md px-3 py-2 hover:bg-sunken">
            <PlaceThumb url={p.photos[0]?.url ?? null} name={p.name} />
            <Link href={`/places/${p.id}`} className="flex min-h-tap min-w-0 flex-1 flex-col justify-center rounded-sm outline-none focus-visible:ring-2 focus-visible:ring-ring">
              <span className="truncate type-label font-semibold text-fg">{p.name}</span>
              <span className="truncate type-caption text-fg-muted">{p.status === "rejected" && p.review_note ? p.review_note : label(p.category)}</span>
            </Link>
            <PlaceStatusBadge status={p.status} />
          </li>
        ))}
      </ul>
      <LoadMore hasNextPage={mine.hasNextPage} isFetchingNextPage={mine.isFetchingNextPage} fetchNextPage={mine.fetchNextPage} />
    </>
  );
}

/** Saved places and the caller's submissions (`/places/saved`, `?tab=mine`). */
export function YourPlacesScreen() {
  const [tab, setTab] = useUrlState<"saved" | "mine">("tab", "saved");
  return (
    <div className="mx-auto flex w-full max-w-content flex-col gap-2 px-gutter py-section">
      <Link href="/places" className="flex min-h-control items-center gap-1 self-start rounded-sm pr-2 type-label font-semibold text-fg-muted hover:text-fg pointer-coarse:min-h-tap">
        <ChevronLeft className="size-4" aria-hidden />
        Places
      </Link>
      <div className="flex flex-col gap-6">
        <PageHeader
          title="Your places"
          action={
            <Button asChild size="sm" leftIcon={<Plus />}>
              <Link href="/places/new">Add</Link>
            </Button>
          }
        />
        <Tabs value={tab} onValueChange={(v) => setTab(v as "saved" | "mine")} className="flex flex-col gap-4">
          <TabsList aria-label="Your places">
            <Tab value="saved">Saved</Tab>
            <Tab value="mine">Added by you</Tab>
          </TabsList>
          <Card padding="compact" className="max-w-3xl">
            <TabPanel value="saved">
              <Saved />
            </TabPanel>
            <TabPanel value="mine">
              <Mine />
            </TabPanel>
          </Card>
        </Tabs>
      </div>
    </div>
  );
}
