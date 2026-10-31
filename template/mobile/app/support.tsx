import { Screen } from "@scaffoldapp/ui-native";
import { SupportOptions } from "@/components/SupportOptions";

/** Settings → Support: the support email (SUPPORT_EMAIL via /v1/config) and features' support links. */
export default function SupportScreen() {
  return (
    <Screen edges={["bottom"]}>
      <SupportOptions />
    </Screen>
  );
}
