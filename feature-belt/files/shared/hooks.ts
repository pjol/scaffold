"use client";

import { keepPreviousData, useMutation, useQueryClient, type QueryClient, type UseMutationResult, type UseQueryResult } from "@tanstack/react-query";
import type { ApiError } from "../api/errors";
import { useApi, useEndpoints } from "../hooks/context";
import { useApiQuery, usePagedQuery, type InfiniteOverrides, type PagedData, type PagedQueryResult, type QueryOverrides } from "../hooks/internal";
import { registerReconnectRefetch, registerRealtimeHandler } from "../hooks/realtime-handlers";
import { filesEndpoints } from "./api";
import { filesConnectedUpload, filesUpload, type FilesConnectedUploadInput, type FilesUploadInput } from "./upload";
import type {
  FilesActivity,
  FilesAdminPublicItem,
  FilesArchive,
  FilesBatchAction,
  FilesBatchResult,
  FilesConnectedDetail,
  FilesConnectedDrive,
  FilesConnectedItem,
  FilesConnectedSharing,
  FilesConnectedText,
  FilesCreateFileRequest,
  FilesCreateFolderRequest,
  FilesDriveInfo,
  FilesItem,
  FilesItemDetail,
  FilesListParams,
  FilesPublicItem,
  FilesPublicView,
  FilesSearchHit,
  FilesSection,
  FilesShortcutTarget,
  FilesSources,
  FilesTextContent,
  FilesTypeSummary,
  FilesUrl,
  FilesVersion,
} from "./types";

/** Query keys. `filesKeys.all()` invalidates everything this feature caches. */
export const filesKeys = {
  all: () => ["files"] as const,
  types: () => ["files", "types"] as const,
  drive: (orgId: string) => ["files", "drive", orgId] as const,
  lists: () => ["files", "list"] as const,
  section: (orgId: string, section: FilesSection, p: FilesListParams = {}) => ["files", "list", "section", orgId, section, p] as const,
  children: (folderId: string, p: FilesListParams = {}) => ["files", "list", "children", folderId, p] as const,
  search: (orgId: string, q: string) => ["files", "list", "search", orgId, q] as const,
  item: (id: string) => ["files", "item", id] as const,
  versions: (id: string) => ["files", "item", id, "versions"] as const,
  activity: (id: string) => ["files", "item", id, "activity"] as const,
  text: (id: string, version: string) => ["files", "item", id, "text", version] as const,
  public: (token: string, itemId: string) => ["files", "public", token, itemId] as const,
  publicChildren: (token: string, folderId: string, p: FilesListParams = {}) => ["files", "public", token, folderId, "children", p] as const,
  publicText: (token: string, itemId: string) => ["files", "public", token, itemId, "text"] as const,
  adminPublic: (status: string) => ["files", "admin", "public", status] as const,
  adminItem: (id: string) => ["files", "admin", "item", id] as const,
  sources: (orgId: string) => ["files", "sources", orgId] as const,
  connected: (connId: string) => ["files", "connected", connId] as const,
  connectedDrives: (connId: string, orgId: string) => ["files", "connected", connId, "drives", orgId] as const,
  connectedItem: (connId: string, ref: string) => ["files", "connected", connId, "item", ref] as const,
  connectedChildren: (connId: string, ref: string, p: FilesListParams = {}) => ["files", "connected", connId, "children", ref, p] as const,
  connectedSearch: (connId: string, q: string) => ["files", "connected", connId, "search", q] as const,
  shortcut: (id: string) => ["files", "item", id, "shortcut"] as const,
};

/** Lists and the item page show changes from any device (the realtime event) and after any write. */
function refreshFiles(qc: QueryClient, ids: string[] = [], orgId?: string) {
  void qc.invalidateQueries({ queryKey: filesKeys.lists() });
  for (const id of ids) void qc.invalidateQueries({ queryKey: filesKeys.item(id) });
  if (orgId) void qc.invalidateQueries({ queryKey: filesKeys.drive(orgId) });
}

// ---- reading -------------------------------------------------------------------------

