"use client";

import { useMutation, useQueryClient, type QueryClient, type UseMutationResult, type UseQueryResult } from "@tanstack/react-query";
import type { ApiError } from "../api/errors";
import { useEndpoints } from "../hooks/context";
import { useApiQuery, usePagedQuery, type InfiniteOverrides, type PagedQueryResult, type QueryOverrides } from "../hooks/internal";
import { registerReconnectRefetch, registerRealtimeHandler } from "../hooks/realtime-handlers";
import { agentsEndpoints, agentsScopeKey } from "./api";
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

/** Query keys. `agentsKeys.all()` invalidates everything this feature caches. */
export const agentsKeys = {
  all: () => ["agents"] as const,
  settings: () => ["agents", "settings"] as const,
  services: () => ["agents", "services"] as const,
  scopes: () => ["agents", "scope"] as const,
  scope: (ref: AgentsScopeRef) => ["agents", "scope", agentsScopeKey(ref)] as const,
  resourceScopes: () => ["agents", "resource-scopes"] as const,
  summary: () => ["agents", "summary"] as const,
  suggestionLists: () => ["agents", "suggestions"] as const,
  suggestions: (params: AgentsSuggestionParams) => ["agents", "suggestions", params] as const,
  suggestion: (id: string) => ["agents", "suggestion", id] as const,
  adminStats: () => ["agents", "admin", "stats"] as const,
  adminUser: (userId: string) => ["agents", "admin", "user", userId] as const,
};

function invalidateScopes(qc: QueryClient) {
  for (const key of [agentsKeys.scopes(), agentsKeys.settings(), agentsKeys.services(), agentsKeys.resourceScopes(), agentsKeys.summary()]) {
    void qc.invalidateQueries({ queryKey: key });
  }
}

function storeSuggestion(qc: QueryClient, s: AgentsSuggestion) {
  qc.setQueryData(agentsKeys.suggestion(s.id), s);
  void qc.invalidateQueries({ queryKey: agentsKeys.suggestionLists() });
  void qc.invalidateQueries({ queryKey: agentsKeys.summary() });
  void qc.invalidateQueries({ queryKey: agentsKeys.settings() });
}

registerRealtimeHandler("agents.suggestion", (qc, e) => storeSuggestion(qc, e.data.suggestion));
registerRealtimeHandler("agents.suggestion_updated", (qc, e) => storeSuggestion(qc, e.data.suggestion));
registerRealtimeHandler("agents.scopes_changed", (qc) => invalidateScopes(qc));
registerReconnectRefetch(agentsKeys.summary());
registerReconnectRefetch(agentsKeys.suggestionLists());

// ---- settings ------------------------------------------------------------------

/** Default level, "turn off all", features with tools, counts, agents seen, installed agent features. */
export function useAgentsSettings(options?: QueryOverrides<AgentsSettings>): UseQueryResult<AgentsSettings, ApiError> {
  const a = useEndpoints(agentsEndpoints);
  return useApiQuery(agentsKeys.settings(), ({ signal }) => a.settings({ signal }), options);
}

export function useAgentsUpdateSettings(): UseMutationResult<AgentsSettings, ApiError, AgentsUpdateSettingsRequest> {
  const a = useEndpoints(agentsEndpoints);
  const qc = useQueryClient();
  return useMutation<AgentsSettings, ApiError, AgentsUpdateSettingsRequest>({
    mutationFn: (body) => a.updateSettings(body),
    onSuccess: (data) => {
      qc.setQueryData(agentsKeys.settings(), data);
      invalidateScopes(qc);
    },
  });
}

/** Connected external services (asks each service for its tools: can take a few seconds). */
export function useAgentsServices(enabled = true, options?: QueryOverrides<{ items: AgentsFeatureScope[] }>): UseQueryResult<{ items: AgentsFeatureScope[] }, ApiError> {
  const a = useEndpoints(agentsEndpoints);
  return useApiQuery(agentsKeys.services(), ({ signal }) => a.services({ signal }), { staleTime: 60_000, ...options }, enabled);
}

// ---- one scope -----------------------------------------------------------------

/**
 * The scope of a feature, a service or one item (the ⋮ menu's "AI agent access"). Menus
 * on one screen share a request. Pass null to skip.
 */
export function useAgentsScope(ref: AgentsScopeRef | null, options?: QueryOverrides<AgentsScopeInfo>): UseQueryResult<AgentsScopeInfo, ApiError> {
  const a = useEndpoints(agentsEndpoints);
  const r = ref ?? { feature: "" };
  return useApiQuery(agentsKeys.scope(r), () => a.scope(r), { staleTime: 30_000, retry: 1, ...options }, !!ref && !!ref.feature);
}

/** Set or clear (`level: null`) a scope. Items under an item and every open menu refresh. */
export function useAgentsSetScope(): UseMutationResult<AgentsScopeInfo, ApiError, AgentsSetScopeRequest> {
  const a = useEndpoints(agentsEndpoints);
  const qc = useQueryClient();
  return useMutation<AgentsScopeInfo, ApiError, AgentsSetScopeRequest>({
    mutationFn: (body) => a.setScope(body),
    onSuccess: (info, body) => {
      qc.setQueryData(agentsKeys.scope(body), info);
      invalidateScopes(qc);
    },
  });
}

/** Items with their own setting, newest first. */
export function useAgentsResourceScopes(options?: InfiniteOverrides<AgentsResourceScope>): PagedQueryResult<AgentsResourceScope> {
  const a = useEndpoints(agentsEndpoints);
  return usePagedQuery(agentsKeys.resourceScopes(), (cursor, signal) => a.resourceScopes({ cursor }, { signal }), options);
}

