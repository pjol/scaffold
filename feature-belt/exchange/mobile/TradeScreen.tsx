import {
  compactExchangeNumber,
  exchangeCountdown,
  formatExchangeFunding,
  formatExchangePrice,
  formatExchangeSize,
  useExchangeAccount,
  useExchangeBook,
  useExchangeMarket,
  useExchangeMarketTopic,
  useExchangeMarkets,
  useExchangeTrades,
  useNow,
  type ExchangeMarket,
  type ExchangeSide,
} from "@scaffoldapp/shared";
import { Button, EmptyState, ErrorState, Screen, SegmentedControl, Skeleton, Text, Title, useTheme } from "@scaffoldapp/ui-native";
import { router, Stack } from "expo-router";
import { CandlestickChart } from "lucide-react-native";
import { useCallback, useState } from "react";
import { RefreshControl, View } from "react-native";
import { AccountStats, FaucetButton, FillList, OrderList, PositionList } from "./AccountLists";
import { CandleChart } from "./CandleChart";
import { ChangeText, IndexStatusBadge, MarketStatusBadge, Stat, StatGrid } from "./parts";
import { TicketSheet } from "./TicketSheet";

type Pane = "book" | "trades";
type Mine = "positions" | "orders" | "fills";

function Book({ market }: { market: ExchangeMarket }) {
  const theme = useTheme();
  const book = useExchangeBook(market.symbol);
  if (book.isPending) return <Skeleton height={theme.tap.xl * 3} />;
  if (book.isError) return <ErrorState compact title="Couldn't load the order book" onRetry={() => void book.refetch()} />;
  const asks = book.data.asks.slice(0, 6).reverse();
  const bids = book.data.bids.slice(0, 6);
  const max = Math.max(1e-12, ...asks.map((l) => Number(l.sz)), ...bids.map((l) => Number(l.sz)));
  const row = (px: string, sz: string, side: "bid" | "ask") => (
    <View key={side + px} style={{ flexDirection: "row", height: 24, alignItems: "center" }} accessibilityLabel={`${side === "bid" ? "Bid" : "Ask"} ${px}, size ${sz}`}>
      <View
        style={{
          position: "absolute",
          right: 0,
          top: 2,
          bottom: 2,
          width: `${Math.min(100, (Number(sz) / max) * 100)}%`,
          backgroundColor: side === "bid" ? theme.colors.successSoft : theme.colors.dangerSoft,
          borderRadius: theme.radius.xs,
        }}
      />
      <Text role="caption" tabular tone={side === "bid" ? "success" : "danger"} style={{ flex: 1 }}>
        {formatExchangePrice(px, market.tick_size)}
      </Text>
      <Text role="caption" tabular>
        {formatExchangeSize(sz, market.sz_decimals)}
      </Text>
    </View>
  );
  return (
    <View>
      {asks.length === 0 ? <Text role="caption" tone="muted">No asks</Text> : asks.map((l) => row(l.px, l.sz, "ask"))}
      <View style={{ flexDirection: "row", paddingVertical: theme.space[1.5], borderTopWidth: 1, borderBottomWidth: 1, borderColor: theme.colors.border }}>
        <Text role="label" tabular style={{ flex: 1 }}>
          {formatExchangePrice(market.stats.mark_px, market.tick_size)}
        </Text>
        <Text role="caption" tone="muted">
          Mark
        </Text>
      </View>
      {bids.length === 0 ? <Text role="caption" tone="muted">No bids</Text> : bids.map((l) => row(l.px, l.sz, "bid"))}
    </View>
  );
}

function Trades({ market }: { market: ExchangeMarket }) {
  const theme = useTheme();
  const trades = useExchangeTrades(market.symbol);
  if (trades.isPending) return <Skeleton height={theme.tap.xl * 3} />;
  if (trades.isError) return <ErrorState compact title="Couldn't load trades" onRetry={() => void trades.refetch()} />;
  if (trades.data.items.length === 0) return <Text role="label" tone="muted">No trades yet</Text>;
  return (
    <View>
      {trades.data.items.slice(0, 12).map((t) => (
        <View key={t.id} style={{ flexDirection: "row", height: 24, alignItems: "center", gap: theme.space[2] }}>
          <Text role="caption" tabular tone={t.side === "buy" ? "success" : "danger"} style={{ flex: 1 }}>
            {formatExchangePrice(t.px, market.tick_size)}
            {t.liquidation ? " L" : ""}
          </Text>
          <Text role="caption" tabular style={{ flex: 1, textAlign: "right" }}>
            {formatExchangeSize(t.sz, market.sz_decimals)}
          </Text>
          <Text role="caption" tone="muted" tabular style={{ flex: 1, textAlign: "right" }}>
            {new Date(t.at).toLocaleTimeString(undefined, { hour: "2-digit", minute: "2-digit", second: "2-digit" })}
            {t.sim ? " · sim" : ""}
          </Text>
        </View>
      ))}
    </View>
  );
}

