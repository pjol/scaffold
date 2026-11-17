"use client";

import {
  compactExchangeNumber,
  exchangeCountdown,
  formatExchangeFunding,
  formatExchangePrice,
  useExchangeAccount,
  useExchangeMarket,
  useExchangeMarketTopic,
  useExchangeMarkets,
  useNow,
  type ExchangeMarket,
} from "@scaffoldapp/shared";
import { Button, EmptyState, ErrorState, InfoTip, Skeleton, SkeletonGroup, Tab, TabPanel, Tabs, TabsList } from "@scaffoldapp/ui-web";
import { ArrowLeft, CandlestickChart } from "lucide-react";
import Link from "next/link";
import { useCallback, useState } from "react";
import { Section } from "@/components/shell/PageHeader";
import { useUrlState } from "@/lib/url-state";
import { FillsList, OpenOrdersList, PositionsList } from "./AccountTables";
import { AccountSummary, FaucetButton } from "./AccountSummary";
import { OrderBook, TradesTape } from "./OrderBook";
import { OrderTicket } from "./OrderTicket";
import { ChangeText, IndexStatusBadge, MarketImage, MarketStatusBadge, Stat } from "./parts";
import { PriceChart } from "./PriceChart";

type AccountTab = "positions" | "orders" | "fills";
type BookTab = "book" | "trades";

function Header({ m }: { m: ExchangeMarket }) {
  const now = useNow(1000);
  const s = m.stats;
  return (
    <div className="flex flex-col gap-4">
      <Link href="/markets" className="flex w-fit items-center gap-1.5 type-label text-fg-muted hover:text-fg">
        <ArrowLeft className="size-4" aria-hidden />
        Markets
      </Link>
      <div className="flex flex-wrap items-center gap-3">
        <MarketImage market={m} size={40} />
        <h1 className="type-title text-fg">{m.name}</h1>
        <span className="type-label text-fg-muted">{m.symbol}-PERP</span>
        <MarketStatusBadge market={m} />
        {m.description && <InfoTip content={m.description} />}
      </div>
      <div className="grid grid-cols-2 gap-4 sm:grid-cols-3 lg:grid-cols-6">
        <Stat label="Mark">{formatExchangePrice(s.mark_px, m.tick_size)}</Stat>
        <Stat label="Index">
          <span className="flex items-center gap-2">
            {s.index_status === "none" ? "–" : formatExchangePrice(s.index_px, m.tick_size)}
            <IndexStatusBadge status={s.index_status} />
          </span>
        </Stat>
        <Stat label="24h change">
          <ChangeText pct={s.day_change_pct} />
        </Stat>
        <Stat label="24h volume">{compactExchangeNumber(s.day_volume)}</Stat>
        <Stat label="Open interest">{compactExchangeNumber(s.open_interest_value)}</Stat>
        <Stat label="Funding / h">
          {formatExchangeFunding(s.funding_rate)} · {exchangeCountdown(s.next_funding_at, now)}
        </Stat>
      </div>
    </div>
  );
}

function ScreenSkeleton() {
  return (
    <SkeletonGroup label="Loading the market" className="flex flex-col gap-section">
      <div className="flex flex-col gap-3">
        <Skeleton shape="text" className="h-4 w-24" />
        <Skeleton shape="text" className="h-8 w-56" />
        <Skeleton className="h-10 w-full" />
      </div>
      <div className="grid gap-8 lg:grid-cols-[minmax(0,1fr)_20rem]">
        <Skeleton className="h-96 w-full" />
        <Skeleton className="h-96 w-full" />
      </div>
    </SkeletonGroup>
  );
}

