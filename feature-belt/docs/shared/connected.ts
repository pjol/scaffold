"use client";

/**
 * Connected Notion pages and Google Docs in Docs: sources, browse trees, search, pages
 * edited through the same `DocsPageSession` as the app's own (block operations go to the
 * platform through the server, which keeps the editor's block ids stable), databases,
 * comments and the bridges (copy into Docs, connected-page blocks). Everything is read
 * live with the person's own connection; nothing here is shared between people.
 */
import { useInfiniteQuery, useMutation, useQueryClient, type QueryClient, type UseMutationResult, type UseQueryResult } from "@tanstack/react-query";
import { useEffect, useRef, useState, useSyncExternalStore } from "react";
import type { ApiError } from "../api/errors";
import { useEndpoints } from "../hooks/context";
import { useApiQuery, useDebouncedValue, usePagedQuery, type PagedQueryResult } from "../hooks/internal";
import { generateId } from "../utils/id";
import { docsEndpoints } from "./api";
import { DocsPageSession, type DocsSessionSnapshot } from "./session";
import type {
  DocsConnectedComment,
  DocsConnectedItem,
  DocsConnectedPageDetail,
  DocsConnectedSources,
  DocsPage,
  DocsRefs,
  DocsResolvedLink,
  DocsRow,
  DocsRowPage,
  DocsRowsParams,
  DocsSpan,
  DocsValue,
} from "./types";

/** Query keys for connected content (all under `docsKeys.all()`). */
export const docsConnectedKeys = {
  all: () => ["docs", "connected"] as const,
  sources: (orgId: string) => ["docs", "connected", "sources", orgId] as const,
  tree: (connId: string, parent: string, section: string) => ["docs", "connected", "tree", connId, parent, section] as const,
  search: (connId: string, q: string) => ["docs", "connected", "search", connId, q] as const,
  page: (connId: string, ref: string) => ["docs", "connected", "page", connId, ref] as const,
  rowsAll: (connId: string, dbRef: string) => ["docs", "connected", "rows", connId, dbRef] as const,
  rows: (connId: string, dbRef: string, params: DocsRowsParams) => ["docs", "connected", "rows", connId, dbRef, params] as const,
  comments: (connId: string, ref: string) => ["docs", "connected", "comments", connId, ref] as const,
  resolve: (provider: string, account: string, ref: string) => ["docs", "connected", "resolve", provider, account, ref] as const,
};

function retryUnlessClientError(count: number, error: ApiError): boolean {
  return error.status >= 500 || error.status === 0 ? count < 2 : false;
}

/** The app's own pages, the connected accounts (Notion, Google Docs) and what could still be connected. */
export function useDocsSources(orgId: string | null | undefined): UseQueryResult<DocsConnectedSources, ApiError> {
  const d = useEndpoints(docsEndpoints);
  return useApiQuery(docsConnectedKeys.sources(orgId ?? ""), ({ signal }) => d.sources(orgId, { signal }), { staleTime: 60_000, retry: retryUnlessClientError }, !!orgId);
}

/** A level of a connection's tree (Notion: shared pages, then subpages; Google: recent or shared with me). */
export function useDocsConnectedTree(
  connId: string | null | undefined,
  opts: { parent?: string; section?: "recent" | "shared"; orgId?: string | null; enabled?: boolean } = {},
): PagedQueryResult<DocsConnectedItem> {
  const d = useEndpoints(docsEndpoints);
  const { parent = "", section, orgId, enabled = true } = opts;
  return usePagedQuery(
    docsConnectedKeys.tree(connId ?? "", parent, section ?? ""),
    (cursor, signal) => d.connectedTree(connId!, { ...(parent ? { parent } : {}), ...(section ? { section } : {}), ...(cursor ? { cursor } : {}), orgId }, { signal }),
    { staleTime: 60_000, retry: retryUnlessClientError },
    !!connId && enabled,
  );
}

/** Search within one connection (debounced; empty q is idle). */
export function useDocsConnectedSearch(connId: string | null | undefined, q: string, orgId?: string | null): UseQueryResult<DocsConnectedItem[], ApiError> {
  const d = useEndpoints(docsEndpoints);
  const query = useDebouncedValue(q.trim(), 300);
  return useApiQuery(
    docsConnectedKeys.search(connId ?? "", query),
    async ({ signal }) => (await d.connectedSearch(connId!, query, { orgId }, { signal })).items,
    { staleTime: 30_000, retry: retryUnlessClientError, placeholderData: (prev) => prev },
    !!connId && query.length > 0,
  );
}

/** A connected page or database. Loaded once per open (each load starts an editing session); reload with `refetch`. */
export function useDocsConnectedPage(connId: string | null | undefined, ref: string | null | undefined, orgId?: string | null): UseQueryResult<DocsConnectedPageDetail, ApiError> {
  const d = useEndpoints(docsEndpoints);
  return useApiQuery(
    docsConnectedKeys.page(connId ?? "", ref ?? ""),
    ({ signal }) => d.connectedPage(connId!, ref!, orgId, { signal }),
    { staleTime: Infinity, gcTime: 60_000, refetchOnWindowFocus: false, refetchOnReconnect: false, retry: retryUnlessClientError },
    !!connId && !!ref,
  );
}

