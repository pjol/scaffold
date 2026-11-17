import { Stack } from "expo-router";
import { LeaderboardScreen } from "@/features/leaderboard/LeaderboardScreen";

export default function Route() {
  return (
    <>
      <Stack.Screen options={{ title: "Leaderboard" }} />
      <LeaderboardScreen />
    </>
  );
}
