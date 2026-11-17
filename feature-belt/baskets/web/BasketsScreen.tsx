"use client";

import { useBasketsList, useMyBaskets, type BasketsListStatus, type BasketsSort, type BasketsSummary, type PagedQueryResult } from "@scaffoldapp/shared";
import { Button, EmptyState, ErrorState, SegmentedControl, Skeleton, SkeletonGroup, Tab, Tabs, TabsList } from "@scaffoldapp/ui-web";
import { Plus, SearchX, ShoppingBasket } from "lucide-react";
import Link from "next/link";
import type { ReactNode } from "react";
import { SearchInput } from "@/components/admin/SearchInput";
import { LoadMore } from "@/components/shell/LoadMore";
import { Screen } from "@/components/shell/PageHeader";
import { useUrlState } from "@/lib/url-state";
import { BasketRow } from "./parts";
import { basketsPaths } from "./registry";

type TabId = BasketsSort | "mine";
type StatusFilter = Exclude<BasketsListStatus, "all">;

const STATUS_OPTIONS: { value: StatusFilter; label: string }[] = [
  { value: "live", label: "Trading" },
  { value: "archived", label: "Archived" },
];

function NewButton({ variant = "primary" }: { variant?: "primary" | "secondary" }) {
  return (
    <Button asChild variant={variant} leftIcon={<Plus />}>
      <Link href={basketsPaths.create}>New basket</Link>
    </Button>
  );
}

function Rows({ query, empty }: { query: PagedQueryResult<BasketsSummary>; empty: ReactNode }) {
  if (query.isPending) {
    return (
      <SkeletonGroup label="Loading baskets" className="flex flex-col gap-3">
        {Array.from({ length: 6 }, (_, i) => (
          <Skeleton key={i} className="h-12" />
        ))}
      </SkeletonGroup>
    );
  }
  if (query.isError) {
    return <ErrorState title="Couldn't load baskets" info={query.error.message} onRetry={() => void query.refetch()} retrying={query.isFetching} />;
  }
  const items = query.data.items;
  if (items.length === 0) return <>{empty}</>;
  return (
    <>
      <ul aria-label="Baskets">
        {items.map((b) => (
          <BasketRow key={b.id} b={b} />
        ))}
      </ul>
      <LoadMore hasNextPage={query.hasNextPage} isFetchingNextPage={query.isFetchingNextPage} fetchNextPage={query.fetchNextPage} />
    </>
  );
}

function Discover({ sort }: { sort: BasketsSort }) {
  const [q, setQ] = useUrlState("q", "");
  const [status, setStatus] = useUrlState<StatusFilter>("status", "live");
  const list = useBasketsList({ sort, status, q: q || undefined });
  const filtered = !!q || status !== "live";
  return (
    <div className="flex flex-col gap-4">
      <div className="flex flex-col gap-2 sm:flex-row sm:items-center sm:justify-between">
        <SearchInput value={q} onSearch={setQ} placeholder="Name or symbol" label="Search baskets" className="sm:max-w-sm" />
        <SegmentedControl aria-label="Status" options={STATUS_OPTIONS} value={status} onValueChange={(v) => setStatus(v)} fullWidth={false} />
      </div>
      <Rows
        query={list}
        empty={
          filtered ? (
            <EmptyState icon={<SearchX />} title="No matching baskets" />
          ) : (
            <EmptyState icon={<ShoppingBasket />} title="No baskets yet" action={<NewButton />} />
          )
        }
      />
    </div>
  );
}

function Mine() {
  const mine = useMyBaskets();
  return <Rows query={mine} empty={<EmptyState icon={<ShoppingBasket />} title="You haven't made a basket" action={<NewButton />} />} />;
}

/** /baskets: popular and new baskets, the caller's own, search and the archive. */
export function BasketsScreen() {
  const [tab, setTab] = useUrlState<TabId>("tab", "popular");
  return (
    <div className="mx-auto flex w-full max-w-content flex-col px-gutter py-section">
      <Screen
        title="Baskets"
        info="A basket is one market made of several: one unit holds each line's quantity and is priced from their prices. The same lines always make the same basket, whoever lists it."
        action={<NewButton />}
      >
        <div className="flex flex-col gap-6">
          <Tabs value={tab} onValueChange={(v) => setTab(v as TabId)}>
            <TabsList aria-label="Baskets">
              <Tab value="popular">Popular</Tab>
              <Tab value="new">New</Tab>
              <Tab value="mine">Mine</Tab>
            </TabsList>
          </Tabs>
          {tab === "mine" ? <Mine /> : <Discover key={tab} sort={tab} />}
        </div>
      </Screen>
    </div>
  );
}
