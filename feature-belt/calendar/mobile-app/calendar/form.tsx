import { parseCalendarNewEventLink } from "@scaffoldapp/shared";
import { Stack, useLocalSearchParams } from "expo-router";
import { useMemo } from "react";
import { Platform } from "react-native";
import { HeaderClose } from "@/components/HeaderClose";
import { EventFormScreen } from "@/features/calendar/EventFormScreen";

/**
 * New event, presented as a modal: prefilled from new-event link parameters (`title`, `start`,
 * `end`, `duration`, `video=1`, `guests`; `starts_at`/`ends_at` from the day view), or edit
 * (`?id=&occurrence=`). `event_type=out_of_office|focus` starts a time block.
 */
export default function Route() {
  const params = useLocalSearchParams<Record<string, string>>();
  const { id, occurrence, starts_at, ends_at, event_type } = params;
  const eventType = event_type === "out_of_office" || event_type === "focus" ? event_type : undefined;
  const draft = useMemo(() => parseCalendarNewEventLink({ ...params, new: "1", start: params.start ?? starts_at, end: params.end ?? ends_at }) ?? {}, [params, starts_at, ends_at]);
  return (
    <>
      <Stack.Screen
        options={{
          title: id ? "Edit event" : eventType === "out_of_office" ? "Out of office" : eventType === "focus" ? "Focus time" : "New event",
          ...(Platform.OS === "ios" ? { presentation: "modal" as const } : { animation: "slide_from_bottom" as const }),
          headerLeft: () => <HeaderClose />,
        }}
      />
      <EventFormScreen {...(id ? { id } : { draft })} {...(occurrence ? { occurrence } : {})} {...(eventType && !id ? { eventType } : {})} />
    </>
  );
}
