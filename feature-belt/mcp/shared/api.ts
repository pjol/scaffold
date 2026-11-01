import type { CallOptions, HttpClient } from "../api/http";
import type { Page } from "../types";
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

/** Personal API keys and OAuth connected apps (both managed with the user's session, never with a key), the consent page and the admin views. */
export const mcpEndpoints = (http: HttpClient) => ({
  keys: (params: { cursor?: string; limit?: number } = {}, o?: CallOptions) =>
    http.get<Page<McpApiKey>>("/v1/mcp/keys", { ...o, query: { cursor: params.cursor, limit: params.limit } }),
  createKey: (body: McpCreateKeyRequest) => http.post<McpCreatedKey>("/v1/mcp/keys", body),
  revokeKey: (id: string) => http.delete<void>(`/v1/mcp/keys/${encodeURIComponent(id)}`),
  /** The handle travels in the query so request logs never record it. */
  oauthRequest: (id: string, o?: CallOptions) => http.get<McpOAuthRequest>("/v1/mcp/oauth/request", { ...o, query: { id } }),
  decideOAuthRequest: (body: McpOAuthDecisionRequest) => http.post<McpOAuthDecision>("/v1/mcp/oauth/decision", body),
  connections: (params: { cursor?: string; limit?: number } = {}, o?: CallOptions) =>
    http.get<Page<McpConnection>>("/v1/mcp/connections", { ...o, query: { cursor: params.cursor, limit: params.limit } }),
  revokeConnection: (id: string) => http.delete<void>(`/v1/mcp/connections/${encodeURIComponent(id)}`),
  admin: {
    keys: (params: McpAdminKeyListParams = {}, o?: CallOptions) =>
      http.get<Page<McpAdminKey>>("/v1/admin/mcp/keys", {
        ...o,
        query: { user_id: params.user_id, status: params.status, cursor: params.cursor, limit: params.limit },
      }),
    revokeKey: (id: string) => http.delete<void>(`/v1/admin/mcp/keys/${encodeURIComponent(id)}`),
    connections: (params: McpAdminConnectionListParams = {}, o?: CallOptions) =>
      http.get<Page<McpAdminConnection>>("/v1/admin/mcp/connections", {
        ...o,
        query: { user_id: params.user_id, status: params.status, cursor: params.cursor, limit: params.limit },
      }),
    revokeConnection: (id: string) => http.delete<void>(`/v1/admin/mcp/connections/${encodeURIComponent(id)}`),
  },
});

export type McpEndpoints = ReturnType<typeof mcpEndpoints>;

/** The MCP endpoint agents connect to, from the API origin. */
export function mcpEndpointUrl(apiBaseUrl: string): string {
  return `${apiBaseUrl.trim().replace(/\/+$/, "").replace(/\/v1$/, "")}/mcp`;
}
