import {
  errorMessage,
  exchangeSign,
  formatExchangePrice,
  formatExchangeSize,
  useCancelExchangeOrder,
  useCloseExchangePosition,
  useExchangeFaucet,
  useExchangeFills,
  useNow,
  type ExchangeAccount,
  type ExchangeMarket,
  type ExchangeOrder,
  type ExchangePosition,
} from "@scaffoldapp/shared";
import { Badge, Button, ConfirmDialog, EmptyState, ErrorState, IconButton, Skeleton, Spinner, Text, toast, useTheme } from "@scaffoldapp/ui-native";
import { Droplets, History, Layers, ListOrdered, X } from "lucide-react-native";
import { useState, type ReactNode } from "react";
import { View } from "react-native";
import { signTone, Stat, StatGrid, useExchangeMoney } from "./parts";

type Lookup = (symbol: string) => ExchangeMarket | undefined;

function when(iso: string) {
  return new Date(iso).toLocaleString(undefined, { month: "short", day: "numeric", hour: "2-digit", minute: "2-digit" });
}

/** One record: a title line with an action, then label/value pairs two a row. */
function Item({ title, cells, action }: { title: ReactNode; cells: [string, ReactNode][]; action?: ReactNode }) {
  const theme = useTheme();
  const s = theme.space;
  return (
    <View style={{ gap: s[2], paddingVertical: s[3], borderBottomWidth: 1, borderBottomColor: theme.colors.border }}>
      <View style={{ flexDirection: "row", alignItems: "center", gap: s[2], minHeight: theme.tap.base }}>
        <View style={{ flex: 1, flexDirection: "row", alignItems: "center", gap: s[2], flexWrap: "wrap" }}>{title}</View>
        {action}
      </View>
      <StatGrid>
        {cells.map(([label, value]) => (
          <Stat key={label} label={label}>
            {value}
          </Stat>
        ))}
      </StatGrid>
    </View>
  );
}

/** Account value, PnL and margin. */
export function AccountStats({ account }: { account: ExchangeAccount }) {
  const { money } = useExchangeMoney();
  return (
    <StatGrid>
      <Stat label="Account value">{money(account.account_value)}</Stat>
      <Stat label="Profit and loss">
        <Text role="label" tabular tone={signTone(account.pnl)}>
          {money(account.pnl, true)}
        </Text>
      </Stat>
      <Stat label="Free collateral">{money(account.free_collateral)}</Stat>
      <Stat label="Margin used">{money(account.margin_used)}</Stat>
    </StatGrid>
  );
}

/** The faucet button when the exchange has one and the balance is below its amount. */
export function FaucetButton({ account }: { account: ExchangeAccount }) {
  const { money, config } = useExchangeMoney();
  const faucet = useExchangeFaucet();
  const now = useNow(30_000, !!account.faucet_available_at);
  if (!config?.faucet_amount || Number(account.balance) >= Number(config.faucet_amount)) return null;
  const waiting = !!account.faucet_available_at && Date.parse(account.faucet_available_at) > now;
  return (
    <Button
      variant="secondary"
      leftIcon={Droplets}
      disabled={waiting}
      loading={faucet.isPending}
      onPress={() =>
        faucet.mutate(undefined, {
          onSuccess: () => toast.success(`Balance topped up to ${money(config.faucet_amount)}`),
          onError: (e) => toast.error(errorMessage(e, "Couldn't claim test funds.")),
        })
      }
    >
      {waiting ? "Test funds used" : `Top up to ${money(config.faucet_amount)}`}
    </Button>
  );
}

function PositionItem({ p, market }: { p: ExchangePosition; market: ExchangeMarket | undefined }) {
  const { money } = useExchangeMoney();
  const close = useCloseExchangePosition();
  const [confirm, setConfirm] = useState(false);
  const long = exchangeSign(p.sz) > 0;
  const abs = p.sz.replace(/^-/, "");
  return (
    <>
      <Item
        title={
          <>
            <Text role="label" weight={600}>
              {p.symbol}
            </Text>
            <Badge size="sm" tone={long ? "success" : "danger"}>
              {long ? "Long" : "Short"}
            </Badge>
            <Text role="caption" tone="muted">
              {p.leverage}×
            </Text>
          </>
        }
        action={
          <Button variant="outline" size="md" loading={close.isPending} onPress={() => setConfirm(true)}>
            Close
          </Button>
        }
        cells={[
          ["Size", formatExchangeSize(abs, market?.sz_decimals)],
          [
            "PnL",
            <Text key="pnl" role="label" tabular tone={signTone(p.unrealized_pnl)}>
              {money(p.unrealized_pnl, true)} ({p.roe_pct}%)
            </Text>,
          ],
          ["Entry", formatExchangePrice(p.entry_px, market?.tick_size)],
          ["Liq. price", p.liquidation_px ? formatExchangePrice(p.liquidation_px, market?.tick_size) : "–"],
        ]}
      />
      <ConfirmDialog
        open={confirm}
        onClose={() => setConfirm(false)}
        title={`Close ${p.symbol} at market?`}
        tone="danger"
        confirmLabel="Close position"
        onConfirm={() => close.mutateAsync({ symbol: p.symbol }).then(() => toast.success(`${p.symbol} position closed`))}
        errorText={(e) => errorMessage(e, "Couldn't close the position. Try again.")}
      >
        <Text role="body">
          {long ? "Sells" : "Buys"} {formatExchangeSize(abs, market?.sz_decimals)} {p.symbol} against the book now.
        </Text>
      </ConfirmDialog>
    </>
  );
}

