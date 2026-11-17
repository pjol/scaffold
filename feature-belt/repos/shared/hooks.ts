"use client";

import { keepPreviousData, useMutation, useQueryClient, type QueryClient, type UseMutationResult, type UseQueryResult } from "@tanstack/react-query";
import type { ApiError } from "../api/errors";
import { useEndpoints } from "../hooks/context";
import { useApiQuery, useDebouncedValue, usePagedQuery, type PagedQueryResult, type QueryOverrides } from "../hooks/internal";
import { registerReconnectRefetch, registerRealtimeHandler } from "../hooks/realtime-handlers";
import { reposEndpoints, type ReposAt, type ReposPullFilter } from "./api";
import type { FeatureSources } from "../sources";
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
  ReposDiffFile,
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
  ReposChecks as ReposChecksResult,
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
  ReposMergeMethod,
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

/** Query keys. Everything about one repository sits under `reposKeys.repo(id)`. */
export const reposKeys = {
  all: () => ["repos"] as const,
  list: (orgId: string, q: string) => ["repos", "list", orgId, q] as const,
  lists: () => ["repos", "list"] as const,
  repo: (id: string) => ["repos", "repo", id] as const,
  info: (id: string) => ["repos", "repo", id, "info"] as const,
  refs: (id: string, counts: boolean) => ["repos", "repo", id, "refs", counts] as const,
  tree: (id: string, ref: string, path: string) => ["repos", "repo", id, "tree", ref, path] as const,
  treeCommits: (id: string, ref: string, path: string) => ["repos", "repo", id, "tree-commits", ref, path] as const,
  blob: (id: string, ref: string, path: string) => ["repos", "repo", id, "blob", ref, path] as const,
  blame: (id: string, ref: string, path: string) => ["repos", "repo", id, "blame", ref, path] as const,
  commits: (id: string, ref: string, path: string) => ["repos", "repo", id, "commits", ref, path] as const,
  commit: (id: string, sha: string, path: string) => ["repos", "repo", id, "commit", sha, path] as const,
  compare: (id: string, base: string, head: string, path: string) => ["repos", "repo", id, "compare", base, head, path] as const,
  protections: (id: string) => ["repos", "repo", id, "protections"] as const,
  people: (id: string, q: string) => ["repos", "repo", id, "people", q] as const,
  search: (id: string, q: string, ref: string) => ["repos", "repo", id, "search", q, ref] as const,
  paths: (id: string, q: string, ref: string) => ["repos", "repo", id, "paths", q, ref] as const,
  pulls: (id: string) => ["repos", "repo", id, "pulls"] as const,
  pullList: (id: string, state: string, q: string, mine: string) => ["repos", "repo", id, "pulls", state, q, mine] as const,
  pull: (id: string, n: number) => ["repos", "repo", id, "pull", n] as const,
  timeline: (id: string, n: number) => ["repos", "repo", id, "pull", n, "timeline"] as const,
  pullCommits: (id: string, n: number) => ["repos", "repo", id, "pull", n, "commits"] as const,
  pullDiff: (id: string, n: number, path: string, commit: string) => ["repos", "repo", id, "pull", n, "diff", path, commit] as const,
  threads: (id: string, n: number) => ["repos", "repo", id, "pull", n, "threads"] as const,
  mergeability: (id: string, n: number) => ["repos", "repo", id, "pull", n, "mergeability"] as const,
  importPreview: (folderId: string) => ["repos", "import", folderId] as const,
  credentials: () => ["repos", "credentials"] as const,
  sources: (orgId: string) => ["repos", "sources", orgId] as const,
  connected: (connectionId: string, q: string, sort: string, owner: string) => ["repos", "connected", connectionId, q, sort, owner] as const,
  checks: (id: string, sha: string) => ["repos", "repo", id, "checks", sha] as const,
  link: (linkId: string) => ["repos", "link", linkId] as const,
  gitInfo: () => ["repos", "git-info"] as const,
  issues: (id: string) => ["repos", "repo", id, "issues"] as const,
  issueList: (id: string, q: string) => ["repos", "repo", id, "issues", q] as const,
  issue: (id: string, n: number) => ["repos", "repo", id, "issue", n] as const,
  issueTimeline: (id: string, n: number) => ["repos", "repo", id, "issue", n, "timeline"] as const,
  labels: (id: string) => ["repos", "repo", id, "labels"] as const,
  milestones: (id: string, state: string) => ["repos", "repo", id, "milestones", state] as const,
  templates: (id: string, kind: string) => ["repos", "repo", id, "templates", kind] as const,
  viewed: (id: string, n: number) => ["repos", "repo", id, "pull", n, "viewed"] as const,
  pending: (id: string, n: number) => ["repos", "repo", id, "pull", n, "pending"] as const,
  commitStates: (id: string, shas: string) => ["repos", "repo", id, "commit-states", shas] as const,
  checksFor: (id: string, sha: string, branch: string) => ["repos", "repo", id, "checks-for", sha, branch] as const,
  ciTokens: (id: string) => ["repos", "repo", id, "ci-tokens"] as const,
  releases: (id: string) => ["repos", "repo", id, "releases"] as const,
  release: (id: string, releaseId: string) => ["repos", "repo", id, "release", releaseId] as const,
  insights: (id: string) => ["repos", "repo", id, "insights"] as const,
  webhooks: (id: string) => ["repos", "repo", id, "webhooks"] as const,
  deliveries: (id: string, hookId: string) => ["repos", "repo", id, "webhooks", hookId, "deliveries"] as const,
  delivery: (id: string, hookId: string, deliveryId: string) => ["repos", "repo", id, "webhooks", hookId, "delivery", deliveryId] as const,
};

function retryUnlessClientError(count: number, error: ApiError): boolean {
  return error.status >= 500 || error.status === 0 ? count < 2 : false;
}

const read = { retry: retryUnlessClientError } as const;

// ---- repositories --------------------------------------------------------------

/** Repositories in an org you can open, most recently changed first. */
export function useReposList(orgId: string | null | undefined, q = ""): PagedQueryResult<ReposSummary> {
  const r = useEndpoints(reposEndpoints);
  const query = useDebouncedValue(q.trim(), 250);
  return usePagedQuery(reposKeys.list(orgId ?? "", query), (cursor, signal) => r.list(orgId ?? "", { cursor, q: query }, { signal }), { placeholderData: keepPreviousData }, !!orgId);
}

export function useRepo(id: string | null | undefined, options?: QueryOverrides<ReposRepo>): UseQueryResult<ReposRepo, ApiError> {
  const r = useEndpoints(reposEndpoints);
  return useApiQuery(reposKeys.info(id ?? ""), ({ signal }) => r.get(id ?? "", { signal }), { ...read, ...options }, !!id);
}

export function useReposGitInfo(): UseQueryResult<ReposGitInfo, ApiError> {
  const r = useEndpoints(reposEndpoints);
  return useApiQuery(reposKeys.gitInfo(), ({ signal }) => r.gitInfo({ signal }), { staleTime: 10 * 60_000 });
}

export function useCreateRepo(): UseMutationResult<ReposRepo, ApiError, ReposCreateRequest> {
  const r = useEndpoints(reposEndpoints);
  const qc = useQueryClient();
  return useMutation<ReposRepo, ApiError, ReposCreateRequest>({
    mutationFn: (body) => r.create(body),
    onSuccess: (repo) => {
      qc.setQueryData(reposKeys.info(repo.id), repo);
      void qc.invalidateQueries({ queryKey: reposKeys.lists() });
      void qc.invalidateQueries({ queryKey: ["files"] });
    },
  });
}

