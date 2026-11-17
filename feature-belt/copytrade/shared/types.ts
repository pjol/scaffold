import type { ExchangeAccount } from "../exchange/types";

/** Copy trading DTOs (backend/internal/copytrade). Amounts are decimal strings in the exchange's quote currency. */

export type CopytradeStatus = "active" | "paused" | "stopping" | "stopped";
export type CopytradeStopReason = "" | "user" | "stop_loss" | "drawdown" | "leader_disabled" | "leader_left" | "staff" | "blocked" | "account_closed";
export type CopytradePauseReason = "" | "user" | "leader_ineligible";
export type CopytradeEventKind = "start" | "order" | "skip" | "stop" | "transfer" | "profit_share" | "reconcile" | "pause" | "resume" | "settings";
export type CopytradeSkipClass = "" | "too_small" | "risk_limit" | "rejected" | "slippage";
export type CopytradeLeaderSort = "return_90d" | "return_30d" | "copiers" | "aum";
export type CopytradeAllocationBucket = "lt_100" | "100_1k" | "1k_10k" | "gt_10k";

/** Someone as the app shows them: core display name and avatar. */
export interface CopytradePerson {
  id: string;
  display_name: string;
  avatar_url: string | null;
}

export interface CopytradeSettings {
  /** 1–10. */
  max_leverage: number;
  /** 10–50,000 per copied order. */
  max_trade_notional: string;
  /** 10–1,000,000 per market. */
  max_position_notional: string;
  /** 5–90 (% of what went in); null = none. */
  stop_loss_pct: string | null;
  /** 5–90 (% below the peak); null = none. */
  max_drawdown_pct: string | null;
  copy_open_positions: boolean;
  on_leader_exit: "close" | "keep";
  /** 10–500. */
  slippage_bps: number;
}

/** A partial settings change (absent = unchanged; null clears a stop). */
export type CopytradeSettingsPatch = Partial<CopytradeSettings>;

export interface CopytradeSkipCounts {
  too_small: number;
  risk_limit: number;
  rejected: number;
  slippage: number;
}

export interface CopytradeCopy {
  id: string;
  leader: CopytradePerson;
  status: CopytradeStatus;
  stop_reason: CopytradeStopReason;
  pause_reason: CopytradePauseReason;
  /** Net amount moved in. */
  allocation: string;
  equity: string;
  pnl: string;
  /** Of everything moved in. */
  pnl_pct: string;
  peak_equity: string;
  /** Copy equity ÷ leader equity. */
  ratio: string;
  settings: CopytradeSettings;
  /** The copy's sub-account; null once it is closed. */
  account: ExchangeAccount | null;
  profit_share_pct: string;
  profit_share_paid: string;
  skipped_30d: CopytradeSkipCounts;
  /** The leader sees the follower's name (they showed copiers when it started). */
  identity_visible: boolean;
  created_at: string;
  updated_at: string;
  stopped_at: string | null;
}

export interface CopytradeEvent {
  id: string;
  copy_id: string;
  kind: CopytradeEventKind;
  symbol: string;
  side: "" | "buy" | "sell";
  sz: string | null;
  notional: string | null;
  order_id: string | null;
  skip_class: CopytradeSkipClass;
  reason: string;
  latency_ms: number | null;
  created_at: string;
}

export interface CopytradeLeaderSettings {
  enabled: boolean;
  profit_share_pct: string;
  min_allocation: string;
  max_copiers: number;
  /** Copiers who start while this is on let you see their name. */
  show_copiers: boolean;
}

export interface CopytradeRequirement {
  key: string;
  label: string;
  met: boolean;
  progress: string;
}

export interface CopytradeLeaderStatus {
  settings: CopytradeLeaderSettings;
  eligible: boolean;
  requirements: CopytradeRequirement[];
  copiers: number;
  aum: string;
  profit_share_earned: string;
}

export interface CopytradeLeaderCard {
  user: CopytradePerson;
  accepting: boolean;
  /** Time-weighted from daily snapshots; null until there are two. */
  return_30d_pct: string | null;
  return_90d_pct: string | null;
  max_drawdown_90d_pct: string | null;
  history_days: number;
  copiers: number;
  aum: string;
  profit_share_pct: string;
  min_allocation: string;
  show_copiers: boolean;
  copying: boolean;
  is_me: boolean;
  leading_since: string;
}

export interface CopytradeCopier {
  /** Null unless they started while you showed copiers. */
  user: CopytradePerson | null;
  allocation: CopytradeAllocationBucket;
  since: string;
}

export interface CopytradeConfig {
  disclosure_version: string;
  disclosure_md: string;
  /** The kill switch: no new copies or copied orders. */
  paused: boolean;
  min_allocation: string;
  max_profit_share_pct: string;
  max_copiers_per_leader: number;
  max_copies_per_follower: number;
  settle_interval_hours: number;
  /** Defaults at the minimum allocation; max_position_notional defaults to 3 × the allocation. */
  default_settings: CopytradeSettings;
}

export interface CopytradeStartRequest {
  leader_id: string;
  allocation: string;
  settings?: CopytradeSettingsPatch;
  disclosure_version: string;
}

export interface CopytradeOverview {
  paused: boolean;
  running: boolean;
  active: number;
  paused_copies: number;
  stopping: number;
  stopped: number;
  leaders: number;
  followers: number;
  aum: string;
  profit_share_paid: string;
  orders_24h: number;
  skips_24h: number;
  fills_dropped: number;
}

export interface CopytradeAdminCopy {
  id: string;
  follower: CopytradePerson;
  leader: CopytradePerson;
  status: CopytradeStatus;
  stop_reason: CopytradeStopReason;
  allocation: string;
  equity: string;
  created_at: string;
  stopped_at: string | null;
}

declare module "../types" {
  interface RealtimeEventMap {
    "copytrade.copy": CopytradeCopy;
    "copytrade.event": CopytradeEvent;
  }
}
