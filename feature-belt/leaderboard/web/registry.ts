import { Trophy } from "lucide-react";
import type { HomeTile, NavItem, SettingsSection } from "../registry";

/** Main nav: the boards. */
export const leaderboardNav: NavItem = { href: "/leaderboard", label: "Leaderboard", icon: Trophy };

/** Home tile (a separate name: `scaffold add` skips lines already in the registry). */
export const leaderboardHomeTile: HomeTile = { href: "/leaderboard", label: "Leaderboard", icon: Trophy };

/** Settings → Leaderboard: show your name, an alias, or hide. */
export const leaderboardSettings: SettingsSection = { href: "/settings/leaderboard", label: "Leaderboard", icon: Trophy };