export function PositionList({ positions, lookup }: { positions: ExchangePosition[]; lookup: Lookup }) {
  if (positions.length === 0) return <EmptyState compact icon={Layers} title="No open positions" />;
  return (
    <View>
      {positions.map((p) => (
        <PositionItem key={p.symbol} p={p} market={lookup(p.symbol)} />
      ))}
    </View>
  );
}

export function OrderList({ orders, lookup }: { orders: ExchangeOrder[]; lookup: Lookup }) {
  const cancel = useCancelExchangeOrder();
  if (orders.length === 0) return <EmptyState compact icon={ListOrdered} title="No open orders" />;
  return (
    <View>
      {orders.map((o) => {
        const m = lookup(o.symbol);
        return (
          <Item
            key={o.id}
            title={
              <>
                <Text role="label" weight={600}>
                  {o.symbol}
                </Text>
                <Badge size="sm" tone={o.side === "buy" ? "success" : "danger"}>
                  {o.side === "buy" ? "Buy" : "Sell"}
                </Badge>
              </>
            }
            action={
              <IconButton
                label={`Cancel ${o.side} ${o.symbol} order`}
                icon={X}
                variant="ghost"
                onPress={() => cancel.mutate(o.id, { onError: (e) => toast.error(errorMessage(e, "Couldn't cancel the order.")) })}
              />
            }
            cells={[
              ["Price", formatExchangePrice(o.px, m?.tick_size)],
              ["Filled", `${formatExchangeSize(o.filled_sz, m?.sz_decimals)} / ${formatExchangeSize(o.sz, m?.sz_decimals)}`],
            ]}
          />
        );
      })}
    </View>
  );
}

export function FillList({ lookup, symbol }: { lookup: Lookup; symbol?: string }) {
  const theme = useTheme();
  const { money } = useExchangeMoney();
  const fills = useExchangeFills();
  if (fills.isPending) return <Skeleton height={theme.tap.xl} />;
  if (fills.isError) return <ErrorState compact title="Couldn't load fills" info={fills.error.message} onRetry={() => void fills.refetch()} />;
  const rows = fills.data.items.filter((f) => !symbol || f.symbol === symbol);
  if (rows.length === 0) return <EmptyState compact icon={History} title="No trades yet" />;
  return (
    <View>
      {rows.map((f) => {
        const m = lookup(f.symbol);
        return (
          <Item
            key={f.id}
            title={
              <>
                <Text role="label" weight={600}>
                  {f.symbol}
                </Text>
                <Badge size="sm" tone={f.side === "buy" ? "success" : "danger"}>
                  {f.side === "buy" ? "Buy" : "Sell"}
                </Badge>
                {f.liquidation && (
                  <Badge size="sm" tone="warning">
                    Liquidation
                  </Badge>
                )}
                <Text role="caption" tone="muted">
                  {when(f.created_at)}
                </Text>
              </>
            }
            cells={[
              ["Price", formatExchangePrice(f.px, m?.tick_size)],
              ["Size", formatExchangeSize(f.sz, m?.sz_decimals)],
              [
                "Realized",
                <Text key="r" role="label" tabular tone={signTone(f.realized_pnl)}>
                  {money(f.realized_pnl, true)}
                </Text>,
              ],
              ["Fee", money(f.fee)],
            ]}
          />
        );
      })}
      {fills.hasNextPage &&
        (fills.isFetchingNextPage ? (
          <Spinner />
        ) : (
          <Button variant="ghost" onPress={() => void fills.fetchNextPage()}>
            Load more
          </Button>
        ))}
    </View>
  );
}
