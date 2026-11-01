import { Stack } from "expo-router";
import { LinksScreen } from "@/features/calendar/LinksScreen";

export default function Route() {
  return (
    <>
      <Stack.Screen options={{ title: "Booking links" }} />
      <LinksScreen />
    </>
  );
}