/** Item types other features registered, for New menus and icons. */
export function useFilesTypes(options?: QueryOverrides<{ items: FilesTypeSummary[] }>): UseQueryResult<{ items: FilesTypeSummary[] }, ApiError> {
  const f = useEndpoints(filesEndpoints);
  return useApiQuery(filesKeys.types(), ({ signal }) => f.types({ signal }), { staleTime: 10 * 60_000, ...options });
}

/** Storage used and the quota, upload limit, trash period, and whether you may create shared folders. */
export function useFilesDrive(orgId: string | null | undefined, options?: QueryOverrides<FilesDriveInfo>): UseQueryResult<FilesDriveInfo, ApiError> {
  const f = useEndpoints(filesEndpoints);
  return useApiQuery(filesKeys.drive(orgId ?? ""), ({ signal }) => f.drive(orgId ?? "", { signal }), { staleTime: 30_000, ...options }, !!orgId);
}

/** One section of an org's drive (My drive, Shared folders, Shared with me, Recent, Starred, Trash). */
export function useFilesSection(
  orgId: string | null | undefined,
  section: FilesSection,
  params: FilesListParams = {},
  options?: InfiniteOverrides<FilesItem>,
): PagedQueryResult<FilesItem> {
  const f = useEndpoints(filesEndpoints);
  return usePagedQuery(
    filesKeys.section(orgId ?? "", section, params),
    (cursor, signal) => f.section(orgId ?? "", section, { ...params, cursor }, { signal }),
    { placeholderData: keepPreviousData, ...options },
    !!orgId,
  );
}

/** What a folder holds, folders first. */
export function useFilesChildren(folderId: string | null | undefined, params: FilesListParams = {}, options?: InfiniteOverrides<FilesItem>): PagedQueryResult<FilesItem> {
  const f = useEndpoints(filesEndpoints);
  return usePagedQuery(
    filesKeys.children(folderId ?? "", params),
    (cursor, signal) => f.children(folderId ?? "", { ...params, cursor }, { signal }),
    { placeholderData: keepPreviousData, ...options },
    !!folderId,
  );
}

/** Name and text search in one org (pass a debounced query; under 1 character it stays idle). */
export function useFilesSearch(orgId: string | null | undefined, q: string, options?: InfiniteOverrides<FilesSearchHit>): PagedQueryResult<FilesSearchHit> {
  const f = useEndpoints(filesEndpoints);
  const query = q.trim();
  return usePagedQuery(filesKeys.search(orgId ?? "", query), (cursor, signal) => f.search(orgId ?? "", query, cursor, { signal }), options, !!orgId && query.length > 0);
}

/** An item with its path. 404 when you can't open it. */
export function useFilesItem(id: string | null | undefined, options?: QueryOverrides<FilesItemDetail>): UseQueryResult<FilesItemDetail, ApiError> {
  const f = useEndpoints(filesEndpoints);
  return useApiQuery(filesKeys.item(id ?? ""), ({ signal }) => f.item(id ?? "", { signal }), { retry: retryServerErrors, ...options }, !!id);
}

function retryServerErrors(count: number, error: ApiError): boolean {
  return error.status >= 500 || error.status === 0 ? count < 2 : false;
}

export function useFilesVersions(id: string | null | undefined, options?: QueryOverrides<{ items: FilesVersion[] }>): UseQueryResult<{ items: FilesVersion[] }, ApiError> {
  const f = useEndpoints(filesEndpoints);
  return useApiQuery(filesKeys.versions(id ?? ""), ({ signal }) => f.versions(id ?? "", { signal }), options, !!id);
}

export function useFilesActivity(id: string | null | undefined, options?: InfiniteOverrides<FilesActivity>): PagedQueryResult<FilesActivity> {
  const f = useEndpoints(filesEndpoints);
  return usePagedQuery(filesKeys.activity(id ?? ""), (cursor, signal) => f.activity(id ?? "", cursor, { signal }), options, !!id);
}

