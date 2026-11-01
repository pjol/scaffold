"use client";

import { keepPreviousData, useMutation, useQueryClient, type QueryClient, type UseMutationResult, type UseQueryResult } from "@tanstack/react-query";
import type { ApiError } from "../api/errors";
import { useEndpoints } from "../hooks/context";
import { useApiQuery, useDebouncedValue, usePagedQuery, type PagedQueryResult, type QueryOverrides } from "../hooks/internal";
import { registerReconnectRefetch, registerRealtimeHandler } from "../hooks/realtime-handlers";
import { reposEndpoints, type ReposAt, type ReposPullFilter } from "./api";
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
  gitInfo: () => ["repos", "git-info"] as const,
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

export function useReposTimeline(id: string, n: number): UseQueryResult<{ items: ReposTimelineItem[] }, ApiError> {
  const r = useEndpoints(reposEndpoints);
  return useApiQuery(reposKeys.timeline(id, n), ({ signal }) => r.timeline(id, n, { signal }), read, n > 0);
}

export function useReposPullCommits(id: string, n: number, enabled = true): UseQueryResult<{ items: ReposCommit[] }, ApiError> {
  const r = useEndpoints(reposEndpoints);
  return useApiQuery(reposKeys.pullCommits(id, n), ({ signal }) => r.pullCommits(id, n, { signal }), read, enabled && n > 0);
}

/** The whole change, one file's full patch (`path`), or one commit's (`commit`). */
export function useReposPullDiff(id: string, n: number, params: { path?: string; commit?: string } = {}, enabled = true): UseQueryResult<{ base: string; head: string; diff: ReposDiff }, ApiError> {
  const r = useEndpoints(reposEndpoints);
  return useApiQuery(reposKeys.pullDiff(id, n, params.path ?? "", params.commit ?? ""), ({ signal }) => r.pullDiff(id, n, params, { signal }), read, enabled && n > 0);
}

export function useReposThreads(id: string, n: number): UseQueryResult<{ items: ReposThread[]; head_sha: string }, ApiError> {
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
registerReconnectRefetch(reposKeys.all());
