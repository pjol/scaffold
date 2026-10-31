import { Flag, ShieldBan } from "lucide-react";
import type { NavItem, SettingsSection } from "../registry";

/** Admin tab: the moderation queue. */
export const moderationAdminNav: NavItem = { href: "/admin/moderation", label: "Moderation", icon: Flag };

/** Settings → Safety: blocked people and your reports. */
export const moderationSettings: SettingsSection = { href: "/settings/safety", label: "Safety", icon: ShieldBan };
