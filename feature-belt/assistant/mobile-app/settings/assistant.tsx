import { Stack } from "expo-router";
import { AssistantSettingsScreen } from "@/features/assistant/AssistantSettingsScreen";

export default function AssistantSettings() {
  return (
    <>
      <Stack.Screen options={{ title: "Assistant" }} />
      <AssistantSettingsScreen />
    </>
  );
}
