import type { CallOptions, HttpClient } from "../api/http";
import { seg } from "../api/query-string";
import type { Page } from "../types";
import type {
  DocsBlock,
  DocsComment,
  DocsCommentRequest,
  DocsConnectedComment,
  DocsConnectedItem,
  DocsConnectedList,
  DocsConnectedPageDetail,
  DocsConnectedSources,
  DocsCreatePageRequest,
  DocsCreateRowRequest,
  DocsFile,
  DocsImportRequest,
  DocsMoveRequest,
  DocsOp,
  DocsOpsResult,
  DocsPage,
  DocsPageDetail,
  DocsPageSummary,
  DocsProperty,
  DocsPublicPage,
  DocsRefs,
  DocsResolvedLink,
  DocsRow,
  DocsRowPage,
  DocsRowsParams,
  DocsRowValuesRequest,
  DocsSearchHit,
  DocsSidebar,
  DocsSnapshot,
  DocsSyncResult,
  DocsUpdatePageRequest,
  DocsValue,
  DocsView,
  DocsViewer,
  DocsViewRequest,
} from "./types";

function rowsQuery(p: DocsRowsParams) {
  return {
    view: p.view,
    filters: p.filters ? JSON.stringify(p.filters) : undefined,
    sorts: p.sorts ? JSON.stringify(p.sorts) : undefined,
    q: p.q || undefined,
    cursor: p.cursor,
    limit: p.limit,
  };
}

