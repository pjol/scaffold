import * as Linking from "expo-linking";
import { WEB_URL } from "@/lib/env";

/** A link to share for an app path: the web URL when EXPO_PUBLIC_WEB_URL is set, else the app link. */
export function shareableForumLink(path: string): string {
  return WEB_URL ? `${WEB_URL}${path}` : Linking.createURL(path);
}
