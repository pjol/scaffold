import type { CallOptions, HttpClient } from "../api/http";
import { seg } from "../api/query-string";
import type { Page } from "../types";
import type {
  FilesActivity,
  FilesAdminPublicItem,
  FilesComment,
  FilesCreateRequest,
  FilesNewComment,
  FilesPublicLinkInfo,
  FilesPublicRequest,
  FilesRequest,
  FilesRequestUpload,
  FilesSearchFilters,
  FilesStorage,
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
    search: (orgId: string, q: string, cursor?: string, o?: CallOptions, filters: FilesSearchFilters = {}) =>
      http.get<Page<FilesSearchHit>>(`${org(orgId)}/search`, {
        ...o,
        query: {
          q,
          cursor,
          kind: filters.kind,
          owner: filters.owner,
          modified_after: filters.modified_after,
          modified_before: filters.modified_before,
          location: filters.location,
          starred: filters.starred ? "1" : undefined,
          shared_with_me: filters.shared_with_me ? "1" : undefined,
        },
      }),
    /** Your storage, and everyone's for org owners and admins. */
    storage: (orgId: string, o?: CallOptions) => http.get<FilesStorage>(`${org(orgId)}/storage`, o),
    myRequests: (orgId: string, cursor?: string, o?: CallOptions) => http.get<Page<FilesRequest>>(`${org(orgId)}/requests`, { ...o, query: { cursor } }),
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
    patchVersion: (id: string, versionId: string, body: { name?: string; keep_forever?: boolean }, o?: CallOptions) =>
      http.patch<{ items: FilesVersion[] }>(`${item(id)}/versions/${seg(versionId)}`, body, o),
    /** A video's poster frame (or a PDF's first page) drawn in the browser: a JPEG/PNG data URL, ≤ 700 KB. */
    putThumbnail: (id: string, dataUrl: string, o?: CallOptions) => http.put<void>(`${item(id)}/thumbnail`, { image: dataUrl }, o),
    /** Check out (lock) a file; check in or break the lock with checkIn. */
    lock: (id: string, note = "", o?: CallOptions) => http.post<FilesItem>(`${item(id)}/lock`, { note }, o),
    checkIn: (id: string, o?: CallOptions) => http.delete<FilesItem>(`${item(id)}/lock`, o),
    setColor: (id: string, color: string, o?: CallOptions) => http.put<FilesItem>(`${item(id)}/color`, { color }, o),
    /** Hand a top-level My drive item to another member (you keep edit access). */
    transferOwner: (id: string, userId: string, o?: CallOptions) => http.post<FilesItem>(`${item(id)}/owner`, { user_id: userId }, o),
    comments: (id: string, status?: "open" | "resolved", cursor?: string, o?: CallOptions) =>
      http.get<Page<FilesComment>>(`${item(id)}/comments`, { ...o, query: { status, cursor } }),
    addComment: (id: string, body: FilesNewComment, o?: CallOptions) => http.post<{ comment: FilesComment; not_notified: string[] }>(`${item(id)}/comments`, body, o),
    patchComment: (commentId: string, body: { body?: string; resolved?: boolean }, o?: CallOptions) => http.patch<FilesComment>(`${F}/comments/${seg(commentId)}`, body, o),
    deleteComment: (commentId: string, o?: CallOptions) => http.delete<void>(`${F}/comments/${seg(commentId)}`, o),
    publicLinks: (id: string, o?: CallOptions) => http.get<{ items: FilesPublicLinkInfo[] }>(`${item(id)}/public-links`, o),
    /** password: new one; "" removes it; leave out to keep it. */
    setLinkOptions: (id: string, linkId: string, body: { password?: string; allow_download?: boolean }, o?: CallOptions) =>
      http.put<{ items: FilesPublicLinkInfo[] }>(`${item(id)}/public-links/${seg(linkId)}`, body, o),
    folderRequests: (id: string, cursor?: string, o?: CallOptions) => http.get<Page<FilesRequest>>(`${item(id)}/requests`, { ...o, query: { cursor } }),
    createRequest: (folderId: string, body: FilesCreateRequest, o?: CallOptions) => http.post<FilesRequest>(`${item(folderId)}/requests`, body, o),
    closeRequest: (requestId: string, o?: CallOptions) => http.post<FilesRequest>(`${F}/requests/${seg(requestId)}/close`, {}, o),
    requestUploads: (requestId: string, o?: CallOptions) => http.get<{ items: FilesRequestUpload[] }>(`${F}/requests/${seg(requestId)}/uploads`, o),

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
    // `proof` shows the visitor typed the link's password (publicPassword).
    publicView: (token: string, itemId?: string, o?: CallOptions, proof?: string) =>
      http.get<FilesPublicView>(itemId ? `${pub(token)}/items/${seg(itemId)}` : pub(token), { ...o, auth: false, query: { proof } }),
    publicChildren: (token: string, folderId: string, p: FilesListParams & { cursor?: string | undefined } = {}, o?: CallOptions, proof?: string) =>
      http.get<Page<FilesPublicItem>>(`${pub(token)}/items/${seg(folderId)}/children`, { ...o, auth: false, query: { ...listQuery(p), proof } }),
    publicUrl: (token: string, itemId: string, disposition: "inline" | "attachment" = "attachment", o?: CallOptions, proof?: string) =>
      http.get<FilesUrl>(`${pub(token)}/items/${seg(itemId)}/url`, { ...o, auth: false, query: { disposition, proof } }),
    publicText: (token: string, itemId: string, o?: CallOptions, proof?: string) =>
      http.get<FilesTextContent>(`${pub(token)}/items/${seg(itemId)}/text`, { ...o, auth: false, query: { proof } }),
    publicPassword: (token: string, password: string, o?: CallOptions) =>
      http.post<{ proof: string; expires_at: string | null }>(`${pub(token)}/password`, { password }, { ...o, auth: false }),
    publicRequest: (token: string, o?: CallOptions) => http.get<FilesPublicRequest>(`${F}/requests/public/${seg(token)}`, { ...o, auth: false }),
    publicRequestUpload: (
      token: string,
      body: { name: string; content_type: string; size: number; uploader_name: string; uploader_email: string },
      o?: CallOptions,
    ) => http.post<FilesUploadTicket>(`${F}/requests/public/${seg(token)}/uploads`, body, { ...o, auth: false }),
    publicRequestComplete: (token: string, uploadId: string, o?: CallOptions) =>
      http.post<{ name: string; size: number }>(`${F}/requests/public/${seg(token)}/uploads/${seg(uploadId)}/complete`, {}, { ...o, auth: false }),

    // ---- staff
    adminPublic: (status: "active" | "disabled", cursor?: string, o?: CallOptions) =>
      http.get<Page<FilesAdminPublicItem>>(`/v1/admin/files/public`, { ...o, query: { status, cursor } }),
    adminItem: (id: string, o?: CallOptions) => http.get<FilesAdminPublicItem>(`/v1/admin/files/items/${seg(id)}`, o),
    adminDisable: (id: string, reason: string, o?: CallOptions) => http.post<FilesAdminPublicItem>(`/v1/admin/files/items/${seg(id)}/disable`, { reason }, o),
    adminEnable: (id: string, o?: CallOptions) => http.post<FilesAdminPublicItem>(`/v1/admin/files/items/${seg(id)}/enable`, {}, o),
  };
}

export type FilesEndpoints = ReturnType<typeof filesEndpoints>;
