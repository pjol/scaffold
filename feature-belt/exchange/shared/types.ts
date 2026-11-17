/**
 * Exchange DTOs, mirroring backend/internal/exchange (engine/wire.go). Prices, sizes and
 * amounts are decimal strings (never parse them into floats for money math; format them
 * for display only). Times are ISO 8601 except candle `t` (unix ms, the bucket's open).
 */

export type ExchangeSide = "buy" | "sell";
export type ExchangeOrderType = "market" | "limit";
export type ExchangeOrderStatus = "open" | "filled" | "cancelled" | "rejected";
export type ExchangeMarketStatus = "active" | "reduce_only" | "halted";
/** none: no price yet · seed: catalog reference (development) · live: price feed · manual: set by an admin · stale: the feed stopped. */
export type ExchangeIndexStatus = "none" | "seed" | "live" | "manual" | "stale";
export type ExchangeCandleInterval = "1m" | "5m" | "15m" | "1h" | "4h" | "1d";

export const EXCHANGE_INTERVALS: readonly ExchangeCandleInterval[] = ["1m", "5m", "15m", "1h", "4h", "1d"];
export const EXCHANGE_INTERVAL_MS: Readonly<Record<ExchangeCandleInterval, number>> = {
  "1m": 60_000,
  "5m": 300_000,
  "15m": 900_000,
  "1h": 3_600_000,
  "4h": 14_400_000,
  "1d": 86_400_000,
};

/** `GET /v1/exchange/config`. */
export interface ExchangeConfig {
  quote_currency: string;
  quote_decimals: number;
  /** Null: there is no faucet. */
  faucet_amount: string | null;
  faucet_cooldown_hours: number;
  /** Simulated market maker and traders are running (their trades are flagged `sim`). */
  sim_enabled: boolean;
  intervals: ExchangeCandleInterval[];
  funding_interval_minutes: number;
}

export interface ExchangeMarketStats {
  symbol: string;
  mark_px: string;
  index_px: string;
  index_status: ExchangeIndexStatus;
  index_updated_at: string | null;
  mid_px: string | null;
  prev_day_px: string;
  day_change_pct: string;
  /** Quote currency. */
  day_volume: string;
  /** Share of 24h volume with a simulated account on either side, "0.0"–"100.0". */
  day_volume_sim_pct: string;
  /** Long units (= short units). */
  open_interest: string;
  open_interest_value: string;
  /** Per hour; positive: longs pay shorts. */
  funding_rate: string;
  next_funding_at: string;
}

export interface ExchangeMarket {
  id: string;
  symbol: string;
  name: string;
  category: string;
  description: string;
  image_url: string;
  status: ExchangeMarketStatus;
  max_leverage: number;
  maintenance_margin_ratio: string;
  taker_fee: string;
  maker_fee: string;
  sz_decimals: number;
  tick_size: string;
  min_notional: string;
  oi_cap: string;
  stats: ExchangeMarketStats;
}

export interface ExchangeBookLevel {
  px: string;
  sz: string;
  /** Orders at this price. */
  n: number;
}

export interface ExchangeBook {
  symbol: string;
  bids: ExchangeBookLevel[];
  asks: ExchangeBookLevel[];
  at: string;
}

export interface ExchangeTrade {
  id: string;
  symbol: string;
  px: string;
  sz: string;
  /** Taker side. */
  side: ExchangeSide;
  /** A simulated account was on either side. */
  sim: boolean;
  liquidation: boolean;
  at: string;
}

export interface ExchangeCandle {
  /** Bucket open, unix ms. */
  t: number;
  o: string;
  h: string;
  l: string;
  c: string;
  v: string;
  /** A daily bar from the price feed's history, before the market listed. */
  backfill: boolean;
}

/** `GET /v1/exchange/markets/{symbol}/candles`: oldest first; pass `next_before` as `before` for older. */
export interface ExchangeCandlePage {
  items: ExchangeCandle[];
  next_before: number | null;
}

export interface ExchangeOrder {
  id: string;
  symbol: string;
  side: ExchangeSide;
  type: ExchangeOrderType;
  /** Limit price; null for market orders. */
  px: string | null;
  sz: string;
  filled_sz: string;
  status: ExchangeOrderStatus;
  reduce_only: boolean;
  post_only: boolean;
  leverage: number;
  liquidation: boolean;
  created_at: string;
  updated_at: string;
}

export interface ExchangeFill {
  id: string;
  order_id: string;
  symbol: string;
  side: ExchangeSide;
  px: string;
  sz: string;
  fee: string;
  realized_pnl: string;
  liquidity: "maker" | "taker";
  liquidation: boolean;
  created_at: string;
}

export interface ExchangePosition {
  symbol: string;
  /** Signed: long > 0. */
  sz: string;
  entry_px: string;
  mark_px: string;
  notional: string;
  unrealized_pnl: string;
  roe_pct: string;
  leverage: number;
  margin_used: string;
  liquidation_px: string | null;
  funding_since_open: string;
}

