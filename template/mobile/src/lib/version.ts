import type { ClientConfig } from "@scaffoldapp/shared";
import * as Application from "expo-application";
import Constants from "expo-constants";
import { Linking, Platform } from "react-native";

const FALLBACK_BUNDLE_ID = "com.example.scaffoldapp";

/** The installed build's marketing version ("1.2.0"). Sent as X-Client-Version. */
export function appVersion(): string {
  return Application.nativeApplicationVersion ?? Constants.expoConfig?.version ?? "0.0.0";
}

/** The store build number ("42"), when the binary has one. */
export function buildNumber(): string | null {
  return Application.nativeBuildVersion ?? null;
}

export function appName(): string {
  return Constants.expoConfig?.name ?? "Scaffold App";
}

/** Numeric dotted compare: -1, 0 or 1. Missing parts count as 0; suffixes ("-beta") are ignored. */
export function compareVersions(a: string, b: string): number {
  const parse = (v: string) => v.split(".").map((p) => Number.parseInt(p, 10) || 0);
  const x = parse(a);
  const y = parse(b);
  for (let i = 0; i < Math.max(x.length, y.length); i++) {
    const d = (x[i] ?? 0) - (y[i] ?? 0);
    if (d !== 0) return d < 0 ? -1 : 1;
  }
  return 0;
}

/** `min_app_version[platform]` from /v1/config when this build is older, else null. */
export function requiredUpdate(config: ClientConfig | undefined): string | null {
  if (!config || (Platform.OS !== "ios" && Platform.OS !== "android")) return null;
  const minimum = config.min_app_version?.[Platform.OS];
  return minimum && compareVersions(appVersion(), minimum) < 0 ? minimum : null;
}

/**
 * Open this app's store page. iOS looks the listing up by bundle id (no App Store id to
 * configure); Android opens Play by package name.
 */
export async function openStore(): Promise<void> {
  const id = Application.applicationId ?? FALLBACK_BUNDLE_ID;
  if (Platform.OS === "android") {
    try {
      await Linking.openURL(`market://details?id=${id}`);
    } catch {
      await Linking.openURL(`https://play.google.com/store/apps/details?id=${id}`);
    }
    return;
  }
  let url = `itms-apps://search.itunes.apple.com/WebObjects/MZSearch.woa/wa/search?media=software&term=${encodeURIComponent(appName())}`;
  try {
    const res = await fetch(`https://itunes.apple.com/lookup?bundleId=${encodeURIComponent(id)}`);
    const body = (await res.json()) as { results?: { trackViewUrl?: string }[] };
    const listing = body.results?.[0]?.trackViewUrl;
    if (listing) url = listing;
  } catch {
    // Offline or not listed yet: fall back to a store search.
  }
  await Linking.openURL(url);
}
