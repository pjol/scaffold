import { Gauge, ShieldAlert } from "lucide-react";
import type { AccountMenuItem, HomeTile, NavItem } from "../registry";

/** App paths (screens and the backend's links use the same ones). */
export const reputationPaths = {
  me: "/reputation",
  admin: "/admin/reputation",
} as const;

/** Home tile: your reputation explained. */
export const reputationHomeTile: HomeTile = { href: reputationPaths.me, label: "Reputation", icon: Gauge };

/** Account menu → Reputation. */
export const reputationAccountMenu: AccountMenuItem = { id: "reputation", href: reputationPaths.me, label: "Reputation", icon: Gauge };

/** Admin → Reputation: the integrity flag queue. */
export const reputationAdminNav: NavItem = { href: reputationPaths.admin, label: "Reputation", icon: ShieldAlert };
