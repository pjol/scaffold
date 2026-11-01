/**
 * Repositories DTOs: snake_case, matching backend/internal/repos exactly.
 * Timestamps are RFC 3339 strings; ids are UUID strings; commit ids are 40 hex characters.
 */
import type { Page } from "../types";

export type ReposLevel = "none" | "view" | "comment" | "edit" | "manage";

/** A repository as clients see it (`GET /v1/repos/{id}`). */
export interface ReposRepo {
  id: string;
  org_id: string;
  name: string;
  slug: string;
  description: string;
  folder_id: string | null;
  default_branch: string;
  clone_url: string;
  link: string;
  /** No commits yet. */
  empty: boolean;
  my_level: ReposLevel;
  can_push: boolean;
  can_manage: boolean;
  allow_merge_commit: boolean;
  allow_squash: boolean;
  delete_branch_on_merge: boolean;
  open_pulls: number;
  size_bytes: number;
  pushed_at: string | null;
  created_at: string;
  updated_at: string;
}

/** A row of the repository list. */
export interface ReposSummary {
  id: string;
  name: string;
  slug: string;
  description: string;
  folder_id: string | null;
  default_branch: string;
  open_pulls: number;
  my_level: ReposLevel;
  pushed_at: string | null;
  updated_at: string;
}

export interface ReposCreateRequest {
  org_id: string;
  /** Null: the top of your own drive. */
  folder_id?: string | null;
  name: string;
  description?: string;
  default_branch?: string;
  /** Start with a README.md commit. */
  readme?: boolean;
}

export interface ReposUpdateRequest {
  description?: string;
  default_branch?: string;
  slug?: string;
  allow_merge_commit?: boolean;
  allow_squash?: boolean;
  delete_branch_on_merge?: boolean;
}

export interface ReposProtection {
  id: string;
  pattern: string;
  require_pull_request: boolean;
  required_approvals: number;
  dismiss_stale_approvals: boolean;
  allow_force_push: boolean;
  allow_deletion: boolean;
  created_at: string;
}

export type ReposProtectionRequest = Partial<Omit<ReposProtection, "id" | "created_at">>;

export interface ReposRef {
  name: string;
  kind: "branch" | "tag";
  sha: string;
  subject: string;
  author: string;
  updated_at: string;
  default: boolean;
  protected: boolean;
  /** Branches, with `counts`: commits not on the default branch, and default-branch commits it lacks. */
  ahead: number | null;
  behind: number | null;
  open_pull: number | null;
}

export interface ReposRefs {
  items: ReposRef[];
  default_branch: string;
}

export interface ReposTreeEntry {
  name: string;
  path: string;
  type: "blob" | "tree" | "commit" | "symlink";
  mode: string;
  oid: string;
  /** Blobs; -1 otherwise. */
  size: number;
}

export interface ReposSignature {
  name: string;
  email: string;
  at: string;
}

export interface ReposCommit {
  sha: string;
  parents: string[];
  author: ReposSignature;
  committer: ReposSignature;
  subject: string;
  body: string;
}

export interface ReposBlob {
  path: string;
  name: string;
  /** The commit it was read at. */
  sha: string;
  ref: string;
  oid: string;
  mode: string;
  size: number;
  binary: boolean;
  image: boolean;
  too_large: boolean;
  editable: boolean;
  encoding: "utf-8" | "base64" | "";
  content: string;
  /** Highlighter language id from the name ("typescript", "plaintext"). */
  language: string;
  lines: number;
  symlink: boolean;
  submodule: boolean;
}

export interface ReposTree {
  ref: string;
  sha: string;
  path: string;
  entries: ReposTreeEntry[];
  readme: ReposBlob | null;
  last_commit: ReposCommit | null;
}

export interface ReposTreeCommits {
  sha: string;
  /** Entry name → the newest commit touching it. */
  commits: Record<string, ReposCommit>;
}

export interface ReposDiffLine {
  type: "context" | "add" | "del" | "note";
  old: number;
  new: number;
  text: string;
}

