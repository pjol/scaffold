"use client";

import { formatExchangeMoney, useExchangeConfig, useRealtimeEvent } from "@scaffoldapp/shared";
import { toast } from "@scaffoldapp/ui-web";

/** Tells the signed-in person about liquidations and settled transfers wherever they are. */
export function ExchangeToasts() {
  const config = useExchangeConfig();
  const currency = config.data?.quote_currency ?? "USD";
  const decimals = config.data?.quote_decimals ?? 2;
  useRealtimeEvent("exchange.account", (e) => {
    const d = e.data;
    if (d.type === "liquidation") {
      toast.error(`${d.symbol} position liquidated: ${d.sz} at ${d.px}`);
    } else if (d.type === "transfer" && d.amount) {
      if (d.status === "applied") toast.success(`${formatExchangeMoney(d.amount, currency, decimals)} settled in your trading account`);
      else toast.error(d.reason || "A transfer was rejected");
    }
  });
  return null;
}
