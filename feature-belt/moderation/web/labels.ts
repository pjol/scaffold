import { MODERATION_CATEGORY_LABELS, REPORT_REASONS, type CaseAction, type ModerationContentItem, type ModerationDecisionAction, type MyReport, type ReportReason } from "@scaffoldapp/shared";
import type { BadgeTone } from "@scaffoldapp/ui-web";

const KIND_LABEL: Record<string, string> = {
  user: "Person",
  "chat.message": "Message",
  "feed.post": "Post",
  "feed.comment": "Comment",
  "forum.post": "Forum post",
  "forum.comment": "Forum comment",
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

/** "spam" → "Spam"; unknown categories read as words. */
export function categoryLabel(category: string): string {
  return MODERATION_CATEGORY_LABELS[category] ?? category.replace(/_/g, " ");
}

export const CONTENT_STATE: Record<ModerationContentItem["state"], { label: string; tone: BadgeTone }> = {
  in_review: { label: "In review", tone: "warning" },
  removed: { label: "Removed", tone: "danger" },
  approved: { label: "Approved", tone: "success" },
};

export const DECISION_TONE: Record<ModerationDecisionAction, BadgeTone> = {
  allow: "neutral",
  label: "info",
  hold: "warning",
  remove: "danger",
};

export const DECISION_LABEL: Record<ModerationDecisionAction, string> = {
  allow: "Allowed",
  label: "Labelled",
  hold: "Held",
  remove: "Removed",
};

/** Screening question names in words. */
export const QUESTION_LABEL: Record<string, string> = {
  spam: "Spam",
  scam: "Scam",
  harassment: "Harassment",
  manipulation: "Coordinated manipulation",
  on_topic: "On topic",
  addresses_moderator: "Addresses moderators",
};

/** Rule ids in words. */
export const RULE_LABEL: Record<string, string> = {
  blocked_term: "Blocked term",
  unsafe_link: "Unsafe or look-alike link",
  near_brand_link: "Link spelled like this app's",
  link_shortener: "Link shortener",
  new_account_links: "Links from a new account",
  many_links: "Many links",
  credential_ask: "Asks for credentials",
  wallet_address: "Wallet address",
  off_platform_contact: "Off-platform contact",
  repeat_self: "Repeated by the author",
  repeat_accounts: "Posted by several accounts",
  mention_flood: "Mentions many people",
  staff_impersonation: "Claims to be staff",
  classifier: "Content classifier",
};

export function ruleLabel(rule: string): string {
  if (rule.startsWith("model:")) return `Model: ${QUESTION_LABEL[rule.slice(6)] ?? rule.slice(6)}`;
  return RULE_LABEL[rule] ?? rule.replace(/_/g, " ");
}
