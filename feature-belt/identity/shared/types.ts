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

/** `GET /v1/identity/users/{id}/badge` (public). Unknown users read as not verified. */
export interface IdentityBadge {
  user_id: string;
  verified: boolean;
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
  }
}
