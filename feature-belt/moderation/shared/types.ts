import type { UserStatus } from "../types";

/** Why something was reported (`moderation.Reasons` in Go, same order). */
export type ReportReason = "spam" | "harassment" | "hate" | "sexual" | "violence" | "self_harm" | "illegal" | "other";

/** Reasons in display order with their labels. */
export const REPORT_REASONS: readonly { value: ReportReason; label: string }[] = [
  { value: "spam", label: "Spam or scam" },
  { value: "harassment", label: "Harassment or bullying" },
  { value: "hate", label: "Hate speech" },
  { value: "sexual", label: "Sexual content" },
  { value: "violence", label: "Violence or threats" },
  { value: "self_harm", label: "Self-harm or suicide" },
  { value: "illegal", label: "Illegal activity" },
  { value: "other", label: "Something else" },
];

/** `target_kind` for reporting a person; content kinds are `<feature>.<thing>` ("chat.message"). */
export const REPORT_TARGET_USER = "user";

/** `POST /v1/moderation/reports`. */
export interface CreateReportRequest {
  target_kind: string;
  target_id: string;
  reason: ReportReason;
  note?: string;
}

/** A report as its reporter sees it. `outcome` never says what staff did. */
export interface MyReport {
  id: string;
  target_kind: string;
  target_id: string;
  reason: ReportReason;
  note: string;
  outcome: "open" | "actioned" | "closed";
  created_at: string;
}

/** Someone the caller blocked (`GET /v1/moderation/blocks`). */
export interface BlockedUser {
  user_id: string;
  display_name: string;
  avatar_url: string | null;
  created_at: string;
}

/** `GET /v1/moderation/blocks/ids`: everyone the caller blocked. */
export interface BlockedIds {
  user_ids: string[];
}

/** Reported content as captured at report time. */
export interface ModerationSnapshot {
  author_id: string | null;
  text: string;
  media_keys: string[];
  /** App path to the content. */
  link: string;
}

export type CaseStatus = "open" | "resolved" | "dismissed";
export type CaseStatusFilter = CaseStatus | "closed";
export type CaseAction = "dismiss" | "remove" | "warn" | "suspend" | "ban" | "resolve";

/** A person on a case, read live. */
export interface CaseUser {
  id: string;
  display_name: string;
  email: string;
  status: UserStatus;
  avatar_url: string | null;
}

/** Where a case came from: people's reports, or screening that held or removed the content. */
export type ModerationCaseSource = "report" | "auto";

/** One reported or screened target in the staff queue. */
export interface ModerationCase {
  id: string;
  target_kind: string;
  target_id: string;
  status: CaseStatus;
  source: ModerationCaseSource;
  /** Screening categories (spam, scam, harassment, manipulation, off_topic, injection, other). */
  categories: string[];
  report_count: number;
  reasons: Partial<Record<ReportReason, number>>;
  snapshot: ModerationSnapshot;
  media_urls: string[];
  author: CaseUser | null;
  hidden: boolean;
  auto_hidden: boolean;
  action: CaseAction | null;
  resolution_note: string;
  resolved_by: string | null;
  resolved_at: string | null;
  /** Open: time waiting. Closed: time to close. */
  age_seconds: number;
  over_sla: boolean;
  last_reported_at: string;
  created_at: string;
}

export interface CaseReport {
  id: string;
  reporter: CaseUser | null;
  reason: ReportReason;
  note: string;
  snapshot: ModerationSnapshot;
  media_urls: string[];
  created_at: string;
}

export interface CaseDetail extends ModerationCase {
  reports: CaseReport[];
  /** Screening decisions on the content, newest first (empty for people). */
  decisions: ModerationDecision[];
  author_history: { cases: number; actioned: number; strikes: number };
}

/** `GET /v1/admin/moderation/summary`. */
export interface ModerationSummary {
  open: number;
  over_sla: number;
  oldest_open_age_seconds: number | null;
  closed_7d: number;
  median_close_seconds_7d: number | null;
  sla_seconds: number;
  /** Open cases screening opened. */
  held_open: number;
  decision_model: ModerationDecisionModelHealth;
}

