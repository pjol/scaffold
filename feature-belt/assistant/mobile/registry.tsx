import { Sparkles } from "lucide-react-native";
import type { HomeEntry, SettingsSection } from "../registry";

/** Home row that opens the assistant. Promote it to a tab if the assistant is central to the app. */
export const assistantHome: HomeEntry = { id: "assistant", title: "Assistant", icon: Sparkles, href: "/assistant" };

/** Settings → Assistant: provider consent and today's usage. */
export const assistantSettings: SettingsSection = {
  id: "assistant",
  items: [{ id: "assistant", title: "Assistant", icon: Sparkles, href: "/settings/assistant" }],
};