/** A text file's content (the current version unless `version` is given). */
export function useFilesText(id: string | null | undefined, version?: string | null, options?: QueryOverrides<FilesTextContent>): UseQueryResult<FilesTextContent, ApiError> {
  const f = useEndpoints(filesEndpoints);
  return useApiQuery(filesKeys.text(id ?? "", version ?? "current"), ({ signal }) => f.text(id ?? "", version ?? undefined, { signal }), { staleTime: 60_000, retry: retryServerErrors, ...options }, !!id);
}

/** A short-lived download or preview link (fetch it when the person asks, then open it). */
export function useFilesGetUrl(): UseMutationResult<FilesUrl, ApiError, { id: string; disposition?: "inline" | "attachment"; version?: string }> {
  const f = useEndpoints(filesEndpoints);
  return useMutation<FilesUrl, ApiError, { id: string; disposition?: "inline" | "attachment"; version?: string }>({
    mutationFn: ({ id, ...p }) => f.url(id, p),
  });
}

/** An inline link for previews (images, PDF, video, audio), refreshed before it expires. */
export function useFilesPreviewUrl(id: string | null | undefined, version?: string | null, enabled = true): UseQueryResult<FilesUrl, ApiError> {
  const f = useEndpoints(filesEndpoints);
  return useApiQuery(
    ["files", "item", id ?? "", "preview-url", version ?? "current"],
    ({ signal }) => f.url(id ?? "", { disposition: "inline", ...(version ? { version } : {}) }, { signal }),
    { staleTime: 10 * 60_000, refetchInterval: 12 * 60_000, retry: retryServerErrors },
    !!id && enabled,
  );
}

// ---- writing --------------------------------------------------------------------------

export function useFilesCreateFolder(orgId: string): UseMutationResult<FilesItem, ApiError, FilesCreateFolderRequest> {
  const f = useEndpoints(filesEndpoints);
  const qc = useQueryClient();
  return useMutation<FilesItem, ApiError, FilesCreateFolderRequest>({ mutationFn: (body) => f.createFolder(orgId, body), onSuccess: (it) => refreshFiles(qc, [it.id]) });
}

export function useFilesCreateFile(orgId: string): UseMutationResult<FilesItem, ApiError, FilesCreateFileRequest> {
  const f = useEndpoints(filesEndpoints);
  const qc = useQueryClient();
  return useMutation<FilesItem, ApiError, FilesCreateFileRequest>({ mutationFn: (body) => f.createFile(orgId, body), onSuccess: () => refreshFiles(qc, [], orgId) });
}

/** Upload one file (presign → PUT → complete); lists refresh when it lands. */
export function useFilesUpload(): UseMutationResult<FilesItem, ApiError, FilesUploadInput> {
  const api = useApi();
  const qc = useQueryClient();
  return useMutation<FilesItem, ApiError, FilesUploadInput>({
    mutationFn: (input) => filesUpload(api.http, input),
    onSuccess: (it, input) => refreshFiles(qc, input.itemId ? [input.itemId] : [], it.org_id),
  });
}

export function useFilesRename(): UseMutationResult<FilesItem, ApiError, { id: string; name: string }> {
  const f = useEndpoints(filesEndpoints);
  const qc = useQueryClient();
  return useMutation<FilesItem, ApiError, { id: string; name: string }>({ mutationFn: ({ id, name }) => f.rename(id, name), onSuccess: (it) => refreshFiles(qc, [it.id]) });
}

/** parentId null: the top of your My drive. */
export function useFilesMove(): UseMutationResult<FilesItem, ApiError, { id: string; parentId: string | null }> {
  const f = useEndpoints(filesEndpoints);
  const qc = useQueryClient();
  return useMutation<FilesItem, ApiError, { id: string; parentId: string | null }>({ mutationFn: ({ id, parentId }) => f.move(id, parentId), onSuccess: (it) => refreshFiles(qc, [it.id]) });
}

export function useFilesCopy(): UseMutationResult<FilesItem, ApiError, { id: string; parentId: string | null; name?: string }> {
  const f = useEndpoints(filesEndpoints);
  const qc = useQueryClient();
  return useMutation<FilesItem, ApiError, { id: string; parentId: string | null; name?: string }>({
    mutationFn: ({ id, parentId, name }) => f.copy(id, parentId, name),
    onSuccess: (it) => refreshFiles(qc, [], it.org_id),
  });
}

