import { KeyRound } from "lucide-react";
import type { NavItem, SettingsSection } from "../registry";

/** Settings → API keys. */
export const mcpSettings: SettingsSection = { href: "/settings/api-keys", label: "API keys", icon: KeyRound };

/** Admin tab: keys across users, with revoke. */
export const mcpAdminNav: NavItem = { href: "/admin/api-keys", label: "API keys", icon: KeyRound };
