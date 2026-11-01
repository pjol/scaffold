"use client";

import { useMutation, useQueryClient, type UseMutationResult } from "@tanstack/react-query";
import type { ApiError } from "../api/errors";
import { useEndpoints } from "../hooks/context";
import { usePagedQuery, type InfiniteOverrides, type PagedQueryResult } from "../hooks/internal";
import { listKeyParams } from "../hooks/keys";
import { mcpEndpoints } from "./api";
import type { McpAdminKey, McpAdminKeyListParams, McpApiKey, McpCreateKeyRequest, McpCreatedKey } from "./types";

export const mcpKeys = {
  all: () => ["mcp"] as const,
  keys: () => ["mcp", "keys"] as const,
  admin: () => ["mcp", "admin", "keys"] as const,
  adminList: (params: Record<string, unknown> = {}) => ["mcp", "admin", "keys", params] as const,
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