/** Star or unstar (optimistic in loaded lists). */
export function useFilesStar(): UseMutationResult<void, ApiError, { id: string; on: boolean }, { snapshot: [readonly unknown[], unknown][] }> {
  const f = useEndpoints(filesEndpoints);
  const qc = useQueryClient();
  return useMutation<void, ApiError, { id: string; on: boolean }, { snapshot: [readonly unknown[], unknown][] }>({
    mutationFn: ({ id, on }) => f.star(id, on),
    onMutate: async ({ id, on }) => {
      await qc.cancelQueries({ queryKey: filesKeys.lists() });
      const snapshot = qc.getQueriesData({ queryKey: filesKeys.lists() });
      qc.setQueriesData<PagedData<FilesItem>>({ queryKey: filesKeys.lists() }, (data) => patchPages(data, id, { starred: on }));
      qc.setQueryData<FilesItemDetail>(filesKeys.item(id), (d) => (d ? { ...d, item: { ...d.item, starred: on } } : d));
      return { snapshot };
    },
    onError: (_e, _v, ctx) => {
      for (const [key, data] of ctx?.snapshot ?? []) qc.setQueryData(key, data);
    },
    onSettled: (_d, _e, { id }) => refreshFiles(qc, [id]),
  });
}

function patchPages(data: PagedData<FilesItem> | undefined, id: string, patch: Partial<FilesItem>): PagedData<FilesItem> | undefined {
  if (!data?.pages) return data;
  const pages = data.pages.map((p) => ({ ...p, items: p.items.map((it) => (it.id === id ? { ...it, ...patch } : it)) }));
  return { ...data, pages, items: pages.flatMap((p) => p.items) };
}

export function useFilesTrash(): UseMutationResult<void, ApiError, string> {
  const f = useEndpoints(filesEndpoints);
  const qc = useQueryClient();
  return useMutation<void, ApiError, string>({ mutationFn: (id) => f.trash(id), onSuccess: (_d, id) => refreshFiles(qc, [id]) });
}

export function useFilesRestore(): UseMutationResult<FilesItem, ApiError, string> {
  const f = useEndpoints(filesEndpoints);
  const qc = useQueryClient();
  return useMutation<FilesItem, ApiError, string>({ mutationFn: (id) => f.restore(id), onSuccess: (it) => refreshFiles(qc, [it.id]) });
}

/** Delete a trashed item for good (manage). */
export function useFilesDelete(): UseMutationResult<void, ApiError, string> {
  const f = useEndpoints(filesEndpoints);
  const qc = useQueryClient();
  return useMutation<void, ApiError, string>({ mutationFn: (id) => f.remove(id), onSuccess: () => refreshFiles(qc) });
}

export function useFilesEmptyTrash(orgId: string): UseMutationResult<{ deleted: number }, ApiError, void> {
  const f = useEndpoints(filesEndpoints);
  const qc = useQueryClient();
  return useMutation<{ deleted: number }, ApiError, void>({ mutationFn: () => f.emptyTrash(orgId), onSuccess: () => refreshFiles(qc, [], orgId) });
}

/** Move, trash, restore, delete, star or unstar many items; each succeeds or fails on its own. */
export function useFilesBatch(): UseMutationResult<{ results: FilesBatchResult[] }, ApiError, { action: FilesBatchAction; ids: string[]; parentId?: string | null }> {
  const f = useEndpoints(filesEndpoints);
  const qc = useQueryClient();
  return useMutation<{ results: FilesBatchResult[] }, ApiError, { action: FilesBatchAction; ids: string[]; parentId?: string | null }>({
    mutationFn: ({ action, ids, parentId }) => f.batch(action, ids, parentId),
    onSuccess: (_d, v) => refreshFiles(qc, v.ids),
  });
}

export function useFilesRestoreVersion(id: string): UseMutationResult<FilesItem, ApiError, string> {
  const f = useEndpoints(filesEndpoints);
  const qc = useQueryClient();
  return useMutation<FilesItem, ApiError, string>({
    mutationFn: (versionId) => f.restoreVersion(id, versionId),
    onSuccess: (it) => {
      refreshFiles(qc, [it.id], it.org_id);
      void qc.invalidateQueries({ queryKey: filesKeys.versions(id) });
    },
  });
}

