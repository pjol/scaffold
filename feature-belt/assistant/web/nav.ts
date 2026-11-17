import { BarChart3, Sparkles } from "lucide-react";
import type { HomeTile, NavItem, QuickAction, ResourceMenuProvider, SessionMount, SettingsSection } from "../registry";
import { useAssistantMenuActions } from "./AssistantMenu";
import { AssistantPanel } from "./AssistantPanel";

/** Main navigation. */
export const assistantNav: NavItem = { href: "/assistant", label: "Assistant", icon: Sparkles };

/** Home tile (its own constant: registry wiring skips a line that already exists). */
export const assistantHomeTile: HomeTile = { href: "/assistant", label: "Assistant", icon: Sparkles };

/** Admin tab: daily usage and estimated cost. */
export const assistantAdminNav: NavItem = { href: "/admin/assistant", label: "AI usage", icon: BarChart3 };

/** Settings → Assistant: provider consent and today's usage. */
export const assistantSettings: SettingsSection = { href: "/settings/assistant", label: "Assistant", icon: Sparkles };

/** Every ⋮ menu on a resource the assistant can read: Summarise and Ask about this. */
export const assistantResourceMenu: ResourceMenuProvider = { id: "assistant", useActions: useAssistantMenuActions };

/** The assistant side panel those menu rows open. */
export const assistantPanelMount: SessionMount = AssistantPanel;

/** ⌘K: "Ask the assistant: …" with what was typed, and a new conversation. */
export const assistantAskAction: QuickAction = {
  id: "assistant.ask",
  label: "Ask the assistant",
  icon: Sparkles,
  withQuery: true,
  run: ({ query, navigate }) => navigate(`/assistant/new?q=${encodeURIComponent(query)}`),
};
export const assistantNewAction: QuickAction = { id: "assistant.new", label: "New assistant conversation", icon: Sparkles, keywords: ["ai", "chat", "ask"], href: "/assistant/new" };
