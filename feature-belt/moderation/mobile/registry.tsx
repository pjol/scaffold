import { ShieldBan } from "lucide-react-native";
import type { SettingsSection } from "../registry";

/** Settings → Safety: blocked people and your reports. */
export const moderationSettings: SettingsSection = {
  id: "moderation",
  items: [{ id: "safety", title: "Safety", icon: ShieldBan, href: "/settings/safety", info: "People you blocked and reports you sent." }],
};
