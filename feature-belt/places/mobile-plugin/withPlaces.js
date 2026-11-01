// Config plugin for the places feature (expo-maps + expo-location). `scaffold add`
// lists it in mobile/app.config.ts plugins (at the scaffold:plugins anchor).
//
// - iOS: Apple Maps through expo-maps needs iOS 17, so the deployment target is raised
//   with expo-build-properties (merge by hand if the app already configures it).
// - Android: Google Maps needs an API key in the manifest. It comes from
//   EXPO_PUBLIC_GOOGLE_MAPS_ANDROID_KEY at build time and is set only when non-empty
//   (an empty key corrupts the manifest).
// - Location: a when-in-use purpose string; no background location. The app asks at the
//   moment the user taps "Show my location" or "Use my location".
const { withPlugins } = require("expo/config-plugins");

module.exports = function withPlaces(config) {
  const key = (process.env.EXPO_PUBLIC_GOOGLE_MAPS_ANDROID_KEY || "").trim();
  if (key) {
    config.android = config.android || {};
    config.android.config = { ...(config.android.config || {}), googleMaps: { apiKey: key } };
  }
  const name = config.name || "this app";
  return withPlugins(config, [
    [
      "expo-location",
      {
        locationWhenInUsePermission: `Allow ${name} to use your location to show places near you.`,
        locationAlwaysAndWhenInUsePermission: false,
        locationAlwaysPermission: false,
        isIosBackgroundLocationEnabled: false,
        isAndroidBackgroundLocationEnabled: false,
        isAndroidForegroundServiceEnabled: false,
        motionUsagePermission: false,
      },
    ],
    ["expo-build-properties", { ios: { deploymentTarget: "17.0" } }],
  ]);
};
