import {
  errorMessage,
  exchangeMarginFor,
  formatExchangePrice,
  formatExchangeSize,
  usePlaceExchangeOrder,
  type ExchangeAccount,
  type ExchangeMarket,
  type ExchangeOrderResult,
  type ExchangeOrderType,
  type ExchangeSide,
} from "@scaffoldapp/shared";
import { Button, Checkbox, Field, Input, SegmentedControl, Sheet, Stepper, Text, haptic, toast, useTheme } from "@scaffoldapp/ui-native";
import { useEffect, useMemo, useState } from "react";
import { Pressable, View } from "react-native";
import { useExchangeMoney } from "./parts";

type Unit = "base" | "quote";

function outcome(res: ExchangeOrderResult, market: ExchangeMarket): string {
  const o = res.order;
  if (res.fills.length > 0) {
    let qty = 0;
    let cost = 0;
    for (const f of res.fills) {
      qty += Number(f.sz);
      cost += Number(f.sz) * Number(f.px);
    }
    const avg = qty > 0 ? (cost / qty).toFixed(market.tick_size.split(".")[1]?.length ?? 0) : "";
    return `${o.side === "buy" ? "Bought" : "Sold"} ${formatExchangeSize(o.filled_sz, market.sz_decimals)} ${market.symbol} at ${formatExchangePrice(avg, market.tick_size)}`;
  }
  if (o.status === "open") return `Order for ${formatExchangeSize(o.sz, market.sz_decimals)} ${market.symbol} at ${formatExchangePrice(o.px, market.tick_size)} is on the book`;
  return "The order didn't fill";
}

/** Order ticket in a sheet: market or limit, size in the asset or the quote currency, leverage, reduce-only. */
export function TicketSheet({
  market,
  account,
  side: initialSide,
  open,
  onClose,
}: {
  market: ExchangeMarket;
  account: ExchangeAccount | undefined;
  side: ExchangeSide;
  open: boolean;
  onClose: () => void;
}) {
  const theme = useTheme();
  const s = theme.space;
  const { money, currency } = useExchangeMoney();
  const place = usePlaceExchangeOrder();
  const position = account?.positions.find((p) => p.symbol === market.symbol);
  const [side, setSide] = useState<ExchangeSide>(initialSide);
  const [type, setType] = useState<ExchangeOrderType>("market");
  const [px, setPx] = useState("");
  const [size, setSize] = useState("");
  const [unit, setUnit] = useState<Unit>("quote");
  const [leverage, setLeverage] = useState(position?.leverage ?? Math.min(3, market.max_leverage));
  const [reduceOnly, setReduceOnly] = useState(false);
  useEffect(() => {
    if (open) setSide(initialSide);
  }, [open, initialSide]);

  const refPx = type === "limit" && Number(px) > 0 ? Number(px) : Number(market.stats.mark_px);
  const est = useMemo(() => {
    const n = Number(size);
    if (!(n > 0) || !(refPx > 0)) return null;
    const notional = unit === "quote" ? n : n * refPx;
    return { notional, margin: exchangeMarginFor(notional, leverage) };
  }, [size, unit, refPx, leverage]);
  const invalid = !(Number(size) > 0) || (type === "limit" && !(Number(px) > 0)) || market.status === "halted";

  const submit = () =>
    place.mutate(
      {
        symbol: market.symbol,
        side,
        type,
        ...(unit === "quote" ? { sz_quote: size } : { sz: size }),
        ...(type === "limit" ? { px } : {}),
        leverage,
        reduce_only: reduceOnly || undefined,
      },
      {
        onSuccess: (res) => {
          haptic.success();
          toast.success(outcome(res, market));
          setSize("");
          onClose();
        },
        onError: (e) => toast.error(errorMessage(e, "Couldn't place the order. Try again.")),
      },
    );

  return (
    <Sheet
      open={open}
      onClose={onClose}
      title={`${side === "buy" ? "Buy" : "Sell"} ${market.symbol}`}
      snapPoints={["85%"]}
      footer={
        <Button variant={side === "buy" ? "primary" : "danger"} fullWidth loading={place.isPending} disabled={invalid} onPress={submit}>
          {side === "buy" ? "Buy · long" : "Sell · short"}
        </Button>
      }
    >
      <View style={{ gap: s[4] }}>
        <SegmentedControl<ExchangeSide>
          accessibilityLabel="Side"
          options={[
            { value: "buy", label: "Buy · long" },
            { value: "sell", label: "Sell · short" },
          ]}
          value={side}
          onValueChange={setSide}
        />
        <SegmentedControl<ExchangeOrderType>
          accessibilityLabel="Order type"
          options={[
            { value: "market", label: "Market" },
            { value: "limit", label: "Limit" },
          ]}
          value={type}
          onValueChange={setType}
        />
        {type === "limit" && (
          <Field label="Limit price">
            <Input keyboardType="decimal-pad" value={px} onChangeText={(t) => setPx(t.replace(/[^0-9.]/g, ""))} placeholder={market.stats.mark_px} trailing={<Text role="caption" tone="muted">{currency}</Text>} />
          </Field>
        )}
        <Field
          label="Size"
          labelEnd={
            <Pressable accessibilityRole="button" hitSlop={12} onPress={() => setUnit(unit === "quote" ? "base" : "quote")}>
              <Text role="caption" tone="primary" weight={600}>
                In {unit === "quote" ? market.symbol : currency}
              </Text>
            </Pressable>
          }
        >
          <Input keyboardType="decimal-pad" value={size} onChangeText={(t) => setSize(t.replace(/[^0-9.]/g, ""))} placeholder="0" trailing={<Text role="caption" tone="muted">{unit === "quote" ? currency : market.symbol}</Text>} />
        </Field>
        <Stepper label="Leverage" value={leverage} min={1} max={market.max_leverage} onValueChange={setLeverage} format={(n) => `${n}×`} />
        <Checkbox checked={reduceOnly} onCheckedChange={setReduceOnly} label="Reduce only" info="Only shrinks an open position; never opens or flips one." />
        <View style={{ gap: s[1.5] }}>
          {[
            ["Order value", est ? money(est.notional.toFixed(2)) : "–"],
            ["Margin", est ? money(est.margin.toFixed(2)) : "–"],
            ["Free collateral", account ? money(account.free_collateral) : "–"],
          ].map(([k, v]) => (
            <View key={k} style={{ flexDirection: "row", justifyContent: "space-between", gap: s[3] }}>
              <Text role="caption" tone="muted">
                {k}
              </Text>
              <Text role="caption" tabular>
                {v}
              </Text>
            </View>
          ))}
        </View>
        {market.status !== "active" && (
          <Text role="label" tone="warning">
            {market.status === "halted" ? "Trading in this market is paused." : "This market only accepts orders that reduce a position."}
          </Text>
        )}
      </View>
    </Sheet>
  );
}
