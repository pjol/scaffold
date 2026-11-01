import { Stack, useLocalSearchParams } from "expo-router";
import { JoinEventScreen } from "@/features/events/JoinEventScreen";

export default function Route() {
  const { code } = useLocalSearchParams<{ code?: string }>();
  return (
    <>
      <Stack.Screen options={{ title: "Join event" }} />
      <JoinEventScreen code={(code ?? "").trim()} />
    </>
  );
}
