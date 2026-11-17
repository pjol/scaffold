/** agent-web DTOs, mirroring backend/internal/agentweb. */

/** A tool as /llms.txt lists it. */
export interface AgentWebManifestTool {
  name: string;
  /** First sentence of the description. */
  description: string;
  read_only: boolean;
  /** Agent-scope key (the providing module, or `Tool.Feature`). */
  feature: string;
}

/** `GET /v1/agent-web/manifest` (public). */
export interface AgentWebManifest {
  api_url: string;
  /** Set when the mcp feature is installed. */
  mcp: { endpoint: string; server_card: string } | null;
  /** Whether signed-in pages register WebMCP tools. */
  webmcp: boolean;
  tools: AgentWebManifestTool[];
}

/** A read tool a browser agent may call as the signed-in person (`GET /v1/agent-web/tools`). */
export interface AgentWebTool {
  name: string;
  description: string;
  /** JSON Schema (object) for the arguments. */
  input_schema: Record<string, unknown>;
  feature: string;
}

/** `POST /v1/agent-web/tools/{name}`. */
export interface AgentWebCallResult {
  /** `suggested`: the person's agent scope turned the call into a suggestion they review. */
  status: "ok" | "suggested";
  result?: unknown;
  suggestion_id?: string;
}
