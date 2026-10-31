import { Stack } from "expo-router";
import { EventsScreen } from "@/features/events/EventsScreen";

export default function Route() {
  return (
    <>
      <Stack.Screen options={{ title: "Events" }} />
      <EventsScreen />
    </>
  );
}
