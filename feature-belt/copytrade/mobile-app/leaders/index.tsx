import { Stack } from "expo-router";
import { LeadersScreen } from "@/features/copytrade/LeadersScreen";

export default function Route() {
  return (
    <>
      <Stack.Screen options={{ title: "Traders" }} />
      <LeadersScreen />
    </>
  );
}
