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

/** One reported target in the staff queue. */
export interface ModerationCase {
  id: string;
  target_kind: string;
  target_id: string;
  status: CaseStatus;
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
  author_history: { cases: number; actioned: number };
}

/** `GET /v1/admin/moderation/summary`. */
export interface ModerationSummary {
  open: number;
  over_sla: number;
  oldest_open_age_seconds: number | null;
  closed_7d: number;
  median_close_seconds_7d: number | null;
  sla_seconds: number;
}

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