export function useAgentsRemoveResourceScope(): UseMutationResult<void, ApiError, string> {
  const a = useEndpoints(agentsEndpoints);
  const qc = useQueryClient();
  return useMutation<void, ApiError, string>({ mutationFn: (id) => a.removeResourceScope(id), onSuccess: () => invalidateScopes(qc) });
}

// ---- suggestions ---------------------------------------------------------------

/** Waiting count and whether agent access is off; realtime keeps it fresh. */
export function useAgentsSummary(options?: QueryOverrides<AgentsSummary>): UseQueryResult<AgentsSummary, ApiError> {
  const a = useEndpoints(agentsEndpoints);
  return useApiQuery(agentsKeys.summary(), ({ signal }) => a.summary({ signal }), { staleTime: 30_000, ...options });
}

/** Badge hook: the waiting count from the cache (the session mount keeps it loaded). Never fetches. */
export function useAgentsPendingCount(): number | undefined {
  const pending = useAgentsSummary({ enabled: false }).data?.pending;
  return pending ? pending : undefined;
}

export function useAgentsSuggestions(params: AgentsSuggestionParams = {}, options?: InfiniteOverrides<AgentsSuggestion>): PagedQueryResult<AgentsSuggestion> {
  const a = useEndpoints(agentsEndpoints);
  return usePagedQuery(agentsKeys.suggestions(params), (cursor, signal) => a.suggestions({ ...params, cursor }, { signal }), options);
}

export function useAgentsSuggestion(id: string | null | undefined, options?: QueryOverrides<AgentsSuggestion>): UseQueryResult<AgentsSuggestion, ApiError> {
  const a = useEndpoints(agentsEndpoints);
  return useApiQuery(agentsKeys.suggestion(id ?? ""), ({ signal }) => a.suggestion(id ?? "", { signal }), options, !!id);
}

/**
 * Accept: runs the change as you. Resolves to the suggestion (status `accepted`, or
 * `failed` with `error`). Rejects with 409 `target_changed` (details: the target now,
 * {@link AgentsTargetChanged}) unless `force`.
 */
export function useAgentsAccept(): UseMutationResult<AgentsSuggestion, ApiError, { id: string; force?: boolean }> {
  const a = useEndpoints(agentsEndpoints);
  const qc = useQueryClient();
  return useMutation<AgentsSuggestion, ApiError, { id: string; force?: boolean }>({
    mutationFn: ({ id, force }) => a.accept(id, force ?? false),
    onSuccess: (s) => storeSuggestion(qc, s),
    onError: (_e, { id }) => void qc.invalidateQueries({ queryKey: agentsKeys.suggestion(id) }),
  });
}

export function useAgentsReject(): UseMutationResult<AgentsSuggestion, ApiError, string> {
  const a = useEndpoints(agentsEndpoints);
  const qc = useQueryClient();
  return useMutation<AgentsSuggestion, ApiError, string>({
    mutationFn: (id) => a.reject(id),
    onSuccess: (s) => storeSuggestion(qc, s),
    onError: (_e, id) => void qc.invalidateQueries({ queryKey: agentsKeys.suggestion(id) }),
  });
}

/** Accept or reject everything waiting from one agent (50 at a time). */
export function useAgentsBulk(): UseMutationResult<AgentsBulkResult, ApiError, AgentsBulkRequest> {
  const a = useEndpoints(agentsEndpoints);
  const qc = useQueryClient();
  return useMutation<AgentsBulkResult, ApiError, AgentsBulkRequest>({
    mutationFn: (body) => a.bulk(body),
    onSettled: (data) => {
      for (const s of data?.items ?? []) qc.setQueryData(agentsKeys.suggestion(s.id), s);
      void qc.invalidateQueries({ queryKey: agentsKeys.suggestionLists() });
      void qc.invalidateQueries({ queryKey: agentsKeys.summary() });
      void qc.invalidateQueries({ queryKey: agentsKeys.settings() });
    },
  });
}

// ---- staff ---------------------------------------------------------------------

export function useAgentsAdminStats(options?: QueryOverrides<AgentsAdminStats>): UseQueryResult<AgentsAdminStats, ApiError> {
  const a = useEndpoints(agentsEndpoints);
  return useApiQuery(agentsKeys.adminStats(), ({ signal }) => a.adminStats({ signal }), options);
}

export function useAgentsAdminUser(userId: string | null | undefined, options?: QueryOverrides<AgentsAdminUser>): UseQueryResult<AgentsAdminUser, ApiError> {
  const a = useEndpoints(agentsEndpoints);
  return useApiQuery(agentsKeys.adminUser(userId ?? ""), ({ signal }) => a.adminUser(userId ?? "", { signal }), options, !!userId);
}

/** Admins only; audited. */
export function useAgentsAdminSetPaused(): UseMutationResult<AgentsAdminUser, ApiError, { userId: string; paused: boolean; reason?: string }> {
  const a = useEndpoints(agentsEndpoints);
  const qc = useQueryClient();
  return useMutation<AgentsAdminUser, ApiError, { userId: string; paused: boolean; reason?: string }>({
    mutationFn: ({ userId, paused, reason }) => a.adminSetPaused(userId, paused, reason),
    onSuccess: (data) => {
      qc.setQueryData(agentsKeys.adminUser(data.user_id), data);
      void qc.invalidateQueries({ queryKey: agentsKeys.adminStats() });
    },
  });
}
