import type { AdminOverview, AdminUser, AuditEntry, Page, UpdateUserRequest } from "../../types";
import type { CallOptions, HttpClient } from "../http";
import type { AdminAuditListParams, AdminUserListParams } from "../params";
import { seg } from "../query-string";

/**
 * Core admin endpoints (`/v1/admin`, support or admin). † = admin role only. Features add
 * their admin routes through their own endpoint factory.
 */
export function adminEndpoints(http: HttpClient) {
  const A = "/v1/admin";
  return {
    /** `GET /v1/admin/overview`. */
    overview: (o?: CallOptions) => http.get<AdminOverview>(`${A}/overview`, o),
    /** `GET /v1/admin/users?q=&status=`, newest first. */
    users: (params: AdminUserListParams = {}, o?: CallOptions) =>
      http.get<Page<AdminUser>>(`${A}/users`, { ...o, query: { q: params.q, status: params.status, limit: params.limit, cursor: params.cursor } }),
    /** `GET /v1/admin/users/{id}`. */
    user: (id: string, o?: CallOptions) => http.get<AdminUser>(`${A}/users/${seg(id)}`, o),
    /** `PATCH /v1/admin/users/{id}`: status, and the role †. Audited. */
    updateUser: (id: string, body: UpdateUserRequest, o?: CallOptions) => http.patch<AdminUser>(`${A}/users/${seg(id)}`, body, o),
    /** `DELETE /v1/admin/users/{id}` †: deletes the account. Irreversible. Audited. */
    deleteUser: (id: string, o?: CallOptions) => http.delete<void>(`${A}/users/${seg(id)}`, o),
    /** `GET /v1/admin/audit?target_type=&target_id=`, newest first. */
    audit: (params: AdminAuditListParams = {}, o?: CallOptions) =>
      http.get<Page<AuditEntry>>(`${A}/audit`, {
        ...o,
        query: { target_type: params.target_type, target_id: params.target_id, limit: params.limit, cursor: params.cursor },
      }),
  };
}

/** The `data` of an audit entry (the mutation's input), or null. */
export function auditData(entry: Pick<AuditEntry, "data">): Record<string, unknown> | null {
  return entry.data ?? null;
}