/** What turning a folder into a repository would do. */
export function useReposImportPreview(folderId: string | null | undefined): UseQueryResult<ReposImportPlan, ApiError> {
  const r = useEndpoints(reposEndpoints);
  return useApiQuery(reposKeys.importPreview(folderId ?? ""), ({ signal }) => r.importPreview(folderId ?? "", { signal }), read, !!folderId);
}

export function useImportReposFolder(): UseMutationResult<ReposRepo, ApiError, ReposImportRequest> {
  const r = useEndpoints(reposEndpoints);
  const qc = useQueryClient();
  return useMutation<ReposRepo, ApiError, ReposImportRequest>({
    mutationFn: (body) => r.importFolder(body),
    onSuccess: (repo) => {
      qc.setQueryData(reposKeys.info(repo.id), repo);
      void qc.invalidateQueries({ queryKey: reposKeys.lists() });
      void qc.invalidateQueries({ queryKey: ["files"] });
    },
  });
}

export function useUpdateRepo(id: string): UseMutationResult<ReposRepo, ApiError, ReposUpdateRequest> {
  const r = useEndpoints(reposEndpoints);
  const qc = useQueryClient();
  return useMutation<ReposRepo, ApiError, ReposUpdateRequest>({
    mutationFn: (body) => r.update(id, body),
    onSuccess: (repo) => {
      qc.setQueryData(reposKeys.info(id), repo);
      void qc.invalidateQueries({ queryKey: reposKeys.repo(id) });
      void qc.invalidateQueries({ queryKey: reposKeys.lists() });
    },
  });
}

/** Reviewer suggestions: org members who can open the repository. */
export function useReposPeople(id: string, q: string): UseQueryResult<{ items: ReposPerson[] }, ApiError> {
  const r = useEndpoints(reposEndpoints);
  const query = useDebouncedValue(q.trim(), 200);
  return useApiQuery(reposKeys.people(id, query), ({ signal }) => r.people(id, query, { signal }), { placeholderData: keepPreviousData });
}

// ---- connected platforms -------------------------------------------------------

/** The app's own repositories, each connected GitHub/GitLab account (with status), and offers. */
export function useReposSources(orgId: string | null | undefined): UseQueryResult<FeatureSources, ApiError> {
  const r = useEndpoints(reposEndpoints);
  return useApiQuery(reposKeys.sources(orgId ?? ""), ({ signal }) => r.sources(orgId ?? undefined, { signal }), { ...read, staleTime: 60_000 }, !!orgId);
}

export interface ReposConnectedQuery {
  q?: string;
  sort?: ReposConnectedSort;
  owner?: string;
  orgId?: string | null | undefined;
}

/** One connected account's repositories, most recently active first (live from the platform). */
export function useReposConnectedList(connectionId: string | null | undefined, query: ReposConnectedQuery = {}): PagedQueryResult<ReposSummary> & { meta: Omit<ReposConnectedPage, "items" | "next_cursor"> | undefined } {
  const r = useEndpoints(reposEndpoints);
  const qc = useQueryClient();
  const q = useDebouncedValue((query.q ?? "").trim(), 300);
  const sort = query.sort ?? "recent";
  const owner = query.owner ?? "";
  const key = reposKeys.connected(connectionId ?? "", q, sort, owner);
  const res = usePagedQuery(
    key,
    (cursor, signal) => r.connected(connectionId ?? "", { cursor, q, sort, owner, orgId: query.orgId ?? undefined }, { signal }),
    { placeholderData: keepPreviousData, retry: retryUnlessClientError, staleTime: 60_000 },
    !!connectionId,
  );
  // The first page's account, provider and owners (the select keeps only rows).
  const first = qc.getQueryData<{ pages: ReposConnectedPage[] }>(key)?.pages?.[0];
  return Object.assign(res, { meta: first ? { owners: first.owners, account: first.account, provider: first.provider } : undefined });
}

/** CI checks on a commit (connected repositories; the app's own have none). */
export function useReposChecks(id: string, sha: string | null | undefined, enabled = true): UseQueryResult<ReposChecks, ApiError> {
  const r = useEndpoints(reposEndpoints);
  return useApiQuery(reposKeys.checks(id, sha ?? ""), ({ signal }) => r.checks(id, sha ?? "", { signal }), { ...read, staleTime: 30_000, refetchInterval: 60_000 }, enabled && !!sha);
}

/** Copy a connected repository into the drive (branches and tags). */
export function useImportConnectedRepo(): UseMutationResult<ReposRepo, ApiError, ReposImportConnectedRequest> {
  const r = useEndpoints(reposEndpoints);
  const qc = useQueryClient();
  return useMutation<ReposRepo, ApiError, ReposImportConnectedRequest>({
    mutationFn: (body) => r.importConnected(body),
    onSuccess: (repo) => {
      qc.setQueryData(reposKeys.info(repo.id), repo);
      void qc.invalidateQueries({ queryKey: reposKeys.lists() });
      void qc.invalidateQueries({ queryKey: ["files"] });
    },
  });
}

/** Put a link to a connected repository in a drive folder. */
export function useCreateReposLink(): UseMutationResult<ReposLink, ApiError, ReposCreateLinkRequest> {
  const r = useEndpoints(reposEndpoints);
  const qc = useQueryClient();
  return useMutation<ReposLink, ApiError, ReposCreateLinkRequest>({
    mutationFn: (body) => r.createLink(body),
    onSuccess: () => void qc.invalidateQueries({ queryKey: ["files"] }),
  });
}

/** A drive link to a connected repository, resolved with the opener's own connections. */
export function useReposLink(linkId: string | null | undefined): UseQueryResult<ReposLink, ApiError> {
  const r = useEndpoints(reposEndpoints);
  return useApiQuery(reposKeys.link(linkId ?? ""), ({ signal }) => r.link(linkId ?? "", { signal }), read, !!linkId);
}

// ---- protection ----------------------------------------------------------------

export function useReposProtections(id: string): UseQueryResult<{ items: ReposProtection[] }, ApiError> {
  const r = useEndpoints(reposEndpoints);
  return useApiQuery(reposKeys.protections(id), ({ signal }) => r.protections(id, { signal }), read);
}

function invalidateRepo(qc: QueryClient, id: string) {
  return () => qc.invalidateQueries({ queryKey: reposKeys.repo(id) });
}

export function useCreateReposProtection(id: string): UseMutationResult<ReposProtection, ApiError, ReposProtectionRequest> {
  const r = useEndpoints(reposEndpoints);
  const qc = useQueryClient();
  return useMutation<ReposProtection, ApiError, ReposProtectionRequest>({ mutationFn: (body) => r.createProtection(id, body), onSuccess: invalidateRepo(qc, id) });
}

export function useUpdateReposProtection(id: string): UseMutationResult<ReposProtection, ApiError, { protectionId: string; body: ReposProtectionRequest }> {
  const r = useEndpoints(reposEndpoints);
  const qc = useQueryClient();
  return useMutation<ReposProtection, ApiError, { protectionId: string; body: ReposProtectionRequest }>({
    mutationFn: ({ protectionId, body }) => r.updateProtection(id, protectionId, body),
    onSuccess: invalidateRepo(qc, id),
  });
}

export function useDeleteReposProtection(id: string): UseMutationResult<void, ApiError, string> {
  const r = useEndpoints(reposEndpoints);
  const qc = useQueryClient();
  return useMutation<void, ApiError, string>({ mutationFn: (pid) => r.deleteProtection(id, pid), onSuccess: invalidateRepo(qc, id) });
}

