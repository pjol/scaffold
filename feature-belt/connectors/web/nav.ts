import { Plug } from "lucide-react";
import type { NavItem, SettingsSection } from "../registry";

/** Settings → Connections: connect outside services for the assistant. */
export const connectorsSettings: SettingsSection = { href: "/settings/connections", label: "Connections", icon: Plug };

/** Admin tab: the connector catalog. */
export const connectorsAdminNav: NavItem = { href: "/admin/connectors", label: "Connectors", icon: Plug };
