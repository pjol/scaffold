import type { CallOptions, HttpClient } from "../api/http";
import { seg } from "../api/query-string";
import type { Page } from "../types";
import type {
  ReposBlameRange,
  ReposBlob,
  ReposComment,
  ReposCommentRequest,
  ReposCommit,
  ReposCommitDetail,
  ReposCommitRequest,
  ReposCommitResult,
  ReposComparison,
  ReposCreateCredentialRequest,
  ReposCreatedCredential,
  ReposCreateRequest,
  ReposCredential,
  ReposDiff,
  ReposDownload,
  ReposGitInfo,
  ReposImportPlan,
  ReposImportRequest,
  ReposMergeability,
  ReposMergeRequest,
  ReposOpenPullRequest,
  ReposPerson,
  ReposProtection,
  ReposProtectionRequest,
  ReposPull,
  ReposPullPage,
  ReposRef,
  ReposRefs,
  ReposRepo,
  ReposReviewer,
  ReposReviewRequest,
  ReposSearchResult,
  ReposSummary,
  ReposThread,
  ReposTimelineItem,
  ReposTree,
  ReposTreeCommits,
  ReposUpdatePullRequest,
  ReposUpdateRequest,
} from "./types";

type PageParams = { cursor?: string | undefined; limit?: number };
/** Where to read: a branch, tag or commit (default: the default branch) and a path. */
export type ReposAt = { ref?: string | undefined; path?: string | undefined };
export type ReposPullFilter = "open" | "closed" | "merged" | "all";

