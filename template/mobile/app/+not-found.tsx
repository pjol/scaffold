import { Button, EmptyState, Screen } from "@scaffoldapp/ui-native";
import { router } from "expo-router";
import { SearchX } from "lucide-react-native";

export default function NotFoundScreen() {
  return (
    <Screen edges={["bottom"]} centered>
      <EmptyState
        icon={SearchX}
        title="This page doesn't exist"
        action={<Button onPress={() => router.replace("/")}>Go home</Button>}
      />
    </Screen>
  );
}
