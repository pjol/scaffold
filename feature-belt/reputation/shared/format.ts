import type { ReputationEquityPoint, ReputationFlagStatus, ReputationStatus, ReputationTier } from "./types";

/** Tier names people read. A tier key the app added falls back to the key. */
export const REPUTATION_TIER_LABEL: Record<ReputationTier, string> = {
  building: "Building",
  steady: "Steady",
  strong: "Strong",
  elite: "Elite",
};

export function reputationTierLabel(tier: string | null | undefined): string {
  if (!tier) return "";
  return REPUTATION_TIER_LABEL[tier as ReputationTier] ?? tier.charAt(0).toUpperCase() + tier.slice(1);
}

/** The ui packages' badge tone for a tier. */
export function reputationTierTone(tier: string | null | undefined): "neutral" | "info" | "success" | "primary" {
  switch (tier) {
    case "elite":
      return "primary";
    case "strong":
      return "success";
    case "steady":
      return "info";
    default:
      return "neutral";
  }
}

export const REPUTATION_STATUS_LABEL: Record<ReputationStatus, string> = {
  rated: "Rated",
  unrated: "Not rated yet",
  under_review: "Under review",
};

export const REPUTATION_STATUS_INFO: Record<ReputationStatus, string> = {
  rated: "Every requirement is met and no integrity check is open.",
  unrated: "A score shows once every requirement is met.",
  under_review: "An integrity check flagged this account. The score is hidden until staff decide.",
};

export const REPUTATION_FLAG_STATUS_LABEL: Record<ReputationFlagStatus, string> = {
  open: "Open",
  cleared: "Cleared",
  confirmed: "Confirmed",
};

/** Flag kinds people read; a module's own kind falls back to its words. */
export const REPUTATION_FLAG_KIND_LABEL: Record<string, string> = {
  wash_pair: "Wash trading",
  vote_ring: "Vote ring",
  copy_front_run: "Copy front-running",
};

export const REPUTATION_FLAG_KIND_INFO: Record<string, string> = {
  wash_pair: "Two people traded with each other a lot (30% or more of either one's volume, 10 trades or more in 30 days), or one's gain mirrors the other's loss.",
  vote_ring: "Half or more of 20+ upvotes in 30 days went to one author.",
};

export function reputationFlagKindLabel(kind: string): string {
  const k = REPUTATION_FLAG_KIND_LABEL[kind];
  if (k) return k;
  const words = kind.replace(/_/g, " ");
  return words.charAt(0).toUpperCase() + words.slice(1);
}

/** Labels for components' raw inputs and flags' metrics; unknown keys fall back to their words. */
export const REPUTATION_METRIC_LABEL: Record<string, string> = {
  sharpe_30d: "Sharpe-like, 30 days",
  sharpe_90d: "Sharpe-like, 90 days",
  shrunk_30d: "Shrunk, 30 days",
  shrunk_90d: "Shrunk, 90 days",
  active_days_30d: "Active days, 30 days",
  active_days_90d: "Active days, 90 days",
  max_drawdown_90d_pct: "Max drawdown, 90 days (%)",
  incidents_90d: "Incidents, 90 days",
  active_weeks_13w: "Active weeks of 13",
  positive_weeks_13w: "Positive weeks of 13",
  karma_90d: "Karma, 90 days",
  strikes_90d: "Strikes, 90 days",
  age_days: "Days since first activity",
  trades_30d: "Trades together, 30 days",
  pair_volume: "Volume together",
  share_of_user_pct: "Share of their volume (%)",
  share_of_counterparty_pct: "Share of the other's volume (%)",
  gain_user: "Their gain",
  gain_counterparty: "The other's gain",
  mirrored: "Gains mirror",
  upvotes_30d: "Upvotes given, 30 days",
  to_counterparty: "To one author",
  share_pct: "Share to one author (%)",
};

export function reputationMetricLabel(key: string): string {
  const k = REPUTATION_METRIC_LABEL[key];
  if (k) return k;
  const words = key.replace(/_/g, " ");
  return words.charAt(0).toUpperCase() + words.slice(1);
}

/** "0.50" → "50%". */
export function formatReputationWeight(weight: string): string {
  return `${Math.round(Number(weight) * 100)}%`;
}

/** A signed percentage: "7.69" → "+7.69%", "-90.00" → "−90.00%". */
export function formatReputationPct(pct: string, signed = true): string {
  const n = Number(pct);
  if (!Number.isFinite(n)) return pct;
  const body = `${Math.abs(n).toFixed(2)}%`;
  if (!signed || n === 0) return n < 0 ? `−${body}` : body;
  return n > 0 ? `+${body}` : `−${body}`;
}

/** A decimal string with thousands separators: "12345.6" → "12,345.60". */
export function formatReputationAmount(amount: string, decimals = 2): string {
  const n = Number(amount);
  if (!Number.isFinite(n)) return amount;
  const s = Math.abs(n).toLocaleString("en-US", { minimumFractionDigits: decimals, maximumFractionDigits: decimals });
  return n < 0 ? `−${s}` : s;
}

/** The equity index scaled into a w × h box (pad inside), with its range. */
export function reputationEquityScale(points: readonly ReputationEquityPoint[], w: number, h: number, pad = 4) {
  const vals = points.map((p) => Number(p.index));
  const min = Math.min(100, ...vals);
  const max = Math.max(100, ...vals);
  const span = max - min || 1;
  const step = vals.length > 1 ? (w - 2 * pad) / (vals.length - 1) : 0;
  const y = (v: number) => pad + (h - 2 * pad) * (1 - (v - min) / span);
  const xy = vals.map((v, i) => [pad + i * step, y(v)] as const);
  return { xy, min, max, baseline: y(100) };
}

/** SVG path through the points. */
export function reputationLinePath(xy: readonly (readonly [number, number])[]): string {
  return xy.map(([x, y], i) => `${i ? "L" : "M"}${x.toFixed(1)} ${y.toFixed(1)}`).join(" ");
}