// ---- reading -------------------------------------------------------------------

/** Branches and tags (`counts`: ahead/behind the default branch, for the branches page). */
export function useReposRefs(id: string | null | undefined, counts = false): UseQueryResult<ReposRefs, ApiError> {
  const r = useEndpoints(reposEndpoints);
  return useApiQuery(reposKeys.refs(id ?? "", counts), ({ signal }) => r.refs(id ?? "", counts, { signal }), read, !!id);
}

export function useReposTree(id: string, at: ReposAt, enabled = true): UseQueryResult<ReposTree, ApiError> {
  const r = useEndpoints(reposEndpoints);
  return useApiQuery(reposKeys.tree(id, at.ref ?? "", at.path ?? ""), ({ signal }) => r.tree(id, at, { signal }), { ...read, placeholderData: keepPreviousData }, enabled);
}

/** The newest commit of each entry in a folder (slower; fills in after the listing). */
export function useReposTreeCommits(id: string, at: ReposAt, enabled = true): UseQueryResult<ReposTreeCommits, ApiError> {
  const r = useEndpoints(reposEndpoints);
  return useApiQuery(reposKeys.treeCommits(id, at.ref ?? "", at.path ?? ""), ({ signal }) => r.treeCommits(id, at, { signal }), { ...read, staleTime: 60_000 }, enabled);
}

export function useReposBlob(id: string, at: ReposAt & { path: string }, enabled = true): UseQueryResult<ReposBlob, ApiError> {
  const r = useEndpoints(reposEndpoints);
  return useApiQuery(reposKeys.blob(id, at.ref ?? "", at.path), ({ signal }) => r.blob(id, at, { signal }), read, enabled && !!at.path);
}

export function useReposBlame(id: string, at: ReposAt & { path: string }, enabled = true): UseQueryResult<{ sha: string; path: string; ranges: ReposBlameRange[] }, ApiError> {
  const r = useEndpoints(reposEndpoints);
  return useApiQuery(reposKeys.blame(id, at.ref ?? "", at.path), ({ signal }) => r.blame(id, at, { signal }), { ...read, staleTime: 5 * 60_000 }, enabled && !!at.path);
}

/** Resolves to the file's bytes as base64 (save it through the platform's download flow). */
export function useReposDownload(id: string): UseMutationResult<ReposDownload, ApiError, ReposAt & { path: string }> {
  const r = useEndpoints(reposEndpoints);
  return useMutation<ReposDownload, ApiError, ReposAt & { path: string }>({ mutationFn: (at) => r.download(id, at) });
}

/** File finder: paths matching every character of `q` in order. */
export function useReposPaths(id: string, q: string, ref: string | undefined, enabled = true): UseQueryResult<{ sha: string; items: string[]; truncated: boolean }, ApiError> {
  const r = useEndpoints(reposEndpoints);
  const query = useDebouncedValue(q.trim(), 120);
  return useApiQuery(reposKeys.paths(id, query, ref ?? ""), ({ signal }) => r.paths(id, query, ref, { signal }), { ...read, placeholderData: keepPreviousData, staleTime: 60_000 }, enabled);
}

/** Lines containing `q` (2+ characters). */
export function useReposCodeSearch(id: string, q: string, ref: string | undefined): UseQueryResult<ReposSearchResult, ApiError> {
  const r = useEndpoints(reposEndpoints);
  const query = useDebouncedValue(q.trim(), 300);
  return useApiQuery(reposKeys.search(id, query, ref ?? ""), ({ signal }) => r.search(id, query, ref, { signal }), { ...read, placeholderData: keepPreviousData }, query.length >= 2);
}

export function useReposCommits(id: string, at: ReposAt): PagedQueryResult<ReposCommit> {
  const r = useEndpoints(reposEndpoints);
  return usePagedQuery(reposKeys.commits(id, at.ref ?? "", at.path ?? ""), (cursor, signal) => r.commits(id, { ...at, cursor }, { signal }), { retry: retryUnlessClientError });
}

export function useReposCommit(id: string, sha: string, path?: string): UseQueryResult<ReposCommitDetail, ApiError> {
  const r = useEndpoints(reposEndpoints);
  return useApiQuery(reposKeys.commit(id, sha, path ?? ""), ({ signal }) => r.commit(id, sha, path, { signal }), { ...read, staleTime: Infinity }, !!sha);
}

export function useReposCompare(id: string, base: string, head: string, path?: string): UseQueryResult<ReposComparison, ApiError> {
  const r = useEndpoints(reposEndpoints);
  return useApiQuery(reposKeys.compare(id, base, head, path ?? ""), ({ signal }) => r.compare(id, base, head, path, { signal }), read, !!head && base !== head);
}

/**
 * Loads one file's full patch for diffs cut by the size limits: of a pull request
 * (`{ pull }`), a commit (`{ sha }`) or a comparison (`{ base, head }`).
 */
export function useReposDiffFileLoader(id: string, source: { pull: number } | { sha: string } | { base: string; head: string }): (path: string) => Promise<ReposDiffFile | null> {
  const r = useEndpoints(reposEndpoints);
  const qc = useQueryClient();
  return async (path: string) => {
    let diff: ReposDiff;
    if ("pull" in source) {
      diff = (await qc.fetchQuery({ queryKey: reposKeys.pullDiff(id, source.pull, path, ""), queryFn: () => r.pullDiff(id, source.pull, { path }) })).diff;
    } else if ("sha" in source) {
      diff = (await qc.fetchQuery({ queryKey: reposKeys.commit(id, source.sha, path), queryFn: () => r.commit(id, source.sha, path) })).diff;
    } else {
      diff = (await qc.fetchQuery({ queryKey: reposKeys.compare(id, source.base, source.head, path), queryFn: () => r.compare(id, source.base, source.head, path) })).diff;
    }
    return diff.files.find((f) => f.path === path) ?? diff.files[0] ?? null;
  };
}

// ---- writing -------------------------------------------------------------------

/** One commit of file changes. Errors: 409 head_moved (branch moved), 403 branch_protected. */
export function useReposCommitChanges(id: string): UseMutationResult<ReposCommitResult, ApiError, ReposCommitRequest> {
  const r = useEndpoints(reposEndpoints);
  const qc = useQueryClient();
  return useMutation<ReposCommitResult, ApiError, ReposCommitRequest>({ mutationFn: (body) => r.commitChanges(id, body), onSuccess: invalidateRepo(qc, id) });
}

export function useCreateReposBranch(id: string): UseMutationResult<ReposRef, ApiError, { name: string; from?: string }> {
  const r = useEndpoints(reposEndpoints);
  const qc = useQueryClient();
  return useMutation<ReposRef, ApiError, { name: string; from?: string }>({ mutationFn: ({ name, from }) => r.createBranch(id, name, from), onSuccess: invalidateRepo(qc, id) });
}

export function useDeleteReposBranch(id: string): UseMutationResult<void, ApiError, string> {
  const r = useEndpoints(reposEndpoints);
  const qc = useQueryClient();
  return useMutation<void, ApiError, string>({ mutationFn: (name) => r.deleteBranch(id, name), onSuccess: invalidateRepo(qc, id) });
}

// ---- pull requests -------------------------------------------------------------

export interface ReposPullQuery {
  state?: ReposPullFilter;
  q?: string;
  /** Only yours, or only those asking for your review. */
  mine?: "author" | "review_requested" | "";
}