const emptySnapshot: DocsSessionSnapshot = {
  blocks: [],
  byId: new Map(),
  children: new Map(),
  version: 0,
  saveState: "saved",
  error: null,
  canUndo: false,
  canRedo: false,
  remoteChanged: new Set(),
  revision: 0,
};
const noopSubscribe = () => () => {};
const emptyGet = () => emptySnapshot;

/**
 * The editing session of a connected page: the same `DocsPageSession` the app's pages use,
 * with operations sent to the platform through the server (edits are written to Notion or
 * Google Docs as they're saved). One session per load (`session_id`); there are no live
 * events from the platform, so a change made there is caught when saving ("changed_on_platform").
 */
export function useDocsConnectedPageSession<S = unknown>(
  detail: DocsConnectedPageDetail | undefined,
  orgId: string | null | undefined,
  onRejected?: (message: string, code: string) => void,
): { session: DocsPageSession<S> | null; snapshot: DocsSessionSnapshot } {
  const d = useEndpoints(docsEndpoints);
  const qc = useQueryClient();
  const [session, setSession] = useState<DocsPageSession<S> | null>(null);
  const rejected = useRef(onRejected);
  rejected.current = onRejected;
  const sessionId = detail?.session_id;
  useEffect(() => {
    if (!detail || !sessionId) return undefined;
    const { connection_id: connId, ref } = detail;
    const clientId = generateId();
    const s = new DocsPageSession<S>({
      pageId: detail.page.id,
      clientId,
      blocks: detail.blocks,
      version: detail.page.version,
      // Platform writes are slower than ours and rate-limited: batch typing longer.
      debounceMs: 900,
      transport: {
        send: (ops) => d.connectedOps(connId, ref, { session_id: sessionId, client_id: clientId, ops }, orgId),
        sync: () => d.connectedSync(connId, ref, sessionId, orgId),
      },
      onPage: (page) =>
        qc.setQueryData<DocsConnectedPageDetail>(docsConnectedKeys.page(connId, ref), (old) => (old ? { ...old, page: { ...old.page, title: page.title, icon: page.icon } } : old)),
      onRejected: (m, c) => rejected.current?.(m, c),
    });
    setSession(s);
    return () => {
      s.dispose();
      setSession(null);
    };
    // One session per load.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [sessionId, d, qc]);
  const snapshot = useSyncExternalStore(session ? session.subscribe : noopSubscribe, session ? session.getSnapshot : emptyGet, emptyGet);
  return { session, snapshot };
}

/** Rename a connected page on the platform (optimistic). */
export function useDocsConnectedRename(connId: string, ref: string): UseMutationResult<{ title: string }, ApiError, string, { prev: DocsConnectedPageDetail | undefined }> {
  const d = useEndpoints(docsEndpoints);
  const qc = useQueryClient();
  const key = docsConnectedKeys.page(connId, ref);
  return useMutation({
    mutationFn: (title) => d.connectedRename(connId, ref, title),
    onMutate: (title) => {
      const prev = qc.getQueryData<DocsConnectedPageDetail>(key);
      if (prev) qc.setQueryData<DocsConnectedPageDetail>(key, { ...prev, page: { ...prev.page, title } });
      return { prev };
    },
    onError: (_e, _v, ctx) => {
      if (ctx?.prev) qc.setQueryData(key, ctx.prev);
    },
    onSuccess: () => void qc.invalidateQueries({ queryKey: ["docs", "connected", "tree", connId] }),
  });
}

// ---- databases ---------------------------------------------------------------------

export interface DocsConnectedRowsResult {
  rows: DocsRow[];
  refs: DocsRefs;
}

/** A connected database's rows, paged, with the view's filters, sorts and search (translated to the platform's query). */
export function useDocsConnectedRows(connId: string | null | undefined, dbRef: string | null | undefined, params: DocsRowsParams, enabled = true) {
  const d = useEndpoints(docsEndpoints);
  return useInfiniteQuery<DocsRowPage, ApiError, DocsConnectedRowsResult, readonly unknown[], string | undefined>({
    queryKey: docsConnectedKeys.rows(connId ?? "", dbRef ?? "", params),
    queryFn: ({ pageParam, signal }) => d.connectedRows(connId!, dbRef!, { ...params, ...(pageParam ? { cursor: pageParam } : {}) }, { signal }),
    initialPageParam: undefined,
    getNextPageParam: (last) => last.next_cursor ?? undefined,
    select: (data) => {
      const refs: DocsRefs = { users: {}, pages: {} };
      for (const p of data.pages) {
        Object.assign(refs.users, p.refs.users);
        Object.assign(refs.pages, p.refs.pages);
      }
      return { rows: data.pages.flatMap((p) => p.items), refs };
    },
    enabled: !!connId && !!dbRef && enabled,
    staleTime: 15_000,
    retry: retryUnlessClientError,
    placeholderData: (prev) => prev,
  });
}

type RowsCache = { pages: DocsRowPage[]; pageParams: unknown[] };

function patchConnectedRows(qc: QueryClient, connId: string, dbRef: string, fn: (row: DocsRow) => DocsRow) {
  for (const [key, data] of qc.getQueriesData<RowsCache>({ queryKey: docsConnectedKeys.rowsAll(connId, dbRef) })) {
    if (!data) continue;
    qc.setQueryData<RowsCache>(key, { ...data, pages: data.pages.map((p) => ({ ...p, items: p.items.map(fn) })) });
  }
}

/** Set a connected row's title or property values on the platform (optimistic in every cached view). */
export function useDocsConnectedSetRow(connId: string, dbRef: string): UseMutationResult<DocsRow, ApiError, { rowRef: string; rowId: string; title?: string; values?: Record<string, DocsValue> }> {
  const d = useEndpoints(docsEndpoints);
  const qc = useQueryClient();
  return useMutation({
    mutationFn: ({ rowRef, title, values }) => d.connectedSetRow(connId, rowRef, { ...(title !== undefined ? { title } : {}), ...(values ? { values } : {}) }),
    onMutate: ({ rowId, title, values }) => {
      patchConnectedRows(qc, connId, dbRef, (r) => (r.id === rowId ? { ...r, ...(title !== undefined ? { title } : {}), props: { ...r.props, ...(values ?? {}) } } : r));
    },
    onSuccess: (row) => patchConnectedRows(qc, connId, dbRef, (r) => (r.id === row.id ? { ...row, position: r.position } : r)),
    onError: () => void qc.invalidateQueries({ queryKey: docsConnectedKeys.rowsAll(connId, dbRef) }),
  });
}

/** Add a row (a page in the database) on the platform. */
export function useDocsConnectedCreateRow(connId: string, dbRef: string): UseMutationResult<DocsRow, ApiError, { title?: string; values?: Record<string, DocsValue> }> {
  const d = useEndpoints(docsEndpoints);
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (body) => d.connectedCreateRow(connId, dbRef, body),
    onSuccess: () => void qc.invalidateQueries({ queryKey: docsConnectedKeys.rowsAll(connId, dbRef) }),
  });
}

