import type { CallOptions, HttpClient } from "../api/http";
import { seg } from "../api/query-string";
import type { AgentWebCallResult, AgentWebManifest, AgentWebTool } from "./types";

/** agent-web endpoints: the public manifest and the signed-in person's WebMCP tools. */
export function agentWebEndpoints(http: HttpClient) {
  return {
    /** `GET /v1/agent-web/manifest`: tools and the MCP endpoint, for /llms.txt. */
    manifest: (o?: CallOptions) => http.get<AgentWebManifest>("/v1/agent-web/manifest", o),
    /** `GET /v1/agent-web/tools`: read tools the person's agent scope allows. */
    tools: (o?: CallOptions) => http.get<{ items: AgentWebTool[] }>("/v1/agent-web/tools", o),
    /** `POST /v1/agent-web/tools/{name}`: run a read tool as the person (rate limited). */
    call: (name: string, args: Record<string, unknown>, o?: CallOptions) =>
      http.post<AgentWebCallResult>(`/v1/agent-web/tools/${seg(name)}`, { arguments: args }, o),
  };
}

export type AgentWebEndpoints = ReturnType<typeof agentWebEndpoints>;
