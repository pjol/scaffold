import { Plug, ShieldCheck } from "lucide-react";
import type { NavItem, SettingsSection } from "../registry";

/** Settings → Connections: your accounts on other platforms, and the assistant's tools. */
export const connectorsSettings: SettingsSection = { href: "/settings/connections", label: "Connections", icon: Plug };

/** Settings → Connection policy: which platforms the active workspace allows (admins change it). */
export const connectorsPolicySettings: SettingsSection = { href: "/settings/connection-policy", label: "Connection policy", icon: ShieldCheck };

/** Admin tab: platform setup and the MCP connector catalog. */
export const connectorsAdminNav: NavItem = { href: "/admin/connectors", label: "Connectors", icon: Plug };
