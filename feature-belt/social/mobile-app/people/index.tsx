import { Stack } from "expo-router";
import { PeopleScreen } from "@/features/social/PeopleScreen";

export default function Route() {
  return (
    <>
      <Stack.Screen options={{ title: "People" }} />
      <PeopleScreen />
    </>
  );
}
