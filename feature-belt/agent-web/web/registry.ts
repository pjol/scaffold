import type { SessionMount } from "../registry";
import { AgentWebWebMcp } from "./WebMcp";

/** Registers WebMCP tools on every signed-in page (no-op where the browser has no `document.modelContext`). */
export const agentWebSession: SessionMount = AgentWebWebMcp;
