import { BadgeCheck } from "lucide-react";
import type { NavItem, SettingsSection } from "@/features/registry";

/** Settings → Verification. */
export const identitySettings: SettingsSection = { href: "/settings/identity", label: "Verification", icon: BadgeCheck };
/** Admin tab: the review queue. */
export const identityAdminNav: NavItem = { href: "/admin/identity", label: "Verification", icon: BadgeCheck };
