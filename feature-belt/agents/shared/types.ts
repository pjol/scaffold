/**
 * AI agent scope: how far AI agents acting for the user (MCP clients, the in-app
 * assistant) may go, and the suggestions they leave for review. JSON mirrors
 * backend/internal/agents exactly.
 */

/** off: agents can't see it · read: read only (the default) · suggest: changes wait for you · edit: changes run. */
export type AgentsLevel = "off" | "read" | "suggest" | "edit";

export const AGENTS_LEVELS: readonly AgentsLevel[] = ["off", "read", "suggest", "edit"];

/** One feature or external service in settings. */
export interface AgentsFeatureScope {
  /** Module name ("calendar") or "connectors:<id>" for an external service. */
  feature: string;
  label: string;
  /** Its own setting; null follows the default. */
  level: AgentsLevel | null;
  /** What applies (ignoring "turn off all"). */
  effective: AgentsLevel;
  read_tools: number;
  write_tools: number;
  /** Items in it with their own setting. */
  resource_overrides: number;
  /** Services: still connected (false = a setting kept for a service no longer connected). */
  connected: boolean;
}

export interface AgentsAgentRef {
  /** "mcp" | "assistant" */
  via: string;
  client: string;
  client_key: string;
}

/** An agent that has acted for the user. */
export interface AgentsAgentInfo extends AgentsAgentRef {
  /** First seen. */
  created_at: string;
  last_seen_at: string;
  pending: number;
}

/** `GET /v1/agents/settings`. */
export interface AgentsSettings {
  default_level: AgentsLevel;
  /** You turned off all agent access (settings kept). */
  paused: boolean;
  /** Support turned it off; only they turn it back on. */
  paused_by_staff: boolean;
  features: AgentsFeatureScope[];
  resource_overrides: number;
  pending_suggestions: number;
  agents: AgentsAgentInfo[];
  /** Installed agent features: "mcp", "assistant", "connectors". */
  surfaces: string[];
}

export interface AgentsUpdateSettingsRequest {
  default_level?: AgentsLevel;
  paused?: boolean;
}

/** What a feature or item gets without its own setting. */
export interface AgentsInherited {
  level: AgentsLevel;
  from: "parent" | "feature" | "default";
  /** parent: the ancestor the level comes from. */
  kind?: string;
  id?: string;
  /** parent: its title, when you can open it. */
  title?: string;
  /** parent: "folder", "page". */
  noun?: string;
}

/** The scope of a feature (no kind/id), an external service, or one item. */
export interface AgentsScopeInfo {
  feature: string;
  feature_label: string;
  kind?: string;
  id?: string;
  /** What applies now (off while all agent access is off). */
  level: AgentsLevel;
  /** Its own setting. */
  override: AgentsLevel | null;
  inherited: AgentsInherited;
  paused: boolean;
  /** Your own access to an access resource ("view" … "manage"): agents never go past it. Null for other kinds. */
  my_level: "none" | "view" | "comment" | "edit" | "manage" | null;
}

/** A feature (kind/id omitted), a service, or one item. */
export interface AgentsScopeRef {
  feature: string;
  kind?: string | undefined;
  id?: string | undefined;
}

export interface AgentsSetScopeRequest extends AgentsScopeRef {
  /** null clears the item's or feature's own setting. */
  level: AgentsLevel | null;
  /** Kinds outside access, and services: the title to show in settings. */
  title?: string | undefined;
  /** Kinds outside access: the app path that opens it. */
  link?: string | undefined;
}

/** One item with its own setting. */
export interface AgentsResourceScope {
  id: string;
  feature: string;
  feature_label: string;
  kind: string;
  /** "folder", "page"; "item" for kinds outside access. */
  noun: string;
  resource_id: string;
  title: string;
  link: string;
  level: AgentsLevel;
  /** Passes its level to the items under it. */
  inherits: boolean;
  /** Deleted, or you can no longer open it. */
  missing: boolean;
  created_at: string;
}

export type AgentsSuggestionStatus = "pending" | "accepting" | "accepted" | "rejected" | "failed" | "expired";

export interface AgentsSuggestionError {
  code: string;
  message: string;
}

/** A change an agent left for you to accept or reject. */
export interface AgentsSuggestion {
  id: string;
  status: AgentsSuggestionStatus;
  tool: string;
  /** "Create event". */
  tool_title: string;
  feature: string;
  /** The feature's name, or the external service's. */
  feature_label: string;
  /** The outside service serving the tool ("Linear"); "" for this app's own. */
  source: string;
  summary: string;
  before: string;
  after: string;
  /** App path of the target. */
  link: string;
  target: { kind: string; id: string } | null;
  input: Record<string, unknown>;
  agent: AgentsAgentRef;
  /** The tool's output once accepted (omitted when large). */
  result: unknown;
  error: AgentsSuggestionError | null;
  created_at: string;
  expires_at: string;
  decided_at: string | null;
}

/** Statuses the list filters by; "pending" also returns ones being applied. */
export type AgentsSuggestionFilter = "pending" | "accepted" | "rejected" | "failed" | "expired";

export interface AgentsSuggestionParams {
  status?: AgentsSuggestionFilter[] | undefined;
  via?: string | undefined;
  client_key?: string | undefined;
  limit?: number | undefined;
}

/** `GET /v1/agents/summary`: badge state. */
export interface AgentsSummary {
  pending: number;
  paused: boolean;
}

/** `409 target_changed` details: the target as it is now. */
export interface AgentsTargetChanged {
  summary: string;
  before: string;
  after: string;
}

export interface AgentsBulkRequest {
  action: "accept" | "reject";
  via: string;
  client_key: string;
}

export interface AgentsBulkResult {
  accepted: number;
  rejected: number;
  failed: number;
  /** Changed since suggested: left waiting for review. */
  skipped: number;
  items: AgentsSuggestion[];
}

/** Staff: `GET /v1/admin/agents/stats`. */
export interface AgentsAdminStats {
  paused_users: number;
  staff_paused_users: number;
  pending_now: number;
  active_agents_7d: number;
  suggestions_7d: Record<"pending" | "accepted" | "rejected" | "failed" | "expired", number>;
}

/** Staff: one person's agent access. */
export interface AgentsAdminUser {
  user_id: string;
  email: string;
  display_name: string;
  default_level: AgentsLevel;
  paused: boolean;
  staff_paused: boolean;
  staff_paused_at: string | null;
  feature_overrides: number;
  resource_overrides: number;
  pending: number;
  agents: AgentsAgentInfo[];
}

declare module "../types" {
  interface RealtimeEventMap {
    "agents.suggestion": { suggestion: AgentsSuggestion };
    "agents.suggestion_updated": { suggestion: AgentsSuggestion };
    "agents.scopes_changed": Record<string, never>;
  }
}
