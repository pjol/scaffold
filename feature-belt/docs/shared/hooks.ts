"use client";

import { useInfiniteQuery, useMutation, useQueryClient, type QueryClient, type UseMutationResult, type UseQueryResult } from "@tanstack/react-query";
import { useEffect, useMemo, useRef, useState, useSyncExternalStore } from "react";
import type { ApiError } from "../api/errors";
import { uploadFile, type UploadSource } from "../api/upload";
import { useApi, useEndpoints, useRealtime } from "../hooks/context";
import { useApiQuery, useDebouncedValue, usePagedQuery, useRealtimeEvent, useTopic, type PagedQueryResult, type QueryOverrides } from "../hooks/internal";
import { registerRealtimeHandler } from "../hooks/realtime-handlers";
import { topic } from "../realtime/topics";
import { generateId } from "../utils/id";
import { docsEndpoints } from "./api";
import { DocsPageSession, type DocsSessionSnapshot } from "./session";
import type {
  DocsBlock,
  DocsComment,
  DocsCommentRequest,
  DocsCreatePageRequest,
  DocsCreateRowRequest,
  DocsFile,
  DocsImportRequest,
  DocsMoveRequest,
  DocsPage,
  DocsPageDetail,
  DocsPageSummary,
  DocsProperty,
  DocsPublicPage,
  DocsRefs,
  DocsRow,
  DocsRowPage,
  DocsRowsParams,
  DocsRowValuesRequest,
  DocsSearchHit,
  DocsSidebar,
  DocsSnapshot,
  DocsUpdatePageRequest,
  DocsView,
  DocsViewer,
  DocsViewRequest,
} from "./types";

/** Query keys. `docsKeys.all()` invalidates everything this feature caches. */
export const docsKeys = {
  all: () => ["docs"] as const,
  sidebar: (orgId: string) => ["docs", "sidebar", orgId] as const,
  sidebarAll: () => ["docs", "sidebar"] as const,
  childrenAll: () => ["docs", "children"] as const,
  children: (pageId: string) => ["docs", "children", pageId] as const,
  recent: (orgId: string) => ["docs", "recent", orgId] as const,
  trashAll: (orgId: string) => ["docs", "trash", orgId] as const,
  trash: (orgId: string, q: string) => ["docs", "trash", orgId, q] as const,
  search: (orgId: string, q: string, opts: object) => ["docs", "search", orgId, q, opts] as const,
  page: (id: string) => ["docs", "page", id] as const,
  backlinks: (id: string) => ["docs", "backlinks", id] as const,
  rowsAll: (dbId: string) => ["docs", "rows", dbId] as const,
  rows: (dbId: string, params: DocsRowsParams) => ["docs", "rows", dbId, params] as const,
  commentsAll: (pageId: string) => ["docs", "comments", pageId] as const,
  comments: (pageId: string, status: string) => ["docs", "comments", pageId, status] as const,
  history: (pageId: string) => ["docs", "history", pageId] as const,
  snapshot: (id: string) => ["docs", "snapshot", id] as const,
  file: (id: string) => ["docs", "file", id] as const,
  public: (token: string, pageId?: string | null) => ["docs", "public", token, pageId ?? "root"] as const,
  publicRows: (token: string, dbId: string, params: DocsRowsParams) => ["docs", "public-rows", token, dbId, params] as const,
  publicFile: (token: string, id: string) => ["docs", "public-file", token, id] as const,
};

function retryUnlessClientError(count: number, error: ApiError): boolean {
  return error.status >= 500 || error.status === 0 ? count < 2 : false;
}

// ---- workspace -------------------------------------------------------------------

/** Favorites, Shared and Private for the org (live: docs.tree). */
export function useDocsSidebar(orgId: string | null | undefined, options?: QueryOverrides<DocsSidebar>): UseQueryResult<DocsSidebar, ApiError> {
  const d = useEndpoints(docsEndpoints);
  useTopic(orgId ? topic("docs", orgId) : null);
  return useApiQuery(docsKeys.sidebar(orgId ?? ""), ({ signal }) => d.sidebar(orgId!, { signal }), { staleTime: 30_000, ...options }, !!orgId);
}