/** Repositories endpoints. */
export function reposEndpoints(http: HttpClient) {
  const R = "/v1/repos";
  const repo = (id: string) => `${R}/${seg(id)}`;
  const pull = (id: string, n: number) => `${repo(id)}/pulls/${n}`;
  return {
    // ---- repositories
    list: (orgId: string, params: PageParams & { q?: string } = {}, o?: CallOptions) =>
      http.get<Page<ReposSummary>>(`${R}/orgs/${seg(orgId)}/repos`, { ...o, query: { cursor: params.cursor, limit: params.limit, q: params.q || undefined } }),
    create: (body: ReposCreateRequest, o?: CallOptions) => http.post<ReposRepo>(R, body, o),
    importPreview: (folderId: string, o?: CallOptions) => http.get<ReposImportPlan>(`${R}/folders/${seg(folderId)}/preview`, o),
    importFolder: (body: ReposImportRequest, o?: CallOptions) => http.post<ReposRepo>(`${R}/import`, body, { ...o, timeoutMs: 330_000 }),
    get: (id: string, o?: CallOptions) => http.get<ReposRepo>(repo(id), o),
    update: (id: string, body: ReposUpdateRequest, o?: CallOptions) => http.patch<ReposRepo>(repo(id), body, o),
    gitInfo: (o?: CallOptions) => http.get<ReposGitInfo>(`${R}/git`, o),
    people: (id: string, q: string, o?: CallOptions) => http.get<{ items: ReposPerson[] }>(`${repo(id)}/people`, { ...o, query: { q: q || undefined } }),

    // ---- protection
    protections: (id: string, o?: CallOptions) => http.get<{ items: ReposProtection[] }>(`${repo(id)}/protections`, o),
    createProtection: (id: string, body: ReposProtectionRequest, o?: CallOptions) => http.post<ReposProtection>(`${repo(id)}/protections`, body, o),
    updateProtection: (id: string, pid: string, body: ReposProtectionRequest, o?: CallOptions) =>
      http.patch<ReposProtection>(`${repo(id)}/protections/${seg(pid)}`, body, o),
    deleteProtection: (id: string, pid: string, o?: CallOptions) => http.delete<void>(`${repo(id)}/protections/${seg(pid)}`, o),

    // ---- reading
    refs: (id: string, counts = false, o?: CallOptions) => http.get<ReposRefs>(`${repo(id)}/refs`, { ...o, query: { counts: counts ? "true" : undefined } }),
    tree: (id: string, at: ReposAt = {}, o?: CallOptions) => http.get<ReposTree>(`${repo(id)}/tree`, { ...o, query: { ref: at.ref, path: at.path || undefined } }),
    treeCommits: (id: string, at: ReposAt = {}, o?: CallOptions) =>
      http.get<ReposTreeCommits>(`${repo(id)}/tree/commits`, { ...o, query: { ref: at.ref, path: at.path || undefined } }),
    blob: (id: string, at: ReposAt & { path: string }, o?: CallOptions) => http.get<ReposBlob>(`${repo(id)}/blob`, { ...o, query: { ref: at.ref, path: at.path } }),
    download: (id: string, at: ReposAt & { path: string }, o?: CallOptions) =>
      http.get<ReposDownload>(`${repo(id)}/download`, { ...o, query: { ref: at.ref, path: at.path }, timeoutMs: 120_000 }),
    blame: (id: string, at: ReposAt & { path: string }, o?: CallOptions) =>
      http.get<{ sha: string; path: string; ranges: ReposBlameRange[] }>(`${repo(id)}/blame`, { ...o, query: { ref: at.ref, path: at.path } }),
    paths: (id: string, q: string, ref?: string, o?: CallOptions) =>
      http.get<{ sha: string; items: string[]; truncated: boolean }>(`${repo(id)}/paths`, { ...o, query: { q, ref } }),
    search: (id: string, q: string, ref?: string, o?: CallOptions) => http.get<ReposSearchResult>(`${repo(id)}/search`, { ...o, query: { q, ref } }),
    commits: (id: string, params: ReposAt & PageParams = {}, o?: CallOptions) =>
      http.get<Page<ReposCommit>>(`${repo(id)}/commits`, { ...o, query: { ref: params.ref, path: params.path || undefined, cursor: params.cursor, limit: params.limit } }),
    commit: (id: string, sha: string, path?: string, o?: CallOptions) =>
      http.get<ReposCommitDetail>(`${repo(id)}/commits/${seg(sha)}`, { ...o, query: { path: path || undefined } }),
    compare: (id: string, base: string, head: string, path?: string, o?: CallOptions) =>
      http.get<ReposComparison>(`${repo(id)}/compare`, { ...o, query: { base, head, path: path || undefined } }),

    // ---- writing
    /** One commit of file changes; 409 head_moved when the branch moved since `expected_head`. */
    commitChanges: (id: string, body: ReposCommitRequest, o?: CallOptions) => http.post<ReposCommitResult>(`${repo(id)}/commits`, body, { ...o, timeoutMs: 120_000 }),
    createBranch: (id: string, name: string, from?: string, o?: CallOptions) => http.post<ReposRef>(`${repo(id)}/branches`, { name, from }, o),
    deleteBranch: (id: string, name: string, o?: CallOptions) => http.delete<void>(`${repo(id)}/branches`, { ...o, query: { name } }),

    // ---- pull requests
    pulls: (id: string, params: PageParams & { state?: ReposPullFilter; q?: string; author?: "me"; review_requested?: "me" } = {}, o?: CallOptions) =>
      http.get<ReposPullPage>(`${repo(id)}/pulls`, {
        ...o,
        query: { cursor: params.cursor, limit: params.limit, state: params.state, q: params.q || undefined, author: params.author, review_requested: params.review_requested },
      }),
    openPull: (id: string, body: ReposOpenPullRequest, o?: CallOptions) => http.post<ReposPull>(`${repo(id)}/pulls`, body, o),
    pull: (id: string, n: number, o?: CallOptions) => http.get<ReposPull>(pull(id, n), o),
    updatePull: (id: string, n: number, body: ReposUpdatePullRequest, o?: CallOptions) => http.patch<ReposPull>(pull(id, n), body, o),
    closePull: (id: string, n: number, o?: CallOptions) => http.post<ReposPull>(`${pull(id, n)}/close`, undefined, o),
    reopenPull: (id: string, n: number, o?: CallOptions) => http.post<ReposPull>(`${pull(id, n)}/reopen`, undefined, o),
    timeline: (id: string, n: number, o?: CallOptions) => http.get<{ items: ReposTimelineItem[] }>(`${pull(id, n)}/timeline`, o),
    pullCommits: (id: string, n: number, o?: CallOptions) => http.get<{ items: ReposCommit[] }>(`${pull(id, n)}/commits`, o),
    pullDiff: (id: string, n: number, params: { path?: string; commit?: string } = {}, o?: CallOptions) =>
      http.get<{ base: string; head: string; diff: ReposDiff }>(`${pull(id, n)}/diff`, { ...o, query: { path: params.path, commit: params.commit } }),
    threads: (id: string, n: number, o?: CallOptions) => http.get<{ items: ReposThread[]; head_sha: string }>(`${pull(id, n)}/threads`, o),
    mergeability: (id: string, n: number, o?: CallOptions) => http.get<ReposMergeability>(`${pull(id, n)}/mergeability`, o),
    merge: (id: string, n: number, body: ReposMergeRequest, o?: CallOptions) => http.post<ReposPull>(`${pull(id, n)}/merge`, body, o),
    addComment: (id: string, n: number, body: ReposCommentRequest, o?: CallOptions) => http.post<ReposComment>(`${pull(id, n)}/comments`, body, o),
    editComment: (id: string, n: number, commentId: string, body: string, o?: CallOptions) =>
      http.patch<ReposComment>(`${pull(id, n)}/comments/${seg(commentId)}`, { body }, o),
    deleteComment: (id: string, n: number, commentId: string, o?: CallOptions) => http.delete<void>(`${pull(id, n)}/comments/${seg(commentId)}`, o),
    resolveThread: (id: string, n: number, commentId: string, resolved: boolean, o?: CallOptions) =>
      http.put<void>(`${pull(id, n)}/comments/${seg(commentId)}/resolved`, { resolved }, o),
    submitReview: (id: string, n: number, body: ReposReviewRequest, o?: CallOptions) => http.post<ReposPull>(`${pull(id, n)}/reviews`, body, o),
    requestReviewers: (id: string, n: number, userIds: string[], o?: CallOptions) =>
      http.post<{ items: ReposReviewer[] }>(`${pull(id, n)}/reviewers`, { user_ids: userIds }, o),
    removeReviewer: (id: string, n: number, userId: string, o?: CallOptions) => http.delete<void>(`${pull(id, n)}/reviewers/${seg(userId)}`, o),

    // ---- git credentials (your own)
    credentials: (params: PageParams = {}, o?: CallOptions) =>
      http.get<Page<ReposCredential>>(`${R}/credentials`, { ...o, query: { cursor: params.cursor, limit: params.limit } }),
    createCredential: (body: ReposCreateCredentialRequest, o?: CallOptions) => http.post<ReposCreatedCredential>(`${R}/credentials`, body, o),
    revokeCredential: (credId: string, o?: CallOptions) => http.delete<void>(`${R}/credentials/${seg(credId)}`, o),
  };
}

export type ReposEndpoints = ReturnType<typeof reposEndpoints>;
