import { Stack } from "expo-router";
import { ConversationsScreen } from "@/features/assistant/ConversationsScreen";

export default function AssistantIndex() {
  return (
    <>
      <Stack.Screen options={{ title: "Assistant" }} />
      <ConversationsScreen />
    </>
  );
}