/** A zip of the items (folders with what you can see in them), as a 5-minute link. */
export function useFilesArchive(orgId: string): UseMutationResult<FilesArchive, ApiError, { ids: string[]; name?: string }> {
  const f = useEndpoints(filesEndpoints);
  return useMutation<FilesArchive, ApiError, { ids: string[]; name?: string }>({ mutationFn: ({ ids, name }) => f.archive(orgId, ids, name) });
}

// ---- connected drives ----------------------------------------------------------------------

/** The app's drive and your connected accounts in Files, with capabilities still on offer (FeatureSources). */
export function useFilesSources(orgId: string | null | undefined, options?: QueryOverrides<FilesSources>): UseQueryResult<FilesSources, ApiError> {
  const f = useEndpoints(filesEndpoints);
  return useApiQuery(filesKeys.sources(orgId ?? ""), ({ signal }) => f.sources(orgId, { signal }), { staleTime: 60_000, retry: retryServerErrors, ...options });
}

/** The places to browse in one connection (My Drive, Shared with me, shared drives; OneDrive, libraries). */
export function useFilesConnectedDrives(
  connId: string | null | undefined,
  orgId?: string | null,
  options?: InfiniteOverrides<FilesConnectedDrive>,
): PagedQueryResult<FilesConnectedDrive> {
  const f = useEndpoints(filesEndpoints);
  return usePagedQuery(
    filesKeys.connectedDrives(connId ?? "", orgId ?? ""),
    (cursor, signal) => f.connectedDrives(connId ?? "", orgId, cursor, { signal }),
    { staleTime: 5 * 60_000, retry: retryServerErrors, ...options },
    !!connId,
  );
}

/** A connected item with its folders. 404 when your account can't open it. */
export function useFilesConnectedItem(connId: string | null | undefined, ref: string | null | undefined, orgId?: string | null, options?: QueryOverrides<FilesConnectedDetail>): UseQueryResult<FilesConnectedDetail, ApiError> {
  const f = useEndpoints(filesEndpoints);
  return useApiQuery(filesKeys.connectedItem(connId ?? "", ref ?? ""), ({ signal }) => f.connectedItem(connId ?? "", ref ?? "", orgId, { signal }), { retry: retryServerErrors, staleTime: 30_000, ...options }, !!connId && !!ref);
}

/** What a connected folder holds, in the platform's pages. */
export function useFilesConnectedChildren(
  connId: string | null | undefined,
  ref: string | null | undefined,
  params: FilesListParams = {},
  orgId?: string | null,
  options?: InfiniteOverrides<FilesConnectedItem>,
): PagedQueryResult<FilesConnectedItem> {
  const f = useEndpoints(filesEndpoints);
  return usePagedQuery(
    filesKeys.connectedChildren(connId ?? "", ref ?? "", params),
    (cursor, signal) => f.connectedChildren(connId ?? "", ref ?? "", { ...params, cursor }, orgId, { signal }),
    { placeholderData: keepPreviousData, staleTime: 30_000, retry: retryServerErrors, ...options },
    !!connId && !!ref,
  );
}

/** The platform's own search in one connection (pass a debounced query). */
export function useFilesConnectedSearch(connId: string | null | undefined, q: string, orgId?: string | null, options?: InfiniteOverrides<FilesConnectedItem>): PagedQueryResult<FilesConnectedItem> {
  const f = useEndpoints(filesEndpoints);
  const query = q.trim();
  return usePagedQuery(
    filesKeys.connectedSearch(connId ?? "", query),
    (cursor, signal) => f.connectedSearch(connId ?? "", query, cursor, orgId, { signal }),
    { retry: retryServerErrors, ...options },
    !!connId && query.length > 0,
  );
}

