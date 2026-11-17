import type { Metadata } from "next";
import { API_URL } from "@/lib/env";

/**
 * <head> for a published page, read on the server: its title, and whether the publisher lets search
 * engines index it (off unless they turned it on; off too when the API can't be reached).
 */
export async function docsPublicMetadata(token: string, pageId?: string): Promise<Metadata> {
  const closed: Metadata = { title: "Shared page", robots: { index: false, follow: false }, referrer: "no-referrer" };
  if (!API_URL) return closed;
  try {
    const q = pageId ? `?page=${encodeURIComponent(pageId)}` : "";
    const res = await fetch(`${API_URL}/v1/docs/public/${encodeURIComponent(token)}/meta${q}`, { cache: "no-store", signal: AbortSignal.timeout(2500) });
    if (!res.ok) return closed;
    const meta = (await res.json()) as { title?: string; indexing?: boolean };
    return { title: meta.title || "Shared page", robots: meta.indexing ? { index: true, follow: true } : { index: false, follow: false }, referrer: "no-referrer" };
  } catch {
    return closed;
  }
}
