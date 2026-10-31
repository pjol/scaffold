import { REPORT_REASONS, type CaseAction, type MyReport, type ReportReason } from "@scaffoldapp/shared";
import type { BadgeTone } from "@scaffoldapp/ui-web";

const KIND_LABEL: Record<string, string> = {
  user: "Person",
  "chat.message": "Message",
  "feed.post": "Post",
  "feed.comment": "Comment",
  "assistant.message": "AI reply",
};

/** "feed.post" → "Post"; unknown kinds use their last part. */
export function kindLabel(kind: string): string {
  const known = KIND_LABEL[kind];
  if (known) return known;
  const last = kind.split(".").pop() ?? kind;
  return last.charAt(0).toUpperCase() + last.slice(1).replace(/_/g, " ");
}

export function reasonLabel(reason: ReportReason): string {
  return REPORT_REASONS.find((r) => r.value === reason)?.label ?? reason;
}

export const OUTCOME: Record<MyReport["outcome"], { label: string; tone: BadgeTone }> = {
  open: { label: "In review", tone: "info" },
  actioned: { label: "Action taken", tone: "success" },
  closed: { label: "Closed", tone: "neutral" },
};

export const ACTION_LABEL: Record<CaseAction, string> = {
  dismiss: "Dismissed",
  remove: "Removed",
  warn: "Warned",
  suspend: "Suspended",
  ban: "Banned",
  resolve: "Resolved",
};

/** "45 min", "3 h", "2 d". */
export function formatAge(seconds: number): string {
  if (seconds < 3600) return `${Math.max(1, Math.round(seconds / 60))} min`;
  if (seconds < 86_400) return `${Math.round(seconds / 3600)} h`;
  return `${Math.round(seconds / 86_400)} d`;
}
