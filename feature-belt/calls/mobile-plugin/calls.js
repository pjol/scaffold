// Config plugin for the calls feature: LiveKit's native WebRTC on iOS and Android.
// `scaffold add` lists it in mobile/app.config.ts plugins (at the scaffold:plugins anchor).
// Native code: dev builds only (Expo Go can't load WebRTC); rebuild the dev client after
// installing (`pnpm --filter <app>/mobile exec expo run:ios`).
//
// - @livekit/react-native-expo-plugin: registers LiveKit in the native app delegates.
// - @config-plugins/react-native-webrtc: camera/microphone permissions and Android setup.
// - Purpose strings: the camera string covers both photos people share (the template's
//   wording) and video calls; the microphone string is for calls only.
// - The template blocks RECORD_AUDIO on Android and expo-image-picker drops the iOS
//   microphone string (`microphonePermission: false`). Calls need both, so this plugin
//   lifts them, and re-applies them after every other plugin has run (a mod that edits
//   the result of the rest of the chain) so plugin order in app.config.ts doesn't matter.
// - iOS background audio, so a call keeps its sound when the app goes to the background.
const { withPlugins, withBaseMod } = require("expo/config-plugins");

const ANDROID_PERMISSIONS = [
  "android.permission.CAMERA",
  "android.permission.RECORD_AUDIO",
  "android.permission.MODIFY_AUDIO_SETTINGS",
  "android.permission.BLUETOOTH_CONNECT",
  "android.permission.ACCESS_NETWORK_STATE",
  "android.permission.INTERNET",
  "android.permission.WAKE_LOCK",
];

/** Run `edit` on a mod's result after every other plugin's edit of it. */
function withFinalMod(config, platform, mod, edit) {
  return withBaseMod(config, {
    platform,
    mod,
    isProvider: false,
    async action({ modRequest: { nextMod, ...modRequest }, modResults, ...rest }) {
      const results = await nextMod({ ...rest, modResults, modRequest });
      results.modResults = edit(results.modResults);
      return results;
    },
  });
}

module.exports = function withCalls(config, props = {}) {
  const name = config.name || "this app";
  const camera = props.cameraPermission || `Allow ${name} to use your camera to take photos you choose to share and to show you in video calls.`;
  const microphone = props.microphonePermission || `Allow ${name} to use your microphone in calls.`;

  config.android = config.android || {};
  config.android.blockedPermissions = (config.android.blockedPermissions || []).filter((p) => !ANDROID_PERMISSIONS.includes(p));
  config.android.permissions = Array.from(new Set([...(config.android.permissions || []), ...ANDROID_PERMISSIONS]));

  config = withPlugins(config, [
    ["@livekit/react-native-expo-plugin", { android: { audioType: "communication" } }],
    ["@config-plugins/react-native-webrtc", { cameraPermission: camera, microphonePermission: microphone }],
  ]);

  config = withFinalMod(config, "ios", "infoPlist", (plist) => {
    plist.NSCameraUsageDescription = camera;
    plist.NSMicrophoneUsageDescription = microphone;
    plist.UIBackgroundModes = Array.from(new Set([...(plist.UIBackgroundModes || []), "audio"]));
    return plist;
  });

  config = withFinalMod(config, "android", "manifest", (manifest) => {
    const m = manifest.manifest;
    const perms = m["uses-permission"] || [];
    // Drop tools:node="remove" blocks other plugins added for permissions calls need.
    m["uses-permission"] = perms.filter((p) => !(ANDROID_PERMISSIONS.includes(p.$["android:name"]) && p.$["tools:node"] === "remove"));
    for (const name of ANDROID_PERMISSIONS) {
      if (!m["uses-permission"].some((p) => p.$["android:name"] === name)) m["uses-permission"].push({ $: { "android:name": name } });
    }
    return manifest;
  });
  return config;
};
