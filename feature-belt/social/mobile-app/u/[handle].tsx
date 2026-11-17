import type { SocialProfileTab } from "@scaffoldapp/shared";
import { Stack, useLocalSearchParams } from "expo-router";
import { ProfileScreen } from "@/features/social/ProfileScreen";

/** /u/<handle>[?tab=followers|following]: the path notifications and activity link to ("me" opens your own). */
export default function Route() {
  const { handle, tab } = useLocalSearchParams<{ handle: string; tab?: string }>();
  const initial: SocialProfileTab = tab === "followers" || tab === "following" ? tab : "activity";
  return (
    <>
      <Stack.Screen options={{ title: handle === "me" ? "Your profile" : `@${handle}` }} />
      <ProfileScreen handle={handle} initialTab={initial} />
    </>
  );
}
