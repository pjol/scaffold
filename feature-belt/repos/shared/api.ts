import type { CallOptions, HttpClient } from "../api/http";
import { seg } from "../api/query-string";
import type { FeatureSources } from "../sources";
import type { Page } from "../types";
import type {
  ReposChecks,
  ReposConnectedPage,
  ReposConnectedSort,
  ReposCreateLinkRequest,
  ReposImportConnectedRequest,
  ReposLink,
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
  ReposApplySuggestionsRequest,
  ReposArchiveFormat,
  ReposArchiveLink,
  ReposCITokens,
  ReposCreatedCIToken,
  ReposCreateIssueRequest,
  ReposInsights,
  ReposIssue,
  ReposIssueComment,
  ReposIssuePage,
  ReposIssueTimeline,
  ReposLabel,
  ReposLabelRequest,
  ReposLabelRow,
  ReposMilestone,
  ReposMilestoneRequest,
  ReposMention,
  ReposPendingReview,
  ReposReaction,
  ReposReactionEmoji,
  ReposReactionSubject,
  ReposRelease,
  ReposReleaseAsset,
  ReposReleaseRequest,
  ReposTemplate,
  ReposUpdateIssueRequest,
  ReposWebhook,
  ReposWebhookDelivery,
  ReposWebhookEvent,
  ReposWebhookRequest,
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

    // ---- connected platforms
    /** The app's own repositories, each connected GitHub/GitLab account, and what could be connected. */
    sources: (orgId: string | undefined, o?: CallOptions) => http.get<FeatureSources>(`${R}/sources`, { ...o, query: { org_id: orgId } }),
    connected: (
      connectionId: string,
      params: PageParams & { q?: string; sort?: ReposConnectedSort; owner?: string; orgId?: string | undefined } = {},
      o?: CallOptions,
    ) =>
      http.get<ReposConnectedPage>(`${R}/connections/${seg(connectionId)}/repos`, {
        ...o,
        query: { cursor: params.cursor, q: params.q || undefined, sort: params.sort, owner: params.owner || undefined, org_id: params.orgId },
      }),
    importConnected: (body: ReposImportConnectedRequest, o?: CallOptions) => http.post<ReposRepo>(`${R}/connected/import`, body, { ...o, timeoutMs: 330_000 }),
    createLink: (body: ReposCreateLinkRequest, o?: CallOptions) => http.post<ReposLink>(`${R}/links`, body, o),
    link: (linkId: string, o?: CallOptions) => http.get<ReposLink>(`${R}/links/${seg(linkId)}`, o),
    checks: (id: string, sha: string, o?: CallOptions) => http.get<ReposChecks>(`${repo(id)}/commits/${seg(sha)}/checks`, o),

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
    timeline: (id: string, n: number, o?: CallOptions) => http.get<{ items: ReposTimelineItem[]; mentions?: ReposMention[] }>(`${pull(id, n)}/timeline`, o),
    pullCommits: (id: string, n: number, o?: CallOptions) => http.get<{ items: ReposCommit[] }>(`${pull(id, n)}/commits`, o),
    pullDiff: (id: string, n: number, params: { path?: string; commit?: string; ignoreWhitespace?: boolean } = {}, o?: CallOptions) =>
      http.get<{ base: string; head: string; diff: ReposDiff }>(`${pull(id, n)}/diff`, {
        ...o,
        query: { path: params.path, commit: params.commit, ignore_ws: params.ignoreWhitespace ? "1" : undefined },
      }),
    threads: (id: string, n: number, o?: CallOptions) => http.get<{ items: ReposThread[]; head_sha: string; mentions?: ReposMention[] }>(`${pull(id, n)}/threads`, o),
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
    setDraft: (id: string, n: number, draft: boolean, o?: CallOptions) => http.post<ReposPull>(`${pull(id, n)}/${draft ? "draft" : "ready"}`, undefined, o),
    setPullLabels: (id: string, n: number, ids: string[], o?: CallOptions) => http.put<ReposPull>(`${pull(id, n)}/labels`, { ids }, o),
    setPullAssignees: (id: string, n: number, ids: string[], o?: CallOptions) => http.put<ReposPull>(`${pull(id, n)}/assignees`, { ids }, o),
    enableAutoMerge: (id: string, n: number, method: ReposMergeRequest["method"], o?: CallOptions) => http.put<ReposPull>(`${pull(id, n)}/auto-merge`, { method }, o),
    disableAutoMerge: (id: string, n: number, o?: CallOptions) => http.delete<ReposPull>(`${pull(id, n)}/auto-merge`, o),
    updateBranch: (id: string, n: number, expectedHead?: string, o?: CallOptions) => http.post<ReposPull>(`${pull(id, n)}/update-branch`, { expected_head: expectedHead }, o),
    applySuggestions: (id: string, n: number, body: ReposApplySuggestionsRequest, o?: CallOptions) => http.post<ReposCommitResult>(`${pull(id, n)}/suggestions`, body, o),
    viewedFiles: (id: string, n: number, o?: CallOptions) => http.get<{ items: string[] }>(`${pull(id, n)}/viewed`, o),
    setViewed: (id: string, n: number, path: string, viewed: boolean, o?: CallOptions) =>
      viewed ? http.put<void>(`${pull(id, n)}/viewed`, { path }, o) : http.delete<void>(`${pull(id, n)}/viewed`, { ...o, body: { path } }),
    pendingReview: (id: string, n: number, o?: CallOptions) => http.get<ReposPendingReview>(`${pull(id, n)}/reviews/pending`, o),
    discardPendingReview: (id: string, n: number, o?: CallOptions) => http.delete<void>(`${pull(id, n)}/reviews/pending`, o),

    // ---- issues
    issues: (id: string, params: PageParams & { q?: string } = {}, o?: CallOptions) =>
      http.get<ReposIssuePage>(`${repo(id)}/issues`, { ...o, query: { cursor: params.cursor, limit: params.limit, q: params.q || undefined } }),
    createIssue: (id: string, body: ReposCreateIssueRequest, o?: CallOptions) => http.post<ReposIssue>(`${repo(id)}/issues`, body, o),
    issue: (id: string, n: number, o?: CallOptions) => http.get<ReposIssue>(`${repo(id)}/issues/${n}`, o),
    updateIssue: (id: string, n: number, body: ReposUpdateIssueRequest, o?: CallOptions) => http.patch<ReposIssue>(`${repo(id)}/issues/${n}`, body, o),
    deleteIssue: (id: string, n: number, o?: CallOptions) => http.delete<void>(`${repo(id)}/issues/${n}`, o),
    setIssueLabels: (id: string, n: number, ids: string[], o?: CallOptions) => http.put<ReposIssue>(`${repo(id)}/issues/${n}/labels`, { ids }, o),
    setIssueAssignees: (id: string, n: number, ids: string[], o?: CallOptions) => http.put<ReposIssue>(`${repo(id)}/issues/${n}/assignees`, { ids }, o),
    issueTimeline: (id: string, n: number, o?: CallOptions) => http.get<ReposIssueTimeline>(`${repo(id)}/issues/${n}/timeline`, o),
    addIssueComment: (id: string, n: number, body: string, o?: CallOptions) => http.post<ReposIssueComment>(`${repo(id)}/issues/${n}/comments`, { body }, o),
    editIssueComment: (id: string, n: number, commentId: string, body: string, o?: CallOptions) =>
      http.patch<ReposIssueComment>(`${repo(id)}/issues/${n}/comments/${seg(commentId)}`, { body }, o),
    deleteIssueComment: (id: string, n: number, commentId: string, o?: CallOptions) => http.delete<void>(`${repo(id)}/issues/${n}/comments/${seg(commentId)}`, o),
    react: (id: string, subject: { type: ReposReactionSubject; id: string }, emoji: ReposReactionEmoji, add: boolean, o?: CallOptions) => {
      const body = { subject_type: subject.type, subject_id: subject.id, emoji };
      return add ? http.put<{ items: ReposReaction[] }>(`${repo(id)}/reactions`, body, o) : http.delete<{ items: ReposReaction[] }>(`${repo(id)}/reactions`, { ...o, body });
    },
    templates: (id: string, kind: "issue" | "pull", o?: CallOptions) => http.get<{ items: ReposTemplate[] }>(`${repo(id)}/templates`, { ...o, query: { kind } }),
    labels: (id: string, o?: CallOptions) => http.get<{ items: ReposLabelRow[] }>(`${repo(id)}/labels`, o),
    createLabel: (id: string, body: ReposLabelRequest, o?: CallOptions) => http.post<ReposLabel>(`${repo(id)}/labels`, body, o),
    updateLabel: (id: string, labelId: string, body: ReposLabelRequest, o?: CallOptions) => http.patch<ReposLabel>(`${repo(id)}/labels/${seg(labelId)}`, body, o),
    deleteLabel: (id: string, labelId: string, o?: CallOptions) => http.delete<void>(`${repo(id)}/labels/${seg(labelId)}`, o),
    milestones: (id: string, state: "open" | "closed" | "all" = "open", o?: CallOptions) => http.get<{ items: ReposMilestone[] }>(`${repo(id)}/milestones`, { ...o, query: { state } }),
    milestone: (id: string, milestoneId: string, o?: CallOptions) => http.get<ReposMilestone>(`${repo(id)}/milestones/${seg(milestoneId)}`, o),
    createMilestone: (id: string, body: ReposMilestoneRequest, o?: CallOptions) => http.post<ReposMilestone>(`${repo(id)}/milestones`, body, o),
    updateMilestone: (id: string, milestoneId: string, body: ReposMilestoneRequest, o?: CallOptions) =>
      http.patch<ReposMilestone>(`${repo(id)}/milestones/${seg(milestoneId)}`, body, o),
    deleteMilestone: (id: string, milestoneId: string, o?: CallOptions) => http.delete<void>(`${repo(id)}/milestones/${seg(milestoneId)}`, o),

    // ---- statuses, CI tokens
    commitStates: (id: string, shas: string[], o?: CallOptions) =>
      http.get<{ states: Record<string, "success" | "failure" | "pending"> }>(`${repo(id)}/commit-states`, { ...o, query: { shas: shas.join(",") } }),
    checksFor: (id: string, sha: string, branch?: string, o?: CallOptions) => http.get<ReposChecks>(`${repo(id)}/commits/${seg(sha)}/checks`, { ...o, query: { branch } }),
    ciTokens: (id: string, o?: CallOptions) => http.get<ReposCITokens>(`${repo(id)}/ci-tokens`, o),
    createCIToken: (id: string, name: string, o?: CallOptions) => http.post<ReposCreatedCIToken>(`${repo(id)}/ci-tokens`, { name }, o),
    revokeCIToken: (id: string, tokenId: string, o?: CallOptions) => http.delete<void>(`${repo(id)}/ci-tokens/${seg(tokenId)}`, o),

    // ---- releases, tags, archives, insights
    releases: (id: string, params: PageParams = {}, o?: CallOptions) =>
      http.get<Page<ReposRelease>>(`${repo(id)}/releases`, { ...o, query: { cursor: params.cursor, limit: params.limit } }),
    release: (id: string, releaseId: string, o?: CallOptions) => http.get<ReposRelease>(`${repo(id)}/releases/${seg(releaseId)}`, o),
    createRelease: (id: string, body: ReposReleaseRequest, o?: CallOptions) => http.post<ReposRelease>(`${repo(id)}/releases`, body, o),
    updateRelease: (id: string, releaseId: string, body: ReposReleaseRequest, o?: CallOptions) => http.patch<ReposRelease>(`${repo(id)}/releases/${seg(releaseId)}`, body, o),
    deleteRelease: (id: string, releaseId: string, deleteTag: boolean, o?: CallOptions) =>
      http.delete<void>(`${repo(id)}/releases/${seg(releaseId)}`, { ...o, query: { delete_tag: deleteTag ? "1" : undefined } }),
    releaseNotes: (id: string, target: string, previous?: string, o?: CallOptions) =>
      http.get<{ body: string; pulls: number; commits: number }>(`${repo(id)}/releases/notes`, { ...o, query: { target, previous } }),
    addReleaseAsset: (id: string, releaseId: string, key: string, name: string, o?: CallOptions) =>
      http.post<ReposReleaseAsset>(`${repo(id)}/releases/${seg(releaseId)}/assets`, { key, name }, o),
    downloadReleaseAsset: (id: string, releaseId: string, assetId: string, o?: CallOptions) =>
      http.get<{ url: string; name: string }>(`${repo(id)}/releases/${seg(releaseId)}/assets/${seg(assetId)}`, o),
    deleteReleaseAsset: (id: string, releaseId: string, assetId: string, o?: CallOptions) => http.delete<void>(`${repo(id)}/releases/${seg(releaseId)}/assets/${seg(assetId)}`, o),
    createTag: (id: string, name: string, target?: string, o?: CallOptions) => http.post<{ name: string; sha: string }>(`${repo(id)}/tags`, { name, target }, o),
    deleteTag: (id: string, name: string, o?: CallOptions) => http.delete<void>(`${repo(id)}/tags`, { ...o, query: { name } }),
    archive: (id: string, ref: string | undefined, format: ReposArchiveFormat, o?: CallOptions) => http.post<ReposArchiveLink>(`${repo(id)}/archive`, { ref, format }, o),
    insights: (id: string, o?: CallOptions) => http.get<ReposInsights>(`${repo(id)}/insights`, o),

    // ---- webhooks (manage)
    webhooks: (id: string, o?: CallOptions) => http.get<{ items: ReposWebhook[]; events: ReposWebhookEvent[] }>(`${repo(id)}/webhooks`, o),
    createWebhook: (id: string, body: ReposWebhookRequest, o?: CallOptions) => http.post<ReposWebhook>(`${repo(id)}/webhooks`, body, o),
    updateWebhook: (id: string, hookId: string, body: ReposWebhookRequest, o?: CallOptions) => http.patch<ReposWebhook>(`${repo(id)}/webhooks/${seg(hookId)}`, body, o),
    deleteWebhook: (id: string, hookId: string, o?: CallOptions) => http.delete<void>(`${repo(id)}/webhooks/${seg(hookId)}`, o),
    pingWebhook: (id: string, hookId: string, o?: CallOptions) => http.post<{ queued: boolean }>(`${repo(id)}/webhooks/${seg(hookId)}/ping`, undefined, o),
    deliveries: (id: string, hookId: string, params: PageParams = {}, o?: CallOptions) =>
      http.get<Page<ReposWebhookDelivery>>(`${repo(id)}/webhooks/${seg(hookId)}/deliveries`, { ...o, query: { cursor: params.cursor, limit: params.limit } }),
    delivery: (id: string, hookId: string, deliveryId: string, o?: CallOptions) =>
      http.get<ReposWebhookDelivery>(`${repo(id)}/webhooks/${seg(hookId)}/deliveries/${seg(deliveryId)}`, o),
    redeliver: (id: string, hookId: string, deliveryId: string, o?: CallOptions) =>
      http.post<{ queued: boolean }>(`${repo(id)}/webhooks/${seg(hookId)}/deliveries/${seg(deliveryId)}/redeliver`, undefined, o),

    // ---- git credentials (your own)
    credentials: (params: PageParams = {}, o?: CallOptions) =>
      http.get<Page<ReposCredential>>(`${R}/credentials`, { ...o, query: { cursor: params.cursor, limit: params.limit } }),
    createCredential: (body: ReposCreateCredentialRequest, o?: CallOptions) => http.post<ReposCreatedCredential>(`${R}/credentials`, body, o),
    revokeCredential: (credId: string, o?: CallOptions) => http.delete<void>(`${R}/credentials/${seg(credId)}`, o),
  };
}

export type ReposEndpoints = ReturnType<typeof reposEndpoints>;
