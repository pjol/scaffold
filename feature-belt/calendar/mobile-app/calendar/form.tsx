import { parseCalendarNewEventLink } from "@scaffoldapp/shared";
import { Stack, useLocalSearchParams } from "expo-router";
import { useMemo } from "react";
import { Platform } from "react-native";
import { HeaderClose } from "@/components/HeaderClose";
import { EventFormScreen } from "@/features/calendar/EventFormScreen";

/**
 * New event, presented as a modal: prefilled from new-event link parameters (`title`, `start`,
 * `end`, `duration`, `video=1`, `guests`; `starts_at`/`ends_at` from the day view), or edit
 * (`?id=&occurrence=`).
 */
export default function Route() {
  const params = useLocalSearchParams<Record<string, string>>();
  const { id, occurrence, starts_at, ends_at } = params;
  const draft = useMemo(() => parseCalendarNewEventLink({ ...params, new: "1", start: params.start ?? starts_at, end: params.end ?? ends_at }) ?? {}, [params, starts_at, ends_at]);
  return (
    <>
      <Stack.Screen
        options={{
          title: id ? "Edit event" : "New event",
          ...(Platform.OS === "ios" ? { presentation: "modal" as const } : { animation: "slide_from_bottom" as const }),
          headerLeft: () => <HeaderClose />,
        }}
      />
      <EventFormScreen {...(id ? { id } : { draft })} {...(occurrence ? { occurrence } : {})} />
    </>
  );
}
