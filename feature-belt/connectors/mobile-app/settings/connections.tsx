import { Stack } from "expo-router";
import { ConnectionsScreen } from "@/features/connectors/ConnectionsScreen";

export default function ConnectionsSettings() {
  return (
    <>
      <Stack.Screen options={{ title: "Connections" }} />
      <ConnectionsScreen />
    </>
  );
}
