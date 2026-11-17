/** DTOs for the identity feature (backend/internal/identity). snake_case, same shapes as the Go structs. */
import type { PageParams } from "../api/params";

export type IdentityState = "none" | "pending" | "verified" | "rejected" | "expired";
export type IdentityProvider = "persona" | "manual";

/** `GET /v1/identity/status`. */
export interface IdentityStatus {
  status: IdentityState;
  /** The current attempt's provider, or what a new start uses. */
  provider: IdentityProvider;
  /** Pending: the user finished their part; awaiting a decision. */
  submitted: boolean;
  /** Rejected: why, written for the user. */
  reason: string;
  verified_at: string | null;
  /** Verified with an expiry (`IDENTITY_VERIFICATION_TTL`). */
  expires_at: string | null;
  /** Starts left before the user must contact support. */
  attempts_left: number;
  updated_at: string | null;
}

/** A provider flow to open (Persona hosted URL or embedded session). */
export interface IdentityFlow {
  provider: IdentityProvider;
  inquiry_id: string;
  session_token: string;
  hosted_url: string;
  environment_id: string;
}

/** `POST /v1/identity/start`. `flow` is null in manual mode and when nothing is left to do. */
export interface IdentityStartResult {
  status: IdentityStatus;
  flow: IdentityFlow | null;
}

/** `GET /v1/identity/users/{id}/badge` (public). Unknown users read as not verified with no badge. */
export interface IdentityBadge {
  user_id: string;
  /** ID verification. */
  verified: boolean;
  /** The highest verification badge, null when none. */
  badge: IdentityHeldBadge | null;
}

// ---- verification badges -------------------------------------------------------

/** Badge kinds, highest first: official > org > notable > paid. */
export type IdentityBadgeKind = "official" | "org" | "notable" | "paid";

/** A badge someone holds, as anyone may see it (`identity.BadgeOf` / `BadgeSet` on the server). */
export interface IdentityHeldBadge {
  kind: IdentityBadgeKind;
  /** What the badge says ("Riverside Bikes"); the kind's label when none. */
  label: string;
  /** The kind's name (`IDENTITY_BADGE_LABELS`). */
  kind_label: string;
  /** Start of the current unbroken hold. */
  since: string;
}

/** A badge as its holder sees it. */
export interface IdentityOwnBadge extends IdentityHeldBadge {
  category: string | null;
  source: "review" | "admin" | "billing";
  /** Renews or ends then (paid), or is reviewed again (org, notable). */
  expires_at: string | null;
}

export interface IdentityPaidRequirement {
  key: "account_age" | "good_standing" | "identity_verified" | "not_removed";
  label: string;
  met: boolean;
  detail: string;
}

/** The paid badge for its (would-be) holder. */
export interface IdentityPaidStatus {
  /** The payments entitlement that grants it (`IDENTITY_PAID_ENTITLEMENT`). */
  entitlement: string;
  active: boolean;
  expires_at: string | null;
  /** Staff removed it; only staff can bring it back. */
  held: boolean;
  eligible: boolean;
  requirements: IdentityPaidRequirement[];
}

export type IdentityEvidenceKind = "url" | "domain" | "account" | "text";

/** One item of evidence; `verified` is computed by the server (domain proofs, linked accounts). */
export interface IdentityEvidence {
  kind: IdentityEvidenceKind;
  ref: string;
  verified: boolean;
}

export type IdentityApplicationStatus = "submitted" | "needs_info" | "approved" | "rejected" | "withdrawn";

/** A badge application as its applicant sees it. */
export interface IdentityBadgeApplication {
  id: string;
  category: string;
  display_label: string;
  status: IdentityApplicationStatus;
  evidence: IdentityEvidence[];
  note: string;
  /** Staff's note (needs_info, rejected, sometimes approved). */
  reviewer_note: string;
  submitted_at: string;
  decided_at: string | null;
  created_at: string;
}

/** A domain proof with the TXT record to add. */
export interface IdentityDomain {
  domain: string;
  txt_name: string;
  txt_value: string;
  verified: boolean;
  verified_at: string | null;
  checked_at: string | null;
  last_error: string;
}

/** A category people apply in (`IDENTITY_BADGE_CATEGORIES`). */
export interface IdentityBadgeCategory {
  key: string;
  kind: Extract<IdentityBadgeKind, "org" | "notable">;
  label: string;
}

