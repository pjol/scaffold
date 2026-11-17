/**
 * Repositories DTOs: snake_case, matching backend/internal/repos exactly.
 * Timestamps are RFC 3339 strings; ids are UUID strings; commit ids are 40 hex characters.
 */
import type { SourceRef } from "../sources";
import type { Page } from "../types";

export type ReposLevel = "none" | "view" | "comment" | "edit" | "manage";

/**
 * What a repository's host offers. The app's own repositories leave it out (everything but
 * `checks` and `import`); connected GitHub and GitLab ones always send it. Read it with
 * `reposSupports(repo, key)`.
 */
export interface ReposSupports {
  settings: boolean;
  credentials: boolean;
  upload: boolean;
  blame: boolean;
  resolve_threads: boolean;
  request_changes: boolean;
  reviewers: boolean;
  realtime: boolean;
  checks: boolean;
  /** Code search on any ref; otherwise the default branch only. */
  code_search_refs: boolean;
  tree_commits: boolean;
  import: boolean;
}

/**
 * A repository as clients see it (`GET /v1/repos/{id}`). Connected repositories have an id
 * like `github~<connection>~acme~web`, `org_id` "", and `source`, `web_url`, `supports`.
 */
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
  /** The app's own: auto-merge may be turned on for pull requests. */
  allow_auto_merge?: boolean;
  open_pulls: number;
  /** The app's own repositories (connected ones leave these out). */
  open_issues?: number;
  releases?: number;
  size_bytes: number;
  pushed_at: string | null;
  created_at: string;
  updated_at: string;
  /** Connected: "acme/web", "acme/platform/api". */
  full_name?: string;
  source?: SourceRef | null;
  web_url?: string;
  private?: boolean;
  archived?: boolean;
  allow_rebase?: boolean;
  supports?: ReposSupports;
  /** Connected with reading only: the app path that turns on changes. */
  grant_write_url?: string;
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
  full_name?: string;
  /** Connected: the account or group that owns it. */
  owner?: string;
  private?: boolean;
  source?: SourceRef | null;
}

/** One connected account's repositories (`GET /v1/repos/connections/{id}/repos`). */
export interface ReposConnectedPage extends Page<ReposSummary> {
  /** The account and the orgs or groups it belongs to, for the owner filter. */
  owners: string[];
  account: string;
  provider: string;
}

export type ReposConnectedSort = "recent" | "name";

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
  allow_rebase?: boolean;
  allow_auto_merge?: boolean;
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
  /** Approval from a code owner (CODEOWNERS on the base) of every changed file. */
  require_code_owner_review: boolean;
  /** Status contexts that must succeed before merging. */
  required_checks: string[];
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
  merge_method: "merge" | "squash" | "rebase" | null;
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
  /** Connected: the viewer's platform account opened it. */
  is_author?: boolean;
  draft?: boolean;
  web_url?: string;
  source?: SourceRef | null;
  /** The app's own repositories: */
  labels?: ReposLabel[];
  assignees?: ReposUser[];
  milestone?: ReposMilestoneRef | null;
  auto_merge?: ReposAutoMerge | null;
  /** The head's combined status ("" without statuses). */
  checks_state?: ReposChecksState;
  can_triage?: boolean;
  /** Detail: issues a closing keyword in the description closes on merge. */
  closing_issues?: ReposIssueLink[];
  reactions?: ReposReaction[];
  mentions?: ReposMention[];
}

export type ReposChecksState = "success" | "failure" | "pending" | "";

export interface ReposAutoMerge {
  method: ReposMergeMethod;
  enabled_by: ReposUser | null;
  enabled_at: string;
}

export interface ReposIssueLink {
  number: number;
  title: string;
  state: ReposIssueState;
}

export interface ReposPullPage extends Page<ReposPull> {
  /** Totals for the query without its state (the app's own). */
  counts?: ReposIssueCounts;
  /** Qualifiers the search didn't understand. */
  ignored?: string[];
}

