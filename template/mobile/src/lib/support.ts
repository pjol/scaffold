import { Linking, Platform } from "react-native";
import { appName, appVersion, buildNumber } from "./version";

/** Open the mail app to `to`, with the app version and OS under the message. Rejects when no mail app answers. */
export async function emailSupport(to: string): Promise<void> {
  const build = buildNumber();
  const subject = `${appName()} support`;
  const body = `\n\n—\n${appName()} ${appVersion()}${build ? ` (${build})` : ""} · ${Platform.OS} ${String(Platform.Version)}`;
  await Linking.openURL(`mailto:${to}?subject=${encodeURIComponent(subject)}&body=${encodeURIComponent(body)}`);
}
