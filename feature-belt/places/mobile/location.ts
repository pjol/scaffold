import type { LatLng } from "@scaffoldapp/shared";
import { toast } from "@scaffoldapp/ui-native";
import * as Location from "expo-location";
import { Alert, Linking } from "react-native";

/**
 * The device position, asking for foreground location at this moment (never at launch).
 * Denied → a toast or a Settings nudge, and null; the map and list keep working.
 */
export async function deviceLocation(): Promise<LatLng | null> {
  try {
    const perm = await Location.requestForegroundPermissionsAsync();
    if (!perm.granted) {
      if (!perm.canAskAgain) {
        Alert.alert("Location is off", "Turn it on in Settings to see places near you.", [
          { text: "Not now", style: "cancel" },
          { text: "Open Settings", onPress: () => void Linking.openSettings() },
        ]);
      }
      return null;
    }
    const last = await Location.getLastKnownPositionAsync({ maxAge: 5 * 60_000 });
    const pos = last ?? (await Location.getCurrentPositionAsync({ accuracy: Location.Accuracy.Balanced }));
    return { lat: pos.coords.latitude, lng: pos.coords.longitude };
  } catch {
    toast.error("Couldn't find your location. Try again.");
    return null;
  }
}