/** Subpages of a page (sidebar expansion, the page's own list). */
export function useDocsChildren(pageId: string | null | undefined, enabled = true): UseQueryResult<DocsPageSummary[], ApiError> {
  const d = useEndpoints(docsEndpoints);
  return useApiQuery(docsKeys.children(pageId ?? ""), async ({ signal }) => (await d.children(pageId!, { signal })).items, { staleTime: 30_000, retry: retryUnlessClientError }, !!pageId && enabled);
}

export function useDocsRecent(orgId: string | null | undefined): UseQueryResult<DocsPageSummary[], ApiError> {
  const d = useEndpoints(docsEndpoints);
  return useApiQuery(docsKeys.recent(orgId ?? ""), async ({ signal }) => (await d.recent(orgId!, { signal })).items, { staleTime: 15_000 }, !!orgId);
}

export function useDocsTrash(orgId: string | null | undefined, q = ""): PagedQueryResult<DocsPageSummary> {
  const d = useEndpoints(docsEndpoints);
  const query = useDebouncedValue(q.trim(), 250);
  return usePagedQuery(docsKeys.trash(orgId ?? "", query), (cursor, signal) => d.trash(orgId!, { q: query, ...(cursor ? { cursor } : {}) }, { signal }), undefined, !!orgId);
}

/** Page search for pickers (empty q = recently opened). Debounced. */
export function useDocsSearch(
  orgId: string | null | undefined,
  q: string,
  opts: { kind?: "page" | "database"; rows?: boolean; min_level?: "view" | "edit"; limit?: number; enabled?: boolean } = {},
): UseQueryResult<DocsSearchHit[], ApiError> {
  const d = useEndpoints(docsEndpoints);
  const query = useDebouncedValue(q.trim(), 200);
  const { enabled = true, ...params } = opts;
  return useApiQuery(
    docsKeys.search(orgId ?? "", query, params),
    async ({ signal }) => (await d.search(orgId!, { q: query, ...params }, { signal })).items,
    { staleTime: 10_000, placeholderData: (prev) => prev },
    !!orgId && enabled,
  );
}

function invalidateTree(qc: QueryClient, orgId?: string) {
  void qc.invalidateQueries({ queryKey: orgId ? docsKeys.sidebar(orgId) : docsKeys.sidebarAll() });
  void qc.invalidateQueries({ queryKey: docsKeys.childrenAll() });
  if (orgId) void qc.invalidateQueries({ queryKey: docsKeys.trashAll(orgId) });
}

export function useDocsCreatePage(orgId: string): UseMutationResult<DocsPage, ApiError, DocsCreatePageRequest> {
  const d = useEndpoints(docsEndpoints);
  const qc = useQueryClient();
  return useMutation<DocsPage, ApiError, DocsCreatePageRequest>({
    mutationFn: (body) => d.createPage(orgId, body),
    onSuccess: () => invalidateTree(qc, orgId),
  });
}

export function useDocsImport(orgId: string): UseMutationResult<DocsPage, ApiError, DocsImportRequest> {
  const d = useEndpoints(docsEndpoints);
  const qc = useQueryClient();
  return useMutation<DocsPage, ApiError, DocsImportRequest>({ mutationFn: (body) => d.importMarkdown(orgId, body), onSuccess: () => invalidateTree(qc, orgId) });
}

// ---- one page --------------------------------------------------------------------

