import type { CallOptions, HttpClient } from "../api/http";
import { seg } from "../api/query-string";
import type { Page } from "../types";
import type {
  AdminVerification,
  AdminVerificationDetail,
  AdminVerificationListParams,
  IdentityAdminBadge,
  IdentityAdminBadgeApplication,
  IdentityAdminBadgeApplicationDetail,
  IdentityAdminBadgeApplicationListParams,
  IdentityAdminBadgeListParams,
  IdentityBadge,
  IdentityBadgeApplication,
  IdentityBadgeApplicationInput,
  IdentityBadgeDecision,
  IdentityBadgeGrantInput,
  IdentityBadgeKind,
  IdentityBadges,
  IdentityDomain,
  IdentityStartResult,
  IdentityStatus,
} from "./types";

/** Identity endpoints. Reach them with `useEndpoints(identityEndpoints)`. */
export function identityEndpoints(http: HttpClient) {
  const I = "/v1/identity";
  const A = "/v1/admin/identity/verifications";
  const B = "/v1/identity/badges";
  const AB = "/v1/admin/identity";
  return {
    /** `GET /v1/identity/status`. */
    status: (o?: CallOptions) => http.get<IdentityStatus>(`${I}/status`, o),
    /**
     * `POST /v1/identity/start`: creates or resumes an attempt. `redirect_uri` (hosted flow
     * return) must be a web origin of this app or the app's scheme.
     */
    start: (body: { redirect_uri?: string | undefined } = {}, o?: CallOptions) => http.post<IdentityStartResult>(`${I}/start`, body, o),
    /** `POST /v1/identity/documents` (manual mode): keys from uploads with purpose `identity_document`. */
    submitDocuments: (body: { document_key: string; selfie_key: string }, o?: CallOptions) => http.post<IdentityStatus>(`${I}/documents`, body, o),
    /** `GET /v1/identity/users/{id}/badge` (public): ID verification and the highest badge. */
    badge: (userId: string, o?: CallOptions) => http.get<IdentityBadge>(`${I}/users/${seg(userId)}/badge`, o),
    /** `GET /v1/identity/badges`: the caller's badges, paid badge, applications and domain proofs. */
    badges: (o?: CallOptions) => http.get<IdentityBadges>(B, o),
    /** `POST /v1/identity/badges/applications` (3 per 30 days; one open per category). */
    applyForBadge: (body: IdentityBadgeApplicationInput, o?: CallOptions) => http.post<IdentityBadgeApplication>(`${B}/applications`, body, o),
    /** `POST …/applications/{id}/resubmit`: answer staff's request for more (needs_info). */
    resubmitBadgeApplication: (id: string, body: IdentityBadgeApplicationInput, o?: CallOptions) =>
      http.post<IdentityBadgeApplication>(`${B}/applications/${seg(id)}/resubmit`, body, o),
    /** `POST …/applications/{id}/withdraw`. */
    withdrawBadgeApplication: (id: string, o?: CallOptions) => http.post<IdentityBadgeApplication>(`${B}/applications/${seg(id)}/withdraw`, undefined, o),
    /** `POST /v1/identity/badges/domains`: start a proof (adding again keeps the token). */
    addBadgeDomain: (domain: string, o?: CallOptions) => http.post<IdentityDomain>(`${B}/domains`, { domain }, o),
    /** `POST …/domains/{domain}/check`: 422 `domain_unverified` with what was found. */
    checkBadgeDomain: (domain: string, o?: CallOptions) => http.post<IdentityDomain>(`${B}/domains/${seg(domain)}/check`, undefined, o),
    /** `DELETE …/domains/{domain}`. */
    removeBadgeDomain: (domain: string, o?: CallOptions) => http.delete<void>(`${B}/domains/${seg(domain)}`, o),
    admin: {
      /** `GET /v1/admin/identity/verifications?status=`. */
      list: (params: AdminVerificationListParams = {}, o?: CallOptions) =>
        http.get<Page<AdminVerification>>(A, { ...o, query: { status: params.status, limit: params.limit, cursor: params.cursor } }),
      /** `GET /v1/admin/identity/verifications/{id}`: photo links last 5 minutes; audited. */
      get: (id: string, o?: CallOptions) => http.get<AdminVerificationDetail>(`${A}/${seg(id)}`, o),
      /** `POST …/{id}/approve`. Audited; deletes the photos. */
      approve: (id: string, o?: CallOptions) => http.post<AdminVerification>(`${A}/${seg(id)}/approve`, undefined, o),
      /** `POST …/{id}/reject`: declines a submitted attempt or revokes a verified one. Audited. */
      reject: (id: string, reason: string, o?: CallOptions) => http.post<AdminVerification>(`${A}/${seg(id)}/reject`, { reason }, o),
      /** `GET /v1/admin/identity/badge-applications?status=`: no status = the review queue. */
      badgeApplications: (params: IdentityAdminBadgeApplicationListParams = {}, o?: CallOptions) =>
        http.get<Page<IdentityAdminBadgeApplication>>(`${AB}/badge-applications`, { ...o, query: { status: params.status, limit: params.limit, cursor: params.cursor } }),
      /** `GET …/badge-applications/{id}`. */
      badgeApplication: (id: string, o?: CallOptions) => http.get<IdentityAdminBadgeApplicationDetail>(`${AB}/badge-applications/${seg(id)}`, o),
      /** `POST …/badge-applications/{id}/decision`. Audited; not your own. */
      decideBadgeApplication: (id: string, body: IdentityBadgeDecision, o?: CallOptions) =>
        http.post<IdentityAdminBadgeApplicationDetail>(`${AB}/badge-applications/${seg(id)}/decision`, body, o),
      /** `GET /v1/admin/identity/badges?kind=&state=&user_id=`, most recently changed first. */
      badges: (params: IdentityAdminBadgeListParams = {}, o?: CallOptions) =>
        http.get<Page<IdentityAdminBadge>>(`${AB}/badges`, { ...o, query: { kind: params.kind, state: params.state, user_id: params.user_id, limit: params.limit, cursor: params.cursor } }),
      /** `POST /v1/admin/identity/badges` (admins): grant, or lift a paid-badge removal (kind paid). Audited. */
      grantBadge: (body: IdentityBadgeGrantInput, o?: CallOptions) => http.post<IdentityAdminBadge>(`${AB}/badges`, body, o),
      /** `POST /v1/admin/identity/badges/{user_id}/{kind}/revoke`: the person sees `reason`. Audited. */
      revokeBadge: (userId: string, kind: IdentityBadgeKind, reason: string, o?: CallOptions) =>
        http.post<IdentityAdminBadge>(`${AB}/badges/${seg(userId)}/${seg(kind)}/revoke`, { reason }, o),
    },
  };
}

export type IdentityApi = ReturnType<typeof identityEndpoints>;
