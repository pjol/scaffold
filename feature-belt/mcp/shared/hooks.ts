"use client";

import { useMutation, useQueryClient, type UseMutationResult, type UseQueryResult } from "@tanstack/react-query";
import type { ApiError } from "../api/errors";
import { useEndpoints } from "../hooks/context";
import { useApiQuery, usePagedQuery, type InfiniteOverrides, type PagedQueryResult } from "../hooks/internal";
import { listKeyParams } from "../hooks/keys";
import { mcpEndpoints } from "./api";
import type {
  McpAdminConnection,
  McpAdminConnectionListParams,
  McpAdminKey,
  McpAdminKeyListParams,
  McpApiKey,
  McpConnection,
  McpCreateKeyRequest,
  McpCreatedKey,
  McpOAuthDecision,
  McpOAuthDecisionRequest,
  McpOAuthRequest,
} from "./types";

export const mcpKeys = {
  all: () => ["mcp"] as const,
  keys: () => ["mcp", "keys"] as const,
  admin: () => ["mcp", "admin", "keys"] as const,
  adminList: (params: Record<string, unknown> = {}) => ["mcp", "admin", "keys", params] as const,
  oauthRequest: (id: string) => ["mcp", "oauth", "request", id] as const,
  connections: () => ["mcp", "connections"] as const,
  adminConnections: () => ["mcp", "admin", "connections"] as const,
  adminConnectionList: (params: Record<string, unknown> = {}) => ["mcp", "admin", "connections", params] as const,
};

/** The caller's active API keys, newest first (`data.items`). */
export function useMcpKeys(options?: InfiniteOverrides<McpApiKey>): PagedQueryResult<McpApiKey> {
  const ep = useEndpoints(mcpEndpoints);
  return usePagedQuery<McpApiKey>(mcpKeys.keys(), (cursor, signal) => ep.keys(cursor ? { cursor } : {}, { signal }), options);
}

/** Create a key; the result carries the secret, shown once. */
export function useCreateMcpKey(): UseMutationResult<McpCreatedKey, ApiError, McpCreateKeyRequest> {
  const ep = useEndpoints(mcpEndpoints);
  const qc = useQueryClient();
  return useMutation<McpCreatedKey, ApiError, McpCreateKeyRequest>({
    mutationFn: (body) => ep.createKey(body),
    onSettled: () => qc.invalidateQueries({ queryKey: mcpKeys.keys() }),
  });
}

export function useRevokeMcpKey(): UseMutationResult<void, ApiError, string> {
  const ep = useEndpoints(mcpEndpoints);
  const qc = useQueryClient();
  return useMutation<void, ApiError, string>({
    mutationFn: (id) => ep.revokeKey(id),
    onSettled: () => qc.invalidateQueries({ queryKey: mcpKeys.keys() }),
  });
}

/** Admin: keys across users (filter by user id and status). */
export function useAdminMcpKeys(params: Omit<McpAdminKeyListParams, "cursor"> = {}, options?: InfiniteOverrides<McpAdminKey>): PagedQueryResult<McpAdminKey> {
  const ep = useEndpoints(mcpEndpoints);
  return usePagedQuery<McpAdminKey>(
    mcpKeys.adminList(listKeyParams(params)),
    (cursor, signal) => ep.admin.keys({ ...params, ...(cursor ? { cursor } : {}) }, { signal }),
    { placeholderData: (prev) => prev, ...options },
  );
}

/** Admin: revoke a key (audited as mcp.key.revoke). */
export function useAdminRevokeMcpKey(): UseMutationResult<void, ApiError, string> {
  const ep = useEndpoints(mcpEndpoints);
  const qc = useQueryClient();
  return useMutation<void, ApiError, string>({
    mutationFn: (id) => ep.admin.revokeKey(id),
    onSettled: () => qc.invalidateQueries({ queryKey: mcpKeys.admin() }),
  });
}

/** A pending OAuth authorization request, for the consent page. Never retried: a 404 means it expired or was used. */
export function useMcpOAuthRequest(id: string | null): UseQueryResult<McpOAuthRequest, ApiError> {
  const ep = useEndpoints(mcpEndpoints);
  return useApiQuery<McpOAuthRequest>(mcpKeys.oauthRequest(id ?? ""), ({ signal }) => ep.oauthRequest(id ?? "", { signal }), { retry: false, staleTime: Infinity }, !!id);
}

/** Allow or deny a pending request; the result is where to send the browser. */
export function useDecideMcpOAuthRequest(): UseMutationResult<McpOAuthDecision, ApiError, McpOAuthDecisionRequest> {
  const ep = useEndpoints(mcpEndpoints);
  const qc = useQueryClient();
  return useMutation<McpOAuthDecision, ApiError, McpOAuthDecisionRequest>({
    mutationFn: (body) => ep.decideOAuthRequest(body),
    onSuccess: () => qc.invalidateQueries({ queryKey: mcpKeys.connections() }),
  });
}

/** The caller's connected apps, most recently connected first (`data.items`). */
export function useMcpConnections(options?: InfiniteOverrides<McpConnection>): PagedQueryResult<McpConnection> {
  const ep = useEndpoints(mcpEndpoints);
  return usePagedQuery<McpConnection>(mcpKeys.connections(), (cursor, signal) => ep.connections(cursor ? { cursor } : {}, { signal }), options);
}

/** Disconnect an app: ends every grant and token the caller gave it. */
export function useRevokeMcpConnection(): UseMutationResult<void, ApiError, string> {
  const ep = useEndpoints(mcpEndpoints);
  const qc = useQueryClient();
  return useMutation<void, ApiError, string>({
    mutationFn: (id) => ep.revokeConnection(id),
    onSettled: () => qc.invalidateQueries({ queryKey: mcpKeys.connections() }),
  });
}

/** Admin: OAuth grants across users (filter by user id and status). */
export function useAdminMcpConnections(
  params: Omit<McpAdminConnectionListParams, "cursor"> = {},
  options?: InfiniteOverrides<McpAdminConnection>,
): PagedQueryResult<McpAdminConnection> {
  const ep = useEndpoints(mcpEndpoints);
  return usePagedQuery<McpAdminConnection>(
    mcpKeys.adminConnectionList(listKeyParams(params)),
    (cursor, signal) => ep.admin.connections({ ...params, ...(cursor ? { cursor } : {}) }, { signal }),
    { placeholderData: (prev) => prev, ...options },
  );
}

/** Admin: revoke one grant (audited as mcp.oauth.revoke). */
export function useAdminRevokeMcpConnection(): UseMutationResult<void, ApiError, string> {
  const ep = useEndpoints(mcpEndpoints);
  const qc = useQueryClient();
  return useMutation<void, ApiError, string>({
    mutationFn: (id) => ep.admin.revokeConnection(id),
    onSettled: () => qc.invalidateQueries({ queryKey: mcpKeys.adminConnections() }),
  });
}
