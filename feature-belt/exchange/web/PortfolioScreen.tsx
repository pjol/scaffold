"use client";

import { useExchangeAccount, useExchangeMarkets } from "@scaffoldapp/shared";
import { Button, ErrorState, Skeleton, SkeletonGroup, Tab, TabPanel, Tabs, TabsList } from "@scaffoldapp/ui-web";
import Link from "next/link";
import { useCallback } from "react";
import { PageHeader, Section } from "@/components/shell/PageHeader";
import { useUrlState } from "@/lib/url-state";
import { FillsList, FundingList, LedgerList, OpenOrdersList, PositionsList } from "./AccountTables";
import { AccountSummary, FaucetButton } from "./AccountSummary";

type HistoryTab = "fills" | "funding" | "ledger";

/** The trading account: value and PnL, positions, open orders and history (fills, funding, ledger). */
export function PortfolioScreen() {
  const account = useExchangeAccount();
  const markets = useExchangeMarkets();
  const [history, setHistory] = useUrlState<HistoryTab>("history", "fills");
  const lookup = useCallback((s: string) => markets.data?.items.find((m) => m.symbol === s), [markets.data]);

  return (
    <div className="mx-auto flex w-full max-w-content flex-col gap-section px-gutter py-section">
      <PageHeader
        title="Portfolio"
        info="One cross-margin account: every position shares its collateral. PnL is account value minus what you put in."
        action={
          <Button asChild variant="secondary" size="auto">
            <Link href="/markets">Markets</Link>
          </Button>
        }
      />
      {account.isPending ? (
        <SkeletonGroup label="Loading your account" className="grid grid-cols-2 gap-4 sm:grid-cols-3 lg:grid-cols-6">
          {Array.from({ length: 6 }, (_, i) => (
            <Skeleton key={i} className="h-11 w-full" />
          ))}
        </SkeletonGroup>
      ) : account.isError ? (
        <ErrorState title="Couldn't load your account" info={account.error.message} onRetry={() => void account.refetch()} retrying={account.isFetching} />
      ) : (
        <>
          <div className="flex flex-col gap-4">
            <AccountSummary account={account.data} />
            <FaucetButton account={account.data} className="self-start" />
          </div>
          <Section title="Positions">
            <PositionsList positions={account.data.positions} lookup={lookup} />
          </Section>
          <Section title="Open orders">
            <OpenOrdersList orders={account.data.open_orders} lookup={lookup} />
          </Section>
        </>
      )}
      <Section title="History">
        <Tabs value={history} onValueChange={(v) => setHistory(v as HistoryTab)}>
          <TabsList aria-label="History">
            <Tab value="fills">Fills</Tab>
            <Tab value="funding">Funding</Tab>
            <Tab value="ledger">Ledger</Tab>
          </TabsList>
          <TabPanel value="fills" className="pt-3">
            <FillsList lookup={lookup} />
          </TabPanel>
          <TabPanel value="funding" className="pt-3">
            <FundingList />
          </TabPanel>
          <TabPanel value="ledger" className="pt-3">
            <LedgerList />
          </TabPanel>
        </Tabs>
      </Section>
    </div>
  );
}
