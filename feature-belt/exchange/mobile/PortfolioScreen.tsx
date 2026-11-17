import { useExchangeAccount, useExchangeMarkets } from "@scaffoldapp/shared";
import { ErrorState, Screen, Skeleton, TitleSm, useTheme } from "@scaffoldapp/ui-native";
import { useCallback, type ReactNode } from "react";
import { RefreshControl, View } from "react-native";
import { AccountStats, FaucetButton, FillList, OrderList, PositionList } from "./AccountLists";

function Group({ title, children }: { title: string; children: ReactNode }) {
  const theme = useTheme();
  return (
    <View style={{ gap: theme.space[3] }}>
      <TitleSm>{title}</TitleSm>
      {children}
    </View>
  );
}

/** The trading account: value and PnL, positions, open orders and recent fills. */
export function PortfolioScreen() {
  const theme = useTheme();
  const s = theme.space;
  const account = useExchangeAccount();
  const markets = useExchangeMarkets();
  const lookup = useCallback((x: string) => markets.data?.items.find((m) => m.symbol === x), [markets.data]);

  return (
    <Screen edges={["bottom"]} gap={s[8]} refreshControl={<RefreshControl refreshing={account.isRefetching} onRefresh={() => void account.refetch()} />}>
      {account.isPending ? (
        <View style={{ gap: s[3] }} accessibilityLabel="Loading your account" accessibilityRole="progressbar">
          <Skeleton height={theme.tap.xl} />
          <Skeleton height={theme.tap.xl} />
        </View>
      ) : account.isError ? (
        <ErrorState title="Couldn't load your account" info={account.error.message} onRetry={() => void account.refetch()} retrying={account.isFetching} />
      ) : (
        <>
          <View style={{ gap: s[4] }}>
            <AccountStats account={account.data} />
            <FaucetButton account={account.data} />
          </View>
          <Group title="Positions">
            <PositionList positions={account.data.positions} lookup={lookup} />
          </Group>
          <Group title="Open orders">
            <OrderList orders={account.data.open_orders} lookup={lookup} />
          </Group>
        </>
      )}
      <Group title="Fills">
        <FillList lookup={lookup} />
      </Group>
    </Screen>
  );
}