export function useReposPulls(id: string, query: ReposPullQuery = {}): PagedQueryResult<ReposPull> {
  const r = useEndpoints(reposEndpoints);
  const q = useDebouncedValue((query.q ?? "").trim(), 250);
  const state = query.state ?? "open";
  const mine = query.mine ?? "";
  return usePagedQuery(
    reposKeys.pullList(id, state, q, mine),
    (cursor, signal) =>
      r.pulls(id, { cursor, state, q, ...(mine === "author" ? { author: "me" as const } : {}), ...(mine === "review_requested" ? { review_requested: "me" as const } : {}) }, { signal }),
    { placeholderData: keepPreviousData, retry: retryUnlessClientError },
  );
}

export function useReposPull(id: string, n: number): UseQueryResult<ReposPull, ApiError> {
  const r = useEndpoints(reposEndpoints);
  return useApiQuery(reposKeys.pull(id, n), ({ signal }) => r.pull(id, n, { signal }), read, n > 0);
}

export function useReposTimeline(id: string, n: number): UseQueryResult<{ items: ReposTimelineItem[]; mentions?: ReposMention[] }, ApiError> {
  const r = useEndpoints(reposEndpoints);
  return useApiQuery(reposKeys.timeline(id, n), ({ signal }) => r.timeline(id, n, { signal }), read, n > 0);
}

export function useReposPullCommits(id: string, n: number, enabled = true): UseQueryResult<{ items: ReposCommit[] }, ApiError> {
  const r = useEndpoints(reposEndpoints);
  return useApiQuery(reposKeys.pullCommits(id, n), ({ signal }) => r.pullCommits(id, n, { signal }), read, enabled && n > 0);
}

/** The whole change, one file's full patch (`path`), or one commit's (`commit`); `ignoreWhitespace` hides whitespace-only changes. */
export function useReposPullDiff(
  id: string,
  n: number,
  params: { path?: string; commit?: string; ignoreWhitespace?: boolean } = {},
  enabled = true,
): UseQueryResult<{ base: string; head: string; diff: ReposDiff }, ApiError> {
  const r = useEndpoints(reposEndpoints);
  return useApiQuery(
    [...reposKeys.pullDiff(id, n, params.path ?? "", params.commit ?? ""), params.ignoreWhitespace ? "ws" : ""],
    ({ signal }) => r.pullDiff(id, n, params, { signal }),
    read,
    enabled && n > 0,
  );
}

export function useReposThreads(id: string, n: number): UseQueryResult<{ items: ReposThread[]; head_sha: string; mentions?: ReposMention[] }, ApiError> {
  const r = useEndpoints(reposEndpoints);
  return useApiQuery(reposKeys.threads(id, n), ({ signal }) => r.threads(id, n, { signal }), read, n > 0);
}

export function useReposMergeability(id: string, n: number, enabled = true): UseQueryResult<ReposMergeability, ApiError> {
  const r = useEndpoints(reposEndpoints);
  return useApiQuery(reposKeys.mergeability(id, n), ({ signal }) => r.mergeability(id, n, { signal }), read, enabled && n > 0);
}

function invalidatePull(qc: QueryClient, id: string, n: number) {
  void qc.invalidateQueries({ queryKey: reposKeys.pull(id, n) });
  void qc.invalidateQueries({ queryKey: reposKeys.pulls(id) });
}

export function useOpenReposPull(id: string): UseMutationResult<ReposPull, ApiError, ReposOpenPullRequest> {
  const r = useEndpoints(reposEndpoints);
  const qc = useQueryClient();
  return useMutation<ReposPull, ApiError, ReposOpenPullRequest>({
    mutationFn: (body) => r.openPull(id, body),
    onSuccess: (p) => {
      qc.setQueryData(reposKeys.pull(id, p.number), p);
      void qc.invalidateQueries({ queryKey: reposKeys.pulls(id) });
      void qc.invalidateQueries({ queryKey: reposKeys.info(id) });
    },
  });
}

function pullMutation<V>(id: string, n: number, fn: (v: V) => Promise<ReposPull>) {
  return {
    mutationFn: fn,
    onSuccess: (qc: QueryClient) => (p: ReposPull) => {
      qc.setQueryData(reposKeys.pull(id, n), p);
      invalidatePull(qc, id, n);
    },
  };
}

export function useUpdateReposPull(id: string, n: number): UseMutationResult<ReposPull, ApiError, ReposUpdatePullRequest> {
  const r = useEndpoints(reposEndpoints);
  const qc = useQueryClient();
  const m = pullMutation<ReposUpdatePullRequest>(id, n, (body) => r.updatePull(id, n, body));
  return useMutation<ReposPull, ApiError, ReposUpdatePullRequest>({ mutationFn: m.mutationFn, onSuccess: m.onSuccess(qc) });
}

export function useSetReposPullOpen(id: string, n: number): UseMutationResult<ReposPull, ApiError, boolean> {
  const r = useEndpoints(reposEndpoints);
  const qc = useQueryClient();
  const m = pullMutation<boolean>(id, n, (open) => (open ? r.reopenPull(id, n) : r.closePull(id, n)));
  return useMutation<ReposPull, ApiError, boolean>({ mutationFn: m.mutationFn, onSuccess: m.onSuccess(qc) });
}

/** Merge as a person. 409 not_mergeable / head_moved / base_moved explain why not. */
export function useMergeReposPull(id: string, n: number): UseMutationResult<ReposPull, ApiError, ReposMergeRequest> {
  const r = useEndpoints(reposEndpoints);
  const qc = useQueryClient();
  return useMutation<ReposPull, ApiError, ReposMergeRequest>({
    mutationFn: (body) => r.merge(id, n, body),
    onSuccess: (p) => {
      qc.setQueryData(reposKeys.pull(id, n), p);
      void qc.invalidateQueries({ queryKey: reposKeys.repo(id) });
    },
  });
}

export function useAddReposComment(id: string, n: number): UseMutationResult<ReposComment, ApiError, ReposCommentRequest> {
  const r = useEndpoints(reposEndpoints);
  const qc = useQueryClient();
  return useMutation<ReposComment, ApiError, ReposCommentRequest>({ mutationFn: (body) => r.addComment(id, n, body), onSuccess: () => invalidatePull(qc, id, n) });
}

export function useEditReposComment(id: string, n: number): UseMutationResult<ReposComment, ApiError, { commentId: string; body: string }> {
  const r = useEndpoints(reposEndpoints);
  const qc = useQueryClient();
  return useMutation<ReposComment, ApiError, { commentId: string; body: string }>({
    mutationFn: ({ commentId, body }) => r.editComment(id, n, commentId, body),
    onSuccess: () => invalidatePull(qc, id, n),
  });
}

export function useDeleteReposComment(id: string, n: number): UseMutationResult<void, ApiError, string> {
  const r = useEndpoints(reposEndpoints);
  const qc = useQueryClient();
  return useMutation<void, ApiError, string>({ mutationFn: (commentId) => r.deleteComment(id, n, commentId), onSuccess: () => invalidatePull(qc, id, n) });
}

export function useResolveReposThread(id: string, n: number): UseMutationResult<void, ApiError, { commentId: string; resolved: boolean }> {
  const r = useEndpoints(reposEndpoints);
  const qc = useQueryClient();
  return useMutation<void, ApiError, { commentId: string; resolved: boolean }>({
    mutationFn: ({ commentId, resolved }) => r.resolveThread(id, n, commentId, resolved),
    onSuccess: () => invalidatePull(qc, id, n),
  });
}

export function useSubmitReposReview(id: string, n: number): UseMutationResult<ReposPull, ApiError, ReposReviewRequest> {
  const r = useEndpoints(reposEndpoints);
  const qc = useQueryClient();
  const m = pullMutation<ReposReviewRequest>(id, n, (body) => r.submitReview(id, n, body));
  return useMutation<ReposPull, ApiError, ReposReviewRequest>({ mutationFn: m.mutationFn, onSuccess: m.onSuccess(qc) });
}