export interface ReposOpenPullRequest {
  title: string;
  body?: string;
  head: string;
  base?: string;
  reviewers?: string[];
  draft?: boolean;
  labels?: string[];
  assignees?: string[];
  milestone_id?: string | null;
}

export interface ReposUpdatePullRequest {
  title?: string;
  body?: string;
  base?: string;
  /** "" clears. */
  milestone_id?: string;
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
  /** Multi-line comments cover start_line..line. */
  start_line?: number | null;
  /** In your review that isn't submitted yet (only you see it). */
  pending?: boolean;
  /** The commit that applied this comment's suggestion. */
  suggestion_applied_sha?: string | null;
  reactions?: ReposReaction[];
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
  | "approvals_dismissed"
  | "ready_for_review"
  | "converted_to_draft"
  | "labeled"
  | "unlabeled"
  | "assigned"
  | "unassigned"
  | "milestoned"
  | "demilestoned"
  | "referenced"
  | "auto_merge_enabled"
  | "auto_merge_disabled"
  | "branch_updated"
  | "suggestions_applied";

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
  /** Multi-line: the first line (same side). */
  start_line?: number;
  side?: "old" | "new";
  reply_to?: string;
  /** Add to your pending review instead of posting now. */
  pending?: boolean;
}

export interface ReposReviewRequest {
  state: "approve" | "request_changes" | "comment";
  body?: string;
  comments?: ReposCommentRequest[];
}

export type ReposMergeState = "clean" | "conflicts" | "blocked" | "up_to_date" | "missing_branch" | "merged" | "closed" | "draft";

export interface ReposMergeability {
  state: ReposMergeState;
  mergeable: boolean;
  can_merge: boolean;
  conflicts: string[];
  blockers: string[];
  methods: ReposMergeMethod[];
  approvals: number;
  required_approvals: number;
  head_sha: string;
  base_sha: string;
  delete_branch: boolean;
  can_delete_branch: boolean;
  /** The app's own repositories: */
  behind?: number;
  can_update_branch?: boolean;
  checks?: ReposChecks;
  required_checks?: string[];
  /** Code owners still to approve, when the base's rule requires them. */
  code_owners?: string[];
  can_auto_merge?: boolean;
  /** Why rebasing isn't possible ("" when it is). */
  rebase_blocked?: string;
}

/** "rebase" on GitHub repositories that allow it. */
export type ReposMergeMethod = "merge" | "squash" | "rebase";

export interface ReposMergeRequest {
  method: ReposMergeMethod;
  title?: string;
  message?: string;
  expected_head?: string;
  delete_branch?: boolean;
}

/** One CI status check (GitHub check run or status, GitLab job), read-only. */
export interface ReposCheck {
  name: string;
  status: "queued" | "in_progress" | "success" | "failure" | "neutral" | "cancelled" | "skipped";
  description: string;
  url: string;
  allow_failure: boolean;
  /** Required by branch protection (the app's own). */
  required?: boolean;
}

/** Every check on a commit (`GET /v1/repos/{id}/commits/{sha}/checks`); "none" for the app's own. */
export interface ReposChecks {
  sha: string;
  state: "success" | "failure" | "pending" | "none";
  items: ReposCheck[];
  url: string;
}

/** Copy a connected repository into the drive (`POST /v1/repos/connected/import`). */
export interface ReposImportConnectedRequest {
  repo_id: string;
  org_id: string;
  folder_id?: string | null;
  name?: string;
}

/** Link a connected repository into a drive folder (`POST /v1/repos/links`). */
export interface ReposCreateLinkRequest {
  repo_id: string;
  org_id: string;
  folder_id?: string | null;
}

/** A drive link to a connected repository, resolved for the person opening it. */
export interface ReposLink {
  id: string;
  name: string;
  provider: string;
  full_name: string;
  web_url: string;
  folder_id: string | null;
  /** The opener's connected id; null when none of their connections can open it. */
  repo_id: string | null;
  reason?: "no_connection" | "no_access" | "unavailable";
  fix_url?: string;
  source?: SourceRef | null;
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
    "repos.issue_changed": { repo_id: string; number: number };
    "repos.status_changed": { repo_id: string; sha: string };
  }
}

