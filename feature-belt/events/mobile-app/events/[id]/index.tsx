import { Stack, useLocalSearchParams } from "expo-router";
import { EventDetailScreen } from "@/features/events/EventDetailScreen";

export default function Route() {
  const { id } = useLocalSearchParams<{ id: string }>();
  return (
    <>
      <Stack.Screen options={{ title: "Event" }} />
      <EventDetailScreen id={id} />
    </>
  );
}
