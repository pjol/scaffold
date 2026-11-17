"use client";

import type { UseQueryResult } from "@tanstack/react-query";
import type { ApiError } from "../api/errors";
import { useEndpoints } from "../hooks/context";
import { useApiQuery, type QueryOverrides } from "../hooks/internal";
import { agentWebEndpoints } from "./api";
import type { AgentWebTool } from "./types";

export const agentWebKeys = {
  all: () => ["agent-web"] as const,
  tools: () => ["agent-web", "tools"] as const,
};

/** The read tools a browser agent may call as the signed-in person (cached 5 min). */
export function useAgentWebTools(options?: QueryOverrides<{ items: AgentWebTool[] }>): UseQueryResult<{ items: AgentWebTool[] }, ApiError> {
  const a = useEndpoints(agentWebEndpoints);
  return useApiQuery(agentWebKeys.tools(), ({ signal }) => a.tools({ signal }), { staleTime: 5 * 60_000, ...options });
}

/** The endpoints, for calling a tool from a WebMCP handler (outside React Query). */
export function useAgentWebEndpoints() {
  return useEndpoints(agentWebEndpoints);
}
