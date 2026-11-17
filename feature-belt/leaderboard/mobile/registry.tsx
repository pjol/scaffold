import { Trophy } from "lucide-react-native";
import type { HomeEntry, SettingsSection } from "@/features/registry";

/** Home → Leaderboard. */
export const leaderboardHome: HomeEntry = { id: "leaderboard", title: "Leaderboard", icon: Trophy, href: "/leaderboard" };

/** Settings → Leaderboard: show your name, an alias, or hide. */
export const leaderboardSettings: SettingsSection = {
  id: "leaderboard",
  items: [{ id: "leaderboard", title: "Leaderboard", icon: Trophy, href: "/settings/leaderboard", info: "Appear under an alias, your name, or not at all." }],
};
