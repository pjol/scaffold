"use client";

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
import { Button, Checkbox, Field, Input, SegmentedControl, Slider, cn, toast } from "@scaffoldapp/ui-web";
import { useEffect, useId, useMemo, useState } from "react";
import { useExchangeMoney } from "./parts";

type Unit = "base" | "quote";

/** What a filled or resting order did, in one line. */
export function orderOutcome(res: ExchangeOrderResult, market: ExchangeMarket): string {
  const o = res.order;
  const verb = o.side === "buy" ? "Bought" : "Sold";
  if (res.fills.length > 0) {
    let qty = 0;
    let cost = 0;
    for (const f of res.fills) {
      qty += Number(f.sz);
      cost += Number(f.sz) * Number(f.px);
    }
    const avg = qty > 0 ? (cost / qty).toFixed(Math.max(0, market.tick_size.split(".")[1]?.length ?? 0)) : o.px ?? "";
    const rest = o.status === "open" ? ", the rest is on the book" : "";
    return `${verb} ${formatExchangeSize(o.filled_sz, market.sz_decimals)} ${market.symbol} at ${formatExchangePrice(avg, market.tick_size)}${rest}`;
  }
  if (o.status === "open") return `${o.side === "buy" ? "Bid" : "Ask"} for ${formatExchangeSize(o.sz, market.sz_decimals)} ${market.symbol} at ${formatExchangePrice(o.px, market.tick_size)} is on the book`;
  return "The order didn't fill";
}

/**
 * Buy or sell ticket: market or limit, size in the asset or the quote currency, leverage up
 * to the market's max, reduce-only and post-only. Estimates are shown before sending; the
 * server decides (margin, bands, caps) and says why when it refuses.
 */