export function useRequestReposReviewers(id: string, n: number): UseMutationResult<{ items: ReposReviewer[] }, ApiError, string[]> {
  const r = useEndpoints(reposEndpoints);
  const qc = useQueryClient();
  return useMutation<{ items: ReposReviewer[] }, ApiError, string[]>({ mutationFn: (ids) => r.requestReviewers(id, n, ids), onSuccess: () => invalidatePull(qc, id, n) });
}

export function useRemoveReposReviewer(id: string, n: number): UseMutationResult<void, ApiError, string> {
  const r = useEndpoints(reposEndpoints);
  const qc = useQueryClient();
  return useMutation<void, ApiError, string>({ mutationFn: (userId) => r.removeReviewer(id, n, userId), onSuccess: () => invalidatePull(qc, id, n) });
}

export function useSetReposPullDraft(id: string, n: number): UseMutationResult<ReposPull, ApiError, boolean> {
  const r = useEndpoints(reposEndpoints);
  const qc = useQueryClient();
  const m = pullMutation<boolean>(id, n, (draft) => r.setDraft(id, n, draft));
  return useMutation<ReposPull, ApiError, boolean>({ mutationFn: m.mutationFn, onSuccess: m.onSuccess(qc) });
}

export function useSetReposPullLabels(id: string, n: number): UseMutationResult<ReposPull, ApiError, string[]> {
  const r = useEndpoints(reposEndpoints);
  const qc = useQueryClient();
  const m = pullMutation<string[]>(id, n, (ids) => r.setPullLabels(id, n, ids));
  return useMutation<ReposPull, ApiError, string[]>({ mutationFn: m.mutationFn, onSuccess: m.onSuccess(qc) });
}

export function useSetReposPullAssignees(id: string, n: number): UseMutationResult<ReposPull, ApiError, string[]> {
  const r = useEndpoints(reposEndpoints);
  const qc = useQueryClient();
  const m = pullMutation<string[]>(id, n, (ids) => r.setPullAssignees(id, n, ids));
  return useMutation<ReposPull, ApiError, string[]>({ mutationFn: m.mutationFn, onSuccess: m.onSuccess(qc) });
}

/** Turn auto-merge on with a method, or off with null. */
export function useReposAutoMerge(id: string, n: number): UseMutationResult<ReposPull, ApiError, ReposMergeMethod | null> {
  const r = useEndpoints(reposEndpoints);
  const qc = useQueryClient();
  const m = pullMutation<ReposMergeMethod | null>(id, n, (method) => (method ? r.enableAutoMerge(id, n, method) : r.disableAutoMerge(id, n)));
  return useMutation<ReposPull, ApiError, ReposMergeMethod | null>({ mutationFn: m.mutationFn, onSuccess: m.onSuccess(qc) });
}

/** Merge the base into the head branch. */
export function useUpdateReposPullBranch(id: string, n: number): UseMutationResult<ReposPull, ApiError, string | undefined> {
  const r = useEndpoints(reposEndpoints);
  const qc = useQueryClient();
  const m = pullMutation<string | undefined>(id, n, (head) => r.updateBranch(id, n, head));
  return useMutation<ReposPull, ApiError, string | undefined>({ mutationFn: m.mutationFn, onSuccess: m.onSuccess(qc) });
}

/** Commit chosen ```suggestion blocks to the head branch in one commit. */
export function useApplyReposSuggestions(id: string, n: number): UseMutationResult<ReposCommitResult, ApiError, ReposApplySuggestionsRequest> {
  const r = useEndpoints(reposEndpoints);
  const qc = useQueryClient();
  return useMutation<ReposCommitResult, ApiError, ReposApplySuggestionsRequest>({
    mutationFn: (body) => r.applySuggestions(id, n, body),
    onSuccess: () => {
      invalidatePull(qc, id, n);
      void qc.invalidateQueries({ queryKey: [...reposKeys.pull(id, n)] });
    },
  });
}

export function useReposViewedFiles(id: string, n: number, enabled = true): UseQueryResult<{ items: string[] }, ApiError> {
  const r = useEndpoints(reposEndpoints);
  return useApiQuery(reposKeys.viewed(id, n), ({ signal }) => r.viewedFiles(id, n, { signal }), read, enabled && n > 0);
}

export function useSetReposViewed(id: string, n: number): UseMutationResult<void, ApiError, { path: string; viewed: boolean }, { prev: { items: string[] } | undefined }> {
  const r = useEndpoints(reposEndpoints);
  const qc = useQueryClient();
  return useMutation<void, ApiError, { path: string; viewed: boolean }, { prev: { items: string[] } | undefined }>({
    mutationFn: ({ path, viewed }) => r.setViewed(id, n, path, viewed),
    onMutate: ({ path, viewed }) => {
      const prev = qc.getQueryData<{ items: string[] }>(reposKeys.viewed(id, n));
      const items = new Set(prev?.items ?? []);
      if (viewed) items.add(path);
      else items.delete(path);
      qc.setQueryData(reposKeys.viewed(id, n), { items: [...items] });
      return { prev };
    },
    onError: (_e, _v, ctx) => qc.setQueryData(reposKeys.viewed(id, n), ctx?.prev),
  });
}

/** Your review in progress (id null when there's none). */
export function useReposPendingReview(id: string, n: number, enabled = true): UseQueryResult<ReposPendingReview, ApiError> {
  const r = useEndpoints(reposEndpoints);
  return useApiQuery(reposKeys.pending(id, n), ({ signal }) => r.pendingReview(id, n, { signal }), read, enabled && n > 0);
}

export function useDiscardReposPendingReview(id: string, n: number): UseMutationResult<void, ApiError, void> {
  const r = useEndpoints(reposEndpoints);
  const qc = useQueryClient();
  return useMutation<void, ApiError, void>({ mutationFn: () => r.discardPendingReview(id, n), onSuccess: () => invalidatePull(qc, id, n) });
}

// ---- issues --------------------------------------------------------------------

/** Issues matching the search syntax (`is:open label:bug assignee:@me`). */
export function useReposIssues(id: string, q: string): PagedQueryResult<ReposIssue> & { meta: Pick<ReposIssuePage, "counts" | "ignored"> | undefined } {
  const r = useEndpoints(reposEndpoints);
  const query = useDebouncedValue(q.trim(), 250);
  const res = usePagedQuery(reposKeys.issueList(id, query), (cursor, signal) => r.issues(id, { cursor, q: query }, { signal }), {
    placeholderData: keepPreviousData,
    retry: retryUnlessClientError,
  });
  // The first page's totals and ignored qualifiers (the select keeps pages).
  const first = res.data?.pages[0] as ReposIssuePage | undefined;
  return Object.assign(res, { meta: first ? { counts: first.counts, ignored: first.ignored } : undefined });
}

export function useReposIssue(id: string, n: number): UseQueryResult<ReposIssue, ApiError> {
  const r = useEndpoints(reposEndpoints);
  return useApiQuery(reposKeys.issue(id, n), ({ signal }) => r.issue(id, n, { signal }), read, n > 0);
}

export function useReposIssueTimeline(id: string, n: number): UseQueryResult<ReposIssueTimeline, ApiError> {
  const r = useEndpoints(reposEndpoints);
  return useApiQuery(reposKeys.issueTimeline(id, n), ({ signal }) => r.issueTimeline(id, n, { signal }), read, n > 0);
}

