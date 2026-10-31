import { Stack } from "expo-router";
import { CalendarFeedScreen } from "@/features/events/CalendarFeedScreen";

export default function Route() {
  return (
    <>
      <Stack.Screen options={{ title: "Calendar feed" }} />
      <CalendarFeedScreen />
    </>
  );
}
