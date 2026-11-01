/** Connectors DTOs: snake_case, mirroring backend/internal/connectors (store.go, connections.go, catalog.go, flow.go). */

export type ConnectorAuthType = "oauth" | "bearer";

/** `disabled`: tools that change things are never offered. `off`/`on`: the starting value of each new connection's changes switch. */
export type ConnectorWritePolicy = "disabled" | "off" | "on";

export type ConnectorConnectionStatus = "active" | "needs_reauth";

/** The caller's connection to a connector. Tokens never leave the server. */
export interface ConnectorConnection {
  id: string;
  connector_id: string;
  status: ConnectorConnectionStatus;
  /** Tools that change things are offered to the assistant (each call still asks first). */
  writes_enabled: boolean;
  /** Remote tool names the user switched off. */
  disabled_tools: string[];
  scopes: string[];
  /** The last passing failure ("Couldn't reach Linear at 14:05 UTC."), or "". */
  last_error: string;
  last_error_at: string | null;
  last_used_at: string | null;
  connected_at: string;
}

/** A catalog entry (or the caller's own server) with the caller's connection. */
export interface Connector {
  id: string;
  /** Tool namespace: the assistant sees `<slug>__<tool>`. */
  slug: string;
  name: string;
  description: string;
  icon_url: string;
  auth_type: ConnectorAuthType;
  write_policy: ConnectorWritePolicy;
  /** The caller's own server (CONNECTORS_ALLOW_CUSTOM). */
  custom: boolean;
  server_host: string;
  /** False when staff turned it off: a connection stays listed to be removed, but its tools aren't offered. */
  available: boolean;
  connection: ConnectorConnection | null;
}

/** `GET /v1/connectors`: one page, plus whether the caller may add their own servers. */
export interface ConnectorList {
  items: Connector[];
  next_cursor: string | null;
  allow_custom: boolean;
}

/** `POST /v1/connectors/{id}/connect`: an authorization URL to open (OAuth), or the connection (pasted token). */
export interface ConnectorConnectStart {
  authorize_url?: string;
  connector?: Connector;
}

/** One remote tool as the settings screen lists it. */
export interface ConnectorTool {
  name: string;
  title: string;
  description: string;
  /** As this app treats it: the server's readOnlyHint for catalog connectors; false for your own servers. */
  read_only: boolean;
  /** Not switched off by the user. */
  enabled: boolean;
  /** The assistant gets it right now. */
  offered: boolean;
}

export interface ConnectorToolList {
  items: ConnectorTool[];
  next_cursor: string | null;
  fetched_at: string;
}

/** `DELETE /v1/connectors/connections/{id}`. `unsupported`: the provider has no revocation endpoint; remove access there. */
export interface ConnectorDisconnected {
  revocation: "revoked" | "pending" | "unsupported";
}

export interface ConnectorCustomRequest {
  name: string;
  server_url: string;
  auth_type: ConnectorAuthType;
}

/** A catalog entry as staff see it. The client secret is write-only. */
export interface AdminConnector {
  id: string;
  slug: string;
  name: string;
  description: string;
  icon_url: string;
  server_url: string;
  auth_type: ConnectorAuthType;
  oauth_client_id: string;
  has_client_secret: boolean;
  oauth_scopes: string;
  write_policy: ConnectorWritePolicy;
  enabled: boolean;
  connections: number;
  needs_reauth: number;
  created_at: string;
  updated_at: string;
}

/** Create (all required but slug, description, icon, client fields) or update (any subset). */
export interface AdminConnectorInput {
  name?: string;
  slug?: string;
  description?: string;
  icon_url?: string;
  server_url?: string;
  auth_type?: ConnectorAuthType;
  oauth_client_id?: string;
  /** Write-only: set a new secret. */
  oauth_client_secret?: string;
  clear_client_secret?: boolean;
  oauth_scopes?: string;
  write_policy?: ConnectorWritePolicy;
  enabled?: boolean;
}

/** `POST /v1/admin/connectors/check`: what connecting to a server will use. */
export interface ConnectorCheckResult {
  ok: boolean;
  requires_auth: boolean;
  issuer: string;
  resource: string;
  scopes: string[];
  registration: "preregistered" | "metadata_document" | "dynamic" | "needs_client_id" | "none" | "";
  revocation: boolean;
  refresh: boolean;
  error_code?: string;
  error?: string;
}

/** Codes the callback puts in `?connect_error=` on the return URL. */
export type ConnectorConnectError = "cancelled" | "expired" | "invalid_callback" | "provider_error" | "issuer_mismatch" | "exchange_failed";