/** One market: stats, chart with history, book and trades, the caller's positions; Buy and Sell open the ticket. */
export function TradeScreen({ symbol }: { symbol: string }) {
  const theme = useTheme();
  const s = theme.space;
  const sym = symbol.toUpperCase();
  const market = useExchangeMarket(sym);
  const markets = useExchangeMarkets();
  const account = useExchangeAccount();
  useExchangeMarketTopic(market.data?.id);
  const now = useNow(1000);
  const [pane, setPane] = useState<Pane>("book");
  const [mine, setMine] = useState<Mine>("positions");
  const [ticket, setTicket] = useState<ExchangeSide | null>(null);
  const lookup = useCallback((x: string) => markets.data?.items.find((m) => m.symbol === x) ?? (market.data?.symbol === x ? market.data : undefined), [markets.data, market.data]);

  if (market.isPending) {
    return (
      <Screen edges={["bottom"]} gap={s[6]}>
        <Stack.Screen options={{ title: sym }} />
        <Skeleton height={theme.type.title.lineHeight} width="50%" />
        <Skeleton height={220} />
      </Screen>
    );
  }
  if (market.isError) {
    return (
      <Screen edges={["bottom"]} centered>
        <Stack.Screen options={{ title: sym }} />
        {market.error.status === 404 ? (
          <EmptyState icon={CandlestickChart} title="This market isn't listed" action={<Button variant="secondary" onPress={() => router.back()}>Back to markets</Button>} />
        ) : (
          <ErrorState title="Couldn't load the market" info={market.error.message} onRetry={() => void market.refetch()} retrying={market.isFetching} />
        )}
      </Screen>
    );
  }
  const m = market.data;
  const st = m.stats;
  const myOrders = account.data?.open_orders.filter((o) => o.symbol === sym) ?? [];

  return (
    <Screen
      edges={["bottom"]}
      gap={s[8]}
      refreshControl={<RefreshControl refreshing={market.isRefetching} onRefresh={() => void Promise.all([market.refetch(), account.refetch()])} />}
      footer={
        <View style={{ flexDirection: "row", gap: s[3] }}>
          <Button style={{ flex: 1 }} onPress={() => setTicket("buy")} disabled={m.status === "halted"}>
            Buy
          </Button>
          <Button style={{ flex: 1 }} variant="danger" onPress={() => setTicket("sell")} disabled={m.status === "halted"}>
            Sell
          </Button>
        </View>
      }
    >
      <Stack.Screen options={{ title: m.symbol }} />
      <View style={{ gap: s[4] }}>
        <View style={{ flexDirection: "row", alignItems: "center", gap: s[2], flexWrap: "wrap" }}>
          <Title>{m.name}</Title>
          <MarketStatusBadge market={m} />
        </View>
        <StatGrid>
          <Stat label="Mark">
            <View style={{ flexDirection: "row", alignItems: "center", gap: s[2] }}>
              <Text role="label" tabular>
                {formatExchangePrice(st.mark_px, m.tick_size)}
              </Text>
              <ChangeText pct={st.day_change_pct} />
            </View>
          </Stat>
          <Stat label="Index">
            <View style={{ flexDirection: "row", alignItems: "center", gap: s[2] }}>
              <Text role="label" tabular>
                {st.index_status === "none" ? "–" : formatExchangePrice(st.index_px, m.tick_size)}
              </Text>
              <IndexStatusBadge status={st.index_status} />
            </View>
          </Stat>
          <Stat label="24h volume">{compactExchangeNumber(st.day_volume)}</Stat>
          <Stat label="Open interest">{compactExchangeNumber(st.open_interest_value)}</Stat>
          <Stat label="Funding / h">{formatExchangeFunding(st.funding_rate)}</Stat>
          <Stat label="Next funding">{exchangeCountdown(st.next_funding_at, now)}</Stat>
        </StatGrid>
      </View>
      <CandleChart market={m} />
      <View style={{ gap: s[3] }}>
        <SegmentedControl<Pane>
          accessibilityLabel="Book or trades"
          options={[
            { value: "book", label: "Order book" },
            { value: "trades", label: "Trades" },
          ]}
          value={pane}
          onValueChange={setPane}
        />
        {pane === "book" ? <Book market={m} /> : <Trades market={m} />}
      </View>
      <View style={{ gap: s[3] }}>
        {account.data ? (
          <>
            <AccountStats account={account.data} />
            <FaucetButton account={account.data} />
          </>
        ) : account.isError ? (
          <ErrorState compact title="Couldn't load your account" onRetry={() => void account.refetch()} />
        ) : (
          <Skeleton height={theme.tap.xl} />
        )}
        <SegmentedControl<Mine>
          accessibilityLabel="Your trading"
          options={[
            { value: "positions", label: `Positions${account.data?.positions.length ? ` ${account.data.positions.length}` : ""}` },
            { value: "orders", label: `Orders${myOrders.length ? ` ${myOrders.length}` : ""}` },
            { value: "fills", label: "Fills" },
          ]}
          value={mine}
          onValueChange={setMine}
        />
        {mine === "positions" ? (
          <PositionList positions={account.data?.positions ?? []} lookup={lookup} />
        ) : mine === "orders" ? (
          <OrderList orders={myOrders} lookup={lookup} />
        ) : (
          <FillList lookup={lookup} symbol={sym} />
        )}
      </View>
      <TicketSheet market={m} account={account.data} side={ticket ?? "buy"} open={ticket !== null} onClose={() => setTicket(null)} />
    </Screen>
  );
}
