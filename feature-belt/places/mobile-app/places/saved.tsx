import { Stack } from "expo-router";
import { SavedScreen } from "@/features/places/SavedScreen";

export default function Route() {
  return (
    <>
      <Stack.Screen options={{ title: "Your places" }} />
      <SavedScreen />
    </>
  );
}
