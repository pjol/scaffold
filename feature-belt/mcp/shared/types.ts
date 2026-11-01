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

// ---- OAuth: the consent page and connected apps ----

export type McpOAuthScope = "read" | "write";

/** What the consent page says about a client. */
export interface McpOAuthClientInfo {
  /** Self-declared by the client; "" when it gave none. */
  name: string;
  kind: "registered" | "metadata_document";
  /** Host publishing the client's metadata document (`metadata_document` only). */
  host: string | null;
}

/** `GET /v1/mcp/oauth/request?id=`: a pending authorization request. */
export interface McpOAuthRequest {
  client: McpOAuthClientInfo;
  redirect_uri: string;
  redirect_host: string;
  /** web = https, loopback = an app on this computer, app = a native app's own scheme. */
  redirect_kind: "web" | "loopback" | "app";
  scopes: McpOAuthScope[];
  expires_at: string;
}

/** `POST /v1/mcp/oauth/decision`. `scopes` may drop write from what was requested. */
export interface McpOAuthDecisionRequest {
  id: string;
  allow: boolean;
  scopes?: McpOAuthScope[];
}

export interface McpOAuthDecision {
  redirect_url: string;
}

/** A connected app as its owner sees it: one row per client, however many devices. */
export interface McpConnection {
  /** The client; revoking it ends every grant the person gave it. */
  id: string;
  name: string;
  kind: "registered" | "metadata_document";
  host: string | null;
  scopes: McpOAuthScope[];
  /** Active authorizations (one per device or reconnect). */
  grants: number;
  connected_at: string;
  first_used_at: string | null;
  last_used_at: string | null;
}

/** One OAuth grant as staff see it. */
export interface McpAdminConnection {
  id: string;
  user_id: string;
  user_email: string;
  client_name: string;
  client_kind: "registered" | "metadata_document";
  client_host: string | null;
  scopes: McpOAuthScope[];
  created_at: string;
  first_used_at: string | null;
  last_used_at: string | null;
  expires_at: string;
  revoked_at: string | null;
  revoke_reason: "user" | "staff" | "client" | "refresh_reuse" | "code_reuse" | null;
}

export interface McpAdminConnectionListParams {
  user_id?: string;
  status?: "active" | "revoked";
  cursor?: string;
  limit?: number;
}
