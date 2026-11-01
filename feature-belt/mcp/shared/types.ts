/** MCP feature DTOs: snake_case, mirroring backend/internal/mcp/keys.go. */

export type McpKeyScope = "read" | "write";

/** A personal API key's metadata (never the secret). */
export interface McpApiKey {
  id: string;
  name: string;
  /** First characters of the key, to tell keys apart. */
  prefix: string;
  scopes: McpKeyScope[];
  last_used_at: string | null;
  created_at: string;
  revoked_at: string | null;
}

/** `POST /v1/mcp/keys`: the only response that carries the secret. */
export interface McpCreatedKey {
  key: McpApiKey;
  secret: string;
}

export interface McpCreateKeyRequest {
  name: string;
  /** `["read"]` or `["read", "write"]`. */
  scopes: McpKeyScope[];
}

/** A key as staff see it. */
export interface McpAdminKey extends McpApiKey {
  user_id: string;
  user_email: string;
  revoked_by: string | null;
}

export interface McpAdminKeyListParams {
  user_id?: string;
  status?: "active" | "revoked";
  cursor?: string;
  limit?: number;
}
