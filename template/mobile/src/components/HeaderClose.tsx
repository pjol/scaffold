import { IconButton } from "@scaffoldapp/ui-native";
import { router } from "expo-router";
import { X } from "lucide-react-native";

/** Close button for modal screens (older users may not discover swipe-down). */
export function HeaderClose() {
  return <IconButton label="Close" icon={X} variant="secondary" onPress={() => (router.canGoBack() ? router.back() : router.replace("/"))} />;
}
