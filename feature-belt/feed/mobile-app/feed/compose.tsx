import { Stack, useLocalSearchParams } from "expo-router";
import { Platform } from "react-native";
import { HeaderClose } from "@/components/HeaderClose";
import { ComposeScreen } from "@/features/feed/ComposeScreen";

/** New post, or edit one with ?id=, presented as a modal. */
export default function Route() {
  const { id } = useLocalSearchParams<{ id?: string }>();
  return (
    <>
      <Stack.Screen
        options={{
          title: id ? "Edit post" : "New post",
          ...(Platform.OS === "ios" ? { presentation: "modal" as const } : { animation: "slide_from_bottom" as const }),
          headerLeft: () => <HeaderClose />,
        }}
      />
      <ComposeScreen {...(id ? { id } : {})} />
    </>
  );
}
