import { Stack, useLocalSearchParams } from "expo-router";
import { ConnectedEventScreen } from "@/features/calendar/ConnectedEventScreen";

/** An event from a connected calendar: /calendar/connected?connection_id=&calendar_id=&event_id= */
export default function Route() {
  const { connection_id, calendar_id, event_id } = useLocalSearchParams<{ connection_id: string; calendar_id: string; event_id: string }>();
  return (
    <>
      <Stack.Screen options={{ title: "Event" }} />
      <ConnectedEventScreen eventRef={{ connection_id: connection_id ?? "", calendar_id: calendar_id ?? "", event_id: event_id ?? "" }} />
    </>
  );
}
