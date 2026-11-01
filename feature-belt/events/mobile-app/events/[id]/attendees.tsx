import { Stack, useLocalSearchParams } from "expo-router";
import { AttendeesScreen } from "@/features/events/AttendeesScreen";

export default function Route() {
  const { id } = useLocalSearchParams<{ id: string }>();
  return (
    <>
      <Stack.Screen options={{ title: "Who's coming" }} />
      <AttendeesScreen id={id} />
    </>
  );
}