/** `GET /v1/exchange/account` (created with the welcome credit on first call). */
export interface ExchangeAccount {
  balance: string;
  deposits: string;
  account_value: string;
  /** account_value − deposits. */
  pnl: string;
  unrealized_pnl: string;
  margin_used: string;
  maintenance_margin: string;
  free_collateral: string;
  positions: ExchangePosition[];
  open_orders: ExchangeOrder[];
  /** When the faucet can next be claimed; null: now (or no faucet). */
  faucet_available_at: string | null;
}

export interface ExchangeFundingPayment {
  id: string;
  symbol: string;
  rate: string;
  sz: string;
  payment: string;
  created_at: string;
}

export type ExchangeLedgerKind = "deposit" | "withdrawal" | "faucet" | "fee" | "realized_pnl" | "funding" | "liquidation" | "adjustment";

export interface ExchangeLedgerEntry {
  id: string;
  kind: ExchangeLedgerKind;
  amount: string;
  symbol?: string;
  note?: string;
  created_at: string;
}

/** `POST /v1/exchange/orders`. Give `sz` (base units) or `sz_quote` (quote currency). */
export interface ExchangePlaceOrderRequest {
  symbol: string;
  side: ExchangeSide;
  type: ExchangeOrderType;
  sz?: string;
  sz_quote?: string;
  px?: string;
  leverage?: number;
  reduce_only?: boolean;
  post_only?: boolean;
  /** Market orders: worst fill price from mark, in basis points (default 500). */
  slippage_bps?: number;
}

export interface ExchangeOrderResult {
  order: ExchangeOrder;
  fills: ExchangeFill[];
}

export interface ExchangeOrderListParams {
  status?: "open" | "filled" | "cancelled";
  limit?: number;
  cursor?: string;
}

// ── admin ───────────────────────────────────────────────────────────────────

/** A catalog entry: the admin create/update body and `EXCHANGE_CATALOG_FILE` entries. */
export interface ExchangeCatalogMarket {
  symbol: string;
  name: string;
  category?: string;
  description?: string;
  image_url?: string;
  /** The price feed's key for the asset ("" = symbol). */
  price_feed?: string;
  max_leverage: number;
  sz_decimals: number;
  tick_size: string;
  min_notional: string;
  oi_cap: string;
  /** Development only: the price used before the feed reports. */
  seed_price?: string;
  status?: ExchangeMarketStatus;
  sort?: number;
}

export interface ExchangeAdminMarket extends ExchangeCatalogMarket {
  id: string;
  /** Admin override of the index price (null: the feed's). */
  manual_price: string | null;
  created_at: string;
  updated_at: string;
  /** Live state; null while the engine is on another instance or starting. */
  stats: ExchangeMarketStats | null;
}

export interface ExchangePlatformStats {
  day_volume: string;
  day_volume_sim_pct: string;
  open_interest_value: string;
  open_interest_sim_pct: string;
  traders_24h: number;
  sim_traders_24h: number;
  backstop_balance: string;
  fees_collected: string;
  accounts: number;
}

export interface ExchangeOverview {
  running: boolean;
  markets: number;
  stats: ExchangePlatformStats | null;
  config: ExchangeConfig;
  pending_transfers: number;
}

// ── realtime ────────────────────────────────────────────────────────────────

/** `exchange.ticker` on the market's topic, every second: stats and the open bucket of every interval. */
export interface ExchangeTickerEvent {
  stats: ExchangeMarketStats;
  candles: Partial<Record<ExchangeCandleInterval, ExchangeCandle>> | null;
}

/** `exchange.trades` on the market's topic: new trades, newest first. */
export interface ExchangeTradesEvent {
  symbol: string;
  trades: ExchangeTrade[];
}

/** `exchange.tickers` on the markets topic, every 2 seconds. */
export interface ExchangeTickersEvent {
  stats: ExchangeMarketStats[];
}

/** `exchange.account` on the user's topic. */
export interface ExchangeAccountEvent {
  type: "order" | "fill" | "funding" | "liquidation" | "transfer";
  fill?: ExchangeFill;
  order?: ExchangeOrder;
  payment?: Omit<ExchangeFundingPayment, "id">;
  symbol?: string;
  sz?: string;
  px?: string;
  amount?: string;
  status?: string;
  reason?: string;
}

declare module "../types" {
  interface RealtimeEventMap {
    "exchange.book": ExchangeBook;
    "exchange.trades": ExchangeTradesEvent;
    "exchange.ticker": ExchangeTickerEvent;
    "exchange.tickers": ExchangeTickersEvent;
    "exchange.account": ExchangeAccountEvent;
    "exchange.summary": ExchangeAccount;
  }
}