/** One market: header stats, chart with history, book and trades, ticket, and the caller's positions and orders. */
export function TradeScreen({ symbol }: { symbol: string }) {
  const sym = symbol.toUpperCase();
  const market = useExchangeMarket(sym);
  const markets = useExchangeMarkets();
  const account = useExchangeAccount();
  useExchangeMarketTopic(market.data?.id);
  const [tab, setTab] = useUrlState<AccountTab>("tab", "positions");
  const [bookTab, setBookTab] = useState<BookTab>("book");
  const [picked, setPicked] = useState<{ px: string; n: number }>();
  const lookup = useCallback((s: string) => markets.data?.items.find((m) => m.symbol === s) ?? (market.data?.symbol === s ? market.data : undefined), [markets.data, market.data]);

  return (
    <div className="mx-auto flex w-full max-w-content flex-col gap-section px-gutter py-section">
      {market.isPending ? (
        <ScreenSkeleton />
      ) : market.isError ? (
        market.error.status === 404 ? (
          <EmptyState
            icon={<CandlestickChart />}
            title="This market isn't listed"
            action={
              <Button asChild variant="secondary" size="auto">
                <Link href="/markets">All markets</Link>
              </Button>
            }
          />
        ) : (
          <ErrorState title="Couldn't load the market" info={market.error.message} onRetry={() => void market.refetch()} retrying={market.isFetching} />
        )
      ) : (
        <>
          <Header m={market.data} />
          <div className="grid gap-section lg:grid-cols-[minmax(0,1fr)_20rem] lg:grid-rows-[auto_1fr] lg:items-start lg:gap-x-8">
            <PriceChart market={market.data} className="min-w-0" />
            <aside className="flex flex-col gap-section lg:row-span-2">
              <Section title="Trade">
                <OrderTicket market={market.data} account={account.data} picked={picked} />
              </Section>
              <Section title={bookTab === "book" ? "Order book" : "Trades"}>
                <Tabs value={bookTab} onValueChange={(v) => setBookTab(v as BookTab)} variant="pill">
                  <TabsList aria-label="Book or trades">
                    <Tab value="book">Book</Tab>
                    <Tab value="trades">Trades</Tab>
                  </TabsList>
                  <TabPanel value="book" className="pt-3">
                    <OrderBook market={market.data} onPick={(px) => setPicked((p) => ({ px, n: (p?.n ?? 0) + 1 }))} />
                  </TabPanel>
                  <TabPanel value="trades" className="pt-3">
                    <TradesTape market={market.data} />
                  </TabPanel>
                </Tabs>
              </Section>
            </aside>
            <div className="flex min-w-0 flex-col gap-4">
              {account.data && (
                <div className="flex flex-col gap-3 sm:flex-row sm:items-start sm:justify-between">
                  <AccountSummary account={account.data} compact className="flex-1 sm:grid-cols-4" />
                  <FaucetButton account={account.data} />
                </div>
              )}
              <Tabs value={tab} onValueChange={(v) => setTab(v as AccountTab)}>
                <TabsList aria-label="Your trading">
                  <Tab value="positions" count={account.data?.positions.length}>
                    Positions
                  </Tab>
                  <Tab value="orders" count={account.data?.open_orders.filter((o) => o.symbol === sym).length}>
                    Open orders
                  </Tab>
                  <Tab value="fills">Fills</Tab>
                </TabsList>
                <TabPanel value="positions" className="pt-3">
                  {account.isPending ? (
                    <Skeleton className="h-16 w-full" />
                  ) : account.isError ? (
                    <ErrorState compact title="Couldn't load your account" info={account.error.message} onRetry={() => void account.refetch()} />
                  ) : (
                    <PositionsList positions={account.data.positions} lookup={lookup} />
                  )}
                </TabPanel>
                <TabPanel value="orders" className="pt-3">
                  {account.isPending ? (
                    <Skeleton className="h-16 w-full" />
                  ) : account.isError ? (
                    <ErrorState compact title="Couldn't load your orders" info={account.error.message} onRetry={() => void account.refetch()} />
                  ) : (
                    <OpenOrdersList orders={account.data.open_orders.filter((o) => o.symbol === sym)} lookup={lookup} symbol={sym} />
                  )}
                </TabPanel>
                <TabPanel value="fills" className="pt-3">
                  <FillsList lookup={lookup} symbol={sym} />
                </TabPanel>
              </Tabs>
            </div>
          </div>
        </>
      )}
    </div>
  );
}