// ---- comments ----------------------------------------------------------------------

export function useDocsConnectedComments(connId: string | null | undefined, ref: string | null | undefined, enabled = true): UseQueryResult<DocsConnectedComment[], ApiError> {
  const d = useEndpoints(docsEndpoints);
  return useApiQuery(
    docsConnectedKeys.comments(connId ?? "", ref ?? ""),
    async ({ signal }) => (await d.connectedComments(connId!, ref!, { signal })).items,
    { staleTime: 30_000, retry: retryUnlessClientError },
    !!connId && !!ref && enabled,
  );
}

export function useDocsConnectedAddComment(connId: string, ref: string): UseMutationResult<DocsConnectedComment, ApiError, { body: DocsSpan[]; thread_id?: string }> {
  const d = useEndpoints(docsEndpoints);
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (body) => d.connectedAddComment(connId, ref, body),
    onSuccess: () => void qc.invalidateQueries({ queryKey: docsConnectedKeys.comments(connId, ref) }),
  });
}

// ---- bridges -----------------------------------------------------------------------

/** Copy a connected page into Docs (a new page in the org, structure kept). */
export function useDocsConnectedCopy(connId: string, ref: string): UseMutationResult<DocsPage, ApiError, { org_id: string; parent_id?: string | null }> {
  const d = useEndpoints(docsEndpoints);
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (body) => d.connectedCopy(connId, ref, body),
    onSuccess: () => {
      void qc.invalidateQueries({ queryKey: ["docs", "sidebar"] });
      void qc.invalidateQueries({ queryKey: ["docs", "recent"] });
    },
  });
}

/** A connected-page block as this viewer sees it, with their own connection (or how to get access). */
export function useDocsResolveConnected(
  props: { provider?: string; ref?: string; account_id?: string } | null | undefined,
  orgId?: string | null,
): UseQueryResult<DocsResolvedLink, ApiError> {
  const d = useEndpoints(docsEndpoints);
  const provider = props?.provider ?? "";
  const ref = props?.ref ?? "";
  const account = props?.account_id ?? "";
  return useApiQuery(
    docsConnectedKeys.resolve(provider, account, ref),
    ({ signal }) => d.resolveConnected({ provider, ref, account_id: account, org_id: orgId }, { signal }),
    { staleTime: 60_000, retry: retryUnlessClientError },
    !!provider && !!ref,
  );
}

/** "Notion", "Google Docs": the platform's own name for its documents. */
export function docsPlatformName(provider: string | null | undefined): string {
  return provider === "google" ? "Google Docs" : provider === "notion" ? "Notion" : (provider ?? "");
}

/** The app path of a connected page. */
export function docsConnectedHref(connId: string, ref: string): string {
  return `/docs/c/${encodeURIComponent(connId)}/${encodeURIComponent(ref)}`;
}
