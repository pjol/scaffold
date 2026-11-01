import { KeyRound, Plug } from "lucide-react";
import type { NavItem, SettingsSection } from "../registry";

/** Settings → Connected apps: agents connected by signing in (OAuth). */
export const mcpConnectedAppsSettings: SettingsSection = { href: "/settings/connected-apps", label: "Connected apps", icon: Plug };

/** Settings → API keys. */
export const mcpSettings: SettingsSection = { href: "/settings/api-keys", label: "API keys", icon: KeyRound };

/** Admin tab: OAuth grants across users, with revoke. */
export const mcpConnectionsAdminNav: NavItem = { href: "/admin/connected-apps", label: "Connected apps", icon: Plug };

/** Admin tab: keys across users, with revoke. */
export const mcpAdminNav: NavItem = { href: "/admin/api-keys", label: "API keys", icon: KeyRound };