function invalidateIssue(qc: QueryClient, id: string, n: number) {
  void qc.invalidateQueries({ queryKey: reposKeys.issue(id, n) });
  void qc.invalidateQueries({ queryKey: reposKeys.issues(id) });
  void qc.invalidateQueries({ queryKey: reposKeys.info(id) });
}

export function useCreateReposIssue(id: string): UseMutationResult<ReposIssue, ApiError, ReposCreateIssueRequest> {
  const r = useEndpoints(reposEndpoints);
  const qc = useQueryClient();
  return useMutation<ReposIssue, ApiError, ReposCreateIssueRequest>({
    mutationFn: (body) => r.createIssue(id, body),
    onSuccess: (is) => {
      qc.setQueryData(reposKeys.issue(id, is.number), is);
      void qc.invalidateQueries({ queryKey: reposKeys.issues(id) });
      void qc.invalidateQueries({ queryKey: reposKeys.repo(id) });
    },
  });
}

function issueMutation<V>(id: string, n: number, qc: QueryClient, fn: (v: V) => Promise<ReposIssue>) {
  return {
    mutationFn: fn,
    onSuccess: (is: ReposIssue) => {
      qc.setQueryData(reposKeys.issue(id, n), is);
      invalidateIssue(qc, id, n);
    },
  };
}

export function useUpdateReposIssue(id: string, n: number): UseMutationResult<ReposIssue, ApiError, ReposUpdateIssueRequest> {
  const r = useEndpoints(reposEndpoints);
  const qc = useQueryClient();
  return useMutation<ReposIssue, ApiError, ReposUpdateIssueRequest>(issueMutation(id, n, qc, (body: ReposUpdateIssueRequest) => r.updateIssue(id, n, body)));
}

export function useSetReposIssueLabels(id: string, n: number): UseMutationResult<ReposIssue, ApiError, string[]> {
  const r = useEndpoints(reposEndpoints);
  const qc = useQueryClient();
  return useMutation<ReposIssue, ApiError, string[]>(issueMutation(id, n, qc, (ids: string[]) => r.setIssueLabels(id, n, ids)));
}

export function useSetReposIssueAssignees(id: string, n: number): UseMutationResult<ReposIssue, ApiError, string[]> {
  const r = useEndpoints(reposEndpoints);
  const qc = useQueryClient();
  return useMutation<ReposIssue, ApiError, string[]>(issueMutation(id, n, qc, (ids: string[]) => r.setIssueAssignees(id, n, ids)));
}

export function useDeleteReposIssue(id: string, n: number): UseMutationResult<void, ApiError, void> {
  const r = useEndpoints(reposEndpoints);
  const qc = useQueryClient();
  return useMutation<void, ApiError, void>({
    mutationFn: () => r.deleteIssue(id, n),
    onSuccess: () => {
      qc.removeQueries({ queryKey: reposKeys.issue(id, n) });
      void qc.invalidateQueries({ queryKey: reposKeys.issues(id) });
    },
  });
}

export function useAddReposIssueComment(id: string, n: number): UseMutationResult<ReposIssueComment, ApiError, string> {
  const r = useEndpoints(reposEndpoints);
  const qc = useQueryClient();
  return useMutation<ReposIssueComment, ApiError, string>({ mutationFn: (body) => r.addIssueComment(id, n, body), onSuccess: () => invalidateIssue(qc, id, n) });
}

export function useEditReposIssueComment(id: string, n: number): UseMutationResult<ReposIssueComment, ApiError, { commentId: string; body: string }> {
  const r = useEndpoints(reposEndpoints);
  const qc = useQueryClient();
  return useMutation<ReposIssueComment, ApiError, { commentId: string; body: string }>({
    mutationFn: ({ commentId, body }) => r.editIssueComment(id, n, commentId, body),
    onSuccess: () => invalidateIssue(qc, id, n),
  });
}

export function useDeleteReposIssueComment(id: string, n: number): UseMutationResult<void, ApiError, string> {
  const r = useEndpoints(reposEndpoints);
  const qc = useQueryClient();
  return useMutation<void, ApiError, string>({ mutationFn: (commentId) => r.deleteIssueComment(id, n, commentId), onSuccess: () => invalidateIssue(qc, id, n) });
}

/** Add or remove a reaction; `refresh` is the issue or pull request number to refetch. */
export function useReposReaction(id: string): UseMutationResult<{ items: ReposReaction[] }, ApiError, { type: ReposReactionSubject; subjectId: string; emoji: ReposReactionEmoji; add: boolean; number: number }> {
  const r = useEndpoints(reposEndpoints);
  const qc = useQueryClient();
  return useMutation<{ items: ReposReaction[] }, ApiError, { type: ReposReactionSubject; subjectId: string; emoji: ReposReactionEmoji; add: boolean; number: number }>({
    mutationFn: (v) => r.react(id, { type: v.type, id: v.subjectId }, v.emoji, v.add),
    onSuccess: (_d, v) => {
      if (v.type.startsWith("pull")) invalidatePull(qc, id, v.number);
      else invalidateIssue(qc, id, v.number);
    },
  });
}

export function useReposTemplates(id: string, kind: "issue" | "pull", enabled = true): UseQueryResult<{ items: ReposTemplate[] }, ApiError> {
  const r = useEndpoints(reposEndpoints);
  return useApiQuery(reposKeys.templates(id, kind), ({ signal }) => r.templates(id, kind, { signal }), { ...read, staleTime: 60_000 }, enabled);
}

export function useReposLabels(id: string, enabled = true): UseQueryResult<{ items: ReposLabelRow[] }, ApiError> {
  const r = useEndpoints(reposEndpoints);
  return useApiQuery(reposKeys.labels(id), ({ signal }) => r.labels(id, { signal }), read, enabled);
}

export function useSaveReposLabel(id: string): UseMutationResult<ReposLabel, ApiError, { labelId?: string; body: ReposLabelRequest }> {
  const r = useEndpoints(reposEndpoints);
  const qc = useQueryClient();
  return useMutation<ReposLabel, ApiError, { labelId?: string; body: ReposLabelRequest }>({
    mutationFn: ({ labelId, body }) => (labelId ? r.updateLabel(id, labelId, body) : r.createLabel(id, body)),
    onSuccess: () => {
      void qc.invalidateQueries({ queryKey: reposKeys.labels(id) });
      void qc.invalidateQueries({ queryKey: reposKeys.issues(id) });
    },
  });
}

export function useDeleteReposLabel(id: string): UseMutationResult<void, ApiError, string> {
  const r = useEndpoints(reposEndpoints);
  const qc = useQueryClient();
  return useMutation<void, ApiError, string>({
    mutationFn: (labelId) => r.deleteLabel(id, labelId),
    onSuccess: () => {
      void qc.invalidateQueries({ queryKey: reposKeys.labels(id) });
      void qc.invalidateQueries({ queryKey: reposKeys.issues(id) });
    },
  });
}

export function useReposMilestones(id: string, state: "open" | "closed" | "all" = "open", enabled = true): UseQueryResult<{ items: ReposMilestone[] }, ApiError> {
  const r = useEndpoints(reposEndpoints);
  return useApiQuery(reposKeys.milestones(id, state), ({ signal }) => r.milestones(id, state, { signal }), read, enabled);
}

