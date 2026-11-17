import { Stack } from "expo-router";
import { ProfileSettingsScreen } from "@/features/social/ProfileSettingsScreen";

export default function Route() {
  return (
    <>
      <Stack.Screen options={{ title: "Public profile" }} />
      <ProfileSettingsScreen />
    </>
  );
}
