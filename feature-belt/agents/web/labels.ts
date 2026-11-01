import type { AgentsLevel, AgentsSuggestionStatus } from "@scaffoldapp/shared";
import type { BadgeTone } from "@scaffoldapp/ui-web";
import { Bot, Plug, Sparkles, type LucideIcon } from "lucide-react";

export const LEVEL_TONE: Record<AgentsLevel, BadgeTone> = { off: "neutral", read: "info", suggest: "warning", edit: "success" };

export const STATUS_LABEL: Record<AgentsSuggestionStatus, string> = {
  pending: "Waiting",
  accepting: "Applying",
  accepted: "Accepted",
  rejected: "Rejected",
  failed: "Failed",
  expired: "Expired",
};

export const STATUS_TONE: Record<AgentsSuggestionStatus, BadgeTone> = {
  pending: "warning",
  accepting: "info",
  accepted: "success",
  rejected: "neutral",
  failed: "danger",
  expired: "neutral",
};

/** MCP clients plug in; the assistant is built in. */
export function agentIcon(via: string): LucideIcon {
  if (via === "mcp") return Plug;
  if (via === "assistant") return Sparkles;
  return Bot;
}

export const VIA_LABEL: Record<string, string> = { mcp: "Connected over MCP", assistant: "In-app assistant" };
