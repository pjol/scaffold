"use client";

import {
  addUnits,
  findChain,
  formatUnits,
  groupByDay,
  isBaseUnits,
  shortAddress,
  useWalletActivity,
  useWalletBalances,
  useWallets,
  useWalletsConfig,
  walletsConfigured,
  type Wallet,
  type WalletBalance,
  type WalletChain,
} from "@scaffoldapp/shared";
import { Badge, Button, Card, Divider, EmptyState, ErrorState, IconButton, InfoTip, ListRow, Select } from "@scaffoldapp/ui-web";
import { ArrowLeftRight, RefreshCw, Settings2, Wallet as WalletIcon } from "lucide-react";
import Link from "next/link";
import { Fragment, useMemo, useState } from "react";
import { LoadMore } from "@/components/shell/LoadMore";
import { PageHeader, Section } from "@/components/shell/PageHeader";
import { LinkWalletDialog } from "../components/LinkWalletDialog";
import { AssetMark, RowsSkeleton, WalletsNotConfigured } from "../components/states";
import { TransferRow } from "../components/TransferRow";

const ALL = "all";

interface AssetTotal {
  key: string;
  symbol: string;
  decimals: number;
  amount: string | null; // sum of the wallets that answered; null when none did
  partial: boolean; // some wallets couldn't be read
}

/** Sum each asset across the selected wallets (string math), per chain in config order. */
function totalsByChain(items: WalletBalance[], chains: WalletChain[], walletId: string): { chain: WalletChain; assets: AssetTotal[] }[] {
  return chains
    .map((chain) => {
      const assets = new Map<string, AssetTotal>();
      for (const b of items) {
        if (b.chain_id !== chain.id || (walletId !== ALL && b.wallet_id !== walletId)) continue;
        const key = b.token_address ?? "native";
        const cur = assets.get(key) ?? { key, symbol: b.symbol, decimals: b.decimals, amount: null, partial: false };
        if (isBaseUnits(b.amount)) cur.amount = cur.amount === null ? b.amount : addUnits(cur.amount, b.amount);
        else cur.partial = true;
        assets.set(key, cur);
      }
      return { chain, assets: [...assets.values()] };
    })
    .filter((g) => g.assets.length > 0);
}

function walletName(w: Wallet): string {
  return w.label || shortAddress(w.address);
}

