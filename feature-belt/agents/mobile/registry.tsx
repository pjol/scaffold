import { useAgentsPendingCount, useAgentsSummary } from "@scaffoldapp/shared";
import { Bot, Inbox } from "lucide-react-native";
import type { ComponentType } from "react";
import type { ResourceMenuProvider, SettingsSection } from "../registry";
import { useAgentsMenuActions } from "./AgentsMenu";

/** Settings → AI agents: agent access, and suggestions waiting for you (badge). */
export const agentsSettings: SettingsSection = {
  id: "agents",
  title: "AI agents",
  items: [
    { id: "agents", title: "Agent access", icon: Bot, href: "/settings/agents" },
    { id: "agents-suggestions", title: "Suggestions", icon: Inbox, href: "/agents/suggestions", useBadge: useAgentsPendingCount },
  ],
};

/** Every ⋮ sheet: "AI agent access" → Default · Off · Read · Suggest · Edit. */
export const agentsResourceMenu: ResourceMenuProvider = { id: "agents", useActions: useAgentsMenuActions };

/**
 * Keeps the waiting count loaded for the Settings badge (realtime refreshes it). The
 * notifications feature's foreground toast opens each suggestion.
 */
function AgentsSummaryMount(): null {
  useAgentsSummary();
  return null;
}

export const agentsSession: ComponentType = AgentsSummaryMount;