/** `/v1/docs` endpoints: pages, block operations, databases, comments, history, presence, files, public pages. */
export function docsEndpoints(http: HttpClient) {
  const D = "/v1/docs";
  const org = (id: string) => `${D}/orgs/${seg(id)}`;
  const page = (id: string) => `${D}/pages/${seg(id)}`;
  const conn = (id: string) => `${D}/connected/${seg(id)}`;
  return {
    // ---- workspace
    sidebar: (orgId: string, o?: CallOptions) => http.get<DocsSidebar>(`${org(orgId)}/sidebar`, o),
    recent: (orgId: string, o?: CallOptions) => http.get<{ items: DocsPageSummary[] }>(`${org(orgId)}/recent`, o),
    trash: (orgId: string, params: { q?: string; cursor?: string } = {}, o?: CallOptions) =>
      http.get<Page<DocsPageSummary>>(`${org(orgId)}/trash`, { ...o, query: { q: params.q || undefined, cursor: params.cursor } }),
    /** Empty `q`: recently opened pages. `min_level: "edit"` for move pickers. */
    search: (orgId: string, params: { q?: string; kind?: "page" | "database"; rows?: boolean; min_level?: "view" | "edit"; limit?: number } = {}, o?: CallOptions) =>
      http.get<{ items: DocsSearchHit[] }>(`${org(orgId)}/search`, {
        ...o,
        query: { q: params.q || undefined, kind: params.kind, rows: params.rows === false ? "false" : undefined, min_level: params.min_level, limit: params.limit },
      }),
    createPage: (orgId: string, body: DocsCreatePageRequest, o?: CallOptions) => http.post<DocsPage>(`${org(orgId)}/pages`, body, o),
    importMarkdown: (orgId: string, body: DocsImportRequest, o?: CallOptions) => http.post<DocsPage>(`${org(orgId)}/import`, body, o),

    // ---- pages
    page: (id: string, o?: CallOptions) => http.get<DocsPageDetail>(page(id), o),
    children: (id: string, o?: CallOptions) => http.get<{ items: DocsPageSummary[] }>(`${page(id)}/children`, o),
    /** What changed after `since` (or the whole page with `full`). */
    sync: (id: string, since: number, o?: CallOptions) => http.get<DocsSyncResult>(`${page(id)}/sync`, { ...o, query: { since } }),
    backlinks: (id: string, o?: CallOptions) => http.get<{ items: DocsPageSummary[] }>(`${page(id)}/backlinks`, o),
    /** The page as a Markdown file's text. */
    exportMarkdown: (id: string, o?: CallOptions) => http.text("GET", `${page(id)}/export`, { ...o, accept: "text/markdown, text/plain;q=0.9" }),
    applyOps: (id: string, clientId: string, ops: DocsOp[], o?: CallOptions) => http.post<DocsOpsResult>(`${page(id)}/ops`, { client_id: clientId, ops }, o),
    updatePage: (id: string, body: DocsUpdatePageRequest, o?: CallOptions) => http.patch<DocsPage>(page(id), body, o),
    movePage: (id: string, body: DocsMoveRequest, o?: CallOptions) => http.post<DocsPage>(`${page(id)}/move`, body, o),
    duplicatePage: (id: string, o?: CallOptions) => http.post<DocsPage>(`${page(id)}/duplicate`, undefined, o),
    trashPage: (id: string, o?: CallOptions) => http.delete<void>(page(id), o),
    restorePage: (id: string, o?: CallOptions) => http.post<DocsPage>(`${page(id)}/restore`, undefined, o),
    deletePermanently: (id: string, o?: CallOptions) => http.delete<void>(`${page(id)}/permanent`, o),
    favorite: (id: string, o?: CallOptions) => http.put<void>(`${page(id)}/favorite`, undefined, o),
    unfavorite: (id: string, o?: CallOptions) => http.delete<void>(`${page(id)}/favorite`, o),

    // ---- files
    /** Attach a finished upload (purpose docs_file) to the page. */
    registerFile: (pageId: string, body: { key: string; name: string; size: number }, o?: CallOptions) => http.post<DocsFile>(`${page(pageId)}/files`, body, o),
    /** A short-lived download link. */
    file: (fileId: string, o?: CallOptions) => http.get<DocsFile>(`${D}/files/${seg(fileId)}`, o),

    // ---- presence
    presence: (id: string, o?: CallOptions) => http.get<{ items: DocsViewer[] }>(`${page(id)}/presence`, o),
    beat: (id: string, body: { session_id: string; block_id: string | null }, o?: CallOptions) =>
      http.post<{ items: DocsViewer[]; interval_seconds: number }>(`${page(id)}/presence`, body, o),
    leave: (id: string, sessionId: string, o?: CallOptions) => http.delete<void>(`${page(id)}/presence/${seg(sessionId)}`, o),

    // ---- comments
    comments: (id: string, status: "open" | "resolved" | "all" = "open", o?: CallOptions) => http.get<{ items: DocsComment[] }>(`${page(id)}/comments`, { ...o, query: { status } }),
    createComment: (id: string, body: DocsCommentRequest, o?: CallOptions) => http.post<DocsComment>(`${page(id)}/comments`, body, o),
    editComment: (commentId: string, body: DocsCommentRequest["body"], o?: CallOptions) => http.patch<DocsComment>(`${D}/comments/${seg(commentId)}`, { body }, o),
    deleteComment: (commentId: string, o?: CallOptions) => http.delete<void>(`${D}/comments/${seg(commentId)}`, o),
    resolveComment: (commentId: string, o?: CallOptions) => http.post<void>(`${D}/comments/${seg(commentId)}/resolve`, undefined, o),
    reopenComment: (commentId: string, o?: CallOptions) => http.delete<void>(`${D}/comments/${seg(commentId)}/resolve`, o),

    // ---- history
    history: (id: string, cursor?: string, o?: CallOptions) => http.get<Page<DocsSnapshot>>(`${page(id)}/history`, { ...o, query: { cursor } }),
    snapshot: (snapshotId: string, o?: CallOptions) => http.get<{ snapshot: DocsSnapshot & { blocks: DocsBlock[] }; refs: DocsRefs }>(`${D}/history/${seg(snapshotId)}`, o),
    restoreSnapshot: (snapshotId: string, o?: CallOptions) => http.post<{ version: number }>(`${D}/history/${seg(snapshotId)}/restore`, undefined, o),

    // ---- databases
    rows: (dbId: string, params: DocsRowsParams = {}, o?: CallOptions) => http.get<DocsRowPage>(`${D}/databases/${seg(dbId)}/rows`, { ...o, query: rowsQuery(params) }),
    createRow: (dbId: string, body: DocsCreateRowRequest, o?: CallOptions) => http.post<DocsPage>(`${D}/databases/${seg(dbId)}/rows`, body, o),
    setRowValues: (rowId: string, body: DocsRowValuesRequest, o?: CallOptions) => http.patch<DocsPage>(`${page(rowId)}/properties`, body, o),
    putSchema: (dbId: string, properties: DocsProperty[], o?: CallOptions) => http.put<DocsPage>(`${D}/databases/${seg(dbId)}/schema`, { properties }, o),
    createView: (dbId: string, body: DocsViewRequest & { type: DocsView["type"] }, o?: CallOptions) => http.post<DocsView>(`${D}/databases/${seg(dbId)}/views`, body, o),
    updateView: (viewId: string, body: DocsViewRequest, o?: CallOptions) => http.patch<DocsView>(`${D}/views/${seg(viewId)}`, body, o),
    deleteView: (viewId: string, o?: CallOptions) => http.delete<void>(`${D}/views/${seg(viewId)}`, o),

    // ---- connected platforms (Notion, Google Docs), live with the caller's own accounts
    /** The app's own pages, each connected account, and what could still be connected (`FeatureSources`). */
    sources: (orgId?: string | null, o?: CallOptions) => http.get<DocsConnectedSources>(`${D}/sources`, { ...o, query: { org_id: orgId || undefined } }),
    /** A level of a connection's tree: the roots (Notion: pages shared with the app; Google: `section` recent or shared), or a page's subpages. */
    connectedTree: (connId: string, p: { parent?: string; section?: "recent" | "shared"; cursor?: string; orgId?: string | null } = {}, o?: CallOptions) =>
      http.get<DocsConnectedList>(`${conn(connId)}/tree`, { ...o, query: { parent: p.parent || undefined, section: p.section, cursor: p.cursor, org_id: p.orgId || undefined } }),
    connectedSearch: (connId: string, q: string, p: { cursor?: string; orgId?: string | null } = {}, o?: CallOptions) =>
      http.get<DocsConnectedList>(`${conn(connId)}/search`, { ...o, query: { q, cursor: p.cursor, org_id: p.orgId || undefined } }),
    connectedPage: (connId: string, ref: string, orgId?: string | null, o?: CallOptions) =>
      http.get<DocsConnectedPageDetail>(`${conn(connId)}/pages/${seg(ref)}`, { ...o, query: { org_id: orgId || undefined } }),
    connectedOps: (connId: string, ref: string, body: { session_id: string; client_id: string; ops: DocsOp[] }, orgId?: string | null, o?: CallOptions) =>
      http.post<DocsOpsResult>(`${conn(connId)}/pages/${seg(ref)}/ops`, body, { ...o, query: { org_id: orgId || undefined } }),
    connectedSync: (connId: string, ref: string, sessionId: string, orgId?: string | null, o?: CallOptions) =>
      http.get<DocsSyncResult>(`${conn(connId)}/pages/${seg(ref)}/sync`, { ...o, query: { session_id: sessionId, org_id: orgId || undefined } }),
    connectedRename: (connId: string, ref: string, title: string, o?: CallOptions) => http.patch<{ title: string }>(`${conn(connId)}/pages/${seg(ref)}`, { title }, o),
    connectedCreatePage: (connId: string, body: { parent_ref?: string; title: string }, o?: CallOptions) => http.post<DocsConnectedItem>(`${conn(connId)}/pages`, body, o),
    connectedRows: (connId: string, dbRef: string, params: DocsRowsParams = {}, o?: CallOptions) =>
      http.get<DocsRowPage>(`${conn(connId)}/databases/${seg(dbRef)}/rows`, { ...o, query: rowsQuery(params) }),
    connectedCreateRow: (connId: string, dbRef: string, body: { title?: string; values?: Record<string, DocsValue> }, o?: CallOptions) =>
      http.post<DocsRow>(`${conn(connId)}/databases/${seg(dbRef)}/rows`, body, o),
    connectedSetRow: (connId: string, rowRef: string, body: { title?: string; values?: Record<string, DocsValue> }, o?: CallOptions) =>
      http.patch<DocsRow>(`${conn(connId)}/rows/${seg(rowRef)}`, body, o),
    connectedComments: (connId: string, ref: string, o?: CallOptions) => http.get<{ items: DocsConnectedComment[] }>(`${conn(connId)}/pages/${seg(ref)}/comments`, o),
    connectedAddComment: (connId: string, ref: string, body: { body: DocsCommentRequest["body"]; thread_id?: string }, o?: CallOptions) =>
      http.post<DocsConnectedComment>(`${conn(connId)}/pages/${seg(ref)}/comments`, body, o),
    /** Copy into Docs: a new page in the org with the connected page's content. */
    connectedCopy: (connId: string, ref: string, body: { org_id: string; parent_id?: string | null }, o?: CallOptions) =>
      http.post<DocsPage>(`${conn(connId)}/pages/${seg(ref)}/copy`, body, o),
    /** A connected-page block, as the caller sees it with their own connection. */
    resolveConnected: (p: { provider: string; ref: string; account_id?: string; org_id?: string | null }, o?: CallOptions) =>
      http.get<DocsResolvedLink>(`${D}/connected/resolve`, { ...o, query: { provider: p.provider, ref: p.ref, account_id: p.account_id || undefined, org_id: p.org_id || undefined } }),

    // ---- public (no account)
    publicPage: (token: string, pageId?: string, o?: CallOptions) =>
      http.get<DocsPublicPage>(pageId ? `${D}/public/${seg(token)}/pages/${seg(pageId)}` : `${D}/public/${seg(token)}`, { ...o, auth: false }),
    publicFile: (token: string, fileId: string, o?: CallOptions) => http.get<DocsFile>(`${D}/public/${seg(token)}/files/${seg(fileId)}`, { ...o, auth: false }),
    publicRows: (token: string, dbId: string, params: DocsRowsParams = {}, o?: CallOptions) =>
      http.get<DocsRowPage>(`${D}/public/${seg(token)}/databases/${seg(dbId)}/rows`, { ...o, auth: false, query: rowsQuery(params) }),
  };
}

export type DocsEndpoints = ReturnType<typeof docsEndpoints>;