/** `GET /v1/identity/badges`: the caller's badges, paid badge, applications and domains. */
export interface IdentityBadges {
  badges: IdentityOwnBadge[];
  paid: IdentityPaidStatus;
  applications: IdentityBadgeApplication[];
  domains: IdentityDomain[];
  categories: IdentityBadgeCategory[];
  /** Every kind with its label, highest first. */
  kinds: { kind: IdentityBadgeKind; label: string }[];
}

/** `POST /v1/identity/badges/applications` (and `…/{id}/resubmit`, which keeps the category). */
export interface IdentityBadgeApplicationInput {
  category: string;
  display_label: string;
  evidence: { kind: IdentityEvidenceKind; ref: string }[];
  note: string;
}

export interface IdentityAdminBadgeApplication extends IdentityBadgeApplication {
  user: AdminVerificationUser;
  reviewed_by: string | null;
}

/** An application with what a reviewer weighs. */
export interface IdentityAdminBadgeApplicationDetail extends IdentityAdminBadgeApplication {
  identity_verified: boolean;
  badges: IdentityOwnBadge[];
  /** The badge an approval grants unless the reviewer picks another. */
  category_kind: Extract<IdentityBadgeKind, "org" | "notable"> | "";
}

/** Omit `status` for the review queue (submitted, oldest first). */
export interface IdentityAdminBadgeApplicationListParams extends PageParams {
  status?: IdentityApplicationStatus | undefined;
}

export interface IdentityBadgeDecision {
  decision: "approve" | "needs_info" | "reject";
  kind?: Extract<IdentityBadgeKind, "org" | "notable"> | undefined;
  label?: string | undefined;
  /** 1–730; default `IDENTITY_BADGE_TTL_DAYS`. */
  expires_in_days?: number | undefined;
  /** The applicant sees it; required for needs_info and reject. */
  note?: string | undefined;
}

/** A badge row as staff see it, ended ones included. */
export interface IdentityAdminBadge extends IdentityOwnBadge {
  user: AdminVerificationUser;
  active: boolean;
  /** A paid badge staff removed; lift it with a grant of kind paid. */
  held: boolean;
  granted_by: string | null;
  revoked_at: string | null;
  revoked_reason: string;
  updated_at: string;
}

export interface IdentityAdminBadgeListParams extends PageParams {
  kind?: IdentityBadgeKind | undefined;
  state?: "active" | "ended" | "held" | undefined;
  user_id?: string | undefined;
}

/** `POST /v1/admin/identity/badges` (admins). Kind paid lifts a staff removal. */
export interface IdentityBadgeGrantInput {
  user_id?: string | undefined;
  email?: string | undefined;
  kind: IdentityBadgeKind;
  label?: string | undefined;
  /** 0 = no expiry. */
  expires_in_days?: number | undefined;
}

export interface AdminVerificationUser {
  id: string;
  email: string;
  display_name: string;
}

/** An attempt as staff see it. */
export interface AdminVerification {
  id: string;
  user: AdminVerificationUser;
  provider: IdentityProvider;
  inquiry_id: string | null;
  status: Exclude<IdentityState, "none">;
  reason: string;
  submitted_at: string | null;
  decided_at: string | null;
  reviewed_by: string | null;
  expires_at: string | null;
  /** Review photos still stored (manual mode, until the decision). */
  has_documents: boolean;
  /** The inquiry in the provider's dashboard. */
  provider_url: string | null;
  created_at: string;
}

/** Detail with 5-minute photo links (each view is audited). */
export interface AdminVerificationDetail extends AdminVerification {
  document_url: string | null;
  selfie_url: string | null;
  urls_expire_at: string | null;
}

/** Omit `status` for the review queue (submitted, pending, oldest first). */
export interface AdminVerificationListParams extends PageParams {
  status?: Exclude<IdentityState, "none"> | undefined;
}

declare module "../types" {
  interface RealtimeEventMap {
    /** On the user's topic whenever their status changes. */
    "identity.status": IdentityStatus;
    /** On `admin:live` when an attempt awaits review. */
    "identity.submitted": { id: string };
    /** On the user's topic whenever their badges or applications change. */
    "identity.badges": { user_id: string };
    /** On `admin:live` when a badge application awaits review. */
    "identity.badge_application": { id: string };
  }
}
