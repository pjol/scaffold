import type { CallOptions, HttpClient } from "../api/http";
import { seg } from "../api/query-string";
import type { Page } from "../types";
import type {
  AgentsAdminStats,
  AgentsAdminUser,
  AgentsBulkRequest,
  AgentsBulkResult,
  AgentsFeatureScope,
  AgentsResourceScope,
  AgentsScopeInfo,
  AgentsScopeRef,
  AgentsSetScopeRequest,
  AgentsSettings,
  AgentsSuggestion,
  AgentsSuggestionParams,
  AgentsSummary,
  AgentsUpdateSettingsRequest,
} from "./types";

/** `feature,kind,id` as the scope endpoint reads it. */
export function agentsScopeKey(ref: AgentsScopeRef): string {
  return `${ref.feature},${ref.kind ?? ""},${ref.id ?? ""}`;
}

const BATCH_MAX = 50;
const BATCH_WAIT_MS = 8;

/** AI agent scope endpoints. */
export function agentsEndpoints(http: HttpClient) {
  const A = "/v1/agents";

  // Every ⋮ menu asks for its scope; menus rendered together share one request.
  let queue: { key: string; resolve: (v: AgentsScopeInfo) => void; reject: (e: unknown) => void }[] = [];
  let timer: ReturnType<typeof setTimeout> | null = null;
  const flush = () => {
    timer = null;
    const batch = queue;
    queue = [];
    for (let i = 0; i < batch.length; i += BATCH_MAX) {
      const chunk = batch.slice(i, i + BATCH_MAX);
      const keys = [...new Set(chunk.map((q) => q.key))];
      http
        .get<{ items: AgentsScopeInfo[] }>(`${A}/scope`, { query: { r: keys } })
        .then((res) => {
          const byKey = new Map(keys.map((k, idx) => [k, res.items[idx]] as const));
          for (const q of chunk) {
            const v = byKey.get(q.key);
            if (v) q.resolve(v);
            else q.reject(new Error("Missing scope in response"));
          }
        })
        .catch((e: unknown) => {
          for (const q of chunk) q.reject(e);
        });
    }
  };

  return {
    settings: (o?: CallOptions) => http.get<AgentsSettings>(`${A}/settings`, o),
    /** Default level and/or "turn off all agent access". */
    updateSettings: (body: AgentsUpdateSettingsRequest, o?: CallOptions) => http.put<AgentsSettings>(`${A}/settings`, body, o),
    /** Your connected external services (connectors feature) and settings kept for disconnected ones. */
    services: (o?: CallOptions) => http.get<{ items: AgentsFeatureScope[] }>(`${A}/services`, o),
    /** One scope; calls made in the same tick are sent as one request. */
    scope: (ref: AgentsScopeRef) =>
      new Promise<AgentsScopeInfo>((resolve, reject) => {
        queue.push({ key: agentsScopeKey(ref), resolve, reject });
        if (queue.length >= BATCH_MAX) {
          if (timer) clearTimeout(timer);
          flush();
        } else if (!timer) {
          timer = setTimeout(flush, BATCH_WAIT_MS);
        }
      }),
    /** Set (or clear with `level: null`) a feature's, service's or item's own setting. */
    setScope: (body: AgentsSetScopeRequest, o?: CallOptions) => http.put<AgentsScopeInfo>(`${A}/scope`, body, o),
    /** Items with their own setting, newest first. */
    resourceScopes: (params: { cursor?: string | undefined; limit?: number } = {}, o?: CallOptions) =>
      http.get<Page<AgentsResourceScope>>(`${A}/scopes`, { ...o, query: { cursor: params.cursor, limit: params.limit } }),
    removeResourceScope: (id: string, o?: CallOptions) => http.delete<void>(`${A}/scopes/${seg(id)}`, o),

    summary: (o?: CallOptions) => http.get<AgentsSummary>(`${A}/summary`, o),
    suggestions: (params: AgentsSuggestionParams & { cursor?: string | undefined } = {}, o?: CallOptions) =>
      http.get<Page<AgentsSuggestion>>(`${A}/suggestions`, {
        ...o,
        query: {
          status: params.status?.length ? params.status.join(",") : undefined,
          via: params.via,
          client_key: params.client_key,
          cursor: params.cursor,
          limit: params.limit,
        },
      }),
    suggestion: (id: string, o?: CallOptions) => http.get<AgentsSuggestion>(`${A}/suggestions/${seg(id)}`, o),
    /** Runs the change as you. 409 `target_changed` (details: the target now) unless `force`. */
    accept: (id: string, force = false, o?: CallOptions) => http.post<AgentsSuggestion>(`${A}/suggestions/${seg(id)}/accept`, { force }, o),
    reject: (id: string, o?: CallOptions) => http.post<AgentsSuggestion>(`${A}/suggestions/${seg(id)}/reject`, {}, o),
    /** Accept or reject up to 50 waiting suggestions from one agent, oldest first. */
    bulk: (body: AgentsBulkRequest, o?: CallOptions) => http.post<AgentsBulkResult>(`${A}/suggestions/bulk`, body, o),

    // ---- staff
    adminStats: (o?: CallOptions) => http.get<AgentsAdminStats>(`/v1/admin/agents/stats`, o),
    adminUser: (userId: string, o?: CallOptions) => http.get<AgentsAdminUser>(`/v1/admin/agents/users/${seg(userId)}`, o),
    /** Admins: turn a person's agent access off (they can't lift it) or back on. Audited. */
    adminSetPaused: (userId: string, paused: boolean, reason?: string, o?: CallOptions) =>
      http.put<AgentsAdminUser>(`/v1/admin/agents/users/${seg(userId)}/paused`, { paused, reason }, o),
  };
}

export type AgentsEndpoints = ReturnType<typeof agentsEndpoints>;
