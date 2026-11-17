/** Who retired an item: the source stopped listing it, or staff did by hand. */
export type CatalogRetiredBy = "" | "source" | "staff";

/** One catalog entry (`GET /v1/catalog/items/{key}`). Items are never deleted, only retired. */
export interface CatalogItem {
  /** The source's stable id. */
  key: string;
  name: string;
  /** Other names it resolves by (a renamed item keeps its old name). */
  aliases: string[];
  category: string;
  subtitle: string;
  image_url: string;
  attrs: Record<string, string>;
  /** A reference price from the source (decimal string), not a live one. */
  ref_price: string | null;
  /** ISO 4217, set with `ref_price`. */
  currency: string;
  retired: boolean;
  retired_at: string | null;
  retired_by: CatalogRetiredBy;
  first_seen_at: string;
  updated_at: string;
}

/** A category with its count of active items. */
export interface CatalogCategory {
  name: string;
  count: number;
}

/** `GET /v1/catalog`. */
export interface CatalogSummary {
  version: number;
  active: number;
  retired: number;
  /** Largest first. */
  categories: CatalogCategory[];
  /** `GET /v1/catalog/index` is served: the catalog is small enough to search on the device. */
  client_index: boolean;
}

export interface CatalogSearchParams {
  q: string;
  category?: string;
  /** Include retired items. */
  retired?: boolean;
  /** Default 20, at most 50. */
  limit?: number;
}

export interface CatalogItemListParams {
  category?: string;
  retired?: boolean;
  limit?: number;
  cursor?: string;
}

export type CatalogMatch = "exact" | "alias" | "fuzzy";
export type CatalogUnresolvedReason = "unknown" | "ambiguous";

/** One name's outcome (`POST /v1/catalog/resolve`). A `fuzzy` match is a guess to confirm. */
export interface CatalogResolution {
  name: string;
  ok: boolean;
  match?: CatalogMatch;
  reason?: CatalogUnresolvedReason;
  item: CatalogItem | null;
  /** Up to 5 when unresolved: the tied items, then names starting with it, then names containing it. */
  candidates: CatalogItem[];
}

/** One item in the compact client index (`GET /v1/catalog/index`). */
export interface CatalogIndexItem {
  key: string;
  name: string;
  aliases: string[];
  category: string;
  subtitle: string;
  image_url: string;
  ref_price: string | null;
  currency: string;
  retired: boolean;
}

/** What a list row shows: a search hit from the server or from the client index. */
export type CatalogListItem = Pick<CatalogItem, "key" | "name" | "category" | "subtitle" | "image_url" | "ref_price" | "currency" | "retired">;

export interface CatalogIndex {
  version: number;
  items: CatalogIndexItem[];
}

// ---- staff ----

export type CatalogSyncTrigger = "schedule" | "staff" | "app" | "seed";
export type CatalogSyncStatus = "running" | "ok" | "failed" | "refused";

/** One sync. A refused run's counts are what it would have done. */
export interface CatalogSyncRun {
  id: string;
  trigger: CatalogSyncTrigger;
  source: string;
  /** The safety valve was skipped. */
  forced: boolean;
  status: CatalogSyncStatus;
  started_at: string;
  finished_at: string | null;
  fetched: number;
  inserted: number;
  updated: number;
  retired: number;
  restored: number;
  unchanged: number;
  skipped: number;
  error: string;
  /** The first few skip reasons. */
  notes: string[];
}

/** `GET /v1/admin/catalog/status`. */
export interface CatalogAdminStatus {
  /** null: no source is set, so nothing syncs. */
  source: { name: string; detail: string } | null;
  /** UTC hour of the nightly sync; null when it's off. */
  nightly_hour: number | null;
  next_sync_at: string | null;
  max_retire_percent: number;
  /** Stored version. */
  version: number;
  /** The copy the answering instance serves. */
  loaded: { version: number; at: string; items: number; active: number; retired: number; retired_by_staff: number };
  running: CatalogSyncRun | null;
  /** The latest finished run. */
  last: CatalogSyncRun | null;
}

export type CatalogAdminItemStatus = "all" | "active" | "retired";

export interface CatalogAdminItemListParams {
  q?: string;
  status?: CatalogAdminItemStatus;
  limit?: number;
  cursor?: string;
}