/** A connected text file's content (first 1 MB). */
export function useFilesConnectedText(connId: string | null | undefined, ref: string | null | undefined, orgId?: string | null, enabled = true): UseQueryResult<FilesConnectedText, ApiError> {
  const f = useEndpoints(filesEndpoints);
  return useApiQuery([...filesKeys.connectedItem(connId ?? "", ref ?? ""), "text"], ({ signal }) => f.connectedText(connId ?? "", ref ?? "", orgId, { signal }), { staleTime: 60_000, retry: retryServerErrors }, !!connId && !!ref && enabled);
}

/** An inline preview link for a connected file, refreshed before it expires. */
export function useFilesConnectedPreviewUrl(connId: string | null | undefined, ref: string | null | undefined, orgId?: string | null, enabled = true): UseQueryResult<FilesUrl, ApiError> {
  const f = useEndpoints(filesEndpoints);
  return useApiQuery(
    [...filesKeys.connectedItem(connId ?? "", ref ?? ""), "preview-url"],
    ({ signal }) => f.connectedUrl(connId ?? "", ref ?? "", "inline", orgId, { signal }),
    { staleTime: 10 * 60_000, refetchInterval: 12 * 60_000, retry: retryServerErrors },
    !!connId && !!ref && enabled,
  );
}

/** A download link for a connected file (names and types kept; documents export as Office files). */
export function useFilesConnectedGetUrl(): UseMutationResult<FilesUrl, ApiError, { connId: string; ref: string; orgId?: string | null; disposition?: "inline" | "attachment" }> {
  const f = useEndpoints(filesEndpoints);
  return useMutation<FilesUrl, ApiError, { connId: string; ref: string; orgId?: string | null; disposition?: "inline" | "attachment" }>({
    mutationFn: ({ connId, ref, orgId, disposition }) => f.connectedUrl(connId, ref, disposition ?? "attachment", orgId),
  });
}

/** The platform's sharing summary (read-only). */
export function useFilesConnectedSharing(connId: string | null | undefined, ref: string | null | undefined, orgId?: string | null): UseQueryResult<FilesConnectedSharing, ApiError> {
  const f = useEndpoints(filesEndpoints);
  return useApiQuery([...filesKeys.connectedItem(connId ?? "", ref ?? ""), "sharing"], ({ signal }) => f.connectedSharing(connId ?? "", ref ?? "", orgId, { signal }), { staleTime: 60_000, retry: retryServerErrors }, !!connId && !!ref);
}

function refreshConnected(qc: QueryClient, connId: string) {
  void qc.invalidateQueries({ queryKey: filesKeys.connected(connId) });
}

export function useFilesConnectedCreateFolder(connId: string, orgId?: string | null): UseMutationResult<FilesConnectedItem, ApiError, { parentRef: string; name: string }> {
  const f = useEndpoints(filesEndpoints);
  const qc = useQueryClient();
  return useMutation<FilesConnectedItem, ApiError, { parentRef: string; name: string }>({
    mutationFn: ({ parentRef, name }) => f.connectedCreateFolder(connId, { parent_ref: parentRef, name }, orgId),
    onSuccess: () => refreshConnected(qc, connId),
  });
}

export function useFilesConnectedRename(orgId?: string | null): UseMutationResult<FilesConnectedItem, ApiError, { connId: string; ref: string; name: string }> {
  const f = useEndpoints(filesEndpoints);
  const qc = useQueryClient();
  return useMutation<FilesConnectedItem, ApiError, { connId: string; ref: string; name: string }>({
    mutationFn: ({ connId, ref, name }) => f.connectedRename(connId, ref, name, orgId),
    onSuccess: (_it, v) => refreshConnected(qc, v.connId),
  });
}

export function useFilesConnectedMove(orgId?: string | null): UseMutationResult<FilesConnectedItem, ApiError, { connId: string; ref: string; parentRef: string }> {
  const f = useEndpoints(filesEndpoints);
  const qc = useQueryClient();
  return useMutation<FilesConnectedItem, ApiError, { connId: string; ref: string; parentRef: string }>({
    mutationFn: ({ connId, ref, parentRef }) => f.connectedMove(connId, ref, parentRef, orgId),
    onSuccess: (_it, v) => refreshConnected(qc, v.connId),
  });
}

