import { BASKETS_STATUS_LABEL, compactExchangeNumber, errorMessage, useBasket, useReportBasket, type BasketsReportReason } from "@scaffoldapp/shared";
import { Banner, Button, Caption, EmptyState, ErrorState, Headline, ListGroup, Screen, Skeleton, Title, toast, useTheme } from "@scaffoldapp/ui-native";
import { router, Stack, useLocalSearchParams } from "expo-router";
import { CandlestickChart, Copy, Flag, ScanSearch } from "lucide-react-native";
import { RefreshControl, View } from "react-native";
import { ResourceMenu } from "@/components/resource/ResourceMenu";
import type { ResourceMenuAction } from "@/features/registry";
import { Stat, StatGrid, useExchangeMoney } from "../exchange/parts";
import { BasketLines, BasketStatusBadge, PriceStatusBadge } from "./parts";

const REASONS: { id: BasketsReportReason; label: string }[] = [
  { id: "name", label: "The name is offensive or misleading" },
  { id: "description", label: "The description is offensive or misleading" },
  { id: "spam", label: "Spam" },
  { id: "other", label: "Something else" },
];

/** One basket: price, status, lines with weights, its market and who listed it. */
export function BasketScreen() {
  const theme = useTheme();
  const s = theme.space;
  const { symbol } = useLocalSearchParams<{ symbol: string }>();
  const sym = (symbol ?? "").toUpperCase();
  const basket = useBasket(sym || null);
  const report = useReportBasket(sym);
  const { money } = useExchangeMoney();

  if (basket.isPending) {
    return (
      <Screen edges={["bottom"]}>
        <Stack.Screen options={{ title: sym }} />
        <Skeleton height={s[8]} width="60%" />
        <Skeleton height={s[24]} />
        <Skeleton height={s[32]} />
      </Screen>
    );
  }
  if (basket.isError) {
    return (
      <Screen edges={["bottom"]}>
        <Stack.Screen options={{ title: sym }} />
        {basket.error.status === 404 ? (
          <EmptyState icon={ScanSearch} title="No such basket" />
        ) : (
          <ErrorState title="Couldn't load this basket" info={errorMessage(basket.error)} onRetry={() => void basket.refetch()} retrying={basket.isRefetching} />
        )}
      </Screen>
    );
  }
  const b = basket.data;
  const actions: ResourceMenuAction[] = [
    { id: "copy", label: "Start a basket from this one", icon: Copy, onSelect: () => router.push({ pathname: "/baskets/new", params: { from: b.symbol } }) },
    {
      id: "report",
      label: b.reported ? "Reported" : "Report",
      icon: Flag,
      disabled: b.reported,
      choices: REASONS.map((r) => ({
        id: r.id,
        label: r.label,
        onSelect: () =>
          report.mutate(
            { reason: r.id },
            { onSuccess: () => toast.success("Reported. Staff will review it."), onError: (e) => toast.error(errorMessage(e, "Couldn't send the report.")) },
          ),
      })),
    },
  ];
  const tradable = b.status !== "archived" && !!b.market;

  return (
    <Screen
      edges={["bottom"]}
      gap={s[6]}
      refreshControl={<RefreshControl refreshing={basket.isRefetching} onRefresh={() => void basket.refetch()} />}
      footer={
        tradable ? (
          <Button fullWidth leftIcon={CandlestickChart} onPress={() => router.push({ pathname: "/markets/[symbol]", params: { symbol: b.symbol } })}>
            Trade
          </Button>
        ) : undefined
      }
    >
      <Stack.Screen options={{ title: b.symbol, headerRight: () => <ResourceMenu feature="baskets" kind="baskets.basket" id={b.id} title={b.name} actions={actions} /> }} />
      <View style={{ gap: s[2] }}>
        <Title>{b.name}</Title>
        <View style={{ flexDirection: "row", alignItems: "center", flexWrap: "wrap", gap: s[2] }}>
          <Headline tabular>{money(b.price ?? b.value)}</Headline>
          <PriceStatusBadge status={b.price_status} />
          <BasketStatusBadge basket={b} />
        </View>
        <Caption>
          Listed by {b.creator} · {b.entries} {b.entries === 1 ? "entry" : "entries"}
        </Caption>
      </View>
      {b.hidden ? (
        <Banner tone="danger" title="Staff removed this basket. Open positions can still be closed." />
      ) : b.status === "archived" ? (
        <Banner
          tone="info"
          title="Archived: nobody traded it for a while. Listing the same lines again reopens it."
          action={{ label: "Reopen", onPress: () => router.push({ pathname: "/baskets/new", params: { from: b.symbol } }) }}
        />
      ) : b.status === "reduce_only" ? (
        <Banner tone="warning" title="Winding down: only orders that close positions are accepted." />
      ) : null}
      {b.description ? <Caption>{b.description}</Caption> : null}
      <StatGrid>
        {[
          <Stat key="value" label="Value now">
            {money(b.value)}
          </Stat>,
          <Stat key="listed" label="Value when listed">
            {money(b.value_at_create)}
          </Stat>,
          <Stat key="pos" label="Positions">
            {`${b.holders} · ${money(b.open_interest)}`}
          </Stat>,
          <Stat key="vol" label="Recent volume">
            {compactExchangeNumber(b.volume_recent)}
          </Stat>,
          <Stat key="status" label="Status">
            {BASKETS_STATUS_LABEL[b.status]}
          </Stat>,
          <Stat key="ends" label="Ends">
            {b.ends_on ?? "–"}
          </Stat>,
        ]}
      </StatGrid>
      <ListGroup title="Lines" info="One basket unit holds this many of each market. Weights move with prices.">
        <BasketLines lines={b.lines} money={money} />
      </ListGroup>
      {b.market && (
        <StatGrid>
          {[
            <Stat key="lev" label="Max leverage">{`${b.market.max_leverage}×`}</Stat>,
            <Stat key="tick" label="Tick size">
              {b.market.tick_size}
            </Stat>,
            <Stat key="min" label="Minimum order">
              {money(b.market.min_notional)}
            </Stat>,
            <Stat key="oi" label="Open interest cap">
              {money(b.market.oi_cap)}
            </Stat>,
          ]}
        </StatGrid>
      )}
    </Screen>
  );
}
