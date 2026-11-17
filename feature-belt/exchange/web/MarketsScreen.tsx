"use client";

import {
  compactExchangeNumber,
  formatExchangeFunding,
  formatExchangePrice,
  useExchangeMarkets,
  useExchangeMarketsTopic,
  useExchangeSparklines,
  type ExchangeMarket,
} from "@scaffoldapp/shared";
import { Button, Card, EmptyState, ErrorState, Input, Select, Skeleton, SkeletonGroup, ToggleChip } from "@scaffoldapp/ui-web";
import { CandlestickChart, Search, SearchX } from "lucide-react";
import Link from "next/link";
import { useMemo, useState } from "react";
import { PageHeader } from "@/components/shell/PageHeader";
import { useUrlState } from "@/lib/url-state";
import { ChangeText, MarketImage, MarketStatusBadge, Sparkline, useExchangeMoney } from "./parts";

type Sort = "volume" | "change" | "name" | "oi";

const SORTS: { value: Sort; label: string }[] = [
  { value: "volume", label: "24h volume" },
  { value: "change", label: "24h change" },
  { value: "oi", label: "Open interest" },
  { value: "name", label: "Name" },
];

function sortMarkets(list: ExchangeMarket[], sort: Sort): ExchangeMarket[] {
  const out = list.slice();
  switch (sort) {
    case "volume":
      return out.sort((a, b) => Number(b.stats.day_volume) - Number(a.stats.day_volume));
    case "change":
      return out.sort((a, b) => Number(b.stats.day_change_pct) - Number(a.stats.day_change_pct));
    case "oi":
      return out.sort((a, b) => Number(b.stats.open_interest_value) - Number(a.stats.open_interest_value));
    default:
      return out.sort((a, b) => a.name.localeCompare(b.name));
  }
}

function MarketRow({ m, closes }: { m: ExchangeMarket; closes: string[] | undefined }) {
  const { money } = useExchangeMoney();
  return (
    <li>
      <Link
        href={`/markets/${m.symbol}`}
        className="grid grid-cols-[minmax(0,1fr)_auto] items-center gap-x-4 gap-y-1 border-b border-border py-3 hover:bg-sunken focus-visible:outline-2 focus-visible:outline-ring sm:grid-cols-[minmax(0,1fr)_6rem_8rem_6rem] lg:grid-cols-[minmax(0,1fr)_6rem_8rem_6rem_7rem_6rem]"
      >
        <span className="flex min-w-0 items-center gap-3">
          <MarketImage market={m} />
          <span className="flex min-w-0 flex-col">
            <span className="flex items-center gap-2">
              <span className="truncate type-label font-semibold text-fg">{m.name}</span>
              <MarketStatusBadge market={m} />
            </span>
            <span className="type-caption text-fg-muted">
              {m.symbol} · up to {m.max_leverage}×
            </span>
          </span>
        </span>
        <Sparkline closes={closes} label={`${m.name}, last 24 hours`} className="hidden sm:block" />
        <span className="flex flex-col items-end">
          <span className="type-label tabular-nums text-fg">{formatExchangePrice(m.stats.mark_px, m.tick_size)}</span>
          <ChangeText pct={m.stats.day_change_pct} className="type-caption" />
        </span>
        <span className="hidden flex-col items-end sm:flex">
          <span className="type-caption text-fg-muted">Volume</span>
          <span className="type-label tabular-nums text-fg">{compactExchangeNumber(m.stats.day_volume)}</span>
        </span>
        <span className="hidden flex-col items-end lg:flex">
          <span className="type-caption text-fg-muted">Open interest</span>
          <span className="type-label tabular-nums text-fg">{money(m.stats.open_interest_value).replace(/\.\d+$/, "")}</span>
        </span>
        <span className="hidden flex-col items-end lg:flex">
          <span className="type-caption text-fg-muted">Funding / h</span>
          <span className="type-label tabular-nums text-fg">{formatExchangeFunding(m.stats.funding_rate)}</span>
        </span>
      </Link>
    </li>
  );
}

