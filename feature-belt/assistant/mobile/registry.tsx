import { Sparkles } from "lucide-react-native";
import type { HomeEntry, ResourceMenuProvider, SettingsSection } from "../registry";
import { useAssistantMenuActions } from "./AssistantMenu";

/** Home row that opens the assistant. Promote it to a tab if the assistant is central to the app. */
export const assistantHome: HomeEntry = { id: "assistant", title: "Assistant", icon: Sparkles, href: "/assistant" };

/** Settings → Assistant: provider consent and today's usage. */
export const assistantSettings: SettingsSection = {
  id: "assistant",
  items: [{ id: "assistant", title: "Assistant", icon: Sparkles, href: "/settings/assistant" }],
};

/** Every ⋮ sheet on a resource the assistant can read: Summarise and Ask about this. */
export const assistantResourceMenu: ResourceMenuProvider = { id: "assistant", useActions: useAssistantMenuActions };
