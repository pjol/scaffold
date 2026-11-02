import type { CallOptions, HttpClient } from "../api/http";
import { seg } from "../api/query-string";
import type { Page } from "../types";
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
  FilesConnectedSource,
  FilesConnectedText,
  FilesConnectedUploadTicket,
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
  const conn = (connId: string) => `${F}/connected/${seg(connId)}`;
  const citem = (connId: string, ref: string) => `${conn(connId)}/items/${seg(ref)}`;
  const orgQ = (orgId?: string | null) => (orgId ? { org_id: orgId } : {});
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

    // ---- connected drives (Google Drive, OneDrive, SharePoint), live with your own connection
    /** The app's drive and your connected accounts, with capabilities still on offer. */
    sources: (orgId?: string | null, o?: CallOptions) => http.get<FilesSources>(`${F}/sources`, { ...o, query: orgQ(orgId) }),
    connectedDrives: (connId: string, orgId?: string | null, cursor?: string, o?: CallOptions) =>
      http.get<Page<FilesConnectedDrive> & { source: FilesConnectedSource }>(`${conn(connId)}/drives`, { ...o, query: { ...orgQ(orgId), cursor } }),
    connectedItem: (connId: string, ref: string, orgId?: string | null, o?: CallOptions) => http.get<FilesConnectedDetail>(citem(connId, ref), { ...o, query: orgQ(orgId) }),
    connectedChildren: (connId: string, ref: string, p: FilesListParams & { cursor?: string | undefined } = {}, orgId?: string | null, o?: CallOptions) =>
      http.get<Page<FilesConnectedItem>>(`${citem(connId, ref)}/children`, { ...o, query: { ...listQuery(p), ...orgQ(orgId) } }),
    connectedSearch: (connId: string, q: string, cursor?: string, orgId?: string | null, o?: CallOptions) =>
      http.get<Page<FilesConnectedItem>>(`${conn(connId)}/search`, { ...o, query: { q, cursor, ...orgQ(orgId) } }),
    /** A 15-minute link streamed through the API (inline previews of documents are PDF renderings). */
    connectedUrl: (connId: string, ref: string, disposition: "inline" | "attachment" = "attachment", orgId?: string | null, o?: CallOptions) =>
      http.get<FilesUrl>(`${citem(connId, ref)}/url`, { ...o, query: { disposition, ...orgQ(orgId) } }),
    connectedText: (connId: string, ref: string, orgId?: string | null, o?: CallOptions) => http.get<FilesConnectedText>(`${citem(connId, ref)}/text`, { ...o, query: orgQ(orgId) }),
    connectedSharing: (connId: string, ref: string, orgId?: string | null, o?: CallOptions) =>
      http.get<FilesConnectedSharing>(`${citem(connId, ref)}/sharing`, { ...o, query: orgQ(orgId) }),
    connectedCreateFolder: (connId: string, body: { parent_ref: string; name: string }, orgId?: string | null, o?: CallOptions) =>
      http.post<FilesConnectedItem>(`${conn(connId)}/folders`, body, { ...o, query: orgQ(orgId) }),
    connectedRename: (connId: string, ref: string, name: string, orgId?: string | null, o?: CallOptions) =>
      http.patch<FilesConnectedItem>(citem(connId, ref), { name }, { ...o, query: orgQ(orgId) }),
    /** Within the same drive. */
    connectedMove: (connId: string, ref: string, parentRef: string, orgId?: string | null, o?: CallOptions) =>
      http.post<FilesConnectedItem>(`${citem(connId, ref)}/move`, { parent_ref: parentRef }, { ...o, query: orgQ(orgId) }),
    /** To the platform's trash or recycle bin. */
    connectedTrash: (connId: string, ref: string, orgId?: string | null, o?: CallOptions) => http.post<void>(`${citem(connId, ref)}/trash`, {}, { ...o, query: orgQ(orgId) }),
    connectedStartUpload: (
      connId: string,
      body: { parent_ref?: string; item_ref?: string; name: string; content_type: string; size: number },
      orgId?: string | null,
      o?: CallOptions,
    ) => http.post<FilesConnectedUploadTicket>(`${conn(connId)}/uploads`, body, { ...o, query: orgQ(orgId) }),
    /** Copy a connected file into a Files folder (it becomes the app's own). */
    copyToFiles: (connId: string, ref: string, body: { org_id: string; parent_id?: string | null }, o?: CallOptions) =>
      http.post<FilesItem>(`${citem(connId, ref)}/copy-to-files`, body, { ...o, timeoutMs: 120_000 }),
    /** Pin a connected item into a Files folder as a shortcut. */
    createShortcut: (orgId: string, body: { parent_id?: string | null; connection_id: string; ref: string }, o?: CallOptions) =>
      http.post<FilesItem>(`${org(orgId)}/shortcuts`, body, o),
    shortcut: (id: string, o?: CallOptions) => http.get<FilesShortcutTarget>(`${item(id)}/shortcut`, o),
    /** Save a Files file to your Google Drive or OneDrive (parent_ref "" = the top). */
    saveTo: (id: string, body: { connection_id: string; parent_ref?: string }, o?: CallOptions) =>
      http.post<FilesConnectedItem>(`${item(id)}/save-to`, body, { ...o, timeoutMs: 120_000 }),

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
