import type { AgentsLevel, AgentsSuggestionStatus } from "@scaffoldapp/shared";
import type { BadgeTone } from "@scaffoldapp/ui-native";
import { Bot, Plug, Sparkles, type LucideIcon } from "lucide-react-native";

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

export const VIA_LABEL: Record<string, string> = { mcp: "Connected over MCP", assistant: "In-app assistant" };

export function agentIcon(via: string): LucideIcon {
  if (via === "mcp") return Plug;
  if (via === "assistant") return Sparkles;
  return Bot;
}

const rtf = new Intl.RelativeTimeFormat(undefined, { numeric: "auto" });

/** "5 minutes ago", "yesterday"; the date after 30 days. */
export function relativeTime(iso: string, now = Date.now()): string {
  const t = new Date(iso).getTime();
  if (Number.isNaN(t)) return iso;
  const s = Math.round((t - now) / 1000);
  const abs = Math.abs(s);
  if (abs < 45) return "just now";
  if (abs < 3600) return rtf.format(Math.round(s / 60), "minute");
  if (abs < 86_400) return rtf.format(Math.round(s / 3600), "hour");
  if (abs < 30 * 86_400) return rtf.format(Math.round(s / 86_400), "day");
  return new Date(iso).toLocaleDateString();
}

export function dateTime(iso: string): string {
  return new Date(iso).toLocaleString(undefined, { dateStyle: "medium", timeStyle: "short" });
}