function ListSkeleton() {
  return (
    <SkeletonGroup label="Loading markets" className="flex flex-col">
      {[0, 1, 2, 3, 4].map((i) => (
        <div key={i} className="flex items-center gap-4 border-b border-border py-3">
          <div className="flex flex-1 flex-col gap-1.5">
            <Skeleton shape="text" className="h-4 w-1/3" />
            <Skeleton shape="text" className="h-3 w-1/5" />
          </div>
          <Skeleton className="hidden h-8 w-24 sm:block" />
          <Skeleton shape="text" className="h-4 w-20" />
        </div>
      ))}
    </SkeletonGroup>
  );
}

/** Every market with live price, 24h change and a day's sparkline; search, category and sort live in the URL. */
export function MarketsScreen() {
  const [query, setQuery] = useUrlState("q", "");
  const [category, setCategory] = useUrlState("category", "");
  const [sort, setSort] = useUrlState<Sort>("sort", "volume");
  const [draft, setDraft] = useState(query);
  useExchangeMarketsTopic();
  const markets = useExchangeMarkets();
  const sparks = useExchangeSparklines("1h", 24);
  const all = markets.data?.items ?? [];
  const categories = useMemo(() => [...new Set(all.map((m) => m.category).filter(Boolean))].sort(), [all]);
  const rows = useMemo(() => {
    const q = query.trim().toLowerCase();
    const filtered = all.filter((m) => (!category || m.category === category) && (!q || `${m.symbol} ${m.name} ${m.category}`.toLowerCase().includes(q)));
    return sortMarkets(filtered, sort);
  }, [all, query, category, sort]);

  return (
    <div className="mx-auto flex w-full max-w-content flex-col gap-section px-gutter py-section">
      <PageHeader
        title="Markets"
        info="Perpetual futures: trade price moves with leverage, no expiry. Funding is paid hourly between longs and shorts."
        action={
          <Button asChild variant="secondary" size="auto">
            <Link href="/portfolio">Portfolio</Link>
          </Button>
        }
      />
      <div className="flex flex-col gap-4">
        <div className="flex flex-col gap-3 sm:flex-row sm:items-center">
          <form
            className="flex-1"
            role="search"
            onSubmit={(e) => {
              e.preventDefault();
              setQuery(draft.trim());
            }}
          >
            <Input
              aria-label="Search markets"
              leadingIcon={<Search />}
              value={draft}
              placeholder="Search markets"
              onChange={(e) => {
                setDraft(e.target.value);
                setQuery(e.target.value.trim());
              }}
            />
          </form>
          <Select options={SORTS} value={sort} onValueChange={(v) => setSort(v as Sort)} className="sm:w-48" />
        </div>
        {categories.length > 1 && (
          <div className="flex gap-2 overflow-x-auto pb-1" role="group" aria-label="Category">
            <ToggleChip size="sm" pressed={!category} onPressedChange={() => setCategory(null)}>
              All
            </ToggleChip>
            {categories.map((c) => (
              <ToggleChip key={c} size="sm" pressed={category === c} onPressedChange={(on) => setCategory(on ? c : null)}>
                {c}
              </ToggleChip>
            ))}
          </div>
        )}
        {markets.isPending ? (
          <ListSkeleton />
        ) : markets.isError ? (
          <Card>
            <ErrorState compact title="Couldn't load markets" info={markets.error.message} onRetry={() => void markets.refetch()} retrying={markets.isFetching} />
          </Card>
        ) : all.length === 0 ? (
          <EmptyState icon={<CandlestickChart />} title="No markets listed yet" />
        ) : rows.length === 0 ? (
          <EmptyState
            compact
            icon={<SearchX />}
            title="No markets match"
            action={
              <Button
                variant="secondary"
                size="auto"
                onClick={() => {
                  setDraft("");
                  setQuery(null);
                  setCategory(null);
                }}
              >
                Clear filters
              </Button>
            }
          />
        ) : (
          <ul aria-label="Markets" className="flex flex-col">
            {rows.map((m) => (
              <MarketRow key={m.symbol} m={m} closes={sparks.data?.[m.symbol]} />
            ))}
          </ul>
        )}
      </div>
    </div>
  );
}
