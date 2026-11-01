import type { Page, UserStatus } from "../types";

/** `?limit=&cursor=` (limit default 20, max 100). */
export interface PageParams {
  limit?: number | undefined;
  cursor?: string | null | undefined;
}

/** Item type of a page. */
export type PageItem<P> = P extends Page<infer T> ? T : never;

/** `GET /v1/admin/users?q=&status=`. `q` matches email, name or exact id. */
export interface AdminUserListParams extends PageParams {
  q?: string | undefined;
  status?: UserStatus | undefined;
}

/** `GET /v1/admin/audit` filters (exact matches). */
export interface AdminAuditListParams extends PageParams {
  target_type?: string | undefined;
  target_id?: string | undefined;
}

/** `getNextPageParam` for react-query infinite queries over `Page<T>`. */
export function nextPageCursor<T>(page: Page<T>): string | undefined {
  return page.next_cursor ?? undefined;
}

/** Flatten loaded pages into one array (dedupes by `id` when items have one). */
export function flattenPages<T>(pages: readonly Page<T>[] | undefined): T[] {
  if (!pages) return [];
  const out: T[] = [];
  const seen = new Set<unknown>();
  for (const page of pages) {
    for (const item of page.items) {
      const id = typeof item === "object" && item !== null ? (item as { id?: unknown }).id : undefined;
      if (id !== undefined) {
        if (seen.has(id)) continue;
        seen.add(id);
      }
      out.push(item);
    }
  }
  return out;
}

/** Walk every page of a list endpoint (exports, small lists). Stops after `maxPages` (default 50). */
export async function fetchAllPages<T>(fetchPage: (cursor: string | undefined) => Promise<Page<T>>, maxPages = 50): Promise<T[]> {
  const pages: Page<T>[] = [];
  let cursor: string | undefined;
  for (let i = 0; i < maxPages; i++) {
    const page = await fetchPage(cursor);
    pages.push(page);
    if (!page.next_cursor) break;
    cursor = page.next_cursor;
  }
  return flattenPages(pages);
}
