import { Stack } from "expo-router";
import { ReputationScreen } from "@/features/reputation/ReputationScreen";

export default function Route() {
  return (
    <>
      <Stack.Screen options={{ title: "Reputation" }} />
      <ReputationScreen />
    </>
  );
}
