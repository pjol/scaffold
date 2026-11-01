import { Button, EmptyState, Screen, toast, useTheme } from "@scaffoldapp/ui-native";
import { router } from "expo-router";
import { CircleArrowUp } from "lucide-react-native";
import { useEffect, useState } from "react";
import { BackHandler, StyleSheet, View } from "react-native";
import { appName, appVersion, openStore } from "@/lib/version";

/**
 * Full-screen, unpassable update gate: this build is older than `/v1/config`
 * `min_app_version` for its platform. One action opens the store listing.
 */
export function UpdateRequired({ minimum }: { minimum: string }) {
  const theme = useTheme();
  const [opening, setOpening] = useState(false);
  useEffect(() => {
    // Native modals sit above this view: close them so the gate is what shows.
    if (router.canDismiss()) router.dismissAll();
    const sub = BackHandler.addEventListener("hardwareBackPress", () => true);
    return () => sub.remove();
  }, []);
  return (
    <View style={[StyleSheet.absoluteFill, { backgroundColor: theme.colors.canvas }]} accessibilityViewIsModal>
      <Screen centered>
        <EmptyState
          icon={CircleArrowUp}
          title="Update required"
          info={`This version of ${appName()} (${appVersion()}) no longer works with the service. Version ${minimum} or later is required.`}
          action={
            <Button
              loading={opening}
              onPress={async () => {
                setOpening(true);
                try {
                  await openStore();
                } catch {
                  toast.error("Couldn't open the store. Update the app from the store app.");
                } finally {
                  setOpening(false);
                }
              }}
            >
              Update
            </Button>
          }
        />
      </Screen>
    </View>
  );
}