export function useFilesConnectedTrash(orgId?: string | null): UseMutationResult<void, ApiError, { connId: string; ref: string }> {
  const f = useEndpoints(filesEndpoints);
  const qc = useQueryClient();
  return useMutation<void, ApiError, { connId: string; ref: string }>({
    mutationFn: ({ connId, ref }) => f.connectedTrash(connId, ref, orgId),
    onSuccess: (_d, v) => refreshConnected(qc, v.connId),
  });
}

/** Upload a file to a connected folder (resumable, straight to the platform). */
export function useFilesConnectedUpload(): UseMutationResult<FilesConnectedItem, ApiError, FilesConnectedUploadInput> {
  const api = useApi();
  const qc = useQueryClient();
  return useMutation<FilesConnectedItem, ApiError, FilesConnectedUploadInput>({
    mutationFn: (input) => filesConnectedUpload(api.http, input),
    onSuccess: (_it, input) => refreshConnected(qc, input.connectionId),
  });
}

/** Copy a connected file into a Files folder (null: the top of your My drive). */
export function useFilesCopyToFiles(): UseMutationResult<FilesItem, ApiError, { connId: string; ref: string; orgId: string; parentId: string | null }> {
  const f = useEndpoints(filesEndpoints);
  const qc = useQueryClient();
  return useMutation<FilesItem, ApiError, { connId: string; ref: string; orgId: string; parentId: string | null }>({
    mutationFn: ({ connId, ref, orgId, parentId }) => f.copyToFiles(connId, ref, { org_id: orgId, parent_id: parentId }),
    onSuccess: (it) => refreshFiles(qc, [], it.org_id),
  });
}

/** Pin a connected item into a Files folder as a shortcut colleagues open with their own connection. */
export function useFilesCreateShortcut(orgId: string): UseMutationResult<FilesItem, ApiError, { connId: string; ref: string; parentId: string | null }> {
  const f = useEndpoints(filesEndpoints);
  const qc = useQueryClient();
  return useMutation<FilesItem, ApiError, { connId: string; ref: string; parentId: string | null }>({
    mutationFn: ({ connId, ref, parentId }) => f.createShortcut(orgId, { connection_id: connId, ref, parent_id: parentId }),
    onSuccess: () => refreshFiles(qc, [], orgId),
  });
}

/** What a shortcut opens for you, checked live with your own connection. */
export function useFilesShortcut(id: string | null | undefined, options?: QueryOverrides<FilesShortcutTarget>): UseQueryResult<FilesShortcutTarget, ApiError> {
  const f = useEndpoints(filesEndpoints);
  return useApiQuery(filesKeys.shortcut(id ?? ""), ({ signal }) => f.shortcut(id ?? "", { signal }), { retry: retryServerErrors, staleTime: 15_000, ...options }, !!id);
}

/** Save a Files file to a connected Google Drive or OneDrive folder. */
export function useFilesSaveTo(): UseMutationResult<FilesConnectedItem, ApiError, { id: string; connId: string; parentRef?: string }> {
  const f = useEndpoints(filesEndpoints);
  const qc = useQueryClient();
  return useMutation<FilesConnectedItem, ApiError, { id: string; connId: string; parentRef?: string }>({
    mutationFn: ({ id, connId, parentRef }) => f.saveTo(id, { connection_id: connId, ...(parentRef ? { parent_ref: parentRef } : {}) }),
    onSuccess: (_it, v) => refreshConnected(qc, v.connId),
  });
}

// ---- public links ------------------------------------------------------------------------

/** What a public link opens (itemId: something inside a shared folder). Works signed out. */
export function useFilesPublic(token: string | null | undefined, itemId?: string | null, options?: QueryOverrides<FilesPublicView>): UseQueryResult<FilesPublicView, ApiError> {
  const f = useEndpoints(filesEndpoints);
  return useApiQuery(filesKeys.public(token ?? "", itemId ?? ""), ({ signal }) => f.publicView(token ?? "", itemId ?? undefined, { signal }), { retry: retryServerErrors, ...options }, !!token);
}

