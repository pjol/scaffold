import { Stack } from "expo-router";
import { LeaderSettingsScreen } from "@/features/copytrade/LeaderSettingsScreen";

export default function Route() {
  return (
    <>
      <Stack.Screen options={{ title: "Copy trading" }} />
      <LeaderSettingsScreen />
    </>
  );
}
