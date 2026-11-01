import type { CallOptions, HttpClient } from "../api/http";
import { seg } from "../api/query-string";
import type { Page } from "../types";
import type {
  BlockedIds,
  BlockedUser,
  CaseActionRequest,
  CaseDetail,
  CreateReportRequest,
  ModerationCase,
  ModerationCaseListParams,
  ModerationSummary,
  MyReport,
} from "./types";

/** Moderation endpoints: reports and blocks for everyone, the case queue for staff. */
export function moderationEndpoints(http: HttpClient) {
  const M = "/v1/moderation";
  const A = "/v1/admin/moderation";
  return {
    /** `POST /v1/moderation/reports`: 201 new, 200 when it updated your earlier report on the same target. */
    report: (body: CreateReportRequest, o?: CallOptions) => http.post<MyReport>(`${M}/reports`, body, o),
    /** `GET /v1/moderation/reports`, newest first. */
    myReports: (params: { cursor?: string | undefined; limit?: number } = {}, o?: CallOptions) =>
      http.get<Page<MyReport>>(`${M}/reports`, { ...o, query: { cursor: params.cursor, limit: params.limit } }),
    /** `GET /v1/moderation/blocks`, newest first. */
    blocks: (params: { cursor?: string | undefined; limit?: number } = {}, o?: CallOptions) =>
      http.get<Page<BlockedUser>>(`${M}/blocks`, { ...o, query: { cursor: params.cursor, limit: params.limit } }),
    /** `GET /v1/moderation/blocks/ids`. */
    blockedIds: (o?: CallOptions) => http.get<BlockedIds>(`${M}/blocks/ids`, o),
    /** `POST /v1/moderation/blocks` (idempotent). */
    block: (userId: string, o?: CallOptions) => http.post<BlockedUser>(`${M}/blocks`, { user_id: userId }, o),
    /** `DELETE /v1/moderation/blocks/{user_id}` (idempotent). */
    unblock: (userId: string, o?: CallOptions) => http.delete<void>(`${M}/blocks/${seg(userId)}`, o),
    admin: {
      /** `GET /v1/admin/moderation/summary`. */
      summary: (o?: CallOptions) => http.get<ModerationSummary>(`${A}/summary`, o),
      /** `GET /v1/admin/moderation/cases`: open oldest first, closed newest first. */
      cases: (params: ModerationCaseListParams = {}, o?: CallOptions) =>
        http.get<Page<ModerationCase>>(`${A}/cases`, {
          ...o,
          query: { status: params.status, kind: params.kind, author_id: params.author_id, limit: params.limit, cursor: params.cursor },
        }),
      /** `GET /v1/admin/moderation/cases/{id}` with every report. */
      case: (id: string, o?: CallOptions) => http.get<CaseDetail>(`${A}/cases/${seg(id)}`, o),
      /** `POST /v1/admin/moderation/cases/{id}/actions`: closes the case. Audited. */
      act: (id: string, body: CaseActionRequest, o?: CallOptions) => http.post<CaseDetail>(`${A}/cases/${seg(id)}/actions`, body, o),
    },
  };
}

export type ModerationEndpoints = ReturnType<typeof moderationEndpoints>;
