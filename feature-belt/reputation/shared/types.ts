/** Reputation DTOs, mirroring backend/internal/reputation. Money-like amounts are decimal strings. */

export type ReputationStatus = "rated" | "unrated" | "under_review";
/** Tier keys of the default model (`reputation.DefaultModel.Tiers`); apps that add tiers extend this. */
export type ReputationTier = "building" | "steady" | "strong" | "elite";
export type ReputationComponentKey = "performance" | "risk" | "consistency" | "community" | "tenure";

/** What anyone signed in sees of a person. */
export interface ReputationSummary {
  user_id: string;
  /** Null unless rated. */
  score: number | null;
  status: ReputationStatus;
  tier: ReputationTier | null;
}

/** One part of the score. */
export interface ReputationComponent {
  key: ReputationComponentKey | (string & {});
  label: string;
  /** Effective weight after unscored parts gave theirs away: "0.50". */
  weight: string;
  /** 0–100, one decimal. */
  value: number;
  /** value × weight, one decimal; the score is the rounded sum. */
  points: number;
  scored: boolean;
  /** Plain-language explanation of the value. */
  detail: string;
  /** Raw inputs by key (`sharpe_90d`, `max_drawdown_90d_pct`…): see REPUTATION_METRIC_LABEL. */
  metrics: Record<string, string>;
}

export interface ReputationRequirement {
  key: "account_age" | "active_days" | "activity" | "volume" | (string & {});
  label: string;
  met: boolean;
  /** "9/14". */
  progress: string;
}

/** One day of the time-weighted index: 100 at the start of the window. */
export interface ReputationEquityPoint {
  /** YYYY-MM-DD (UTC). */
  day: string;
  index: string;
}

/** Time-weighted: money put in or taken out never counts as a gain or loss. */
export interface ReputationStats {
  return_30d_pct: string;
  return_90d_pct: string;
  /** Value change minus flows over 30 days, in the app's units. */
  gain_30d: string;
  max_drawdown_90d_pct: string;
  volume_30d: string;
  activity_30d: number;
  active_days_90d: number;
  incidents_90d: number;
  equity: ReputationEquityPoint[];
}

/** A person's full reputation: only for that person and staff. */
export interface Reputation extends ReputationSummary {
  /** Scoring model ("rep-v1"). */
  version: string;
  computed_at: string | null;
  components: ReputationComponent[];
  requirements: ReputationRequirement[];
  stats: ReputationStats;
}

export type ReputationFlagStatus = "open" | "cleared" | "confirmed";
export type ReputationDecision = "clear" | "confirm";

/** A person as the flag queue shows them. */
export interface ReputationPerson {
  id: string;
  display_name: string;
  email: string;
  avatar_url: string | null;
  status: string;
}

/** An integrity flag in Admin → Reputation. */
export interface ReputationFlag {
  id: string;
  user: ReputationPerson;
  /** `wash_pair`, `vote_ring`, or a module's own (`copy_front_run`). */
  kind: string;
  counterparty: ReputationPerson | null;
  metrics: Record<string, string>;
  status: ReputationFlagStatus;
  note: string;
  created_at: string;
  decided_at: string | null;
  decided_by_name: string | null;
}

export interface ReputationDecideRequest {
  decision: ReputationDecision;
  note?: string;
}
