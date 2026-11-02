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

// ---- connected platforms (connectors 2.0) ------------------------------------------------

/** A platform account's health. `blocked`: the workspace's policy doesn't allow it. */
export type ConnectorPlatformStatus = "active" | "needs_reauth" | "blocked";

/** A scope that grants more than its name suggests (GitHub's `repo`). */
export interface ConnectorScopeNote {
  scope: string;
  note: string;
}

/** One capability a platform offers here, declared by an installed feature. */
export interface ConnectorPlatformCapability {
  /** `<module>.<provider>.<what>`: files.google.drive. */
  key: string;
  module: string;
  /** "Google Drive in Files". */
  label: string;
  /** The platform's OAuth scopes it asks for. */
  scopes: string[];
  scope_notes: ConnectorScopeNote[];
  /** It can change data on the platform. */
  write: boolean;
  policy: "allowed" | "blocked";
  blocked_reason?: string;
}

/** A capability on one account: on, off, needs_grant (on, but the scopes still need consent), blocked (policy). */
export type ConnectorCapabilityState = "on" | "off" | "needs_grant" | "blocked";

/** One of your accounts on a platform. Tokens never leave the server. */
export interface ConnectorPlatformConnection {
  id: string;
  provider: string;
  /** `oauth`: signed in · `token`: a personal or private token. */
  kind: "oauth" | "token";
  /** sam@acme.com, acme-org, Acme. */
  account: string;
  account_id: string;
  /** GitLab instance or Salesforce org host, when it isn't the default. */
  host?: string;
  status: ConnectorPlatformStatus;
  scopes: string[];
  capabilities: { key: string; state: ConnectorCapabilityState }[];
  /** The AI agent access key for this account (`connectors:<id>`): pass it as the ⋮ menu's feature. */
  agent_scope: string;
  last_error: string;
  last_error_at: string | null;
  last_used_at: string | null;
  connected_at: string;
}

export interface ConnectorPlatformToken {
  /** "Personal access token". */
  label: string;
  /** Where people create one. */
  help_url: string;
  /** GitLab: the token may be for any instance. */
  base_url_editable: boolean;
}

/** A workspace's decision for one platform. */
export interface ConnectorPlatformPolicy {
  allowed: boolean;
  allow_writes: boolean;
  denied_capabilities: string[];
  /** The workspace decided on this platform itself (else the default applies). */
  explicit: boolean;
}

/** One platform with your accounts on it. */
export interface ConnectorPlatform {
  id: string;
  name: string;
  /** Sign-in is set up on this server. */
  oauth: boolean;
  /** Personal tokens, where the platform has them. */
  token: ConnectorPlatformToken | null;
  /** GitLab: the instance sign-in uses. */
  base_url?: string;
  /** Disconnecting revokes access at the platform. */
  revocable: boolean;
  policy: ConnectorPlatformPolicy;
  capabilities: ConnectorPlatformCapability[];
  connections: ConnectorPlatformConnection[];
}

/** `GET /v1/connectors/platforms?org_id=`. */
export interface ConnectorPlatformList {
  items: ConnectorPlatform[];
  next_cursor: string | null;
  org_id: string | null;
}

/** A connect or capability change: the connection, or an authorization URL to open for consent. */
export interface ConnectorPlatformChange {
  connection?: ConnectorPlatformConnection;
  authorize_url?: string;
}

/** `POST /v1/connectors/platforms/finish`. */
export interface ConnectorPlatformFinished {
  connection: ConnectorPlatformConnection;
  /** The capability this sign-in was for ("" when none). */
  capability: string;
  /** False when part of the consent was declined. */
  capability_enabled: boolean;
  /** A different account than the one being extended came back; it was added as its own account. */
  other_account: boolean;
  /** App path to go back to. */
  return_to: string;
}

/** `manual`: a personal token; revoke it on the platform if nothing else uses it. */
export interface ConnectorPlatformDisconnected {
  revocation: "revoked" | "pending" | "unsupported" | "manual";
}

export interface ConnectorPlatformConnectInput {
  client?: "web" | "mobile";
  capability?: string;
  /** Reconnect, or grant more on, this account. */
  connection_id?: string;
  return_to?: string;
  org_id?: string;
}

export interface ConnectorPlatformTokenInput {
  token: string;
  /** GitLab self-managed: https://gitlab.example.com. */
  base_url?: string;
  capability?: string;
  org_id?: string;
}

/** One platform in the workspace policy screen. */
export interface ConnectorPolicyPlatform {
  id: string;
  name: string;
  /** Set up on this server (sign-in or personal tokens). */
  available: boolean;
  policy: ConnectorPlatformPolicy;
  capabilities: ConnectorPlatformCapability[];
  /** Members' connected accounts (a count only). */
  connections: number;
}

/** `GET /v1/connectors/orgs/{orgID}/policy`. */
export interface ConnectorPolicyView {
  org_id: string;
  can_edit: boolean;
  default: ConnectorPlatformPolicy;
  platforms: ConnectorPolicyPlatform[];
}

export interface ConnectorPolicyInput {
  allowed?: boolean;
  allow_writes?: boolean;
  denied_capabilities?: string[];
}

/** One platform's setup on this server, for staff. */
export interface ConnectorPlatformSetup {
  id: string;
  name: string;
  oauth: boolean;
  /** Env vars still to set for sign-in. */
  missing: string[];
  tokens: boolean;
  base_url?: string;
  /** Register this with the platform's OAuth app. */
  redirect_uri: string;
  register_url: string;
  scopes: string[];
  capabilities: number;
  connections: number;
  revocation: boolean;
  webhook_path?: string;
  webhook_ready: boolean;
  dev_override?: string;
}