export interface ReposDiffHunk {
  header: string;
  old_start: number;
  old_lines: number;
  new_start: number;
  new_lines: number;
  lines: ReposDiffLine[];
}

export interface ReposDiffFile {
  path: string;
  old_path: string;
  status: "added" | "modified" | "deleted" | "renamed" | "copied" | "type_changed";
  old_mode: string;
  new_mode: string;
  additions: number;
  deletions: number;
  binary: boolean;
  /** Too large or past the diff's caps: fetch this file alone (`path`). */
  truncated: boolean;
  hunks: ReposDiffHunk[];
}

export interface ReposDiff {
  files: ReposDiffFile[];
  additions: number;
  deletions: number;
  files_truncated: boolean;
}

export interface ReposCommitDetail extends ReposCommit {
  diff: ReposDiff;
}

export interface ReposPullLink {
  number: number;
  title: string;
}

export interface ReposComparison {
  base: string;
  head: string;
  base_sha: string;
  head_sha: string;
  merge_base: string;
  ahead: number;
  behind: number;
  commits: ReposCommit[];
  diff: ReposDiff;
  mergeable: boolean | null;
  conflicts: string[];
  open_pull: ReposPullLink | null;
}

export interface ReposBlameRange {
  sha: string;
  start: number;
  lines: number;
  author: string;
  at: number;
  subject: string;
}

export interface ReposGrepMatch {
  path: string;
  line: number;
  text: string;
}

export interface ReposSearchResult {
  sha: string;
  ref: string;
  items: ReposGrepMatch[];
  truncated: boolean;
}

// ───────────────────────────────────────────────────────────── edits ──

export interface ReposChange {
  op: "put" | "delete" | "move" | "replace_lines";
  path: string;
  /** move: the old path. */
  from?: string;
  /** put: the whole file; replace_lines: the new lines. */
  content?: string;
  /** put: "base64" for uploads. */
  encoding?: "utf-8" | "base64";
  start_line?: number;
  end_line?: number;
  executable?: boolean;
}

export interface ReposCommitRequest {
  branch: string;
  /** The commit the edit started from ("" for an empty repository). */
  expected_head: string;
  /** Commit to this new branch, started from `branch`. */
  new_branch?: string;
  message: string;
  changes: ReposChange[];
}

export interface ReposCommitResult {
  commit: ReposCommit;
  branch: string;
  suggest_pull: boolean;
}

// ───────────────────────────────────────────────────────────── pull requests ──

export interface ReposUser {
  id: string;
  display_name: string;
  avatar_url: string | null;
}

export type ReposPullState = "open" | "closed" | "merged";
export type ReposReviewState = "approved" | "changes_requested" | "commented";

export interface ReposReviewer {
  user: ReposUser;
  requested: boolean;
  state: ReposReviewState | "";
  /** Approved an older head (dismissed by new commits). */
  stale: boolean;
}

export interface ReposPull {
  id: string;
  repo_id: string;
  number: number;
  title: string;
  body: string;
  state: ReposPullState;
  head_branch: string;
  base_branch: string;
  head_sha: string;
  base_sha: string;
  author: ReposUser | null;
  merge_method: "merge" | "squash" | null;
  merge_commit_sha: string | null;
  merged_by: ReposUser | null;
  merged_at: string | null;
  closed_by: ReposUser | null;
  closed_at: string | null;
  comment_count: number;
  approvals: number;
  changes_requests: number;
  reviewers: ReposReviewer[];
  created_at: string;
  updated_at: string;
  /** Title, description, close and reopen. */
  can_edit: boolean;
}

export type ReposPullPage = Page<ReposPull>;

export interface ReposOpenPullRequest {
  title: string;
  body?: string;
  head: string;
  base?: string;
  reviewers?: string[];
}

export interface ReposUpdatePullRequest {
  title?: string;
  body?: string;
  base?: string;
}

