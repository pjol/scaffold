import { Bot } from "lucide-react";
import type { NavItem, ResourceMenuProvider, SessionMount, SettingsSection } from "../registry";
import { useAgentsMenuActions } from "./AgentsMenu";
import { AgentsSuggestionToasts } from "./SuggestionToasts";

/** Settings → AI agents (/settings/agents). */
export const agentsSettings: SettingsSection = { href: "/settings/agents", label: "AI agents", icon: Bot };

/** Every ⋮ menu: "AI agent access ▸ Default · Off · Read · Suggest · Edit". */
export const agentsResourceMenu: ResourceMenuProvider = { id: "agents", useActions: useAgentsMenuActions };

/** Live toast with Review and Accept when an agent suggests a change; keeps the waiting count loaded. */
export const agentsSession: SessionMount = AgentsSuggestionToasts;

/** Admin tab: agent counts and a per-person off switch. */
export const agentsAdminNav: NavItem = { href: "/admin/agents", label: "AI agents", icon: Bot };
