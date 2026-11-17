"use client";

import { BASKETS_STATUS_LABEL, compactExchangeNumber, useBasket } from "@scaffoldapp/shared";
import { Banner, Button, EmptyState, ErrorState, InfoTip, Skeleton, SkeletonGroup } from "@scaffoldapp/ui-web";
import { CandlestickChart, ChevronLeft, Copy, Flag, ScanSearch } from "lucide-react";
import Link from "next/link";
import { useRouter } from "next/navigation";
import { useState, type ReactNode } from "react";
import { ResourceMenu } from "@/components/resource/ResourceMenu";
import { Section } from "@/components/shell/PageHeader";
import type { ResourceMenuAction } from "@/features/registry";
import { formatDate } from "@/lib/format";
import { useExchangeMoney } from "../exchange/parts";
import { BasketLines, BasketStatusBadge, PriceStatusBadge, formatBasketsDay } from "./parts";
import { basketsPaths } from "./registry";
import { BasketReportDialog } from "./ReportDialog";

function Back() {
  return (
    <Link href={basketsPaths.list} className="inline-flex h-tap-sm items-center gap-1 self-start type-label text-fg-muted hover:text-fg">
      <ChevronLeft className="size-4" aria-hidden />
      Baskets
    </Link>
  );
}

function Fact({ label, info, children }: { label: string; info?: string; children: ReactNode }) {
  return (
    <div className="flex min-w-0 flex-col gap-1">
      <dt className="flex items-center gap-1 type-label text-fg-muted">
        {label}
        {info && <InfoTip content={info} />}
      </dt>
      <dd className="truncate type-body tabular-nums text-fg">{children}</dd>
    </div>
  );
}

/** /baskets/[symbol]: the basket's lines at current prices, its value and published price, its market, who listed it. */
export function BasketScreen({ symbol }: { symbol: string }) {
  const router = useRouter();
  const basket = useBasket(symbol);
  const { money } = useExchangeMoney();
  const [reporting, setReporting] = useState(false);

  if (basket.isPending) {
    return (
      <SkeletonGroup label="Loading the basket" className="mx-auto flex w-full max-w-content flex-col gap-section px-gutter py-section">
        <Skeleton className="h-10 w-2/3" />
        <Skeleton className="h-24" />
        <Skeleton className="h-64" />
      </SkeletonGroup>
    );
  }
  if (basket.isError) {
    return (
      <div className="mx-auto flex w-full max-w-content flex-col gap-section px-gutter py-section">
        <Back />
        {basket.error.status === 404 ? (
          <EmptyState
            icon={<ScanSearch />}
            title="No such basket"
            action={
              <Button asChild variant="secondary">
                <Link href={basketsPaths.list}>All baskets</Link>
              </Button>
            }
          />
        ) : (
          <ErrorState title="Couldn't load this basket" info={basket.error.message} onRetry={() => void basket.refetch()} retrying={basket.isFetching} />
        )}
      </div>
    );
  }
  const b = basket.data;
  const actions: ResourceMenuAction[] = [
    { id: "copy", label: "Start a basket from this one", icon: Copy, onSelect: () => router.push(basketsPaths.copy(b.symbol)) },
    { id: "report", label: b.reported ? "Reported" : "Report…", icon: Flag, disabled: b.reported, onSelect: () => setReporting(true) },
  ];
  const tradable = b.status !== "archived" && !!b.market;

  return (
    <div className="mx-auto flex w-full max-w-content flex-col gap-section px-gutter py-section">
      <div className="flex flex-col gap-6">
        <Back />
        <div className="flex items-start justify-between gap-4">
          <div className="flex min-w-0 flex-col gap-2">
            <h1 className="flex flex-wrap items-center gap-x-3 gap-y-1 type-title text-fg lg:type-display">
              <span className="truncate">{b.name}</span>
              <span className="type-label font-medium text-fg-muted">{b.symbol}</span>
            </h1>
            <div className="flex flex-wrap items-baseline gap-x-3 gap-y-2">
              <span className="type-headline tabular-nums text-fg">{money(b.price ?? b.value)}</span>
              <PriceStatusBadge status={b.price_status} />
              <BasketStatusBadge basket={b} />
            </div>
          </div>
          <div className="flex shrink-0 items-center gap-2">
            {tradable && (
              <Button asChild leftIcon={<CandlestickChart />}>
                <Link href={basketsPaths.market(b.symbol)}>Trade</Link>
              </Button>
            )}
            <ResourceMenu feature="baskets" kind="baskets.basket" id={b.id} title={b.name} actions={actions} />
          </div>
        </div>
        {b.hidden ? (
          <Banner tone="danger">Staff removed this basket. Open positions can still be closed.</Banner>
        ) : b.status === "archived" ? (
          <Banner
            tone="info"
            action={
              <Link href={basketsPaths.copy(b.symbol)} className="type-label underline underline-offset-2">
                Reopen it
              </Link>
            }
          >
            Archived: nobody traded it for a while. Listing the same lines again reopens it.
          </Banner>
        ) : b.status === "reduce_only" ? (
          <Banner tone="warning">Winding down: only orders that close positions are accepted. It&apos;s archived once every position is closed.</Banner>
        ) : null}
        {b.description && <p className="max-w-prose whitespace-pre-line type-body text-fg">{b.description}</p>}
        <dl className="grid grid-cols-2 gap-4 sm:grid-cols-4">
          <Fact label="Value now" info="One basket unit at its markets' current prices.">
            {money(b.value)}
          </Fact>
          <Fact label="Value when listed">{money(b.value_at_create)}</Fact>
          <Fact label="Entries" info="People who listed or joined this basket.">
            {b.entries}
          </Fact>
          <Fact label="Positions" info="People holding a position, and its open interest. Refreshed hourly.">
            {b.holders} · {money(b.open_interest)}
          </Fact>
          <Fact label="Recent volume" info="People's trading over the last couple of weeks. Refreshed hourly.">
            {compactExchangeNumber(b.volume_recent)}
          </Fact>
          <Fact label="Status">{BASKETS_STATUS_LABEL[b.status]}</Fact>
          {b.ends_on && (
            <Fact label="Ends" info="Some time after this date it winds down.">
              {formatBasketsDay(b.ends_on)}
            </Fact>
          )}
          <Fact label="Listed by">
            {b.creator} · {formatDate(b.created_at)}
          </Fact>
        </dl>
      </div>

      <Section title="Lines" info="One basket unit holds this many of each market. Weights move with prices.">
        <BasketLines lines={b.lines} money={money} label="Lines" />
      </Section>

      {b.market && (
        <Section title="Market" info="The exchange market this basket trades as.">
          <dl className="grid grid-cols-2 gap-4 sm:grid-cols-4">
            <Fact label="Max leverage">{b.market.max_leverage}×</Fact>
            <Fact label="Tick size">{b.market.tick_size}</Fact>
            <Fact label="Minimum order">{money(b.market.min_notional)}</Fact>
            <Fact label="Open interest cap">{money(b.market.oi_cap)}</Fact>
          </dl>
        </Section>
      )}

      <BasketReportDialog symbol={b.symbol} name={b.name} open={reporting} onOpenChange={setReporting} />
    </div>
  );
}
