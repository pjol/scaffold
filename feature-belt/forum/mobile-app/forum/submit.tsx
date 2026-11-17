import { Stack, useLocalSearchParams } from "expo-router";
import { Platform } from "react-native";
import { HeaderClose } from "@/components/HeaderClose";
import { SubmitScreen } from "@/features/forum/SubmitScreen";

/** New post (?community=<slug>), or edit one (?edit=<id>), as a modal. */
export default function Route() {
  const { community, edit } = useLocalSearchParams<{ community?: string; edit?: string }>();
  return (
    <>
      <Stack.Screen
        options={{
          title: edit ? "Edit post" : "New post",
          ...(Platform.OS === "ios" ? { presentation: "modal" as const } : { animation: "slide_from_bottom" as const }),
          headerLeft: () => <HeaderClose />,
        }}
      />
      <SubmitScreen {...(community ? { community } : {})} {...(edit ? { edit } : {})} />
    </>
  );
}
