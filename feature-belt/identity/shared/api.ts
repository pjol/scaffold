import type { CallOptions, HttpClient } from "../api/http";
import { seg } from "../api/query-string";
import type { Page } from "../types";
import type { AdminVerification, AdminVerificationDetail, AdminVerificationListParams, IdentityBadge, IdentityStartResult, IdentityStatus } from "./types";

/** Identity endpoints. Reach them with `useEndpoints(identityEndpoints)`. */
export function identityEndpoints(http: HttpClient) {
  const I = "/v1/identity";
  const A = "/v1/admin/identity/verifications";
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
    /** `GET /v1/identity/users/{id}/badge` (public). */
    badge: (userId: string, o?: CallOptions) => http.get<IdentityBadge>(`${I}/users/${seg(userId)}/badge`, o),
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
    },
  };
}

export type IdentityApi = ReturnType<typeof identityEndpoints>;