/** /wallets: balances per chain and token, and live activity, for one or all linked wallets. */
export function WalletScreen() {
  const config = useWalletsConfig();
  const wallets = useWallets();
  const [walletId, setWalletId] = useState(ALL);
  const [chainId, setChainId] = useState(ALL);
  const [linking, setLinking] = useState(false);
  const chains = config.data?.chains ?? [];
  const hasWallets = (wallets.data?.length ?? 0) > 0;
  const ready = walletsConfigured(config.data) && hasWallets;
  const filter = { ...(walletId !== ALL ? { wallet_id: walletId } : {}), ...(chainId !== ALL ? { chain_id: Number(chainId) } : {}) };
  const balances = useWalletBalances({}, { enabled: ready });
  const activity = useWalletActivity(filter, { enabled: ready });

  const days = useMemo(() => groupByDay(activity.data?.items ?? []), [activity.data]);
  const groups = useMemo(() => {
    const shown = chainId === ALL ? chains : chains.filter((c) => String(c.id) === chainId);
    return totalsByChain(balances.data?.items ?? [], shown, walletId);
  }, [balances.data, chains, chainId, walletId]);

  const header = (
    <PageHeader
      title="Wallet"
      action={
        <>
          {ready && (
            <IconButton
              label="Refresh balances"
              tooltip
              icon={<RefreshCw />}
              variant="ghost"
              loading={balances.isFetching}
              onClick={() => void balances.refetch()}
            />
          )}
          <Button asChild variant="secondary" size="auto" leftIcon={<Settings2 />}>
            <Link href="/settings/wallets">Manage</Link>
          </Button>
        </>
      }
    />
  );

  let body;
  if (config.isPending || wallets.isPending) {
    body = <RowsSkeleton rows={3} label="Loading wallet" />;
  } else if (config.isError || wallets.isError) {
    const failed = config.isError ? config : wallets;
    body = (
      <Card padding="compact">
        <ErrorState compact title="Couldn't load your wallet" info={failed.error?.message} onRetry={() => void failed.refetch()} retrying={failed.isFetching} />
      </Card>
    );
  } else if (!walletsConfigured(config.data)) {
    body = <WalletsNotConfigured />;
  } else if (!hasWallets) {
    body = (
      <Card padding="compact">
        <EmptyState
          icon={<WalletIcon />}
          title="No wallets linked"
          action={
            <Button size="auto" onClick={() => setLinking(true)}>
              Link a wallet
            </Button>
          }
        />
      </Card>
    );
  } else {
    const list = wallets.data ?? [];
    body = (
      <>
        {(list.length > 1 || chains.length > 1) && (
          <div className="flex flex-col gap-2 sm:flex-row">
            {list.length > 1 && (
              <Select
                aria-label="Wallet"
                className="sm:w-60"
                value={walletId}
                onValueChange={setWalletId}
                options={[{ value: ALL, label: "All wallets" }, ...list.map((w) => ({ value: w.id, label: walletName(w) }))]}
              />
            )}
            {chains.length > 1 && (
              <Select
                aria-label="Network"
                className="sm:w-60"
                value={chainId}
                onValueChange={setChainId}
                options={[{ value: ALL, label: "All networks" }, ...chains.map((c) => ({ value: String(c.id), label: c.name }))]}
              />
            )}
          </div>
        )}

        <Section title="Balances" info="Read live from each network. Amounts are exact; nothing is rounded except for display.">
          {balances.isPending ? (
            <RowsSkeleton rows={2} label="Loading balances" />
          ) : balances.isError ? (
            <Card padding="compact">
              <ErrorState compact title="Couldn't load balances" info={balances.error.message} onRetry={() => void balances.refetch()} retrying={balances.isFetching} />
            </Card>
          ) : (
            <div className="grid gap-6 lg:grid-cols-2">
              {groups.map(({ chain, assets }) => (
                <div key={chain.id} className="flex flex-col gap-2">
                  <h3 className="flex min-h-6 items-center gap-2 type-label font-semibold text-fg-muted">
                    {chain.name}
                    {chain.testnet && (
                      <Badge size="sm" tone="neutral">
                        Testnet
                      </Badge>
                    )}
                  </h3>
                  <Card padding="none" className="p-1">
                    {assets.map((a, i) => (
                      <Fragment key={a.key}>
                        {i > 0 && <Divider className="mx-4" />}
                        <ListRow
                          title={a.symbol}
                          leading={<AssetMark symbol={a.symbol} />}
                          trailing={
                            a.amount === null ? (
                              <span className="flex items-center gap-1 type-body text-fg-muted">
                                Unavailable
                                <InfoTip content={`Couldn't reach ${chain.name}. Try again in a minute.`} />
                              </span>
                            ) : (
                              <span className="flex items-center gap-1">
                                <span className="type-body font-semibold tabular-nums text-fg">{formatUnits(a.amount, a.decimals)}</span>
                                {a.partial && <InfoTip tone="muted" content="Some wallets couldn't be read, so this total may be low." />}
                              </span>
                            )
                          }
                        />
                      </Fragment>
                    ))}
                  </Card>
                </div>
              ))}
            </div>
          )}
        </Section>

        <Section title="Activity">
          {activity.isPending ? (
            <RowsSkeleton rows={4} label="Loading activity" />
          ) : activity.isError ? (
            <Card padding="compact">
              <ErrorState compact title="Couldn't load activity" info={activity.error.message} onRetry={() => void activity.refetch()} retrying={activity.isFetching} />
            </Card>
          ) : days.length === 0 ? (
            <Card padding="compact">
              <EmptyState compact icon={<ArrowLeftRight />} title="No transfers yet" />
            </Card>
          ) : (
            <div className="flex flex-col gap-6">
              {days.map((d) => (
                <div key={d.label} className="flex flex-col gap-2">
                  <h3 className="type-label font-semibold text-fg-muted">{d.label}</h3>
                  <Card padding="none" className="p-1">
                    {d.items.map((t, i) => (
                      <Fragment key={t.id}>
                        {i > 0 && <Divider className="mx-4" />}
                        <TransferRow transfer={t} chainName={findChain(config.data, t.chain_id)?.name} />
                      </Fragment>
                    ))}
                  </Card>
                </div>
              ))}
              <LoadMore hasNextPage={activity.hasNextPage} isFetchingNextPage={activity.isFetchingNextPage} fetchNextPage={activity.fetchNextPage} />
            </div>
          )}
        </Section>
      </>
    );
  }

  return (
    <div className="mx-auto flex w-full max-w-content flex-col gap-section px-gutter py-section">
      {header}
      {body}
      <LinkWalletDialog open={linking} onOpenChange={setLinking} />
    </div>
  );
}
