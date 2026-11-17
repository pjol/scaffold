import { Copy, Users } from "lucide-react";
import type { HomeTile, NavItem, SettingsSection } from "../registry";

/** Main nav: the caller's copies (the trader directory is a button there). */
export const copytradeNav: NavItem = { href: "/copy", label: "Copy trading", icon: Copy };

/** Home tile: find traders to copy (a separate name: `scaffold add` skips lines already in the registry). */
export const copytradeHomeTile: HomeTile = { href: "/leaders", label: "Find traders", icon: Users };

/** Settings → Copy trading: offer your trades to copiers. */
export const copytradeSettings: SettingsSection = { href: "/settings/copy-trading", label: "Copy trading", icon: Copy, group: "you" };

/** Admin tab: totals, the kill switch and every copy. */
export const copytradeAdminNav: NavItem = { href: "/admin/copytrade", label: "Copy trading", icon: Copy };
