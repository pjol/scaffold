import { Stack } from "expo-router";
import { LeaderboardSettingsScreen } from "@/features/leaderboard/LeaderboardSettingsScreen";

export default function Route() {
  return (
    <>
      <Stack.Screen options={{ title: "Leaderboard" }} />
      <LeaderboardSettingsScreen />
    </>
  );
}