/** Everything to render a page. Live updates flow through `useDocsPageSession`. */
export function useDocsPage(pageId: string | null | undefined, options?: QueryOverrides<DocsPageDetail>): UseQueryResult<DocsPageDetail, ApiError> {
  const d = useEndpoints(docsEndpoints);
  return useApiQuery(docsKeys.page(pageId ?? ""), ({ signal }) => d.page(pageId!, { signal }), { retry: retryUnlessClientError, staleTime: 60_000, refetchOnWindowFocus: false, ...options }, !!pageId);
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
 * The live editing session of a page: block state with local edits applied, a save
 * state, undo/redo, and realtime operations from others (topic docs:<page id>).
 * Created once per page id from the first loaded detail; sends what's queued when the
 * page closes.
 */
export function useDocsPageSession<S = unknown>(
  pageId: string | null | undefined,
  detail: DocsPageDetail | undefined,
  onRejected?: (message: string, code: string) => void,
): { session: DocsPageSession<S> | null; snapshot: DocsSessionSnapshot } {
  const d = useEndpoints(docsEndpoints);
  const qc = useQueryClient();
  const realtime = useRealtime();
  const [session, setSession] = useState<DocsPageSession<S> | null>(null);
  const rejected = useRef(onRejected);
  rejected.current = onRejected;
  const ready = !!detail && detail.page.id === pageId;
  useEffect(() => {
    if (!pageId || !ready) return undefined;
    const current = qc.getQueryData<DocsPageDetail>(docsKeys.page(pageId));
    if (!current) return undefined;
    const clientId = generateId();
    const s = new DocsPageSession<S>({
      pageId,
      clientId,
      blocks: current.blocks,
      version: current.page.version,
      transport: { send: (ops) => d.applyOps(pageId, clientId, ops), sync: (since) => d.sync(pageId, since) },
      onPage: (page) => qc.setQueryData<DocsPageDetail>(docsKeys.page(pageId), (old) => (old ? { ...old, page: { ...page, my_level: old.page.my_level } } : old)),
      onRejected: (m, c) => rejected.current?.(m, c),
    });
    setSession(s);
    return () => {
      s.dispose();
      setSession(null);
    };
    // The session lives for the page id; refetched details don't replace it.
  }, [pageId, ready, d, qc]);
  useTopic(pageId ? topic("docs", pageId) : null);
  useRealtimeEvent("docs.ops", (e) => session?.receive(e.data));
  useRealtimeEvent("docs.reload", (e) => {
    if (e.data.page_id === pageId) void session?.reconcile();
  });
  useEffect(() => {
    if (!realtime || !session) return undefined;
    return realtime.onReconnect(() => void session.reconcile());
  }, [realtime, session]);
  const snapshot = useSyncExternalStore(session ? session.subscribe : noopSubscribe, session ? session.getSnapshot : emptyGet, emptyGet);
  return { session, snapshot };
}

/** Title, icon, cover, width and lock. Optimistic. */
export function useDocsUpdatePage(pageId: string): UseMutationResult<DocsPage, ApiError, DocsUpdatePageRequest, { prev: DocsPageDetail | undefined }> {
  const d = useEndpoints(docsEndpoints);
  const qc = useQueryClient();
  const key = docsKeys.page(pageId);
  return useMutation<DocsPage, ApiError, DocsUpdatePageRequest, { prev: DocsPageDetail | undefined }>({
    mutationFn: (body) => d.updatePage(pageId, body),
    onMutate: async (body) => {
      await qc.cancelQueries({ queryKey: key });
      const prev = qc.getQueryData<DocsPageDetail>(key);
      if (prev) qc.setQueryData<DocsPageDetail>(key, { ...prev, page: { ...prev.page, ...body } as DocsPage });
      return { prev };
    },
    onError: (_e, _v, ctx) => {
      if (ctx?.prev) qc.setQueryData(key, ctx.prev);
    },
    onSuccess: (page, body) => {
      qc.setQueryData<DocsPageDetail>(key, (old) => (old ? { ...old, page: { ...page, my_level: old.page.my_level } } : old));
      if (body.title !== undefined || body.icon !== undefined) invalidateTree(qc, page.org_id);
    },
  });
}

export function useDocsMovePage(orgId: string): UseMutationResult<DocsPage, ApiError, { pageId: string } & DocsMoveRequest> {
  const d = useEndpoints(docsEndpoints);
  const qc = useQueryClient();
  return useMutation<DocsPage, ApiError, { pageId: string } & DocsMoveRequest>({
    mutationFn: ({ pageId, ...body }) => d.movePage(pageId, body),
    onSuccess: (page) => {
      invalidateTree(qc, orgId);
      void qc.invalidateQueries({ queryKey: docsKeys.page(page.id) });
    },
  });
}

export function useDocsDuplicatePage(orgId: string): UseMutationResult<DocsPage, ApiError, string> {
  const d = useEndpoints(docsEndpoints);
  const qc = useQueryClient();
  return useMutation<DocsPage, ApiError, string>({
    mutationFn: (id) => d.duplicatePage(id),
    onSuccess: (page) => {
      invalidateTree(qc, orgId);
      if (page.is_row && page.parent_id) void qc.invalidateQueries({ queryKey: docsKeys.rowsAll(page.parent_id) });
    },
  });
}

export function useDocsTrashPage(orgId: string): UseMutationResult<void, ApiError, { pageId: string; databaseId?: string | null }> {
  const d = useEndpoints(docsEndpoints);
  const qc = useQueryClient();
  return useMutation<void, ApiError, { pageId: string; databaseId?: string | null }>({
    mutationFn: ({ pageId }) => d.trashPage(pageId),
    onSuccess: (_r, { pageId, databaseId }) => {
      invalidateTree(qc, orgId);
      void qc.invalidateQueries({ queryKey: docsKeys.page(pageId) });
      void qc.invalidateQueries({ queryKey: docsKeys.recent(orgId) });
      if (databaseId) void qc.invalidateQueries({ queryKey: docsKeys.rowsAll(databaseId) });
    },
  });
}

export function useDocsRestorePage(orgId: string): UseMutationResult<DocsPage, ApiError, string> {
  const d = useEndpoints(docsEndpoints);
  const qc = useQueryClient();
  return useMutation<DocsPage, ApiError, string>({
    mutationFn: (id) => d.restorePage(id),
    onSuccess: (page) => {
      invalidateTree(qc, orgId);
      void qc.invalidateQueries({ queryKey: docsKeys.page(page.id) });
      if (page.is_row && page.parent_id) void qc.invalidateQueries({ queryKey: docsKeys.rowsAll(page.parent_id) });
    },
  });
}

export function useDocsDeletePermanently(orgId: string): UseMutationResult<void, ApiError, string> {
  const d = useEndpoints(docsEndpoints);
  const qc = useQueryClient();
  return useMutation<void, ApiError, string>({
    mutationFn: (id) => d.deletePermanently(id),
    onSuccess: (_r, id) => {
      invalidateTree(qc, orgId);
      qc.removeQueries({ queryKey: docsKeys.page(id) });
    },
  });
}

/** Adds or removes a favorite. Optimistic on the page. */
export function useDocsSetFavorite(pageId: string): UseMutationResult<void, ApiError, boolean> {
  const d = useEndpoints(docsEndpoints);
  const qc = useQueryClient();
  return useMutation<void, ApiError, boolean>({
    mutationFn: (on) => (on ? d.favorite(pageId) : d.unfavorite(pageId)),
    onMutate: (on) => {
      qc.setQueryData<DocsPageDetail>(docsKeys.page(pageId), (old) => (old ? { ...old, favorite: on } : old));
    },
    onError: (_e, on) => {
      qc.setQueryData<DocsPageDetail>(docsKeys.page(pageId), (old) => (old ? { ...old, favorite: !on } : old));
    },
    onSettled: () => void qc.invalidateQueries({ queryKey: docsKeys.sidebarAll() }),
  });
}

export function useDocsBacklinks(pageId: string | null | undefined, enabled = true): UseQueryResult<DocsPageSummary[], ApiError> {
  const d = useEndpoints(docsEndpoints);
  return useApiQuery(docsKeys.backlinks(pageId ?? ""), async ({ signal }) => (await d.backlinks(pageId!, { signal })).items, { staleTime: 30_000 }, !!pageId && enabled);
}

export function useDocsExportMarkdown(): UseMutationResult<string, ApiError, string> {
  const d = useEndpoints(docsEndpoints);
  return useMutation<string, ApiError, string>({ mutationFn: (id) => d.exportMarkdown(id) });
}

// ---- files -----------------------------------------------------------------------

/** Uploads a file (purpose docs_file) and attaches it to the page; resolves to the file. */
export function useDocsUploadFile(pageId: string): UseMutationResult<DocsFile, ApiError, { file: UploadSource; name: string; contentType?: string }> {
  const api = useApi();
  const d = useEndpoints(docsEndpoints);
  return useMutation<DocsFile, ApiError, { file: UploadSource; name: string; contentType?: string }>({
    mutationFn: async ({ file, name, contentType }) => {
      const up = await uploadFile(api.http, { file, purpose: "docs_file", ...(contentType ? { contentType } : {}) });
      return d.registerFile(pageId, { key: up.key, name, size: up.size });
    },
  });
}

/** A file's short-lived download link (refreshed before it expires). With `token`, through a public link. */
export function useDocsFileUrl(fileId: string | null | undefined, token?: string | null): UseQueryResult<DocsFile, ApiError> {
  const d = useEndpoints(docsEndpoints);
  return useApiQuery(
    token ? docsKeys.publicFile(token, fileId ?? "") : docsKeys.file(fileId ?? ""),
    ({ signal }) => (token ? d.publicFile(token, fileId!, { signal }) : d.file(fileId!, { signal })),
    { staleTime: 40 * 60_000, gcTime: 50 * 60_000, retry: retryUnlessClientError, refetchOnWindowFocus: false },
    !!fileId,
  );
}

// ---- presence --------------------------------------------------------------------

/**
 * Who else has the page open and which block they're typing in. Beats every 20 s and
 * when `blockId` changes; leaves on unmount. `enabled` false (e.g. a hidden tab) leaves.
 */
export function useDocsPresence(pageId: string | null | undefined, blockId: string | null, enabled = true): DocsViewer[] {
  const d = useEndpoints(docsEndpoints);
  const sessionId = useMemo(() => generateId().replace(/-/g, ""), [pageId]);
  const [viewers, setViewers] = useState<DocsViewer[]>([]);
  const block = useDebouncedValue(blockId, 300);
  const active = !!pageId && enabled;
  useTopic(active ? topic("docs", pageId!) : null);
  useEffect(() => {
    if (!active) return undefined;
    let stopped = false;
    const beat = () =>
      d
        .beat(pageId!, { session_id: sessionId, block_id: block })
        .then((r) => {
          if (!stopped) setViewers(r.items);
        })
        .catch(() => {});
    void beat();
    const t = setInterval(() => void beat(), 20_000);
    return () => {
      stopped = true;
      clearInterval(t);
    };
  }, [active, pageId, sessionId, block, d]);
  useEffect(() => {
    if (!active) return undefined;
    return () => {
      void d.leave(pageId!, sessionId).catch(() => {});
    };
  }, [active, pageId, sessionId, d]);
  useEffect(() => {
    if (!active) return undefined;
    const t = setInterval(() => setViewers((list) => list.filter((v) => Date.now() - new Date(v.seen_at).getTime() < 45_000)), 10_000);
    return () => clearInterval(t);
  }, [active]);
  useRealtimeEvent("docs.presence", (e) => {
    const ev = e.data;
    if (ev.page_id !== pageId) return;
    setViewers((list) => {
      const rest = list.filter((v) => v.session_id !== ev.session_id);
      if (ev.left || !ev.user) return rest;
      return [...rest, { session_id: ev.session_id, user: ev.user, block_id: ev.block_id ?? null, seen_at: new Date().toISOString() }];
    });
  });
  return useMemo(() => viewers.filter((v) => v.session_id !== sessionId), [viewers, sessionId]);
}

// ---- comments --------------------------------------------------------------------

export function useDocsComments(pageId: string | null | undefined, status: "open" | "resolved" | "all" = "open"): UseQueryResult<DocsComment[], ApiError> {
  const d = useEndpoints(docsEndpoints);
  return useApiQuery(docsKeys.comments(pageId ?? "", status), async ({ signal }) => (await d.comments(pageId!, status, { signal })).items, { staleTime: 15_000 }, !!pageId);
}

function refreshComments(qc: QueryClient, pageId: string) {
  void qc.invalidateQueries({ queryKey: docsKeys.commentsAll(pageId) });
  void qc.invalidateQueries({ queryKey: docsKeys.page(pageId) });
}

export function useDocsCreateComment(pageId: string): UseMutationResult<DocsComment, ApiError, DocsCommentRequest> {
  const d = useEndpoints(docsEndpoints);
  const qc = useQueryClient();
  return useMutation<DocsComment, ApiError, DocsCommentRequest>({ mutationFn: (body) => d.createComment(pageId, body), onSuccess: () => refreshComments(qc, pageId) });
}

export function useDocsEditComment(pageId: string): UseMutationResult<DocsComment, ApiError, { id: string; body: DocsCommentRequest["body"] }> {
  const d = useEndpoints(docsEndpoints);
  const qc = useQueryClient();
  return useMutation<DocsComment, ApiError, { id: string; body: DocsCommentRequest["body"] }>({
    mutationFn: ({ id, body }) => d.editComment(id, body),
    onSuccess: () => refreshComments(qc, pageId),
  });
}

export function useDocsDeleteComment(pageId: string): UseMutationResult<void, ApiError, string> {
  const d = useEndpoints(docsEndpoints);
  const qc = useQueryClient();
  return useMutation<void, ApiError, string>({ mutationFn: (id) => d.deleteComment(id), onSuccess: () => refreshComments(qc, pageId) });
}

export function useDocsResolveComment(pageId: string): UseMutationResult<void, ApiError, { id: string; resolved: boolean }> {
  const d = useEndpoints(docsEndpoints);
  const qc = useQueryClient();
  return useMutation<void, ApiError, { id: string; resolved: boolean }>({
    mutationFn: ({ id, resolved }) => (resolved ? d.resolveComment(id) : d.reopenComment(id)),
    onSuccess: () => refreshComments(qc, pageId),
  });
}

// ---- history ---------------------------------------------------------------------

export function useDocsHistory(pageId: string | null | undefined, enabled = true): PagedQueryResult<DocsSnapshot> {
  const d = useEndpoints(docsEndpoints);
  return usePagedQuery(docsKeys.history(pageId ?? ""), (cursor, signal) => d.history(pageId!, cursor, { signal }), undefined, !!pageId && enabled);
}

export function useDocsSnapshot(id: string | null | undefined): UseQueryResult<{ snapshot: DocsSnapshot & { blocks: DocsBlock[] }; refs: DocsRefs }, ApiError> {
  const d = useEndpoints(docsEndpoints);
  return useApiQuery(docsKeys.snapshot(id ?? ""), ({ signal }) => d.snapshot(id!, { signal }), { staleTime: Infinity }, !!id);
}

export function useDocsRestoreSnapshot(pageId: string): UseMutationResult<{ version: number }, ApiError, string> {
  const d = useEndpoints(docsEndpoints);
  const qc = useQueryClient();
  return useMutation<{ version: number }, ApiError, string>({
    mutationFn: (id) => d.restoreSnapshot(id),
    onSuccess: () => {
      void qc.invalidateQueries({ queryKey: docsKeys.history(pageId) });
      void qc.invalidateQueries({ queryKey: docsKeys.page(pageId) });
    },
  });
}

// ---- databases -------------------------------------------------------------------

export interface DocsRowsResult {
  rows: DocsRow[];
  refs: DocsRefs;
}

/** A view's rows, keyset-paged (`fetchNextPage`), with people's names merged across pages. */
export function useDocsRows(dbId: string | null | undefined, params: DocsRowsParams, options: { token?: string | null; enabled?: boolean } = {}) {
  const d = useEndpoints(docsEndpoints);
  const { token, enabled = true } = options;
  return useInfiniteQuery<DocsRowPage, ApiError, DocsRowsResult, readonly unknown[], string | undefined>({
    queryKey: token ? docsKeys.publicRows(token, dbId ?? "", params) : docsKeys.rows(dbId ?? "", params),
    queryFn: ({ pageParam, signal }) => {
      const p = { ...params, ...(pageParam ? { cursor: pageParam } : {}) };
      return token ? d.publicRows(token, dbId!, p, { signal }) : d.rows(dbId!, p, { signal });
    },
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
    enabled: !!dbId && enabled,
    staleTime: 15_000,
    placeholderData: (prev) => prev,
  });
}

type RowsCache = { pages: DocsRowPage[]; pageParams: unknown[] };

function patchRows(qc: QueryClient, dbId: string, fn: (row: DocsRow) => DocsRow | null) {
  for (const [key, data] of qc.getQueriesData<RowsCache>({ queryKey: docsKeys.rowsAll(dbId) })) {
    if (!data) continue;
    qc.setQueryData<RowsCache>(key, {
      ...data,
      pages: data.pages.map((p) => ({ ...p, items: p.items.map(fn).filter((r): r is DocsRow => r !== null) })),
    });
  }
}

/** Sets row values, the title or its place. Optimistic in every cached view of the database. */
export function useDocsSetRowValues(dbId: string): UseMutationResult<DocsPage, ApiError, { rowId: string } & DocsRowValuesRequest> {
  const d = useEndpoints(docsEndpoints);
  const qc = useQueryClient();
  return useMutation<DocsPage, ApiError, { rowId: string } & DocsRowValuesRequest>({
    mutationFn: ({ rowId, ...body }) => d.setRowValues(rowId, body),
    onMutate: async ({ rowId, values, title, position }) => {
      await qc.cancelQueries({ queryKey: docsKeys.rowsAll(dbId) });
      patchRows(qc, dbId, (r) => {
        if (r.id !== rowId) return r;
        const props = { ...r.props };
        for (const [k, v] of Object.entries(values ?? {})) {
          if (v === null) delete props[k];
          else props[k] = v;
        }
        return { ...r, props, ...(title !== undefined ? { title } : {}), ...(position ? { position } : {}) };
      });
      qc.setQueryData<DocsPageDetail>(docsKeys.page(rowId), (old) => {
        if (!old) return old;
        const props = { ...old.page.props };
        for (const [k, v] of Object.entries(values ?? {})) {
          if (v === null) delete props[k];
          else props[k] = v;
        }
        return { ...old, page: { ...old.page, props, ...(title !== undefined ? { title } : {}) } };
      });
    },
    onSuccess: (page) => {
      patchRows(qc, dbId, (r) => (r.id === page.id ? { ...r, title: page.title, props: page.props, position: page.position, updated_at: page.updated_at } : r));
      qc.setQueryData<DocsPageDetail>(docsKeys.page(page.id), (old) => (old ? { ...old, page: { ...page, my_level: old.page.my_level } } : old));
    },
    onError: () => void qc.invalidateQueries({ queryKey: docsKeys.rowsAll(dbId) }),
  });
}

export function useDocsCreateRow(dbId: string): UseMutationResult<DocsPage, ApiError, DocsCreateRowRequest> {
  const d = useEndpoints(docsEndpoints);
  const qc = useQueryClient();
  return useMutation<DocsPage, ApiError, DocsCreateRowRequest>({
    mutationFn: (body) => d.createRow(dbId, body),
    onSuccess: () => void qc.invalidateQueries({ queryKey: docsKeys.rowsAll(dbId) }),
  });
}

function patchDatabase(qc: QueryClient, dbId: string, fn: (detail: DocsPageDetail) => DocsPageDetail) {
  qc.setQueryData<DocsPageDetail>(docsKeys.page(dbId), (old) => (old ? fn(old) : old));
}

/** Replaces the property list (optimistic). */
export function useDocsPutSchema(dbId: string): UseMutationResult<DocsPage, ApiError, DocsProperty[], { prev: DocsPageDetail | undefined }> {
  const d = useEndpoints(docsEndpoints);
  const qc = useQueryClient();
  return useMutation<DocsPage, ApiError, DocsProperty[], { prev: DocsPageDetail | undefined }>({
    mutationFn: (props) => d.putSchema(dbId, props),
    onMutate: (props) => {
      const prev = qc.getQueryData<DocsPageDetail>(docsKeys.page(dbId));
      patchDatabase(qc, dbId, (old) => ({ ...old, page: { ...old.page, schema: props } }));
      return { prev };
    },
    onError: (_e, _v, ctx) => {
      if (ctx?.prev) qc.setQueryData(docsKeys.page(dbId), ctx.prev);
    },
    onSuccess: () => {
      // The database and every open row page (they show its properties).
      void qc.invalidateQueries({ queryKey: ["docs", "page"] });
      void qc.invalidateQueries({ queryKey: docsKeys.rowsAll(dbId) });
    },
  });
}

export function useDocsCreateView(dbId: string): UseMutationResult<DocsView, ApiError, DocsViewRequest & { type: DocsView["type"] }> {
  const d = useEndpoints(docsEndpoints);
  const qc = useQueryClient();
  return useMutation<DocsView, ApiError, DocsViewRequest & { type: DocsView["type"] }>({
    mutationFn: (body) => d.createView(dbId, body),
    onSuccess: (view) => {
      patchDatabase(qc, dbId, (old) => ({ ...old, views: [...old.views.filter((v) => v.id !== view.id), view] }));
      void qc.invalidateQueries({ queryKey: docsKeys.page(dbId) });
    },
  });
}

/** Saves a view's name, type, filters, sorts, grouping or columns (optimistic). */
export function useDocsUpdateView(dbId: string): UseMutationResult<DocsView, ApiError, { viewId: string } & DocsViewRequest, { prev: DocsPageDetail | undefined }> {
  const d = useEndpoints(docsEndpoints);
  const qc = useQueryClient();
  return useMutation<DocsView, ApiError, { viewId: string } & DocsViewRequest, { prev: DocsPageDetail | undefined }>({
    mutationFn: ({ viewId, ...body }) => d.updateView(viewId, body),
    onMutate: ({ viewId, ...body }) => {
      const prev = qc.getQueryData<DocsPageDetail>(docsKeys.page(dbId));
      patchDatabase(qc, dbId, (old) => ({ ...old, views: old.views.map((v) => (v.id === viewId ? ({ ...v, ...body, config: body.config ?? v.config } as DocsView) : v)) }));
      return { prev };
    },
    onError: (_e, _v, ctx) => {
      if (ctx?.prev) qc.setQueryData(docsKeys.page(dbId), ctx.prev);
    },
    onSuccess: (view) => {
      patchDatabase(qc, dbId, (old) => ({ ...old, views: old.views.map((v) => (v.id === view.id ? view : v)) }));
      void qc.invalidateQueries({ queryKey: docsKeys.rowsAll(dbId) });
    },
  });
}

export function useDocsDeleteView(dbId: string): UseMutationResult<void, ApiError, string> {
  const d = useEndpoints(docsEndpoints);
  const qc = useQueryClient();
  return useMutation<void, ApiError, string>({
    mutationFn: (viewId) => d.deleteView(viewId),
    onSuccess: (_r, viewId) => patchDatabase(qc, dbId, (old) => ({ ...old, views: old.views.filter((v) => v.id !== viewId) })),
  });
}

// ---- public pages ----------------------------------------------------------------

/** A published page (no account). 404 `link_not_found` once the link stops working. */
export function useDocsPublicPage(token: string | null | undefined, pageId?: string | null): UseQueryResult<DocsPublicPage, ApiError> {
  const d = useEndpoints(docsEndpoints);
  return useApiQuery(docsKeys.public(token ?? "", pageId), ({ signal }) => d.publicPage(token!, pageId ?? undefined, { signal }), { retry: retryUnlessClientError, staleTime: 60_000 }, !!token);
}

// ---- realtime cache updates --------------------------------------------------------

registerRealtimeHandler("docs.tree", (qc, e) => invalidateTree(qc, e.data.org_id));
registerRealtimeHandler("docs.page", (qc, e) => {
  const page = e.data.page;
  qc.setQueryData<DocsPageDetail>(docsKeys.page(page.id), (old) => (old ? { ...old, page: { ...page, my_level: old.page.my_level } } : old));
});
registerRealtimeHandler("docs.comments", (qc, e) => refreshComments(qc, e.data.page_id));
registerRealtimeHandler("docs.rows", (qc, e) => void qc.invalidateQueries({ queryKey: docsKeys.rowsAll(e.data.database_id) }));
registerRealtimeHandler("docs.schema", (qc, e) => {
  void qc.invalidateQueries({ queryKey: docsKeys.page(e.data.database_id) });
  void qc.invalidateQueries({ queryKey: docsKeys.rowsAll(e.data.database_id) });
});
registerRealtimeHandler("docs.reload", (qc, e) => void qc.invalidateQueries({ queryKey: docsKeys.history(e.data.page_id) }));
