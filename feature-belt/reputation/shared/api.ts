import type { CallOptions, HttpClient } from "../api/http";
import { seg } from "../api/query-string";
import type { Page } from "../types";
import type { Reputation, ReputationDecideRequest, ReputationFlag, ReputationFlagStatus, ReputationSummary } from "./types";

/** Reputation endpoints (`/v1/reputation`, `/v1/admin/reputation`). */
export function reputationEndpoints(http: HttpClient) {
  const R = "/v1/reputation";
  const A = "/v1/admin/reputation";
  return {
    /** The caller's full reputation: components, requirements, stats. */
    me: (o?: CallOptions) => http.get<Reputation>(`${R}/me`, o),
    /** A person's score, tier and status. */
    person: (userId: string, o?: CallOptions) => http.get<ReputationSummary>(`${R}/people/${seg(userId)}`, o),
    /** Up to 100 people at once (lists); unknown ids are left out. */
    people: (userIds: string[], o?: CallOptions) =>
      http.get<{ items: ReputationSummary[] }>(`${R}/people`, { ...o, query: { ids: userIds.join(",") } }),
    admin: {
      flags: (params: { status: ReputationFlagStatus; cursor?: string; limit?: number }, o?: CallOptions) =>
        http.get<Page<ReputationFlag>>(`${A}/flags`, { ...o, query: { status: params.status, cursor: params.cursor, limit: params.limit } }),
      decide: (flagId: string, body: ReputationDecideRequest, o?: CallOptions) => http.post<ReputationFlag>(`${A}/flags/${seg(flagId)}/decision`, body, o),
      /** Anyone's full reputation, for staff. */
      person: (userId: string, o?: CallOptions) => http.get<Reputation>(`${A}/people/${seg(userId)}`, o),
    },
  };
}

export type ReputationEndpoints = ReturnType<typeof reputationEndpoints>;
