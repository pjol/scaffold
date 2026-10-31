import type { CallOptions, HttpClient } from "../api/http";
import type { Page } from "../types";
import type { McpAdminKey, McpAdminKeyListParams, McpApiKey, McpCreateKeyRequest, McpCreatedKey } from "./types";

/** Personal API keys (managed with the user's session, never with a key) and the admin view. */
export const mcpEndpoints = (http: HttpClient) => ({
  keys: (params: { cursor?: string; limit?: number } = {}, o?: CallOptions) =>
    http.get<Page<McpApiKey>>("/v1/mcp/keys", { ...o, query: { cursor: params.cursor, limit: params.limit } }),
  createKey: (body: McpCreateKeyRequest) => http.post<McpCreatedKey>("/v1/mcp/keys", body),
  revokeKey: (id: string) => http.delete<void>(`/v1/mcp/keys/${encodeURIComponent(id)}`),
  admin: {
    keys: (params: McpAdminKeyListParams = {}, o?: CallOptions) =>
      http.get<Page<McpAdminKey>>("/v1/admin/mcp/keys", {
        ...o,
        query: { user_id: params.user_id, status: params.status, cursor: params.cursor, limit: params.limit },
      }),
    revokeKey: (id: string) => http.delete<void>(`/v1/admin/mcp/keys/${encodeURIComponent(id)}`),
  },
});

export type McpEndpoints = ReturnType<typeof mcpEndpoints>;

/** The MCP endpoint agents connect to, from the API origin. */
export function mcpEndpointUrl(apiBaseUrl: string): string {
  return `${apiBaseUrl.trim().replace(/\/+$/, "").replace(/\/v1$/, "")}/mcp`;
}
