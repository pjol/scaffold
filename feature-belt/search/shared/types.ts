/** Search feature DTOs: snake_case, mirroring backend/internal/search and app.SearchResult. */

import type { SourceRef } from "../sources";

/** Shortest query the API searches (backend `search.MinQueryChars`). */
export const SEARCH_MIN_QUERY_CHARS = 2;

/** One hit. `kind` is module-prefixed (`chat.message`); `link` is an app path both clients open. */
export interface SearchResult {
  kind: string;
  id: string;
  title: string;
  /** Plain text around the match, never HTML. */
  snippet?: string;
  link: string;
  /** A lucide icon name (`message-square`). */
  icon?: string;
  /** 0..1, higher first. */
  score: number;
  /** Last activity. */
  at: string;
  /** The organization the hit belongs to, when it belongs to one. */
  org_id?: string;
  /** Where it lives: the org's name first when `org_id` is set, then its container ("Acme · #general"). */
  context?: string;
  /** Set on hits that come live from a connected platform account; absent for the app's own content. */
  source?: SourceRef;
}

/** Hits per source before the source filter: `app`, then `provider:<id>` per connected platform. */
export interface SearchSourceCount {
  id: string;
  provider?: string;
  count: number;
}

/** How one feature answered: `timeout` and `error` make the response partial. */
export interface SearchProviderStatus {
  name: string;
  status: "ok" | "timeout" | "error";
  count: number;
  took_ms: number;
}

/** `GET /v1/search`. */
export interface SearchResponse {
  query: string;
  items: SearchResult[];
  providers: SearchProviderStatus[];
  /** True when a feature failed or timed out; items may be missing its hits. */
  partial: boolean;
  /** Hit counts per source (the app's own and each connected platform), for filter chips. */
  sources?: SearchSourceCount[];
}

export interface SearchParams {
  q: string;
  /** Features (`chat`) or kinds (`chat.message`); empty = everything. */
  kinds?: string[];
  /** Default 20, at most 50. */
  limit?: number;
  /** Only this org's hits (and ones in no org); omit for every org. */
  orgId?: string;
  /** Only hits from one source: `app`, `connected`, `provider:<id>` or a connection id. */
  source?: string;
}

/** Hits of one kind, in result order. */
export interface SearchGroup {
  kind: string;
  label: string;
  items: SearchResult[];
}