export function useSaveReposMilestone(id: string): UseMutationResult<ReposMilestone, ApiError, { milestoneId?: string; body: ReposMilestoneRequest }> {
  const r = useEndpoints(reposEndpoints);
  const qc = useQueryClient();
  return useMutation<ReposMilestone, ApiError, { milestoneId?: string; body: ReposMilestoneRequest }>({
    mutationFn: ({ milestoneId, body }) => (milestoneId ? r.updateMilestone(id, milestoneId, body) : r.createMilestone(id, body)),
    onSuccess: () => void qc.invalidateQueries({ queryKey: ["repos", "repo", id, "milestones"] }),
  });
}

export function useDeleteReposMilestone(id: string): UseMutationResult<void, ApiError, string> {
  const r = useEndpoints(reposEndpoints);
  const qc = useQueryClient();
  return useMutation<void, ApiError, string>({
    mutationFn: (milestoneId) => r.deleteMilestone(id, milestoneId),
    onSuccess: () => void qc.invalidateQueries({ queryKey: ["repos", "repo", id, "milestones"] }),
  });
}

// ---- statuses and CI tokens ----------------------------------------------------

/** The combined status of each commit (commit lists). */
export function useReposCommitStates(id: string, shas: string[], enabled = true): UseQueryResult<{ states: Record<string, "success" | "failure" | "pending"> }, ApiError> {
  const r = useEndpoints(reposEndpoints);
  const key = shas.join(",");
  return useApiQuery(reposKeys.commitStates(id, key), ({ signal }) => r.commitStates(id, shas, { signal }), read, enabled && shas.length > 0);
}

/** A commit's statuses, with the required contexts of `branch` (a pull request's base). */
export function useReposChecksFor(id: string, sha: string | null | undefined, branch?: string, enabled = true): UseQueryResult<ReposChecksResult, ApiError> {
  const r = useEndpoints(reposEndpoints);
  return useApiQuery(reposKeys.checksFor(id, sha ?? "", branch ?? ""), ({ signal }) => r.checksFor(id, sha ?? "", branch, { signal }), read, enabled && !!sha);
}

export function useReposCITokens(id: string, enabled = true): UseQueryResult<ReposCITokens, ApiError> {
  const r = useEndpoints(reposEndpoints);
  return useApiQuery(reposKeys.ciTokens(id), ({ signal }) => r.ciTokens(id, { signal }), read, enabled);
}

/** Resolves with the secret, shown only now. */
export function useCreateReposCIToken(id: string): UseMutationResult<ReposCreatedCIToken, ApiError, string> {
  const r = useEndpoints(reposEndpoints);
  const qc = useQueryClient();
  return useMutation<ReposCreatedCIToken, ApiError, string>({ mutationFn: (name) => r.createCIToken(id, name), onSuccess: () => void qc.invalidateQueries({ queryKey: reposKeys.ciTokens(id) }) });
}

export function useRevokeReposCIToken(id: string): UseMutationResult<void, ApiError, string> {
  const r = useEndpoints(reposEndpoints);
  const qc = useQueryClient();
  return useMutation<void, ApiError, string>({ mutationFn: (tokenId) => r.revokeCIToken(id, tokenId), onSettled: () => void qc.invalidateQueries({ queryKey: reposKeys.ciTokens(id) }) });
}

// ---- releases, tags, archives, insights ---------------------------------------

export function useReposReleases(id: string): PagedQueryResult<ReposRelease> {
  const r = useEndpoints(reposEndpoints);
  return usePagedQuery(reposKeys.releases(id), (cursor, signal) => r.releases(id, { cursor }, { signal }), { retry: retryUnlessClientError });
}

export function useReposRelease(id: string, releaseId: string | null | undefined): UseQueryResult<ReposRelease, ApiError> {
  const r = useEndpoints(reposEndpoints);
  return useApiQuery(reposKeys.release(id, releaseId ?? ""), ({ signal }) => r.release(id, releaseId ?? "", { signal }), read, !!releaseId);
}

export function useSaveReposRelease(id: string): UseMutationResult<ReposRelease, ApiError, { releaseId?: string; body: ReposReleaseRequest }> {
  const r = useEndpoints(reposEndpoints);
  const qc = useQueryClient();
  return useMutation<ReposRelease, ApiError, { releaseId?: string; body: ReposReleaseRequest }>({
    mutationFn: ({ releaseId, body }) => (releaseId ? r.updateRelease(id, releaseId, body) : r.createRelease(id, body)),
    onSuccess: (rl) => {
      qc.setQueryData(reposKeys.release(id, rl.id), rl);
      void qc.invalidateQueries({ queryKey: reposKeys.releases(id) });
      void qc.invalidateQueries({ queryKey: reposKeys.info(id) });
    },
  });
}

export function useDeleteReposRelease(id: string): UseMutationResult<void, ApiError, { releaseId: string; deleteTag: boolean }> {
  const r = useEndpoints(reposEndpoints);
  const qc = useQueryClient();
  return useMutation<void, ApiError, { releaseId: string; deleteTag: boolean }>({
    mutationFn: ({ releaseId, deleteTag }) => r.deleteRelease(id, releaseId, deleteTag),
    onSuccess: () => void qc.invalidateQueries({ queryKey: reposKeys.repo(id) }),
  });
}

export function useAddReposReleaseAsset(id: string, releaseId: string): UseMutationResult<ReposReleaseAsset, ApiError, { key: string; name: string }> {
  const r = useEndpoints(reposEndpoints);
  const qc = useQueryClient();
  return useMutation<ReposReleaseAsset, ApiError, { key: string; name: string }>({
    mutationFn: ({ key, name }) => r.addReleaseAsset(id, releaseId, key, name),
    onSuccess: () => {
      void qc.invalidateQueries({ queryKey: reposKeys.release(id, releaseId) });
      void qc.invalidateQueries({ queryKey: reposKeys.releases(id) });
    },
  });
}

export function useDeleteReposReleaseAsset(id: string, releaseId: string): UseMutationResult<void, ApiError, string> {
  const r = useEndpoints(reposEndpoints);
  const qc = useQueryClient();
  return useMutation<void, ApiError, string>({
    mutationFn: (assetId) => r.deleteReleaseAsset(id, releaseId, assetId),
    onSuccess: () => {
      void qc.invalidateQueries({ queryKey: reposKeys.release(id, releaseId) });
      void qc.invalidateQueries({ queryKey: reposKeys.releases(id) });
    },
  });
}

/** A short-lived signed link to download a release file. */
export function useDownloadReposReleaseAsset(id: string): UseMutationResult<{ url: string; name: string }, ApiError, { releaseId: string; assetId: string }> {
  const r = useEndpoints(reposEndpoints);
  return useMutation<{ url: string; name: string }, ApiError, { releaseId: string; assetId: string }>({
    mutationFn: ({ releaseId, assetId }) => r.downloadReleaseAsset(id, releaseId, assetId),
  });
}

export function useReposReleaseNotes(id: string): UseMutationResult<{ body: string; pulls: number; commits: number }, ApiError, { target: string; previous?: string }> {
  const r = useEndpoints(reposEndpoints);
  return useMutation<{ body: string; pulls: number; commits: number }, ApiError, { target: string; previous?: string }>({
    mutationFn: ({ target, previous }) => r.releaseNotes(id, target, previous),
  });
}

export function useCreateReposTag(id: string): UseMutationResult<{ name: string; sha: string }, ApiError, { name: string; target?: string }> {
  const r = useEndpoints(reposEndpoints);
  const qc = useQueryClient();
  return useMutation<{ name: string; sha: string }, ApiError, { name: string; target?: string }>({
    mutationFn: ({ name, target }) => r.createTag(id, name, target),
    onSuccess: () => void qc.invalidateQueries({ queryKey: reposKeys.repo(id) }),
  });
}

