import * as Linking from "expo-linking";
import { WEB_URL } from "@/lib/env";

/**
 * A link to share for an app path: the web URL when `EXPO_PUBLIC_WEB_URL` is set (opens for
 * anyone, and in the app where universal links are configured), else the
 * `scaffoldapp://` app link.
 */
export function shareableLink(path: string, query?: Record<string, string>): string {
  if (WEB_URL) {
    const qs = query
      ? Object.entries(query)
          .map(([k, v]) => `${encodeURIComponent(k)}=${encodeURIComponent(v)}`)
          .join("&")
      : "";
    return `${WEB_URL}${path}${qs ? `?${qs}` : ""}`;
  }
  return Linking.createURL(path, query ? { queryParams: query } : undefined);
}
