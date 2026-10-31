import {
  addUnits,
  formatUnits,
  groupByDay,
  isBaseUnits,
  useWalletActivity,
  useWalletBalances,
  useWallets,
  useWalletsConfig,
  walletsConfigured,
  type WalletBalance,
  type WalletChain,
} from "@scaffoldapp/shared";
import { Button, EmptyState, ErrorState, IconButton, ListGroup, ListRow, Screen, SegmentedControl, Skeleton, SkeletonRow, Text, TitleSm, useTheme } from "@scaffoldapp/ui-native";
import { Stack, router } from "expo-router";
import { ArrowLeftRight, Settings2, Wallet as WalletIcon } from "lucide-react-native";
import { useMemo, useState } from "react";
import { RefreshControl, View } from "react-native";
import { AssetMark, LinkOnWebButton, TransferRow, WalletsNotConfigured } from "./components";

const ALL = "all";

interface AssetTotal {
  key: string;
  symbol: string;
  decimals: number;
  amount: string | null;
  partial: boolean;
}

/** Sum each asset across every linked wallet (string math), per chain. */
function totals(items: WalletBalance[], chains: WalletChain[]): { chain: WalletChain; assets: AssetTotal[] }[] {
  return chains
    .map((chain) => {
      const assets = new Map<string, AssetTotal>();
      for (const b of items) {
        if (b.chain_id !== chain.id) continue;
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

function ListSkeleton({ rows }: { rows: number }) {
  const theme = useTheme();
  return (
    <View style={{ gap: theme.space[2] }} accessibilityLabel="Loading" accessibilityRole="progressbar">
      <Skeleton shape="text" width={96} height={20} />
      {Array.from({ length: rows }, (_, i) => (
        <SkeletonRow key={i} />
      ))}
    </View>
  );
}

/** Balances per network and token (all wallets summed) and live activity grouped by day. */
export default function WalletScreen() {
  const theme = useTheme();
  const config = useWalletsConfig();
  const wallets = useWallets();
  const [chainId, setChainId] = useState(ALL);
  const chains = config.data?.chains ?? [];
  const hasWallets = (wallets.data?.length ?? 0) > 0;
  const ready = walletsConfigured(config.data) && hasWallets;
  const balances = useWalletBalances({}, { enabled: ready });
  const activity = useWalletActivity(chainId === ALL ? {} : { chain_id: Number(chainId) }, { enabled: ready });

  const groups = useMemo(() => {
    const shown = chainId === ALL ? chains : chains.filter((c) => String(c.id) === chainId);
    return totals(balances.data?.items ?? [], shown);
  }, [balances.data, chains, chainId]);
  const days = useMemo(() => groupByDay(activity.data?.items ?? []), [activity.data]);

  const refresh = () => {
    void balances.refetch();
    void activity.refetch();
  };

  const header = (
    <Stack.Screen
      options={{
        title: "Wallet",
        headerRight: () => <IconButton label="Manage wallets" icon={Settings2} variant="ghost" onPress={() => router.push("/wallets/manage")} />,
      }}
    />
  );

  if (config.isPending || wallets.isPending) {
    return (
      <Screen edges={["bottom"]}>
        {header}
        <ListSkeleton rows={3} />
      </Screen>
    );
  }
  if (config.isError || wallets.isError) {
    const failed = config.isError ? config : wallets;
    return (
      <Screen edges={["bottom"]} centered>
        {header}
        <ErrorState title="Couldn't load your wallet" info={failed.error?.message} onRetry={() => void failed.refetch()} retrying={failed.isRefetching} />
      </Screen>
    );
  }
  if (!walletsConfigured(config.data)) {
    return (
      <Screen edges={["bottom"]} centered>
        {header}
        <WalletsNotConfigured />
      </Screen>
    );
  }
  if (!hasWallets) {
    return (
      <Screen edges={["bottom"]} centered>
        {header}
        <EmptyState icon={WalletIcon} title="No wallets linked" action={<LinkOnWebButton url={config.data?.link_url ?? ""} />} />
      </Screen>
    );
  }

  return (
    <Screen
      edges={["bottom"]}
      gap={theme.space[8]}
      refreshControl={<RefreshControl refreshing={balances.isRefetching && !balances.isPending} onRefresh={refresh} tintColor={theme.colors.primary} />}
    >
      {header}
      {chains.length > 1 && chains.length <= 3 ? (
        <SegmentedControl
          accessibilityLabel="Network"
          value={chainId}
          onValueChange={setChainId}
          options={[{ value: ALL, label: "All" }, ...chains.map((c) => ({ value: String(c.id), label: c.name }))]}
        />
      ) : null}

      <View style={{ gap: theme.space[4] }}>
        <TitleSm accessibilityRole="header">Balances</TitleSm>
      {balances.isPending ? (
        <ListSkeleton rows={2} />
      ) : balances.isError ? (
        <ErrorState compact title="Couldn't load balances" info={balances.error.message} onRetry={() => void balances.refetch()} retrying={balances.isRefetching} />
      ) : (
        groups.map(({ chain, assets }) => (
          <ListGroup key={chain.id} title={chain.testnet ? `${chain.name} (testnet)` : chain.name}>
            {assets.map((a) => (
              <ListRow
                key={a.key}
                title={a.symbol}
                leading={<AssetMark symbol={a.symbol} />}
                {...(a.amount === null
                  ? { info: `Couldn't reach ${chain.name}. Pull to refresh in a minute.` }
                  : a.partial
                    ? { info: "Some wallets couldn't be read, so this total may be low." }
                    : {})}
                trailing={
                  <Text role="body" weight={600} tabular tone={a.amount === null ? "muted" : "default"}>
                    {a.amount === null ? "Unavailable" : formatUnits(a.amount, a.decimals)}
                  </Text>
                }
              />
            ))}
          </ListGroup>
        ))
      )}
      </View>

      <View style={{ gap: theme.space[4] }}>
        <TitleSm accessibilityRole="header">Activity</TitleSm>
      {activity.isPending ? (
        <ListSkeleton rows={4} />
      ) : activity.isError ? (
        <ErrorState compact title="Couldn't load activity" info={activity.error.message} onRetry={() => void activity.refetch()} retrying={activity.isRefetching} />
      ) : days.length === 0 ? (
        <EmptyState compact icon={ArrowLeftRight} title="No transfers yet" />
      ) : (
        <View style={{ gap: theme.space[6] }}>
          {days.map((d) => (
            <ListGroup key={d.label} title={d.label}>
              {d.items.map((t) => (
                <TransferRow key={t.id} transfer={t} />
              ))}
            </ListGroup>
          ))}
          {activity.hasNextPage ? (
            <Button variant="ghost" loading={activity.isFetchingNextPage} onPress={() => void activity.fetchNextPage()}>
              Load more
            </Button>
          ) : null}
        </View>
      )}
      </View>
    </Screen>
  );
}
