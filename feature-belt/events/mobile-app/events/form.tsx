import { Stack, useLocalSearchParams } from "expo-router";
import { Platform } from "react-native";
import { HeaderClose } from "@/components/HeaderClose";
import { EventFormScreen } from "@/features/events/EventFormScreen";

/** Create (no id) or edit (?id=) an event, presented as a modal. */
export default function Route() {
  const { id } = useLocalSearchParams<{ id?: string }>();
  return (
    <>
      <Stack.Screen
        options={{
          title: id ? "Edit event" : "New event",
          ...(Platform.OS === "ios" ? { presentation: "modal" as const } : { animation: "slide_from_bottom" as const }),
          headerLeft: () => <HeaderClose />,
        }}
      />
      <EventFormScreen id={id} />
    </>
  );
}