// ---- screening ----

/** Screening actions, least to most severe: the final one is the more severe of the rules' and the decision model's. */
export type ModerationDecisionAction = "allow" | "label" | "hold" | "remove";

/** How an item is stored after screening. */
export type ModerationContentStatus = "visible" | "held" | "removed";

/** What screening told the author after posting (UGC features return it with the item). */
export interface ModerationOutcome {
  action: ModerationDecisionAction;
  status: ModerationContentStatus;
  labels: string[];
  /** User-safe reasons for a label, hold or removal. */
  reasons: string[];
}

/** One rule (or model answer, `model:<question>`) that fired. */
export interface ModerationHit {
  rule: string;
  action: ModerationDecisionAction;
  category?: string;
  /** For staff: the matched domain, count or probability. */
  detail?: string;
  reason?: string;
}

/** Decision model status recorded with a decision. */
export type ModerationModelStatus = "ok" | "off" | "skipped" | "unavailable" | "rate_limited" | "timeout" | "cap_reached" | "error";

/** A recorded screening decision (admin case page). */
export interface ModerationDecision {
  id: string;
  rules_version: string;
  action: ModerationDecisionAction;
  rules_action: ModerationDecisionAction;
  labels: string[];
  categories: string[];
  hits: ModerationHit[];
  model: string | null;
  model_status: ModerationModelStatus;
  /** P(yes) per question: spam, scam, harassment, manipulation, on_topic, addresses_moderator. */
  answers: Record<string, number> | null;
  distrusted: boolean;
  latency_ms: number | null;
  /** Decimal dollars. */
  cost_usd: string | null;
  edit: boolean;
  created_at: string;
}

/** The decision model's state (Admin → Moderation). */
export interface ModerationDecisionModelHealth {
  configured: boolean;
  model: string;
  status: "ok" | "off" | "unavailable" | "rate_limited" | "cap_reached";
  note: string;
  calls: number;
  failures: number;
  /** Decimal dollars since the server started. */
  cost_usd: string;
  day_cost_usd: string;
  /** "0" = no cap. */
  daily_cap_usd: string;
  paused_until: string | null;
}

/** Something the caller posted that screening held or staff removed (`GET /v1/moderation/content`). */
export interface ModerationContentItem {
  case_id: string;
  target_kind: string;
  target_id: string;
  text: string;
  link: string;
  state: "in_review" | "removed" | "approved";
  reasons: string[];
  created_at: string;
  decided_at: string | null;
}

/** One strike against the caller. */
export interface ModerationStrike {
  id: string;
  action: "remove" | "ban";
  category: string;
  created_at: string;
  expires_at: string;
}

/** `GET /v1/moderation/standing`. */
export interface ModerationStanding {
  /** Strikes that count now, newest first. */
  strikes: ModerationStrike[];
  /** When the caller may post again; null when they may post now. */
  restricted_until: string | null;
  ladder: { strikes: number; hours: number }[];
  window_days: number;
}

/** Labels for screening categories. */
export const MODERATION_CATEGORY_LABELS: Record<string, string> = {
  spam: "Spam",
  scam: "Scam",
  harassment: "Harassment",
  manipulation: "Manipulation",
  off_topic: "Off-topic",
  injection: "Addressed to moderators",
  other: "Flagged",
};

/** `POST /v1/admin/moderation/cases/{id}/actions`. */
export interface CaseActionRequest {
  action: CaseAction;
  note?: string;
  /** With warn, suspend or ban on content: also take the content down. */
  remove_content?: boolean;
  /** Default true. */
  notify_reporters?: boolean;
}

export interface ModerationCaseListParams {
  status?: CaseStatusFilter;
  kind?: string;
  author_id?: string;
  limit?: number;
  cursor?: string;
}

declare module "../types" {
  interface RealtimeEventMap {
    "moderation.blocks_changed": { user_id: string; blocked: boolean };
    "moderation.case_opened": { case_id: string; target_kind: string; reason: ReportReason };
    "moderation.case_closed": { case_id: string; action: CaseAction };
  }
}
