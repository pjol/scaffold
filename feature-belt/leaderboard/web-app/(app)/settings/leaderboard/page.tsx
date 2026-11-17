import type { Metadata } from "next";
import { LeaderboardSettingsScreen } from "@/features/leaderboard/LeaderboardSettingsScreen";

export const metadata: Metadata = { title: "Leaderboard" };

export default function Page() {
  return <LeaderboardSettingsScreen />;
}
