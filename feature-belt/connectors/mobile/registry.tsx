import { Plug } from "lucide-react-native";
import type { SettingsSection } from "../registry";

/** Settings → Connections: connected services, connect and disconnect. */
export const connectorsSettings: SettingsSection = {
  id: "connectors",
  items: [{ id: "connections", title: "Connections", icon: Plug, href: "/settings/connections" }],
};
