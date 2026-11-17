/**
 * Oracle DTOs, mirroring backend/internal/oracle (api.go, admin.go, history.go,
 * realtime.go). Prices are decimal strings in the feed's quote currency; times are
 * RFC 3339.
 */

export type OracleStatus = "live" | "stale" | "seed" | "off";
export type OracleFeedKind = "single" | "basket";
export type OracleVerdict = "accepted" | "downweighted" | "rejected";
export type OraclePrintKind = "sale" | "market" | "listing";
export type OracleProfile = "daily" | "intraday" | "realtime";

/** One feed with its published price (`GET /v1/oracle/feeds`). */
export interface OracleFeed {
  id: string;
  key: string;
  name: string;
  category: string;
  quote: string;
  decimals: number;
  kind: OracleFeedKind;
  listed: boolean;
  enabled: boolean;
  /** Null until a source sets one (and no seed). */
  price: string | null;
  /** Where the slew-limited price is heading. */
  target: string | null;
  status: OracleStatus;
  updated_at: string | null;
  /** Venue names (singles). */
  venues: string[];
  /** Number of parts (baskets). */
  part_count: number;
}

/** Aggregation knobs, in seconds. */
export interface OracleParams {
  half_life_s: number;
  stale_after_s: number;
  max_move_bps_per_min: number;
  lookback_s: number;
  ref_window_s: number;
  burst_window_s: number;
}

/** One venue of a single, with the health of its last fetch. */
export interface OracleFeedSource {
  venue: string;
  source: string;
  last_price: string | null;
  last_at: string | null;
  ok: boolean;
  note?: string;
  /** Premium removed from this venue's prices (0.03 = 3% above the others). */
  premium: number;
  premium_learned: boolean;
  weight: number;
}

/** One part of a basket. */
export interface OracleFeedPart {
  feed_id: string;
  key: string;
  name: string;
  quantity: string;
  price: string | null;
  status: OracleStatus;
}

export interface OraclePricePoint {
  at: string;
  price: string;
}

/** One screened print. */
export interface OracleObservation {
  id: string;
  feed_id: string;
  feed_key: string;
  source: string;
  venue: string;
  kind: OraclePrintKind;
  /** Normalised by the venue premium. */
  price: string;
  /** As the venue reported it. */
  raw_price: string;
  premium: number;
  qty: number | null;
  observed_at: string;
  received_at: string;
  verdict: OracleVerdict;
  /** Manipulation score, 0..1. */
  score: number;
  reasons: string[];
  /** `stat-v1`, or `stat-v1+<model>` when the decision model answered. */
  model: string;
  /** Effective weight in the target when received. */
  weight: number;
}

/** `GET /v1/oracle/feeds/{key}`. */
export interface OracleFeedDetail {
  feed: OracleFeed;
  seed_price: string | null;
  params: OracleParams;
  last_accepted_at: string | null;
  sources: OracleFeedSource[];
  parts: OracleFeedPart[];
  /** Published prices over the last 24 hours (every 30 s on change, at least every 5 min). */
  recent: OraclePricePoint[];
  /** Latest 50, newest first (a basket's include its parts'). */
  observations: OracleObservation[];
}

/** One day of a feed's chart. */
export interface OracleDailyBar {
  /** YYYY-MM-DD (UTC). */
  day: string;
  open: string;
  high: string;
  low: string;
  close: string;
  /** `oracle` for days the oracle published, else where the backfill came from. */
  source: string;
}

/** A published price (realtime `oracle.price`, items of `oracle.prices`). */
export interface OraclePrice {
  feed_id: string;
  key: string;
  price: string | null;
  target: string | null;
  status: OracleStatus;
  updated_at: string;
}

export interface OracleFeedListParams {
  q?: string;
  kind?: OracleFeedKind;
  limit?: number;
  cursor?: string;
}

export interface OracleObservationListParams {
  /** Feed key; a basket includes its parts. */
  feed?: string;
  verdict?: OracleVerdict;
  limit?: number;
  cursor?: string;
}

// ---- staff ----

/** A source row as staff edit it. */
export interface OracleSourceConfig {
  id: string;
  source: string;
  venue: string;
  ref: string;
  params: Record<string, unknown>;
  weight: number | null;
  premium: number | null;
}

export interface OraclePartConfig {
  key: string;
  quantity: string;
}

/** `GET /v1/admin/oracle/feeds/{key}`. */
export interface OracleAdminFeed extends OracleFeedDetail {
  source_config: OracleSourceConfig[];
  part_config: OraclePartConfig[];
}

export interface OracleLaneHealth {
  lane: string;
  ok: boolean;
  note?: string;
  last_ok_at: string | null;
}

export interface OracleSourceHealth {
  name: string;
  kind: "poll" | "push";
  enabled: boolean;
  ok: boolean;
  note?: string;
  feeds: number;
  rows: number;
  failing: number;
  last_ok_at: string | null;
  last_error_at: string | null;
  last_error?: string;
  min_gap_ms: number;
  cadence_s: number;
  quota: number;
  per_s: number;
  lanes: OracleLaneHealth[];
}

export interface OracleDecisionStatus {
  enabled: boolean;
  name?: string;
  note?: string;
  calls: number;
  failures: number;
  cost_usd: number;
  queued: number;
}

/** `GET /v1/admin/oracle/sources`. */
export interface OracleSourcesStatus {
  items: OracleSourceHealth[];
  decision: OracleDecisionStatus;
  leader: { instance?: string; until: string | null; this: boolean };
  polling: boolean;
}

export interface OracleSourceInput {
  source: string;
  venue?: string;
  ref?: string;
  params?: Record<string, unknown>;
  weight?: number | null;
  premium?: number | null;
}

export interface OracleCreateFeedRequest {
  key: string;
  name: string;
  category?: string;
  quote?: string;
  decimals?: number;
  kind?: OracleFeedKind;
  seed_price?: string;
  profile?: OracleProfile;
  params?: OracleParams;
  listed?: boolean;
  sources?: OracleSourceInput[];
  parts?: OraclePartConfig[];
}

export interface OracleUpdateFeedRequest {
  name?: string;
  category?: string;
  quote?: string;
  decimals?: number;
  /** "" clears it. */
  seed_price?: string;
  params?: OracleParams;
  listed?: boolean;
  enabled?: boolean;
}

export interface OraclePostPrintRequest {
  price: string;
  kind?: OraclePrintKind;
  qty?: number;
  observed_at?: string;
  /** Which manual venue, when the feed has more than one. */
  venue?: string;
  note?: string;
}

export interface OracleImportHistoryRequest {
  points: { day: string; close: string; low?: string; high?: string }[];
}
