import type { DocsButtonResult } from "@scaffoldapp/shared";
import { toast } from "@scaffoldapp/ui-native";
import { router } from "expo-router";
import * as WebBrowser from "expo-web-browser";

/** Opens a link a button returned: app paths in the app, web addresses in the in-app browser. */
export function openLink(url: string): void {
  if (url.startsWith("/") && !url.startsWith("//")) {
    router.push(url as never);
    return;
  }
  if (/^https?:\/\//i.test(url)) void WebBrowser.openBrowserAsync(url);
}

/** After a button ran: report rows it added, open the first link it asked to open. */
export function openButtonResult(res: DocsButtonResult): void {
  if (res.created.length) toast.success(res.created.length === 1 ? "Added 1 row." : `Added ${res.created.length} rows.`);
  const first = res.open[0];
  if (first) openLink(first);
}

/** Where a row opens: its page, or the published page through a public link. */
export function rowHref(id: string, token?: string | null): string {
  return token ? `/p/${token}?page=${id}` : `/docs/${id}`;
}

export function openRow(id: string, token?: string | null): void {
  router.push(rowHref(id, token) as never);
}
