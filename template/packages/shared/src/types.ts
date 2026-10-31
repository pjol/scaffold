/**
 * Canonical DTOs for the core API. snake_case, matching the Go structs in
 * `backend/internal/{httpx,accounts,admin,realtime,notify}` exactly; change both
 * sides in the same commit. Timestamps are RFC 3339 strings, ids are UUID strings.
 *
 * Features keep their DTOs in their own folder (`src/<feature>/types.ts`).
 */

// ───────────────────────────────────────────────────────────── envelopes ──

/** Stable error codes the core API returns (`httpx.Error.Code`). Features add their own. */
export type ApiErrorCode =
  | "unauthenticated"
  | "forbidden"
  | "not_found"
  | "invalid_request"
  | "conflict"
  | "rate_limited"
  | "account_suspended"
  | "consent_required"
  | "email_unverified"
  | "bad_signature"
  | "internal";

/** Every non-2xx body: `{"error":{"code","message","details"}}`. `message` is user-facing copy. */
export interface ApiErrorBody {
  error: {
    code: ApiErrorCode;
    message: string;
    details?: Record<string, unknown>;
  };
}

/** List envelope (`httpx.Page[T]`). `next_cursor` is opaque; null on the last page. */
export interface Page<T> {
  items: T[];
  next_cursor: string | null;
}

/** Where a client runs. Sent as `X-Client-Platform` and recorded on consents. */
export type ClientPlatform = "web" | "ios" | "android";

// ───────────────────────────────────────────────────────────── config ──

/** `GET /v1/config` (public): what clients need before sign-in. */
export interface ClientConfig {
  app_name: string;
  terms_version: string;
  privacy_version: string;
  support_email: string;
  /** Oldest supported native build per OS; older builds must update. */
  min_app_version: Record<"ios" | "android", string>;
  /** Installed backend modules (`accounts`, `admin`, feature names). */
  modules: string[];
}

// ───────────────────────────────────────────────────────────── me ──

export type UserStatus = "active" | "suspended" | "banned" | "deleted";
export type AdminRole = "support" | "admin";

/** Consent state on `Me`. `required` gates every mutation until the current versions are accepted. */
export interface Consent {
  required: boolean;
  /** Current versions the server requires. */
  terms_version: string;
  privacy_version: string;
  /** Versions the user accepted ("" = never). */
  accepted_terms: string;
  accepted_privacy: string;
}

/** `GET /v1/me`. */
export interface Me {
  id: string;
  email: string;
  display_name: string;
  avatar_url: string | null;
  admin_role: AdminRole | null;
  status: UserStatus;
  created_at: string;
  consent: Consent;
}

/** `PATCH /v1/me`. Omitted fields are unchanged; `avatar_url: ""` removes the photo. */
export interface UpdateMeRequest {
  /** 1–60 characters. */
  display_name?: string;
  /** A `public_url` from an `avatar` upload, or "" to clear. */
  avatar_url?: string;
}

/** `POST /v1/me/consents`. Versions must equal the current ones or the server answers 409. */
export interface AcceptConsentsRequest {
  terms_version: string;
  privacy_version: string;
  platform: ClientPlatform;
}

/** One row of `AccountExport.legal_acceptances`. */
export interface LegalAcceptance {
  document: "terms" | "privacy";
  version: string;
  platform: string;
  accepted_at: string;
}

/** `GET /v1/me/export`. Each installed module that stores user data adds a key named after itself. */
export interface AccountExport {
  exported_at: string;
  account: Me;
  legal_acceptances: LegalAcceptance[];
  [module: string]: unknown;
}

// ───────────────────────────────────────────────────────────── uploads ──

/** Registered upload purpose. Core ships `avatar`; features register more. */
export type UploadPurpose = "avatar" | (string & {});

/** `POST /v1/uploads`. */
export interface UploadRequest {
  purpose: UploadPurpose;
  content_type: string;
  /** Bytes. */
  size: number;
}

/** `POST /v1/uploads` response: PUT the bytes to `upload_url` with `headers`, then send `key` to the owning endpoint. */
export interface Upload {
  key: string;
  upload_url: string;
  headers: Record<string, string>;
  /** Set for public purposes (avatars), null for private ones. */
  public_url: string | null;
}

// ───────────────────────────────────────────────────────────── admin ──

/** A user as staff see it (`GET /v1/admin/users`, `/users/{id}`). */
export interface AdminUser {
  id: string;
  email: string;
  display_name: string;
  avatar_url: string | null;
  admin_role: AdminRole | null;
  status: UserStatus;
  created_at: string;
  deleted_at: string | null;
}

/** `GET /v1/admin/overview`. */
export interface AdminOverview {
  users: number;
  new_7d: number;
  suspended: number;
}

/** `PATCH /v1/admin/users/{id}`. `admin_role` is admin-only; "" removes the role. */
export interface UpdateUserRequest {
  status?: "active" | "suspended" | "banned";
  admin_role?: AdminRole | "";
}

/** One admin audit row (`GET /v1/admin/audit`). */
export interface AuditEntry {
  id: number;
  admin_id: string | null;
  admin_email: string | null;
  action: string;
  target_type: string;
  target_id: string;
  /** The mutation's input as recorded (small JSON object). */
  data: Record<string, unknown> | null;
  created_at: string;
}

// ───────────────────────────────────────────────────────────── realtime ──

/** In-app notification (`notify.Notification`), delivered as a `notification` event on the user topic. */
export interface InAppNotification {
  user_id: string;
  /** Stable dotted id, e.g. `chat.message`. */
  kind: string;
  title: string;
  body: string;
  /** App path, e.g. `/chat/<id>`. */
  link?: string;
  data?: Record<string, unknown>;
}

/**
 * Event payloads by type. Features extend it from their folder:
 *
 * ```ts
 * declare module "../types" {
 *   interface RealtimeEventMap { "chat.message": ChatMessage }
 * }
 * ```
 */
export interface RealtimeEventMap {
  /** Reply to a client `ping` (clock offset). */
  pong: Record<string, never>;
  notification: InAppNotification;
}

/** Server → client frame: `{"type","topic","data","ts"}`. */
export interface RealtimeEvent<TData = unknown, TType extends string = string> {
  type: TType;
  /** `"<kind>:<id>"`; empty for `pong`. */
  topic: string;
  data: TData;
  ts: string;
}

/** Client → server frame. */
export interface RealtimeClientMessage {
  type: "ping" | "subscribe" | "unsubscribe";
  topic?: string;
}
