import type { CallOptions, HttpClient } from "../api/http";
import { seg } from "../api/query-string";
import type { Page } from "../types";
import type {
  OracleAdminFeed,
  OracleCreateFeedRequest,
  OracleDailyBar,
  OracleFeed,
  OracleFeedDetail,
  OracleFeedListParams,
  OracleImportHistoryRequest,
  OracleObservation,
  OracleObservationListParams,
  OraclePartConfig,
  OraclePostPrintRequest,
  OracleSourceInput,
  OracleSourcesStatus,
  OracleUpdateFeedRequest,
} from "./types";

/** Oracle endpoints: feeds, prices, observations, history, and staff configuration. */
export function oracleEndpoints(http: HttpClient) {
  const O = "/v1/oracle";
  const A = "/v1/admin/oracle";
  return {
    /** `GET /v1/oracle/feeds`: listed, enabled feeds by key. */
    feeds: (p: OracleFeedListParams = {}, o?: CallOptions) =>
      http.get<Page<OracleFeed>>(`${O}/feeds`, { ...o, query: { q: p.q, kind: p.kind, limit: p.limit, cursor: p.cursor } }),
    /** `GET /v1/oracle/feeds/{key}`: price, venues, parts, the last day and 50 observations. */
    feed: (key: string, o?: CallOptions) => http.get<OracleFeedDetail>(`${O}/feeds/${seg(key)}`, o),
    /** `GET /v1/oracle/feeds/{key}/history?days=`: daily bars, oldest first. */
    history: (key: string, days = 365, o?: CallOptions) =>
      http.get<{ items: OracleDailyBar[] }>(`${O}/feeds/${seg(key)}/history`, { ...o, query: { days } }),
    /** `GET /v1/oracle/observations`: screened prints, newest first. */
    observations: (p: OracleObservationListParams = {}, o?: CallOptions) =>
      http.get<Page<OracleObservation>>(`${O}/observations`, { ...o, query: { feed: p.feed, verdict: p.verdict, limit: p.limit, cursor: p.cursor } }),
    admin: {
      /** Every feed, unlisted and disabled too. */
      feeds: (p: OracleFeedListParams = {}, o?: CallOptions) =>
        http.get<Page<OracleFeed>>(`${A}/feeds`, { ...o, query: { q: p.q, kind: p.kind, limit: p.limit, cursor: p.cursor } }),
      feed: (key: string, o?: CallOptions) => http.get<OracleAdminFeed>(`${A}/feeds/${seg(key)}`, o),
      /** Observations of every feed (staff). */
      observations: (p: OracleObservationListParams = {}, o?: CallOptions) =>
        http.get<Page<OracleObservation>>(`${A}/observations`, { ...o, query: { feed: p.feed, verdict: p.verdict, limit: p.limit, cursor: p.cursor } }),
      /** Source health, budgets, the decision model and the leading instance. */
      sources: (o?: CallOptions) => http.get<OracleSourcesStatus>(`${A}/sources`, o),
      /** Admin. Audited. */
      create: (body: OracleCreateFeedRequest, o?: CallOptions) => http.post<OracleAdminFeed>(`${A}/feeds`, body, o),
      /** Admin. Audited. */
      update: (key: string, body: OracleUpdateFeedRequest, o?: CallOptions) => http.patch<OracleAdminFeed>(`${A}/feeds/${seg(key)}`, body, o),
      /** Admin: replaces every source row. Audited. */
      putSources: (key: string, sources: OracleSourceInput[], o?: CallOptions) =>
        http.put<OracleAdminFeed>(`${A}/feeds/${seg(key)}/sources`, { sources }, o),
      /** Admin: replaces a basket's parts. Audited. */
      putParts: (key: string, parts: OraclePartConfig[], o?: CallOptions) => http.put<OracleAdminFeed>(`${A}/feeds/${seg(key)}/parts`, { parts }, o),
      /** Admin: a price typed in by hand (the feed needs a manual source). Queued and screened; 202. Audited. */
      postPrint: (key: string, body: OraclePostPrintRequest, o?: CallOptions) =>
        http.post<{ queued: boolean }>(`${A}/feeds/${seg(key)}/prints`, body, o),
      /** Admin: daily closes for the chart. Audited. */
      importHistory: (key: string, body: OracleImportHistoryRequest, o?: CallOptions) =>
        http.put<{ stored: number }>(`${A}/feeds/${seg(key)}/history`, body, o),
    },
  };
}

export type OracleEndpoints = ReturnType<typeof oracleEndpoints>;
