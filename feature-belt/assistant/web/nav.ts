import { BarChart3, Sparkles } from "lucide-react";
import type { HomeTile, NavItem, SettingsSection } from "../registry";

/** Main navigation. */
export const assistantNav: NavItem = { href: "/assistant", label: "Assistant", icon: Sparkles };

/** Home tile (its own constant: registry wiring skips a line that already exists). */
export const assistantHomeTile: HomeTile = { href: "/assistant", label: "Assistant", icon: Sparkles };

/** Admin tab: daily usage and estimated cost. */
export const assistantAdminNav: NavItem = { href: "/admin/assistant", label: "AI usage", icon: BarChart3 };

/** Settings → Assistant: provider consent and today's usage. */
export const assistantSettings: SettingsSection = { href: "/settings/assistant", label: "Assistant", icon: Sparkles };
