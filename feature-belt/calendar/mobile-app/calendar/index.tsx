import { router, Stack, useLocalSearchParams } from "expo-router";
import { useEffect } from "react";
import { CalendarScreen } from "@/features/calendar/CalendarScreen";

/** The calendar. A new-event link (`/calendar?new=1&…`) opens the new-event form over it with the same parameters. */
export default function Route() {
  const { new: isNew, ...link } = useLocalSearchParams<Record<string, string>>();
  useEffect(() => {
    if (isNew !== "1") return;
    router.setParams({ new: undefined });
    router.push({ pathname: "/calendar/form", params: link });
    // Once per link: `link` is a new object each render.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [isNew]);
  return (
    <>
      <Stack.Screen options={{ title: "Calendar" }} />
      <CalendarScreen />
    </>
  );
}
