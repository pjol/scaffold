import { Banner, toast } from "@scaffoldapp/ui-native";
import { Linking } from "react-native";
import { appName } from "@/lib/version";
import { usePushPermission } from "./lifecycle";

/**
 * Offers push on this phone at a moment the person is thinking about notifications (inbox,
 * notification settings): the OS prompt appears only after they tap "Turn on". Denied →
 * a way to Settings. Granted or unavailable → nothing.
 */
export function PushBanner() {
  const { permission, enable, busy } = usePushPermission();
  if (permission === "undetermined") {
    return (
      <Banner
        tone="info"
        title="Get notifications on this phone"
        action={{
          label: busy ? "Turning on…" : "Turn on",
          onPress: () => {
            if (busy) return;
            void enable().then(
              (p) => {
                if (p === "granted") toast.success("Notifications are on");
                else if (p === "unavailable") toast.error("This build can't receive push notifications.");
              },
              () => toast.error("Couldn't turn on notifications. Try again."),
            );
          },
        }}
      />
    );
  }
  if (permission === "denied") {
    return <Banner tone="warning" title={`Notifications are off for ${appName()}`} action={{ label: "Open Settings", onPress: () => void Linking.openSettings() }} />;
  }
  return null;
}