export function OrderTicket({ market, account, picked }: { market: ExchangeMarket; account: ExchangeAccount | undefined; picked?: { px: string; n: number } }) {
  const { money, currency } = useExchangeMoney();
  const place = usePlaceExchangeOrder();
  const id = useId();
  const position = account?.positions.find((p) => p.symbol === market.symbol);
  const [side, setSide] = useState<ExchangeSide>("buy");
  const [type, setType] = useState<ExchangeOrderType>("market");
  const [px, setPx] = useState("");
  const [size, setSize] = useState("");
  const [unit, setUnit] = useState<Unit>("quote");
  const [leverage, setLeverage] = useState(() => position?.leverage ?? Math.min(3, market.max_leverage));
  const [reduceOnly, setReduceOnly] = useState(false);
  const [postOnly, setPostOnly] = useState(false);

  useEffect(() => {
    if (picked) {
      setType("limit");
      setPx(picked.px);
    }
  }, [picked]);
  useEffect(() => {
    if (position) setLeverage(position.leverage);
  }, [position?.leverage]); // eslint-disable-line react-hooks/exhaustive-deps

  const refPx = type === "limit" && Number(px) > 0 ? Number(px) : Number(market.stats.mark_px);
  const est = useMemo(() => {
    const n = Number(size);
    if (!(n > 0) || !(refPx > 0)) return null;
    const notional = unit === "quote" ? n : n * refPx;
    return { notional, margin: exchangeMarginFor(notional, leverage), fee: notional * Number(market.taker_fee) };
  }, [size, unit, refPx, leverage, market.taker_fee]);

  const blocked = market.status === "halted" ? "Trading in this market is paused." : market.status === "reduce_only" && !reduceOnly ? "This market only accepts orders that reduce a position." : null;
  const invalid = !(Number(size) > 0) || (type === "limit" && !(Number(px) > 0));

  const submit = () => {
    place.mutate(
      {
        symbol: market.symbol,
        side,
        type,
        ...(unit === "quote" ? { sz_quote: size } : { sz: size }),
        ...(type === "limit" ? { px } : {}),
        leverage,
        reduce_only: reduceOnly || undefined,
        post_only: type === "limit" && postOnly ? true : undefined,
      },
      {
        onSuccess: (res) => {
          toast.success(orderOutcome(res, market));
          setSize("");
        },
        onError: (e) => toast.error(errorMessage(e, "Couldn't place the order. Try again.")),
      },
    );
  };

  return (
    <form
      className="flex flex-col gap-4"
      onSubmit={(e) => {
        e.preventDefault();
        if (!invalid && !blocked) submit();
      }}
      aria-label={`Order ticket for ${market.symbol}`}
    >
      <SegmentedControl<ExchangeSide>
        options={[
          { value: "buy", label: "Buy · long" },
          { value: "sell", label: "Sell · short" },
        ]}
        value={side}
        onValueChange={setSide}
      />
      <SegmentedControl<ExchangeOrderType>
        options={[
          { value: "market", label: "Market" },
          { value: "limit", label: "Limit" },
        ]}
        value={type}
        onValueChange={setType}
      />
      {type === "limit" && (
        <Field label="Limit price" id={`${id}-px`}>
          <Input inputMode="decimal" value={px} onChange={(e) => setPx(e.target.value.replace(/[^0-9.]/g, ""))} placeholder={market.stats.mark_px} trailing={<span className="type-caption text-fg-muted">{currency}</span>} />
        </Field>
      )}
      <Field
        label="Size"
        id={`${id}-sz`}
        labelEnd={
          <button type="button" className="type-caption font-semibold text-primary-strong" onClick={() => setUnit(unit === "quote" ? "base" : "quote")}>
            In {unit === "quote" ? market.symbol : currency}
          </button>
        }
      >
        <Input inputMode="decimal" value={size} onChange={(e) => setSize(e.target.value.replace(/[^0-9.]/g, ""))} placeholder="0" trailing={<span className="type-caption text-fg-muted">{unit === "quote" ? currency : market.symbol}</span>} />
      </Field>
      <Field label={`Leverage ${leverage}×`} info="Applies to your whole position in this market. Higher leverage needs less margin and moves the liquidation price closer.">
        <Slider value={[leverage]} min={1} max={market.max_leverage} step={1} onValueChange={(v) => setLeverage(v[0] ?? 1)} formatValue={(v) => `${v}×`} />
      </Field>
      <div className="flex flex-wrap gap-x-6 gap-y-3">
        <Checkbox checked={reduceOnly} onCheckedChange={setReduceOnly} label="Reduce only" info="Only shrinks an open position; never opens or flips one." />
        {type === "limit" && <Checkbox checked={postOnly} onCheckedChange={setPostOnly} label="Post only" info="Rests on the book as a maker order, or is refused if it would trade now." />}
      </div>
      <dl className="flex flex-col gap-1.5 type-caption">
        <div className="flex justify-between gap-3">
          <dt className="text-fg-muted">Order value</dt>
          <dd className="tabular-nums text-fg">{est ? money(est.notional.toFixed(2)) : "–"}</dd>
        </div>
        <div className="flex justify-between gap-3">
          <dt className="text-fg-muted">Margin</dt>
          <dd className="tabular-nums text-fg">{est ? money(est.margin.toFixed(2)) : "–"}</dd>
        </div>
        <div className="flex justify-between gap-3">
          <dt className="text-fg-muted">Fee (taker)</dt>
          <dd className="tabular-nums text-fg">{est ? money(est.fee.toFixed(4), false) : "–"}</dd>
        </div>
        <div className="flex justify-between gap-3">
          <dt className="text-fg-muted">Free collateral</dt>
          <dd className="tabular-nums text-fg">{account ? money(account.free_collateral) : "–"}</dd>
        </div>
      </dl>
      {blocked && <p className="type-label text-warning-strong">{blocked}</p>}
      <Button type="submit" variant={side === "buy" ? "success" : "danger"} size="lg" fullWidth loading={place.isPending} disabled={invalid || !!blocked}>
        {side === "buy" ? "Buy" : "Sell"} {market.symbol}
      </Button>
      {position && (
        <p className={cn("type-caption text-fg-muted")}>
          Position: {formatExchangeSize(position.sz, market.sz_decimals)} {market.symbol} at {formatExchangePrice(position.entry_px, market.tick_size)}
        </p>
      )}
    </form>
  );
}
