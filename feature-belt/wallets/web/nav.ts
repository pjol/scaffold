import { Wallet } from "lucide-react";
import type { HomeTile, NavItem, SettingsSection } from "../registry";

/** Main nav: balances and activity. */
export const walletsNav: NavItem = { href: "/wallets", label: "Wallet", icon: Wallet };

/** Home tile (same destination as the nav entry). */
export const walletsHomeTile: HomeTile = { href: "/wallets", label: "Wallet", icon: Wallet };

/** Settings → Wallets: linked wallets (link, rename, primary, unlink). */
export const walletsSettings: SettingsSection = { href: "/settings/wallets", label: "Wallets", icon: Wallet };

/** Admin → Wallets: audited lookup by address or user, unlink. */
export const walletsAdminNav: NavItem = { href: "/admin/wallets", label: "Wallets", icon: Wallet };
