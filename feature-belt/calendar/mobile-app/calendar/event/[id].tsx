import { Stack, useLocalSearchParams } from "expo-router";
import { EventDetailScreen } from "@/features/calendar/EventDetailScreen";

export default function Route() {
  const { id, occurrence } = useLocalSearchParams<{ id: string; occurrence?: string }>();
  return (
    <>
      <Stack.Screen options={{ title: "Event" }} />
      <EventDetailScreen id={id} occurrence={occurrence ?? null} />
    </>
  );
}