export function useFilesPublicChildren(token: string | null | undefined, folderId: string | null | undefined, params: FilesListParams = {}): PagedQueryResult<FilesPublicItem> {
  const f = useEndpoints(filesEndpoints);
  return usePagedQuery(
    filesKeys.publicChildren(token ?? "", folderId ?? "", params),
    (cursor, signal) => f.publicChildren(token ?? "", folderId ?? "", { ...params, cursor }, { signal }),
    { placeholderData: keepPreviousData },
    !!token && !!folderId,
  );
}

export function useFilesPublicText(token: string | null | undefined, itemId: string | null | undefined, enabled = true): UseQueryResult<FilesTextContent, ApiError> {
  const f = useEndpoints(filesEndpoints);
  return useApiQuery(filesKeys.publicText(token ?? "", itemId ?? ""), ({ signal }) => f.publicText(token ?? "", itemId ?? "", { signal }), { retry: retryServerErrors, staleTime: 60_000 }, !!token && !!itemId && enabled);
}

/** A public download or preview link (counted; limited per visitor). */
export function useFilesPublicUrl(token: string): UseMutationResult<FilesUrl, ApiError, { itemId: string; disposition?: "inline" | "attachment" }> {
  const f = useEndpoints(filesEndpoints);
  return useMutation<FilesUrl, ApiError, { itemId: string; disposition?: "inline" | "attachment" }>({ mutationFn: ({ itemId, disposition }) => f.publicUrl(token, itemId, disposition) });
}

/** An inline public preview link (images, PDF, video, audio), fetched once per item. */
export function useFilesPublicPreviewUrl(token: string | null | undefined, itemId: string | null | undefined, enabled = true): UseQueryResult<FilesUrl, ApiError> {
  const f = useEndpoints(filesEndpoints);
  return useApiQuery(
    ["files", "public", token ?? "", itemId ?? "", "preview-url"],
    ({ signal }) => f.publicUrl(token ?? "", itemId ?? "", "inline", { signal }),
    { staleTime: 10 * 60_000, retry: retryServerErrors },
    !!token && !!itemId && enabled,
  );
}

// ---- staff -------------------------------------------------------------------------------

export function useFilesAdminPublic(status: "active" | "disabled", options?: InfiniteOverrides<FilesAdminPublicItem>): PagedQueryResult<FilesAdminPublicItem> {
  const f = useEndpoints(filesEndpoints);
  return usePagedQuery(filesKeys.adminPublic(status), (cursor, signal) => f.adminPublic(status, cursor, { signal }), options);
}

export function useFilesAdminItem(id: string | null | undefined): UseQueryResult<FilesAdminPublicItem, ApiError> {
  const f = useEndpoints(filesEndpoints);
  return useApiQuery(filesKeys.adminItem(id ?? ""), ({ signal }) => f.adminItem(id ?? "", { signal }), { retry: retryServerErrors }, !!id);
}

/** Turn public access to an item off (`reason` shown to staff) or back on. Audited. */
export function useFilesAdminSetPublic(): UseMutationResult<FilesAdminPublicItem, ApiError, { id: string; disabled: boolean; reason?: string }> {
  const f = useEndpoints(filesEndpoints);
  const qc = useQueryClient();
  return useMutation<FilesAdminPublicItem, ApiError, { id: string; disabled: boolean; reason?: string }>({
    mutationFn: ({ id, disabled, reason }) => (disabled ? f.adminDisable(id, reason ?? "") : f.adminEnable(id)),
    onSuccess: (a) => {
      qc.setQueryData(filesKeys.adminItem(a.id), a);
      void qc.invalidateQueries({ queryKey: ["files", "admin", "public"] });
    },
  });
}

// ---- realtime ------------------------------------------------------------------------------

registerRealtimeHandler("files.changed", (qc, e) => {
  void qc.invalidateQueries({ queryKey: filesKeys.lists() });
  void qc.invalidateQueries({ queryKey: filesKeys.item(e.data.item_id) });
  void qc.invalidateQueries({ queryKey: filesKeys.drive(e.data.org_id) });
});
registerReconnectRefetch(filesKeys.all());
