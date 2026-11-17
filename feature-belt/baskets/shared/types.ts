/**
 * Baskets DTOs, mirroring backend/internal/baskets. Values, prices and weights are
 * decimal strings in the exchange's quote currency (format them; never do money math
 * on floats). Times are ISO 8601; dates are YYYY-MM-DD.
 */

/** A basket's lifecycle; its market follows (archived = halted). */
export type BasketsStatus = "active" | "reduce_only" | "archived";
export type BasketsLineStatus = "resolved" | "unresolved" | "unpriced";
/** How a line resolved: by symbol, name, alias, within 2 edits of a name (warned), or picked from search. */
export type BasketsMatch = "symbol" | "name" | "alias" | "fuzzy" | "picked";
/** Why a line didn't resolve. */
export type BasketsReason = "unknown" | "ambiguous" | "not_listed" | "basket" | "halted";
/** The oracle's status of a price: live, stale (stopped updating), seed (not live yet), off. */
export type BasketsPriceStatus = "live" | "stale" | "seed" | "off" | "";
export type BasketsSort = "new" | "popular";
export type BasketsListStatus = "live" | "archived" | "all";
export type BasketsReportReason = "name" | "description" | "spam" | "other";
export type BasketsAdminFilter = "all" | "reported" | "hidden";

/** A listed market a line may name. */
export interface BasketsCandidate {
  symbol: string;
  name: string;
  category: string;
  image_url: string;
}

export interface BasketsPick {
  symbol: string;
  quantity: number;
}

/** A basket as people send it: a pasted list, picked markets, or both. */
export interface BasketsInput {
  /** One market per line ("2 Gold", "Silver x3", "BTC-USD, 1") or CSV with a header. */
  text?: string;
  picks?: BasketsPick[];
}

/** One input line after resolution and pricing. */
export interface BasketsPreviewLine {
  /** Paste line or CSV row; 0 for picks. */
  line: number;
  text: string;
  quantity: number;
  status: BasketsLineStatus;
  match?: BasketsMatch;
  market: BasketsCandidate | null;
  price: string | null;
  price_status?: BasketsPriceStatus;
  reason?: BasketsReason;
  /** Why it didn't resolve or price, in words. */
  message?: string;
  /** Up to 5 suggestions when unresolved. */
  candidates: BasketsCandidate[];
}

/** One canonical line (merged by market) with its price and weight. */
export interface BasketsLine {
  symbol: string;
  name: string;
  quantity: number;
  price: string | null;
  value: string | null;
  /** Share of the basket value, "0"–"1". */
  weight: string | null;
  price_status: BasketsPriceStatus;
}

/** The exchange market a basket gets. */
export interface BasketsSpec {
  tick_size: string;
  sz_decimals: number;
  max_leverage: number;
  min_notional: string;
  oi_cap: string;
}

/** The basket that already has these exact lines. */
export interface BasketsExisting {
  symbol: string;
  name: string;
  status: BasketsStatus;
  hidden: boolean;
  entries: number;
  /** The caller registered it already. */
  entered: boolean;
}

/** `POST /v1/baskets/preview`: what create would do, without writing. */
export interface BasketsPreview {
  lines: BasketsPreviewLine[];
  warnings: string[];
  basket: BasketsLine[];
  /** Σ quantity × price over the priced lines. */
  value: string;
  /** The largest line's share of the value, "0"–"1". */
  max_share: string;
  /** Why it can't be created, in words. */
  blockers: string[];
  /** Create (or join `existing`) would succeed, rate limits aside. */
  creatable: boolean;
  symbol: string | null;
  spec: BasketsSpec | null;
  existing: BasketsExisting | null;
  hash?: string;
}

/** A basket in lists. */
export interface BasketsSummary {
  id: string;
  symbol: string;
  name: string;
  description: string;
  status: BasketsStatus;
  hidden: boolean;
  /** Enough open reports to leave discovery until staff review it. */
  flagged: boolean;
  /** Display name, or "Deleted user". */
  creator: string;
  /** The caller created it. */
  is_mine: boolean;
  entries: number;
  line_count: number;
  /** Up to 3 market names, largest share at creation first. */
  top_lines: string[];
  value_at_create: string;
  /** People's traded notional over the idle window (refreshed hourly). */
  volume_recent: string;
  open_interest: string;
  holders: number;
  ends_on: string | null;
  created_at: string;
}

export interface BasketsMarketInfo extends BasketsSpec {
  status: "active" | "reduce_only" | "halted";
}

/** `GET /v1/baskets/{symbol}`. */
export interface BasketsDetail extends BasketsSummary {
  /** With current prices and weights. */
  lines: BasketsLine[];
  value: string;
  /** The basket feed's published price. */
  price: string | null;
  price_status: BasketsPriceStatus;
  market: BasketsMarketInfo | null;
  entered: boolean;
  reported: boolean;
}

export interface BasketsCreateRequest extends BasketsInput {
  /** 3–48 characters; generated from the largest lines when empty. */
  name?: string;
  description?: string;
  /** Optional end date (YYYY-MM-DD): winds down after it. */
  ends_on?: string | null;
}

export interface BasketsCreateResult {
  basket: BasketsDetail;
  /** A new market was listed. */
  created: boolean;
  /** An archived basket with these lines reopened. */
  revived: boolean;
  /** The caller's entry was recorded now. */
  joined: boolean;
  warnings: string[];
}

export interface BasketsReportRequest {
  reason: BasketsReportReason;
  note?: string;
}

/** A basket in Admin → Baskets. */
export interface BasketsAdminBasket extends BasketsSummary {
  open_reports: number;
  source: string;
  report_reasons: BasketsReportReason[];
}

export interface BasketsListParams {
  sort?: BasketsSort;
  status?: BasketsListStatus;
  q?: string;
  limit?: number;
  cursor?: string;
}

export interface BasketsAdminListParams {
  filter?: BasketsAdminFilter;
  status?: BasketsListStatus;
  q?: string;
  limit?: number;
  cursor?: string;
}