// ---- issues, labels, milestones -----------------------------------------------------------

/** A label tone from the design tokens (rendered like Badge tones). */
export type ReposLabelColor = "neutral" | "primary" | "accent" | "success" | "warning" | "danger" | "info";

export interface ReposLabel {
  id: string;
  name: string;
  color: ReposLabelColor;
  description: string;
}

/** `GET /v1/repos/{id}/labels` rows. */
export interface ReposLabelRow extends ReposLabel {
  open_issues: number;
  open_pulls: number;
}

export interface ReposLabelRequest {
  name?: string;
  color?: ReposLabelColor;
  description?: string;
}

export interface ReposMilestoneRef {
  id: string;
  title: string;
  due_on: string | null;
  state: "open" | "closed";
}

export interface ReposMilestone extends ReposMilestoneRef {
  description: string;
  closed_at: string | null;
  open_items: number;
  closed_items: number;
  created_at: string;
  updated_at: string;
}

export interface ReposMilestoneRequest {
  title?: string;
  description?: string;
  /** YYYY-MM-DD, "" clears. */
  due_on?: string;
  state?: "open" | "closed";
}

export type ReposReactionEmoji = "+1" | "-1" | "laugh" | "hooray" | "confused" | "heart" | "rocket" | "eyes";

export interface ReposReaction {
  emoji: ReposReactionEmoji;
  count: number;
  me: boolean;
}

export type ReposReactionSubject = "issue" | "issue_comment" | "pull" | "pull_comment";

/** A <@uuid> mention resolved for display. */
export interface ReposMention {
  id: string;
  display_name: string;
}

export type ReposIssueState = "open" | "closed";
export type ReposIssueReason = "completed" | "not_planned";

export interface ReposLinkedPull {
  number: number;
  title: string;
  state: ReposPullState;
  draft: boolean;
  closes: boolean;
}

export interface ReposIssue {
  id: string;
  repo_id: string;
  number: number;
  title: string;
  /** Detail only ("" in lists). */
  body: string;
  state: ReposIssueState;
  state_reason: ReposIssueReason | null;
  author: ReposUser | null;
  closed_by: ReposUser | null;
  closed_at: string | null;
  milestone: ReposMilestoneRef | null;
  labels: ReposLabel[];
  assignees: ReposUser[];
  locked: boolean;
  pinned: boolean;
  comment_count: number;
  linked_pulls: number;
  created_at: string;
  updated_at: string;
  reactions?: ReposReaction[];
  pulls?: ReposLinkedPull[];
  mentions?: ReposMention[];
  can_edit: boolean;
  can_triage: boolean;
  can_comment: boolean;
  can_delete: boolean;
}

export interface ReposIssueCounts {
  open: number;
  closed: number;
}

export interface ReposIssuePage extends Page<ReposIssue> {
  counts: ReposIssueCounts;
  ignored: string[];
}

export interface ReposCreateIssueRequest {
  title: string;
  body?: string;
  labels?: string[];
  assignees?: string[];
  milestone_id?: string | null;
  /** A template file name from `GET /templates?kind=issue`. */
  template?: string;
}

export interface ReposUpdateIssueRequest {
  title?: string;
  body?: string;
  state?: ReposIssueState;
  state_reason?: ReposIssueReason;
  /** "" clears. */
  milestone_id?: string;
  locked?: boolean;
  pinned?: boolean;
}

export interface ReposIssueComment {
  id: string;
  author: ReposUser | null;
  body: string;
  edited_at: string | null;
  deleted: boolean;
  created_at: string;
  reactions: ReposReaction[];
  can_edit: boolean;
  can_delete: boolean;
}