export function useDeleteReposTag(id: string): UseMutationResult<void, ApiError, string> {
  const r = useEndpoints(reposEndpoints);
  const qc = useQueryClient();
  return useMutation<void, ApiError, string>({ mutationFn: (name) => r.deleteTag(id, name), onSuccess: () => void qc.invalidateQueries({ queryKey: reposKeys.repo(id) }) });
}

/** A one-time archive download link (valid for two minutes). */
export function useReposArchive(id: string): UseMutationResult<ReposArchiveLink, ApiError, { ref?: string | undefined; format: ReposArchiveFormat }> {
  const r = useEndpoints(reposEndpoints);
  return useMutation<ReposArchiveLink, ApiError, { ref?: string | undefined; format: ReposArchiveFormat }>({ mutationFn: ({ ref, format }) => r.archive(id, ref, format) });
}

export function useReposInsights(id: string): UseQueryResult<ReposInsights, ApiError> {
  const r = useEndpoints(reposEndpoints);
  return useApiQuery(reposKeys.insights(id), ({ signal }) => r.insights(id, { signal }), read);
}

// ---- webhooks ------------------------------------------------------------------

export function useReposWebhooks(id: string, enabled = true): UseQueryResult<{ items: ReposWebhook[]; events: ReposWebhookEvent[] }, ApiError> {
  const r = useEndpoints(reposEndpoints);
  return useApiQuery(reposKeys.webhooks(id), ({ signal }) => r.webhooks(id, { signal }), read, enabled);
}

/** Create (no hookId; resolves with the secret, shown once) or update a webhook. */
export function useSaveReposWebhook(id: string): UseMutationResult<ReposWebhook, ApiError, { hookId?: string; body: ReposWebhookRequest }> {
  const r = useEndpoints(reposEndpoints);
  const qc = useQueryClient();
  return useMutation<ReposWebhook, ApiError, { hookId?: string; body: ReposWebhookRequest }>({
    mutationFn: ({ hookId, body }) => (hookId ? r.updateWebhook(id, hookId, body) : r.createWebhook(id, body)),
    onSuccess: () => void qc.invalidateQueries({ queryKey: reposKeys.webhooks(id) }),
  });
}

export function useDeleteReposWebhook(id: string): UseMutationResult<void, ApiError, string> {
  const r = useEndpoints(reposEndpoints);
  const qc = useQueryClient();
  return useMutation<void, ApiError, string>({ mutationFn: (hookId) => r.deleteWebhook(id, hookId), onSuccess: () => void qc.invalidateQueries({ queryKey: reposKeys.webhooks(id) }) });
}

export function usePingReposWebhook(id: string): UseMutationResult<{ queued: boolean }, ApiError, string> {
  const r = useEndpoints(reposEndpoints);
  const qc = useQueryClient();
  return useMutation<{ queued: boolean }, ApiError, string>({
    mutationFn: (hookId) => r.pingWebhook(id, hookId),
    onSuccess: (_d, hookId) => setTimeout(() => void qc.invalidateQueries({ queryKey: reposKeys.deliveries(id, hookId) }), 1500),
  });
}

export function useReposDeliveries(id: string, hookId: string | null): PagedQueryResult<ReposWebhookDelivery> {
  const r = useEndpoints(reposEndpoints);
  return usePagedQuery(reposKeys.deliveries(id, hookId ?? ""), (cursor, signal) => r.deliveries(id, hookId ?? "", { cursor }, { signal }), { retry: retryUnlessClientError }, !!hookId);
}

export function useReposDelivery(id: string, hookId: string, deliveryId: string | null): UseQueryResult<ReposWebhookDelivery, ApiError> {
  const r = useEndpoints(reposEndpoints);
  return useApiQuery(reposKeys.delivery(id, hookId, deliveryId ?? ""), ({ signal }) => r.delivery(id, hookId, deliveryId ?? "", { signal }), read, !!deliveryId);
}

export function useRedeliverReposWebhook(id: string, hookId: string): UseMutationResult<{ queued: boolean }, ApiError, string> {
  const r = useEndpoints(reposEndpoints);
  const qc = useQueryClient();
  return useMutation<{ queued: boolean }, ApiError, string>({
    mutationFn: (deliveryId) => r.redeliver(id, hookId, deliveryId),
    onSuccess: () => setTimeout(() => void qc.invalidateQueries({ queryKey: reposKeys.deliveries(id, hookId) }), 1500),
  });
}

// ---- git credentials -----------------------------------------------------------

export function useReposCredentials(): PagedQueryResult<ReposCredential> {
  const r = useEndpoints(reposEndpoints);
  return usePagedQuery(reposKeys.credentials(), (cursor, signal) => r.credentials({ cursor }, { signal }));
}

/** Resolves with the secret, shown only now. */
export function useCreateReposCredential(): UseMutationResult<ReposCreatedCredential, ApiError, ReposCreateCredentialRequest> {
  const r = useEndpoints(reposEndpoints);
  const qc = useQueryClient();
  return useMutation<ReposCreatedCredential, ApiError, ReposCreateCredentialRequest>({
    mutationFn: (body) => r.createCredential(body),
    onSuccess: () => void qc.invalidateQueries({ queryKey: reposKeys.credentials() }),
  });
}

export function useRevokeReposCredential(): UseMutationResult<void, ApiError, string> {
  const r = useEndpoints(reposEndpoints);
  const qc = useQueryClient();
  return useMutation<void, ApiError, string>({
    mutationFn: (credId) => r.revokeCredential(credId),
    onSettled: () => void qc.invalidateQueries({ queryKey: reposKeys.credentials() }),
  });
}

// ---- realtime ------------------------------------------------------------------

/** Topic for one repository's live events (anyone who can view it). */
export const reposTopic = (repoId: string): string => `repos:${repoId}`;

registerRealtimeHandler("repos.refs_changed", (qc, e) => {
  // Reads pinned to a commit id don't change; branch reads and lists do.
  void qc.invalidateQueries({
    queryKey: reposKeys.repo(e.data.repo_id),
    predicate: (q) => !["commit"].includes(String(q.queryKey[3])),
  });
  void qc.invalidateQueries({ queryKey: reposKeys.lists() });
});
registerRealtimeHandler("repos.pull_changed", (qc, e) => {
  void qc.invalidateQueries({ queryKey: reposKeys.pull(e.data.repo_id, e.data.number) });
  void qc.invalidateQueries({ queryKey: reposKeys.pulls(e.data.repo_id) });
  void qc.invalidateQueries({ queryKey: reposKeys.info(e.data.repo_id) });
});
registerRealtimeHandler("repos.repo_changed", (qc, e) => {
  void qc.invalidateQueries({ queryKey: reposKeys.repo(e.data.repo_id) });
});
registerRealtimeHandler("repos.issue_changed", (qc, e) => {
  void qc.invalidateQueries({ queryKey: reposKeys.issue(e.data.repo_id, e.data.number) });
  void qc.invalidateQueries({ queryKey: reposKeys.issues(e.data.repo_id) });
  void qc.invalidateQueries({ queryKey: reposKeys.info(e.data.repo_id) });
});
registerRealtimeHandler("repos.status_changed", (qc, e) => {
  void qc.invalidateQueries({ queryKey: ["repos", "repo", e.data.repo_id, "checks-for", e.data.sha] });
  void qc.invalidateQueries({ queryKey: ["repos", "repo", e.data.repo_id, "commit-states"] });
  void qc.invalidateQueries({ queryKey: reposKeys.checks(e.data.repo_id, e.data.sha) });
});
registerReconnectRefetch(reposKeys.all());
