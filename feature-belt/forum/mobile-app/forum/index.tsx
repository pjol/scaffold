import { Stack } from "expo-router";
import { ForumHomeScreen } from "@/features/forum/ForumHomeScreen";

/** /forum: opened from Home. */
export default function Route() {
  return (
    <>
      <Stack.Screen options={{ title: "Forum" }} />
      <ForumHomeScreen />
    </>
  );
}
