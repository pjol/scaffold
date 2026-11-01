import type { CallOptions, HttpClient } from "../api/http";
import { seg } from "../api/query-string";
import type { Page } from "../types";
import type {
  FilesActivity,
  FilesAdminPublicItem,
  FilesArchive,
  FilesBatchAction,
  FilesBatchResult,
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
  FilesStartUploadRequest,
  FilesTextContent,
  FilesTypeSummary,
  FilesUploadTicket,
  FilesUrl,
  FilesVersion,
} from "./types";

function listQuery(p: FilesListParams & { cursor?: string | undefined } = {}) {
  return { sort: p.sort, dir: p.dir, thumbs: p.thumbs ? "1" : undefined, limit: p.limit, cursor: p.cursor };
}

/** Files endpoints: drive sections, folders, items, uploads, versions, public links, staff. */
export function filesEndpoints(http: HttpClient) {
  const F = "/v1/files";
  const org = (orgId: string) => `${F}/orgs/${seg(orgId)}`;
  const item = (id: string) => `${F}/items/${seg(id)}`;
  const pub = (token: string) => `${F}/public/${seg(token)}`;
  return {
    /** Item types other features registered (repositories…). */
    types: (o?: CallOptions) => http.get<{ items: FilesTypeSummary[] }>(`${F}/types`, o),
    drive: (orgId: string, o?: CallOptions) => http.get<FilesDriveInfo>(`${org(orgId)}/drive`, o),
    section: (orgId: string, section: FilesSection, p: FilesListParams & { cursor?: string | undefined } = {}, o?: CallOptions) =>
      http.get<Page<FilesItem>>(`${org(orgId)}/items`, { ...o, query: { section, ...listQuery(p) } }),
    search: (orgId: string, q: string, cursor?: string, o?: CallOptions) =>
      http.get<Page<FilesSearchHit>>(`${org(orgId)}/search`, { ...o, query: { q, cursor } }),
    createFolder: (orgId: string, body: FilesCreateFolderRequest, o?: CallOptions) => http.post<FilesItem>(`${org(orgId)}/folders`, body, o),
    /** A text file written by the server (also how empty files are made). */
    createFile: (orgId: string, body: FilesCreateFileRequest, o?: CallOptions) => http.post<FilesItem>(`${org(orgId)}/files`, body, o),
    startUpload: (orgId: string, body: FilesStartUploadRequest, o?: CallOptions) => http.post<FilesUploadTicket>(`${org(orgId)}/uploads`, body, o),
    /** The new file (or the file with its new version). `fail` answers 409 name_taken instead of numbering the name. */
    completeUpload: (uploadId: string, onConflict: "rename" | "fail" = "rename", o?: CallOptions) =>
      http.post<FilesItem>(`${F}/uploads/${seg(uploadId)}/complete`, { on_conflict: onConflict }, o),
    cancelUpload: (uploadId: string, o?: CallOptions) => http.delete<void>(`${F}/uploads/${seg(uploadId)}`, o),
    /** A 5-minute link that streams the items as one zip. */
    archive: (orgId: string, ids: string[], name?: string, o?: CallOptions) => http.post<FilesArchive>(`${org(orgId)}/archives`, { ids, name }, o),
    /** Deletes for good every trashed item in the org you manage. */
    emptyTrash: (orgId: string, o?: CallOptions) => http.delete<{ deleted: number }>(`${org(orgId)}/trash`, o),
    batch: (action: FilesBatchAction, ids: string[], parentId?: string | null, o?: CallOptions) =>
      http.post<{ results: FilesBatchResult[] }>(`${F}/batch`, { action, ids, parent_id: parentId ?? null }, o),

    item: (id: string, o?: CallOptions) => http.get<FilesItemDetail>(item(id), o),
    children: (id: string, p: FilesListParams & { cursor?: string | undefined } = {}, o?: CallOptions) =>
      http.get<Page<FilesItem>>(`${item(id)}/children`, { ...o, query: listQuery(p) }),
    url: (id: string, p: { disposition?: "inline" | "attachment"; version?: string } = {}, o?: CallOptions) =>
      http.get<FilesUrl>(`${item(id)}/url`, { ...o, query: { disposition: p.disposition, version: p.version } }),
    text: (id: string, version?: string, o?: CallOptions) => http.get<FilesTextContent>(`${item(id)}/text`, { ...o, query: { version } }),
    versions: (id: string, o?: CallOptions) => http.get<{ items: FilesVersion[] }>(`${item(id)}/versions`, o),
    activity: (id: string, cursor?: string, o?: CallOptions) => http.get<Page<FilesActivity>>(`${item(id)}/activity`, { ...o, query: { cursor } }),
    star: (id: string, on: boolean, o?: CallOptions) => (on ? http.put<void>(`${item(id)}/star`, {}, o) : http.delete<void>(`${item(id)}/star`, o)),
    rename: (id: string, name: string, o?: CallOptions) => http.patch<FilesItem>(item(id), { name }, o),
    /** parentId null: the top of your My drive. */
    move: (id: string, parentId: string | null, o?: CallOptions) => http.post<FilesItem>(`${item(id)}/move`, { parent_id: parentId }, o),
    copy: (id: string, parentId: string | null, name?: string, o?: CallOptions) => http.post<FilesItem>(`${item(id)}/copy`, { parent_id: parentId, name }, o),
    trash: (id: string, o?: CallOptions) => http.post<void>(`${item(id)}/trash`, {}, o),
    restore: (id: string, o?: CallOptions) => http.post<FilesItem>(`${item(id)}/restore`, {}, o),
    /** Trashed items only (manage). */
    remove: (id: string, o?: CallOptions) => http.delete<void>(item(id), o),
    restoreVersion: (id: string, versionId: string, o?: CallOptions) => http.post<FilesItem>(`${item(id)}/versions/${seg(versionId)}/restore`, {}, o),

    // ---- public links (no account)
    publicView: (token: string, itemId?: string, o?: CallOptions) =>
      http.get<FilesPublicView>(itemId ? `${pub(token)}/items/${seg(itemId)}` : pub(token), { ...o, auth: false }),
    publicChildren: (token: string, folderId: string, p: FilesListParams & { cursor?: string | undefined } = {}, o?: CallOptions) =>
      http.get<Page<FilesPublicItem>>(`${pub(token)}/items/${seg(folderId)}/children`, { ...o, auth: false, query: listQuery(p) }),
    publicUrl: (token: string, itemId: string, disposition: "inline" | "attachment" = "attachment", o?: CallOptions) =>
      http.get<FilesUrl>(`${pub(token)}/items/${seg(itemId)}/url`, { ...o, auth: false, query: { disposition } }),
    publicText: (token: string, itemId: string, o?: CallOptions) => http.get<FilesTextContent>(`${pub(token)}/items/${seg(itemId)}/text`, { ...o, auth: false }),

    // ---- staff
    adminPublic: (status: "active" | "disabled", cursor?: string, o?: CallOptions) =>
      http.get<Page<FilesAdminPublicItem>>(`/v1/admin/files/public`, { ...o, query: { status, cursor } }),
    adminItem: (id: string, o?: CallOptions) => http.get<FilesAdminPublicItem>(`/v1/admin/files/items/${seg(id)}`, o),
    adminDisable: (id: string, reason: string, o?: CallOptions) => http.post<FilesAdminPublicItem>(`/v1/admin/files/items/${seg(id)}/disable`, { reason }, o),
    adminEnable: (id: string, o?: CallOptions) => http.post<FilesAdminPublicItem>(`/v1/admin/files/items/${seg(id)}/enable`, {}, o),
  };
}

export type FilesEndpoints = ReturnType<typeof filesEndpoints>;