export interface ReposComment {
  id: string;
  author: ReposUser | null;
  body: string;
  path: string | null;
  line: number | null;
  side: "old" | "new" | null;
  commit_sha: string | null;
  reply_to: string | null;
  review_id: string | null;
  resolved_at: string | null;
  resolved_by: ReposUser | null;
  edited_at: string | null;
  deleted: boolean;
  created_at: string;
  can_edit: boolean;
}

export interface ReposReview {
  id: string;
  author: ReposUser | null;
  state: ReposReviewState;
  body: string;
  commit_sha: string;
  dismissed_at: string | null;
  created_at: string;
}

export type ReposEventKind =
  | "opened"
  | "pushed"
  | "force_pushed"
  | "closed"
  | "reopened"
  | "merged"
  | "review_requested"
  | "review_request_removed"
  | "retitled"
  | "base_changed"
  | "branch_deleted"
  | "approvals_dismissed";

export interface ReposEvent {
  id: string;
  kind: ReposEventKind;
  actor: ReposUser | null;
  data: Record<string, unknown>;
  created_at: string;
}

export interface ReposThreadSummary {
  id: string;
  path: string;
  line: number;
  side: "old" | "new";
  body: string;
  replies: number;
  author: ReposUser | null;
}

export type ReposTimelineItem =
  | { type: "comment"; at: string; comment: ReposComment }
  | { type: "review"; at: string; review: ReposReview; threads?: ReposThreadSummary[] }
  | { type: "thread"; at: string; thread: ReposThreadSummary }
  | { type: "event"; at: string; event: ReposEvent };

export interface ReposThread {
  root: ReposComment;
  replies: ReposComment[];
  /** The file changed since the comment; its line may not hold. */
  outdated: boolean;
}

export interface ReposCommentRequest {
  body: string;
  path?: string;
  line?: number;
  side?: "old" | "new";
  reply_to?: string;
}

export interface ReposReviewRequest {
  state: "approve" | "request_changes" | "comment";
  body?: string;
  comments?: ReposCommentRequest[];
}

export type ReposMergeState = "clean" | "conflicts" | "blocked" | "up_to_date" | "missing_branch" | "merged" | "closed";

export interface ReposMergeability {
  state: ReposMergeState;
  mergeable: boolean;
  can_merge: boolean;
  conflicts: string[];
  blockers: string[];
  methods: ("merge" | "squash")[];
  approvals: number;
  required_approvals: number;
  head_sha: string;
  base_sha: string;
  delete_branch: boolean;
  can_delete_branch: boolean;
}

export interface ReposMergeRequest {
  method: "merge" | "squash";
  title?: string;
  message?: string;
  expected_head?: string;
  delete_branch?: boolean;
}

export interface ReposPerson {
  id: string;
  display_name: string;
  avatar_url: string | null;
  level: ReposLevel;
}

// ───────────────────────────────────────────────────────────── credentials and import ──

export interface ReposCredential {
  id: string;
  name: string;
  prefix: string;
  access: "read" | "write";
  expires_at: string | null;
  last_used_at: string | null;
  created_at: string;
}

export interface ReposCreateCredentialRequest {
  name: string;
  access: "read" | "write";
  /** null or 0: never expires. */
  expires_in_days?: number | null;
}

export interface ReposCreatedCredential {
  credential: ReposCredential;
  /** Shown once. */
  secret: string;
}

export interface ReposGitInfo {
  version: string;
  base_url: string;
  max_push_bytes: number;
  max_edit_bytes: number;
  max_commit_bytes: number;
}

export interface ReposImportPlan {
  folder_id: string;
  name: string;
  files: number;
  bytes: number;
  empty_folders: number;
  skipped: { path: string; reason: string }[];
  blockers: string[];
  can_convert: boolean;
}

export interface ReposImportRequest {
  folder_id: string;
  default_branch?: string;
  description?: string;
  message?: string;
}

export interface ReposDownload {
  name: string;
  size: number;
  content_base64: string;
}

declare module "../types" {
  interface RealtimeEventMap {
    "repos.refs_changed": { repo_id: string; via: string };
    "repos.pull_changed": { repo_id: string; number: number };
    "repos.repo_changed": { repo_id: string };
  }
}