export type ReposIssueEventKind =
  | "closed"
  | "reopened"
  | "renamed"
  | "labeled"
  | "unlabeled"
  | "assigned"
  | "unassigned"
  | "milestoned"
  | "demilestoned"
  | "locked"
  | "unlocked"
  | "pinned"
  | "unpinned"
  | "referenced";

export interface ReposIssueEvent {
  id: string;
  kind: ReposIssueEventKind;
  actor: ReposUser | null;
  data: Record<string, unknown>;
  created_at: string;
}

export type ReposIssueTimelineItem =
  | { type: "comment"; at: string; comment: ReposIssueComment }
  | { type: "event"; at: string; event: ReposIssueEvent };

export interface ReposIssueTimeline {
  items: ReposIssueTimelineItem[];
  mentions: ReposMention[];
}

/** An issue or pull request template from the default branch. */
export interface ReposTemplate {
  file: string;
  name: string;
  about: string;
  title: string;
  body: string;
  labels: string[];
  assignees: string[];
}

// ---- pull request depth -------------------------------------------------------------------

export interface ReposPendingReview {
  id: string | null;
  comments: number;
  commit_sha: string;
}

export interface ReposApplySuggestionsRequest {
  comment_ids: string[];
  message?: string;
  expected_head?: string;
}

// ---- statuses, CI tokens ------------------------------------------------------------------

export interface ReposCIToken {
  id: string;
  name: string;
  prefix: string;
  last_used_at: string | null;
  created_at: string;
}

export interface ReposCITokens {
  items: ReposCIToken[];
  /** Where CI posts: POST <status_url> with Authorization: Bearer <token>. */
  status_url: string;
}

export interface ReposCreatedCIToken {
  token: ReposCIToken;
  secret: string;
}

// ---- releases -----------------------------------------------------------------------------

export interface ReposReleaseAsset {
  id: string;
  name: string;
  content_type: string;
  size_bytes: number;
  download_count: number;
  created_at: string;
}

export interface ReposRelease {
  id: string;
  tag: string;
  name: string;
  body: string;
  draft: boolean;
  prerelease: boolean;
  target_sha: string;
  author: ReposUser | null;
  published_at: string | null;
  created_at: string;
  latest: boolean;
  assets: ReposReleaseAsset[];
  mentions: ReposMention[];
}

export interface ReposReleaseRequest {
  tag?: string;
  /** Branch or commit for a new tag (default: the default branch). */
  target?: string;
  name?: string;
  body?: string;
  draft?: boolean;
  prerelease?: boolean;
}

// ---- archives, insights, webhooks ---------------------------------------------------------

export type ReposArchiveFormat = "zip" | "tar.gz";

export interface ReposArchiveLink {
  url: string;
  name: string;
  sha: string;
}

export interface ReposContributor {
  name: string;
  user: ReposUser | null;
  commits: number;
  additions: number;
  deletions: number;
  first: string;
  last: string;
}

export interface ReposInsights {
  branch: string;
  sha: string;
  total_commits: number;
  truncated: boolean;
  contributors: ReposContributor[];
  weeks: { week: string; commits: number }[];
  open_issues: number;
  closed_issues: number;
  open_pulls: number;
  merged_pulls: number;
}

export type ReposWebhookEvent = "push" | "pull_request" | "pull_request_review" | "issues" | "issue_comment" | "release" | "status";

export interface ReposWebhook {
  id: string;
  url: string;
  events: ReposWebhookEvent[];
  active: boolean;
  last_status: number | null;
  last_delivery_at: string | null;
  created_at: string;
  /** Only in the response that created it. */
  secret?: string;
}

export interface ReposWebhookRequest {
  url?: string;
  events?: ReposWebhookEvent[];
  active?: boolean;
}

export interface ReposWebhookDelivery {
  id: string;
  event: string;
  action: string;
  status_code: number | null;
  error: string;
  duration_ms: number;
  redelivery_of: string | null;
  created_at: string;
  /** Detail only. */
  payload?: string;
  response?: string;
}
