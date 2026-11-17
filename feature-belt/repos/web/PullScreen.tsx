"use client";

import {
  errorMessage,
  hasErrorCode,
  reposRelativeTime,
  reposResolveSteps,
  reposPlatformName,
  reposPullNoun,
  reposShortSha,
  reposSupports,
  useAddReposComment,
  useApplyReposSuggestions,
  useDiscardReposPendingReview,
  useReposAutoMerge,
  useReposChecks,
  useReposPendingReview,
  useReposViewedFiles,
  useSetReposPullAssignees,
  useSetReposPullDraft,
  useSetReposPullLabels,
  useSetReposViewed,
  useUpdateReposPullBranch,
  useDeleteReposComment,
  useEditReposComment,
  useMergeReposPull,
  useMe,
  useRemoveReposReviewer,
  useReposDiffFileLoader,
  useReposMergeability,
  useReposPull,
  useReposPullCommits,
  useReposPullDiff,
  useReposThreads,
  useReposTimeline,
  useRequestReposReviewers,
  useResolveReposThread,
  useSetReposPullOpen,
  useSubmitReposReview,
  useUpdateReposPull,
  type ApiError,
  type ReposChecks,
  type ReposComment,
  type ReposCheck,
  type ReposMention,
  type ReposEvent,
  type ReposMergeMethod,
  type ReposMergeability,
  type ReposPerson,
  type ReposPull,
  type ReposRepo,
  type ReposReviewer,
  type ReposThread,
  type ReposTimelineItem,
} from "@scaffoldapp/shared";
import {
  Badge,
  Banner,
  Button,
  Card,
  Checkbox,
  ConfirmDialog,
  Dialog,
  EmptyState,
  ErrorState,
  Field,
  IconButton,
  Input,
  Menu,
  MenuItem,
  SegmentedControl,
  Skeleton,
  SkeletonGroup,
  SourceBadge,
  Tab,
  Tabs,
  TabsList,
  TextArea,
  Tooltip,
  cn,
  toast,
} from "@scaffoldapp/ui-web";
import {
  AlertTriangle,
  Check,
  CircleCheck,
  FileDiff,
  GitPullRequestDraft,
  Milestone as MilestoneIcon,
  Zap,
  CircleDot,
  CircleX,
  Clock,
  Copy,
  ExternalLink,
  Eye,
  GitBranch,
  GitCommitHorizontal,
  GitMerge,
  GitPullRequestClosed,
  MessageSquare,
  MoreHorizontal,
  Pencil,
  RefreshCw,
  Trash2,
  UserPlus,
  X,
} from "lucide-react";
import Link from "next/link";
import { createContext, useContext, useMemo, useState, type ReactNode } from "react";
import { useUrlState } from "@/lib/url-state";
import { ReviewerPicker } from "./CompareScreen";
import { DiffModeToggle, DiffSkeleton, DiffView, useDiffMode } from "./DiffView";
import { IssueStateIcon, LabelList, LabelPickerDialog, MilestoneMenu, PeopleList, PeoplePicker, Reactions, SideSection, GearButton } from "./issueParts";
import { RepoMarkdown } from "./Markdown";
import { Person } from "./parts";
import { copyText, repoHref, useRepoContext } from "./RepoLayout";

type PullTab = "conversation" | "commits" | "files";

/** Suggestions chosen to commit together, and the names behind <@id> mentions. */
interface PullCtx {
  batch: string[];
  toggle: (commentId: string) => void;
  clear: () => void;
  mentions: ReposMention[];
}
const PullContext = createContext<PullCtx>({ batch: [], toggle: () => undefined, clear: () => undefined, mentions: [] });

/** ```suggestion … ``` in a comment: the text around it and the replacement lines. */
export function splitSuggestion(body: string): { before: string; lines: string; after: string } | null {
  const m = /(^|\n)```suggestion[ \t]*\r?\n([\s\S]*?)(?:\r?\n)?```/.exec(body);
  if (!m) return null;
  return { before: body.slice(0, m.index + m[1]!.length), lines: m[2] ?? "", after: body.slice(m.index + m[0].length) };
}

/** A pull request: conversation, commits and the changed files with line comments; review and merge. */
export function PullScreen({ number }: { number: number }) {
  const repo = useRepoContext();
  const pull = useReposPull(repo.id, number);
  const [tab, setTab] = useUrlState<PullTab>("tab", "conversation");
  const commits = useReposPullCommits(repo.id, number);
  const [ws, setWs] = useUrlState<"" | "1">("w", "");
  const diff = useReposPullDiff(repo.id, number, { ignoreWhitespace: ws === "1" }, tab === "files");
  const timeline = useReposTimeline(repo.id, number);
  const threads = useReposThreads(repo.id, number);
  const [batch, setBatch] = useState<string[]>([]);
  const mentions = useMemo(
    () => [...(pull.data?.mentions ?? []), ...(timeline.data?.mentions ?? []), ...(threads.data?.mentions ?? [])],
    [pull.data?.mentions, timeline.data?.mentions, threads.data?.mentions],
  );
  const ctx = useMemo<PullCtx>(
    () => ({ batch, toggle: (id) => setBatch((b) => (b.includes(id) ? b.filter((x) => x !== id) : [...b, id])), clear: () => setBatch([]), mentions }),
    [batch, mentions],
  );

  if (pull.isPending) {
    return (
      <SkeletonGroup label="Loading pull request" className="flex flex-col gap-4">
        <Skeleton className="h-10 w-2/3" />
        <Skeleton className="h-5 w-1/2" />
        <Skeleton className="h-64 w-full" />
      </SkeletonGroup>
    );
  }
  if (pull.isError) {
    return (
      <Card>
        <ErrorState title={(pull.error as ApiError).status === 404 ? "Pull request not found" : "Couldn't load this pull request"} info={errorMessage(pull.error, "Try again.")} onRetry={() => void pull.refetch()} />
      </Card>
    );
  }
  const p = pull.data;
  return (
    <PullContext.Provider value={ctx}>
    <div className="flex flex-col gap-6">
      <PullHeader repo={repo} pull={p} commits={commits.data?.items.length} />
      <div className="-mx-gutter overflow-x-auto px-gutter">
      <Tabs value={tab} onValueChange={(v) => setTab(v as PullTab)}>
        <TabsList aria-label="Pull request" className="min-w-max">
          <Tab value="conversation" icon={<MessageSquare />} count={p.comment_count || undefined}>
            Conversation
          </Tab>
          <Tab value="commits" icon={<GitCommitHorizontal />} count={commits.data?.items.length}>
            Commits
          </Tab>
          <Tab value="files" icon={<Eye />} count={diff.data?.diff.files.length}>
            Files changed
          </Tab>
        </TabsList>
      </Tabs>
      </div>
      {tab === "conversation" ? (
        <Conversation repo={repo} pull={p} />
      ) : tab === "commits" ? (
        <PullCommits repo={repo} pull={p} commits={commits} />
      ) : (
        <PullFiles repo={repo} pull={p} diff={diff} ignoreWhitespace={ws === "1"} onIgnoreWhitespace={(on) => setWs(on ? "1" : null)} />
      )}
      {batch.length ? <BatchBar repo={repo} pull={p} /> : null}
    </div>
    </PullContext.Provider>
  );
}

/** Commit the chosen suggestions to the head branch in one commit. */
function BatchBar({ repo, pull: p }: { repo: ReposRepo; pull: ReposPull }) {
  const { batch, clear } = useContext(PullContext);
  const apply = useApplyReposSuggestions(repo.id, p.number);
  const [open, setOpen] = useState(false);
  const [message, setMessage] = useState("");
  return (
    <div className="sticky bottom-4 z-10 flex flex-wrap items-center gap-3 self-center rounded-lg border border-border bg-surface-raised px-4 py-2 shadow-lg">
      <span className="type-label text-fg">
        {batch.length} {batch.length === 1 ? "suggestion" : "suggestions"} in the batch
      </span>
      <Button size="sm" variant="ghost" onClick={clear}>
        Clear
      </Button>
      <Button size="sm" onClick={() => setOpen(true)}>
        Commit suggestions
      </Button>
      <Dialog
        open={open}
        onOpenChange={setOpen}
        title={`Commit ${batch.length} ${batch.length === 1 ? "suggestion" : "suggestions"} to ${p.head_branch}`}
        footer={
          <>
            <Button variant="ghost" onClick={() => setOpen(false)}>
              Cancel
            </Button>
            <Button
              loading={apply.isPending}
              onClick={async () => {
                try {
                  await apply.mutateAsync({ comment_ids: batch, ...(message.trim() ? { message: message.trim() } : {}), expected_head: p.head_sha });
                  toast.success("Suggestions committed");
                  clear();
                  setOpen(false);
                } catch (e) {
                  toast.error(errorMessage(e, "Couldn't apply them."));
                }
              }}
            >
              Commit changes
            </Button>
          </>
        }
      >
        <Field label="Commit message">
          <Input value={message} onChange={(e) => setMessage(e.target.value)} placeholder="Apply suggestions from code review" />
        </Field>
      </Dialog>
    </div>
  );
}

function stateBadge(p: ReposPull) {
  if (p.state === "merged") return <Badge tone="accent" size="md" icon={<GitMerge />}>Merged</Badge>;
  if (p.state === "closed") return <Badge tone="danger" size="md" icon={<GitPullRequestClosed />}>Closed</Badge>;
  if (p.draft) return <Badge tone="neutral" size="md" icon={<GitPullRequestDraft />}>Draft</Badge>;
  return <Badge tone="success" size="md" icon={<CircleDot />}>Open</Badge>;
}

function ChecksStateBadge({ state }: { state: NonNullable<ReposPull["checks_state"]> }) {
  if (state === "success") return <Badge tone="success" icon={<Check />}>Checks passed</Badge>;
  if (state === "failure") return <Badge tone="danger" icon={<CircleX />}>Checks failed</Badge>;
  if (state === "pending") return <Badge tone="warning" icon={<Clock />}>Checks running</Badge>;
  return null;
}

function PullHeader({ repo, pull: p, commits }: { repo: ReposRepo; pull: ReposPull; commits?: number | undefined }) {
  const [editing, setEditing] = useState(false);
  const [title, setTitle] = useState(p.title);
  const update = useUpdateReposPull(repo.id, p.number);
  const save = async () => {
    try {
      await update.mutateAsync({ title });
      setEditing(false);
    } catch (e) {
      toast.error(errorMessage(e, "Couldn't rename it."));
    }
  };
  return (
    <div className="flex flex-col gap-3">
      {editing ? (
        <form
          className="flex flex-wrap items-center gap-2"
          onSubmit={(e) => {
            e.preventDefault();
            void save();
          }}
        >
          <Input aria-label="Title" value={title} onChange={(e) => setTitle(e.target.value)} autoFocus wrapperClassName="min-w-0 flex-1" maxLength={256} />
          <Button type="submit" size="sm" loading={update.isPending}>
            Save
          </Button>
          <Button type="button" size="sm" variant="ghost" onClick={() => setEditing(false)}>
            Cancel
          </Button>
        </form>
      ) : (
        <div className="flex items-start gap-3">
          <h2 className="min-w-0 flex-1 type-headline text-fg">
            {p.title} <span className="font-normal text-fg-muted">{repo.source?.type === "connected" && repo.source.provider === "gitlab" ? "!" : "#"}{p.number}</span>
          </h2>
          {p.web_url && reposPlatformName(repo) ? (
            <Tooltip content={`Open in ${reposPlatformName(repo)}`}>
              <IconButton label={`Open in ${reposPlatformName(repo)}`} icon={<ExternalLink />} variant="ghost" size="sm" onClick={() => window.open(p.web_url, "_blank", "noopener,noreferrer")} />
            </Tooltip>
          ) : null}
          {p.can_edit ? <IconButton label="Edit title" icon={<Pencil />} variant="ghost" size="sm" tooltip onClick={() => (setTitle(p.title), setEditing(true))} /> : null}
        </div>
      )}
      <div className="flex flex-wrap items-center gap-2 type-label text-fg-muted">
        {stateBadge(p)}
        {p.checks_state ? <ChecksStateBadge state={p.checks_state} /> : null}
        <SourceBadge source={p.source ?? repo.source} />
        <span>
          <span className="font-semibold text-fg">{p.author?.display_name || "Deleted user"}</span>{" "}
          {p.state === "merged" ? "merged" : "wants to merge"} {commits !== undefined ? `${commits} ${commits === 1 ? "commit" : "commits"} ` : ""}into
        </span>
        <BranchChip repoId={repo.id} name={p.base_branch} />
        <span>from</span>
        <BranchChip repoId={repo.id} name={p.head_branch} />
        <Tooltip content="Copy branch name">
          <IconButton label="Copy branch name" icon={<Copy />} variant="ghost" size="sm" onClick={() => void copyText(p.head_branch, "Branch name copied")} />
        </Tooltip>
      </div>
    </div>
  );
}

function BranchChip({ repoId, name }: { repoId: string; name: string }) {
  return (
    <Link href={repoHref(repoId, "", { ref: name })} className="rounded-sm bg-info-soft px-1.5 py-0.5 font-mono type-caption text-info-strong hover:underline">
      {name}
    </Link>
  );
}

// ---- conversation --------------------------------------------------------------

function Conversation({ repo, pull: p }: { repo: ReposRepo; pull: ReposPull }) {
  const timeline = useReposTimeline(repo.id, p.number);
  const threads = useReposThreads(repo.id, p.number);
  const byId = useMemo(() => new Map((threads.data?.items ?? []).map((t) => [t.root.id, t])), [threads.data]);
  const canComment = repo.my_level !== "view";
  return (
    <div className="flex flex-col gap-section lg:grid lg:grid-cols-[minmax(0,1fr)_16rem] lg:gap-10">
      <div className="flex min-w-0 flex-col gap-4">
        <Description repo={repo} pull={p} />
        {timeline.isPending ? (
          <SkeletonGroup label="Loading conversation" className="flex flex-col gap-3">
            <Skeleton className="h-16 w-full" />
            <Skeleton className="h-16 w-full" />
          </SkeletonGroup>
        ) : timeline.isError ? (
          <Card>
            <ErrorState info={errorMessage(timeline.error, "Try again.")} onRetry={() => void timeline.refetch()} />
          </Card>
        ) : (
          <ol className="flex flex-col gap-3">
            {timeline.data.items.map((item, i) => (
              <li key={i}>
                <TimelineEntry repo={repo} pull={p} item={item} threads={byId} />
              </li>
            ))}
          </ol>
        )}
        {p.state === "open" ? <MergeBox repo={repo} pull={p} /> : <ClosedBox pull={p} />}
        {canComment ? <Composer repo={repo} pull={p} /> : null}
      </div>
      <Sidebar repo={repo} pull={p} />
    </div>
  );
}

function Description({ repo, pull: p }: { repo: ReposRepo; pull: ReposPull }) {
  const [editing, setEditing] = useState(false);
  const [body, setBody] = useState(p.body);
  const update = useUpdateReposPull(repo.id, p.number);
  return (
    <Card padding="none">
      <div className="flex min-h-12 items-center gap-2 border-b border-border px-4 type-label">
        <Person user={p.author} />
        <span className="text-fg-muted">opened {reposRelativeTime(p.created_at)}</span>
        {p.can_edit && !editing ? <IconButton label="Edit description" icon={<Pencil />} variant="ghost" size="sm" className="ml-auto" onClick={() => (setBody(p.body), setEditing(true))} /> : null}
      </div>
      <div className="p-4">
        {editing ? (
          <div className="flex flex-col gap-2">
            <TextArea aria-label="Description" value={body} onChange={(e) => setBody(e.target.value)} minRows={5} autoFocus />
            <div className="flex justify-end gap-2">
              <Button variant="ghost" size="sm" onClick={() => setEditing(false)}>
                Cancel
              </Button>
              <Button
                size="sm"
                loading={update.isPending}
                onClick={async () => {
                  try {
                    await update.mutateAsync({ body });
                    setEditing(false);
                  } catch (e) {
                    toast.error(errorMessage(e, "Couldn't save."));
                  }
                }}
              >
                Save
              </Button>
            </div>
          </div>
        ) : p.body ? (
          <RepoMarkdown source={p.body} repoId={repo.id} gitRef={p.head_branch} dir="" mentions={p.mentions} />
        ) : (
          <p className="type-body text-fg-muted">No description.</p>
        )}
        {p.reactions ? (
          <div className="pt-3">
            <Reactions repoId={repo.id} number={p.number} subject={{ type: "pull", id: p.id }} reactions={p.reactions} canReact={repo.my_level !== "view"} />
          </div>
        ) : null}
      </div>
    </Card>
  );
}

function CommentCard({ repo, pull, comment: c, compact }: { repo: ReposRepo; pull: ReposPull; comment: ReposComment; compact?: boolean }) {
  const { mentions } = useContext(PullContext);
  const [editing, setEditing] = useState(false);
  const [body, setBody] = useState(c.body);
  const [deleting, setDeleting] = useState(false);
  const edit = useEditReposComment(repo.id, pull.number);
  const del = useDeleteReposComment(repo.id, pull.number);
  const canDelete = c.can_edit || repo.can_manage;
  return (
    <div className={cn("flex flex-col gap-2", !compact && "rounded-lg border border-border bg-surface")}>
      <div className={cn("flex min-h-10 items-center gap-2 type-label", !compact && "border-b border-border px-4")}>
        <Person user={c.author} />
        <span className="text-fg-muted">{reposRelativeTime(c.created_at)}</span>
        {c.edited_at ? <span className="type-caption text-fg-subtle">edited</span> : null}
        {c.pending && !compact ? <Badge tone="warning">Pending</Badge> : null}
        {!c.deleted && (c.can_edit || canDelete) ? (
          <span className="ml-auto">
            <Menu trigger={<IconButton label="Comment actions" icon={<MoreHorizontal />} variant="ghost" size="sm" />}>
              {c.can_edit ? (
                <MenuItem icon={<Pencil />} onSelect={() => (setBody(c.body), setEditing(true))}>
                  Edit
                </MenuItem>
              ) : null}
              {canDelete ? (
                <MenuItem icon={<Trash2 />} destructive onSelect={() => setDeleting(true)}>
                  Delete
                </MenuItem>
              ) : null}
            </Menu>
          </span>
        ) : null}
      </div>
      <div className={cn(!compact && "px-4 pb-4")}>
        {c.deleted ? (
          <p className="type-label italic text-fg-muted">This comment was deleted.</p>
        ) : editing ? (
          <div className="flex flex-col gap-2">
            <TextArea aria-label="Comment" value={body} onChange={(e) => setBody(e.target.value)} autoFocus />
            <div className="flex justify-end gap-2">
              <Button size="sm" variant="ghost" onClick={() => setEditing(false)}>
                Cancel
              </Button>
              <Button
                size="sm"
                loading={edit.isPending}
                onClick={async () => {
                  try {
                    await edit.mutateAsync({ commentId: c.id, body });
                    setEditing(false);
                  } catch (e) {
                    toast.error(errorMessage(e, "Couldn't save."));
                  }
                }}
              >
                Save
              </Button>
            </div>
          </div>
        ) : (
          <CommentBody repo={repo} pull={pull} comment={c} mentions={mentions} />
        )}
        {!c.deleted && !editing && c.reactions && !c.pending ? (
          <div className="pt-2">
            <Reactions repoId={repo.id} number={pull.number} subject={{ type: "pull_comment", id: c.id }} reactions={c.reactions} canReact={repo.my_level !== "view"} />
          </div>
        ) : null}
      </div>
      <ConfirmDialog
        open={deleting}
        onOpenChange={setDeleting}
        title="Delete this comment?"
        tone="danger"
        confirmLabel="Delete"
        onConfirm={() => del.mutateAsync(c.id)}
        errorText={(e) => errorMessage(e, "Couldn't delete it.")}
      >
        It can't be undone.
      </ConfirmDialog>
    </div>
  );
}

/** A comment's Markdown, with a ```suggestion block shown as a change to apply. */
function CommentBody({ repo, pull, comment: c, mentions }: { repo: ReposRepo; pull: ReposPull; comment: ReposComment; mentions: ReposMention[] }) {
  const { batch, toggle } = useContext(PullContext);
  const apply = useApplyReposSuggestions(repo.id, pull.number);
  const sug = splitSuggestion(c.body);
  if (!sug) return <RepoMarkdown source={c.body} repoId={repo.id} gitRef={pull.head_branch} dir="" mentions={mentions} />;
  const canApply = repo.can_push && pull.state === "open" && !!c.path && c.side === "new" && !c.suggestion_applied_sha && !c.pending;
  const inBatch = batch.includes(c.id);
  const lines = sug.lines === "" ? [] : sug.lines.split("\n");
  const range = c.start_line && c.line && c.start_line < c.line ? `lines ${c.start_line}–${c.line}` : `line ${c.line ?? ""}`;
  return (
    <div className="flex flex-col gap-2">
      {sug.before.trim() ? <RepoMarkdown source={sug.before} repoId={repo.id} gitRef={pull.head_branch} dir="" mentions={mentions} /> : null}
      <div className="overflow-hidden rounded-md border border-border">
        <div className="flex min-h-9 flex-wrap items-center gap-2 border-b border-border bg-sunken px-3 py-1 type-caption text-fg-muted">
          <FileDiff className="size-4" aria-hidden />
          <span>Suggested change to {range}</span>
          {c.suggestion_applied_sha ? (
            <Badge tone="success" icon={<CircleCheck />}>
              Applied in {reposShortSha(c.suggestion_applied_sha)}
            </Badge>
          ) : null}
          {canApply ? (
            <span className="ml-auto flex flex-wrap justify-end gap-1">
              <Button size="sm" variant="ghost" onClick={() => toggle(c.id)} aria-pressed={inBatch}>
                {inBatch ? "Remove from batch" : "Add to batch"}
              </Button>
              <Button
                size="sm"
                variant="outline"
                loading={apply.isPending}
                onClick={() =>
                  apply.mutate(
                    { comment_ids: [c.id], expected_head: pull.head_sha },
                    { onSuccess: () => toast.success("Suggestion committed"), onError: (e) => toast.error(errorMessage(e, "Couldn't apply it.")) },
                  )
                }
              >
                Commit suggestion
              </Button>
            </span>
          ) : null}
        </div>
        <pre className="overflow-x-auto bg-success-soft px-3 py-2 font-mono type-label text-fg">
          {lines.length ? lines.map((l, i) => <div key={i}>{`+ ${l}`}</div>) : <span className="text-fg-muted italic">Deletes these lines.</span>}
        </pre>
      </div>
      {sug.after.trim() ? <RepoMarkdown source={sug.after} repoId={repo.id} gitRef={pull.head_branch} dir="" mentions={mentions} /> : null}
    </div>
  );
}

const REVIEW_TEXT = { approved: "approved these changes", changes_requested: "requested changes", commented: "reviewed" } as const;

function TimelineEntry({ repo, pull, item, threads }: { repo: ReposRepo; pull: ReposPull; item: ReposTimelineItem; threads: Map<string, ReposThread> }) {
  switch (item.type) {
    case "comment":
      return <CommentCard repo={repo} pull={pull} comment={item.comment} />;
    case "review": {
      const r = item.review;
      const Icon = r.state === "approved" ? Check : r.state === "changes_requested" ? AlertTriangle : Eye;
      const tone = r.state === "approved" ? "bg-success-soft text-success-strong" : r.state === "changes_requested" ? "bg-warning-soft text-warning-strong" : "bg-sunken text-fg-muted";
      return (
        <div className="flex flex-col gap-2">
          <div className="flex flex-wrap items-center gap-2 type-label">
            <span className={cn("flex size-7 items-center justify-center rounded-pill [&_svg]:size-4", tone)}>
              <Icon aria-hidden />
            </span>
            <Person user={r.author} />
            <span className="text-fg-muted">
              {REVIEW_TEXT[r.state]} {reposRelativeTime(r.created_at)}
            </span>
            {r.dismissed_at ? <Badge tone="neutral">outdated</Badge> : null}
          </div>
          {r.body || item.threads?.length ? (
            <Card padding="compact" className="ml-9 flex flex-col gap-3">
              {r.body ? <ReviewBody repo={repo} pull={pull} body={r.body} /> : null}
              {item.threads?.map((t) => {
                const full = threads.get(t.id);
                return full ? <ThreadCard key={t.id} repo={repo} pull={pull} thread={full} showPath /> : null;
              })}
            </Card>
          ) : null}
        </div>
      );
    }
    case "thread": {
      const full = threads.get(item.thread.id);
      return full ? <ThreadCard repo={repo} pull={pull} thread={full} showPath /> : null;
    }
    case "event":
      return <EventLine repo={repo} event={item.event} />;
  }
}

function ReviewBody({ repo, pull, body }: { repo: ReposRepo; pull: ReposPull; body: string }) {
  const { mentions } = useContext(PullContext);
  return <RepoMarkdown source={body} repoId={repo.id} gitRef={pull.head_branch} dir="" mentions={mentions} />;
}

interface EventData {
  commits?: number;
  before?: string;
  after?: string;
  commit?: string;
  method?: string;
  outside_app?: boolean;
  from?: string;
  to?: string;
  branch?: string;
  after_merge?: boolean;
  name?: string;
  color?: string;
  title?: string;
  user_id?: string;
  again?: boolean;
  count?: number;
  source?: "issue" | "pull" | "commit";
  number?: number;
  sha?: string;
  message?: string;
  closes?: boolean;
  base?: string;
  draft?: boolean;
}

function EventLine({ repo, event: e }: { repo: ReposRepo; event: ReposEvent }) {
  const { mentions } = useContext(PullContext);
  const who = e.actor?.display_name || (e.actor ? "Deleted user" : "Someone");
  const noun = reposPullNoun(repo).toLowerCase();
  const d = e.data as EventData;
  let icon: ReactNode = <CircleDot />;
  let text: ReactNode;
  switch (e.kind) {
    case "opened":
      text = d.draft ? `opened this ${noun} as a draft` : `opened this ${noun}`;
      break;
    case "ready_for_review":
      icon = <Eye />;
      text = "marked this ready for review";
      break;
    case "converted_to_draft":
      icon = <GitPullRequestDraft />;
      text = "turned this back into a draft";
      break;
    case "labeled":
    case "unlabeled":
      icon = <Pencil />;
      text = (
        <>
          {e.kind === "labeled" ? "added" : "removed"} <LabelList labels={[{ id: d.name ?? "", name: d.name ?? "", color: (d.color as never) ?? "neutral", description: "" }]} className="inline-flex" />
        </>
      );
      break;
    case "assigned":
    case "unassigned": {
      icon = <UserPlus />;
      const name = mentions.find((m) => m.id === d.user_id)?.display_name ?? "someone";
      text = e.actor && d.user_id === e.actor.id ? (e.kind === "assigned" ? "self-assigned this" : "removed their assignment") : `${e.kind === "assigned" ? "assigned" : "unassigned"} ${name}`;
      break;
    }
    case "milestoned":
    case "demilestoned":
      icon = <MilestoneIcon />;
      text = `${e.kind === "milestoned" ? "added this to" : "removed this from"} the ${d.title ?? ""} milestone`;
      break;
    case "referenced":
      icon = <GitCommitHorizontal />;
      text =
        d.source === "commit" ? (
          <>
            mentioned this in{" "}
            <Link href={repoHref(repo.id, `commits/${d.sha}`)} className="font-mono hover:underline">
              {reposShortSha(d.sha ?? "")}
            </Link>
          </>
        ) : (
          <>
            mentioned this in{" "}
            <Link href={repoHref(repo.id, `${d.source === "pull" ? "pulls" : "issues"}/${d.number}`)} className="font-semibold text-fg hover:underline">
              {d.title} #{d.number}
            </Link>
          </>
        );
      break;
    case "auto_merge_enabled":
      icon = <Zap />;
      text = `turned on auto-merge (${METHOD_LABEL[(d.method as ReposMergeMethod) ?? "merge"]?.toLowerCase() ?? d.method})`;
      break;
    case "auto_merge_disabled":
      icon = <Zap />;
      text = "turned off auto-merge";
      break;
    case "branch_updated":
      icon = <RefreshCw />;
      text = (
        <>
          merged <span className="font-mono">{d.base}</span> into this branch
        </>
      );
      break;
    case "suggestions_applied":
      icon = <FileDiff />;
      text = `applied ${d.count ?? ""} ${d.count === 1 ? "suggestion" : "suggestions"}`;
      break;
    case "pushed":
      icon = <GitCommitHorizontal />;
      text = (
        <>
          added {d.commits ?? "new"} {d.commits === 1 ? "commit" : "commits"}{" "}
          <Link href={repoHref(repo.id, `commits/${String(d.after)}`)} className="font-mono hover:underline">
            {reposShortSha(String(d.after ?? ""))}
          </Link>
        </>
      );
      break;
    case "force_pushed":
      icon = <RefreshCw />;
      text = (
        <>
          force-pushed from <span className="font-mono">{reposShortSha(String(d.before ?? ""))}</span> to <span className="font-mono">{reposShortSha(String(d.after ?? ""))}</span>
        </>
      );
      break;
    case "closed":
      icon = <GitPullRequestClosed />;
      text = `closed this ${noun}`;
      break;
    case "reopened":
      text = `reopened this ${noun}`;
      break;
    case "merged":
      icon = <GitMerge />;
      text = (
        <>
          merged {d.outside_app ? "these commits outside the app" : d.method === "rebase" ? "by rebasing" : `with ${d.method === "squash" ? "a squash" : "a merge commit"}`}{" "}
          <Link href={repoHref(repo.id, `commits/${String(d.commit)}`)} className="font-mono hover:underline">
            {reposShortSha(String(d.commit ?? ""))}
          </Link>
        </>
      );
      break;
    case "review_requested": {
      icon = <UserPlus />;
      const name = mentions.find((m) => m.id === d.user_id)?.display_name;
      text = `${d.again ? "asked again for a review" : "requested a review"}${name ? ` from ${name}` : ""}`;
      break;
    }
    case "review_request_removed":
      icon = <X />;
      text = "removed a review request";
      break;
    case "retitled":
      icon = <Pencil />;
      text = (
        <>
          changed the title from <del>{String(d.from)}</del> to {String(d.to)}
        </>
      );
      break;
    case "base_changed":
      icon = <GitBranch />;
      text = (
        <>
          changed the base from <span className="font-mono">{String(d.from)}</span> to <span className="font-mono">{String(d.to)}</span>
        </>
      );
      break;
    case "branch_deleted":
      icon = <Trash2 />;
      text = (
        <>
          deleted the <span className="font-mono">{String(d.branch)}</span> branch{d.after_merge ? "" : `, which closed this ${noun}`}
        </>
      );
      break;
    case "approvals_dismissed":
      icon = <RefreshCw />;
      text = "New commits reset the approvals";
      break;
  }
  return (
    <div className="flex items-center gap-2 py-1 pl-1 type-label text-fg-muted">
      <span className="flex size-6 shrink-0 items-center justify-center rounded-pill bg-sunken [&_svg]:size-3.5">{icon}</span>
      <span className="min-w-0">
        {e.kind !== "approvals_dismissed" ? <span className="font-semibold text-fg">{who} </span> : null}
        {text} <span className="type-caption">{reposRelativeTime(e.created_at)}</span>
      </span>
    </div>
  );
}

/** A line thread: the comment, replies, a reply box and resolve. */
export function ThreadCard({ repo, pull, thread: t, showPath }: { repo: ReposRepo; pull: ReposPull; thread: ReposThread; showPath?: boolean }) {
  const [replying, setReplying] = useState(false);
  const [body, setBody] = useState("");
  const add = useAddReposComment(repo.id, pull.number);
  const resolve = useResolveReposThread(repo.id, pull.number);
  const resolved = !!t.root.resolved_at;
  const [open, setOpen] = useState(!resolved);
  return (
    <div className="flex flex-col gap-3 rounded-lg border border-border bg-surface p-3">
      <div className="flex flex-wrap items-center gap-2">
        {showPath ? (
          <Link href={repoHref(repo.id, `pulls/${pull.number}`, { tab: "files" })} className="min-w-0 truncate font-mono type-caption text-fg-muted hover:underline">
            {t.root.path}:{t.root.start_line && t.root.start_line < (t.root.line ?? 0) ? `${t.root.start_line}–` : ""}
            {t.root.line}
          </Link>
        ) : null}
        {t.outdated ? <Badge tone="warning">outdated</Badge> : null}
        {t.root.pending ? <Badge tone="warning">Pending</Badge> : null}
        {resolved ? <Badge tone="success" icon={<Check />}>Resolved</Badge> : null}
        <span className="ml-auto flex gap-1">
          {resolved ? (
            <Button size="sm" variant="ghost" onClick={() => setOpen(!open)}>
              {open ? "Hide" : "Show"}
            </Button>
          ) : null}
          {repo.my_level !== "view" && reposSupports(repo, "resolve_threads") ? (
            <Button size="sm" variant="ghost" loading={resolve.isPending} onClick={() => resolve.mutate({ commentId: t.root.id, resolved: !resolved }, { onError: (e) => toast.error(errorMessage(e, "Couldn't change it.")) })}>
              {resolved ? "Unresolve" : "Resolve"}
            </Button>
          ) : null}
        </span>
      </div>
      {open ? (
        <>
          <CommentCard repo={repo} pull={pull} comment={t.root} compact />
          {t.replies.map((r) => (
            <div key={r.id} className="border-t border-border pt-3">
              <CommentCard repo={repo} pull={pull} comment={r} compact />
            </div>
          ))}
          {repo.my_level !== "view" ? (
            replying ? (
              <form
                className="flex flex-col gap-2 border-t border-border pt-3"
                onSubmit={async (e) => {
                  e.preventDefault();
                  try {
                    await add.mutateAsync({ body, reply_to: t.root.id });
                    setBody("");
                    setReplying(false);
                  } catch (err) {
                    toast.error(errorMessage(err, "Couldn't reply."));
                  }
                }}
              >
                <TextArea aria-label="Reply" value={body} onChange={(e) => setBody(e.target.value)} autoFocus minRows={2} />
                <div className="flex justify-end gap-2">
                  <Button type="button" size="sm" variant="ghost" onClick={() => setReplying(false)}>
                    Cancel
                  </Button>
                  <Button type="submit" size="sm" loading={add.isPending} disabled={!body.trim()}>
                    Reply
                  </Button>
                </div>
              </form>
            ) : (
              <Button size="sm" variant="outline" className="self-start" onClick={() => setReplying(true)}>
                Reply
              </Button>
            )
          ) : null}
        </>
      ) : null}
    </div>
  );
}

function Composer({ repo, pull: p }: { repo: ReposRepo; pull: ReposPull }) {
  const [body, setBody] = useState("");
  const add = useAddReposComment(repo.id, p.number);
  const setOpen = useSetReposPullOpen(repo.id, p.number);
  return (
    <Card>
      <form
        className="flex flex-col gap-3"
        onSubmit={async (e) => {
          e.preventDefault();
          try {
            await add.mutateAsync({ body });
            setBody("");
          } catch (err) {
            toast.error(errorMessage(err, "Couldn't comment."));
          }
        }}
      >
        <Field label="Add a comment" info="Markdown works here.">
          <TextArea value={body} onChange={(e) => setBody(e.target.value)} minRows={3} />
        </Field>
        <div className="flex flex-wrap justify-end gap-2">
          {p.can_edit && p.state !== "merged" ? (
            <Button
              type="button"
              variant="outline"
              loading={setOpen.isPending}
              leftIcon={p.state === "open" ? <GitPullRequestClosed /> : <CircleDot />}
              onClick={() => setOpen.mutate(p.state !== "open", { onError: (e) => toast.error(errorMessage(e, "Couldn't change it.")) })}
            >
              {p.state === "open" ? `Close ${reposPullNoun(repo).toLowerCase()}` : "Reopen"}
            </Button>
          ) : null}
          <Button type="submit" loading={add.isPending} disabled={!body.trim()}>
            Comment
          </Button>
        </div>
      </form>
    </Card>
  );
}

function ClosedBox({ pull: p }: { pull: ReposPull }) {
  if (p.state === "merged") {
    return (
      <Banner tone="success" icon={<GitMerge />}>
        {p.merged_by?.display_name || "Someone"} merged this {p.merged_at ? reposRelativeTime(p.merged_at) : ""}
        {p.merge_commit_sha ? ` as ${reposShortSha(p.merge_commit_sha)}` : ""}.
      </Banner>
    );
  }
  return (
    <Banner tone="neutral" icon={<GitPullRequestClosed />}>
      {p.closed_by?.display_name || "Someone"} closed this {p.closed_at ? reposRelativeTime(p.closed_at) : ""}.
    </Banner>
  );
}

function MergeBox({ repo, pull: p }: { repo: ReposRepo; pull: ReposPull }) {
  const m = useReposMergeability(repo.id, p.number);
  const [method, setMethod] = useState<ReposMergeMethod>("merge");
  const [confirm, setConfirm] = useState(false);
  const setDraft = useSetReposPullDraft(repo.id, p.number);
  const autoMerge = useReposAutoMerge(repo.id, p.number);
  const updateBranch = useUpdateReposPullBranch(repo.id, p.number);
  if (m.isPending) return <Skeleton className="h-28 w-full" />;
  if (m.isError) {
    return (
      <Card>
        <ErrorState compact title="Couldn't check whether this can merge" onRetry={() => void m.refetch()} />
      </Card>
    );
  }
  const d = m.data;
  const methods = d.methods;
  const chosen = methods.includes(method) ? method : (methods[0] ?? "merge");
  const tone = d.state === "clean" ? "border-success" : d.state === "conflicts" ? "border-warning" : "border-border";
  const rebaseBlocked = chosen === "rebase" && !!d.rebase_blocked;
  if (d.state === "draft") {
    return (
      <Card className="flex flex-col gap-3 border-2 border-border">
        <p className="flex items-center gap-2 type-body font-semibold text-fg">
          <GitPullRequestDraft className="size-5 text-fg-muted" aria-hidden />
          This {reposPullNoun(repo).toLowerCase()} is still a draft
        </p>
        <p className="pl-7 type-label text-fg-muted">It can't merge, and code owners aren't asked to review, until it's ready.</p>
        {d.checks ? <StatusChecks checks={d.checks} /> : null}
        {p.can_edit ? (
          <Button
            variant="outline"
            className="self-start"
            loading={setDraft.isPending}
            onClick={() => setDraft.mutate(false, { onSuccess: () => toast.success("Ready for review"), onError: (e) => toast.error(errorMessage(e, "Couldn't change it.")) })}
          >
            Ready for review
          </Button>
        ) : null}
      </Card>
    );
  }
  return (
    <Card className={cn("flex flex-col gap-4 border-2", tone)}>
      <MergeStatus repo={repo} pull={p} m={d} />
      {d.checks ? <StatusChecks checks={d.checks} /> : reposSupports(repo, "checks") ? <ChecksList repoId={repo.id} sha={p.head_sha} /> : null}
      {d.behind ? (
        <div className="flex flex-wrap items-center gap-3 border-t border-border pt-3">
          <p className="min-w-0 flex-1 type-label text-fg-muted">
            {p.head_branch} is {d.behind} {d.behind === 1 ? "commit" : "commits"} behind {p.base_branch}.
          </p>
          {d.can_update_branch ? (
            <Button
              size="sm"
              variant="outline"
              loading={updateBranch.isPending}
              onClick={() => updateBranch.mutate(d.head_sha, { onSuccess: () => toast.success(`Merged ${p.base_branch} into ${p.head_branch}`), onError: (e) => toast.error(errorMessage(e, "Couldn't update the branch.")) })}
            >
              Update branch
            </Button>
          ) : null}
        </div>
      ) : null}
      {p.auto_merge ? (
        <Banner tone="info" icon={<Zap />}>
          <span className="flex flex-wrap items-center gap-2">
            Auto-merge is on: {p.auto_merge.enabled_by?.display_name ?? "Someone"} turned it on to {METHOD_LABEL[p.auto_merge.method].toLowerCase()} once every requirement passes.
            {p.can_edit ? (
              <Button size="sm" variant="ghost" loading={autoMerge.isPending} onClick={() => autoMerge.mutate(null, { onError: (e) => toast.error(errorMessage(e, "Couldn't turn it off.")) })}>
                Turn off
              </Button>
            ) : null}
          </span>
        </Banner>
      ) : null}
      {d.state === "clean" || d.state === "blocked" ? (
        <div className="flex flex-wrap items-center gap-3">
          {methods.length > 1 ? (
            <SegmentedControl<ReposMergeMethod>
              aria-label="Merge method"
              fullWidth={false}
              value={chosen}
              onValueChange={setMethod}
              options={methods.map((x) => ({ value: x, label: METHOD_LABEL[x] }))}
            />
          ) : null}
          <Button variant="success" leftIcon={<GitMerge />} disabled={!d.can_merge || rebaseBlocked} onClick={() => setConfirm(true)}>
            {chosen === "squash" ? "Squash and merge" : chosen === "rebase" ? "Rebase and merge" : reposPullNoun(repo) === "Merge request" ? "Merge" : "Merge pull request"}
          </Button>
          {d.can_auto_merge && !p.auto_merge && !rebaseBlocked ? (
            <Tooltip content="Merges by itself, as you, once approvals, code owners and required checks pass.">
              <Button
                variant="outline"
                leftIcon={<Zap />}
                loading={autoMerge.isPending}
                onClick={() => autoMerge.mutate(chosen, { onSuccess: () => toast.success("Auto-merge is on"), onError: (e) => toast.error(errorMessage(e, "Couldn't turn on auto-merge.")) })}
              >
                Merge when ready
              </Button>
            </Tooltip>
          ) : null}
          {!repo.can_push ? <span className="type-caption text-fg-muted">People who can edit the repository merge.</span> : null}
          {rebaseBlocked ? <p className="w-full type-caption text-fg-muted">{d.rebase_blocked}</p> : null}
        </div>
      ) : null}
      <MergeDialog open={confirm} onOpenChange={setConfirm} repo={repo} pull={p} m={d} method={chosen} />
    </Card>
  );
}

function MergeStatus({ repo, pull: p, m }: { repo: ReposRepo; pull: ReposPull; m: ReposMergeability }) {
  const [showSteps, setShowSteps] = useState(false);
  if (m.state === "conflicts") {
    return (
      <div className="flex flex-col gap-3">
        <p className="flex items-center gap-2 type-body font-semibold text-fg">
          <AlertTriangle className="size-5 text-warning-strong" aria-hidden />
          This branch has conflicts with {p.base_branch}
        </p>
        {m.conflicts.length ? (
          <ul className="flex flex-col gap-1 pl-7">
            {m.conflicts.map((c) => (
              <li key={c} className="font-mono type-label text-fg">
                {c}
              </li>
            ))}
          </ul>
        ) : null}
        <Button variant="outline" size="sm" className="self-start" onClick={() => setShowSteps(!showSteps)}>
          {showSteps ? "Hide steps" : "Resolve on your computer"}
        </Button>
        {showSteps ? (
          <div className="flex flex-col gap-2">
            <pre className="overflow-x-auto rounded-md bg-sunken p-3 font-mono type-label text-fg">{reposResolveSteps(repo.clone_url, p.base_branch, p.head_branch)}</pre>
            <Button variant="ghost" size="sm" className="self-start" onClick={() => void copyText(reposResolveSteps(repo.clone_url, p.base_branch, p.head_branch), "Commands copied")}>
              Copy commands
            </Button>
          </div>
        ) : null}
        {m.blockers.map((b) => (
          <p key={b} className="type-label text-fg-muted">
            {b}
          </p>
        ))}
      </div>
    );
  }
  const ok = m.state === "clean";
  return (
    <div className="flex flex-col gap-2">
      <p className="flex items-center gap-2 type-body font-semibold text-fg">
        {ok ? <Check className="size-5 text-success-strong" aria-hidden /> : <AlertTriangle className="size-5 text-warning-strong" aria-hidden />}
        {ok ? "No conflicts with the base branch" : m.state === "up_to_date" ? "Nothing to merge" : m.state === "missing_branch" ? "A branch is missing" : "Merging is blocked"}
      </p>
      {m.required_approvals > 0 ? (
        <p className="pl-7 type-label text-fg-muted">
          {m.approvals} of {m.required_approvals} required {m.required_approvals === 1 ? "approval" : "approvals"}
        </p>
      ) : null}
      {m.blockers.map((b) => (
        <p key={b} className="pl-7 type-label text-fg-muted">
          {b}
        </p>
      ))}
    </div>
  );
}

const METHOD_LABEL: Record<ReposMergeMethod, string> = { merge: "Merge commit", squash: "Squash", rebase: "Rebase" };

const CHECK_ICON: Record<ReposCheck["status"], ReactNode> = {
  success: <Check className="size-4 text-success-strong" aria-label="Passed" />,
  failure: <CircleX className="size-4 text-danger-strong" aria-label="Failed" />,
  in_progress: <Clock className="size-4 text-warning-strong" aria-label="Running" />,
  queued: <Clock className="size-4 text-fg-subtle" aria-label="Queued" />,
  neutral: <CircleDot className="size-4 text-fg-subtle" aria-label="Neutral" />,
  cancelled: <X className="size-4 text-fg-subtle" aria-label="Cancelled" />,
  skipped: <CircleDot className="size-4 text-fg-subtle" aria-label="Skipped" />,
};

/** CI status checks / pipeline jobs on a commit, read-only, linking to the platform. */
export function ChecksList({ repoId, sha }: { repoId: string; sha: string }) {
  const checks = useReposChecks(repoId, sha);
  if (checks.isPending) return <Skeleton className="h-10 w-full" />;
  if (checks.isError || checks.data.items.length === 0) return null;
  const d = checks.data;
  const failed = d.items.filter((c) => c.status === "failure" && !c.allow_failure).length;
  const running = d.items.filter((c) => c.status === "queued" || c.status === "in_progress").length;
  const summary = failed ? `${failed} failing ${failed === 1 ? "check" : "checks"}` : running ? `${running} ${running === 1 ? "check" : "checks"} running` : "All checks passed";
  return (
    <section aria-label="Checks" className="flex flex-col gap-2 border-t border-border pt-3">
      <p className="flex items-center gap-2 type-label font-semibold text-fg">
        {d.state === "failure" ? CHECK_ICON.failure : d.state === "pending" ? CHECK_ICON.in_progress : CHECK_ICON.success}
        {summary}
      </p>
      <ul className="flex flex-col gap-1 pl-6">
        {d.items.map((c) => (
          <li key={c.name} className="flex min-h-8 items-center gap-2 type-label">
            {CHECK_ICON[c.status]}
            <span className="min-w-0 truncate font-medium text-fg">{c.name}</span>
            {c.allow_failure && c.status === "failure" ? <Badge tone="neutral">allowed to fail</Badge> : null}
            {c.description ? <span className="hidden min-w-0 truncate text-fg-muted sm:inline">{c.description}</span> : null}
            {c.url ? (
              <a href={c.url} target="_blank" rel="noopener noreferrer" className="ml-auto shrink-0 text-primary-strong hover:underline">
                Details
              </a>
            ) : null}
          </li>
        ))}
      </ul>
    </section>
  );
}

/** The app's own statuses on the head, with required ones marked (from mergeability). */
function StatusChecks({ checks: d }: { checks: ReposChecks }) {
  if (d.items.length === 0) return null;
  const failed = d.items.filter((c) => c.status === "failure").length;
  const running = d.items.filter((c) => c.status === "queued" || c.status === "in_progress").length;
  const summary = failed ? `${failed} failing ${failed === 1 ? "check" : "checks"}` : running ? `${running} ${running === 1 ? "check" : "checks"} waiting` : "All checks passed";
  return (
    <section aria-label="Checks" className="flex flex-col gap-2 border-t border-border pt-3">
      <p className="flex items-center gap-2 type-label font-semibold text-fg">
        {d.state === "failure" ? CHECK_ICON.failure : d.state === "pending" ? CHECK_ICON.in_progress : CHECK_ICON.success}
        {summary}
      </p>
      <ul className="flex flex-col gap-1 pl-6">
        {d.items.map((c) => (
          <li key={c.name} className="flex min-h-8 items-center gap-2 type-label">
            {CHECK_ICON[c.status]}
            <span className="min-w-0 truncate font-medium text-fg">{c.name}</span>
            {c.required ? <Badge tone="neutral">Required</Badge> : null}
            {c.description ? <span className="hidden min-w-0 truncate text-fg-muted sm:inline">{c.description}</span> : null}
            {c.url ? (
              <a href={c.url} target="_blank" rel="noopener noreferrer" className="ml-auto shrink-0 text-primary-strong hover:underline">
                Details
              </a>
            ) : null}
          </li>
        ))}
      </ul>
    </section>
  );
}

function MergeDialog({ open, onOpenChange, repo, pull: p, m, method }: { open: boolean; onOpenChange: (o: boolean) => void; repo: ReposRepo; pull: ReposPull; m: ReposMergeability; method: ReposMergeMethod }) {
  const merge = useMergeReposPull(repo.id, p.number);
  const [title, setTitle] = useState("");
  const [del, setDel] = useState(m.delete_branch);
  const placeholder = method === "squash" ? `${p.title} (#${p.number})` : method === "rebase" ? "Each commit keeps its own message" : `Merge pull request #${p.number} from ${p.head_branch}`;
  return (
    <ConfirmDialog
      open={open}
      onOpenChange={onOpenChange}
      title={method === "squash" ? `Squash ${p.head_branch} into ${p.base_branch}?` : method === "rebase" ? `Rebase ${p.head_branch} onto ${p.base_branch}?` : `Merge ${p.head_branch} into ${p.base_branch}?`}
      tone="success"
      confirmLabel={method === "squash" ? "Squash and merge" : method === "rebase" ? "Rebase and merge" : "Merge"}
      onConfirm={async () => {
        await merge.mutateAsync({ method, expected_head: m.head_sha, delete_branch: del && m.can_delete_branch, ...(title.trim() ? { title: title.trim() } : {}) });
        toast.success(`Merged #${p.number}`);
      }}
      errorText={(e) => (hasErrorCode(e, "head_moved") ? "The branch has new commits since you looked. Close this and review them first." : errorMessage(e, "Couldn't merge."))}
    >
      <div className="flex flex-col gap-4">
        {method !== "rebase" ? (
          <Field label="Commit message">
            <Input value={title} onChange={(e) => setTitle(e.target.value)} placeholder={placeholder} />
          </Field>
        ) : (
          <p className="type-label text-fg-muted">Each commit lands on {p.base_branch} with its own message and author; you are the committer.</p>
        )}
        {m.can_delete_branch ? <Checkbox checked={del} onCheckedChange={setDel} label={`Delete ${p.head_branch} after merging`} /> : null}
      </div>
    </ConfirmDialog>
  );
}

// ---- sidebar -------------------------------------------------------------------

const REVIEWER_ICON: Record<ReposReviewer["state"], ReactNode> = {
  approved: <Check className="size-4 text-success-strong" aria-label="Approved" />,
  changes_requested: <AlertTriangle className="size-4 text-warning-strong" aria-label="Requested changes" />,
  commented: <MessageSquare className="size-4 text-fg-muted" aria-label="Commented" />,
  "": <CircleDot className="size-4 text-fg-subtle" aria-label="Waiting" />,
};

function Sidebar({ repo, pull: p }: { repo: ReposRepo; pull: ReposPull }) {
  const [asking, setAsking] = useState(false);
  const [picked, setPicked] = useState<ReposPerson[]>([]);
  const request = useRequestReposReviewers(repo.id, p.number);
  const remove = useRemoveReposReviewer(repo.id, p.number);
  const me = useMe().data;
  const checkout = `git fetch origin ${p.head_branch}\ngit switch ${p.head_branch}`;
  const own = !reposPlatformName(repo);
  return (
    <aside className="flex flex-col gap-6">
      {own ? <TriageSidebar repo={repo} pull={p} /> : null}
      <section className="flex flex-col gap-3">
        <div className="flex items-center justify-between">
          <h3 className="type-label font-semibold text-fg">Reviewers</h3>
          {p.can_edit && p.state === "open" && reposSupports(repo, "reviewers") ? <IconButton label="Ask for reviews" icon={<UserPlus />} variant="ghost" size="sm" tooltip onClick={() => (setPicked([]), setAsking(true))} /> : null}
        </div>
        {p.reviewers.length === 0 ? (
          <p className="type-label text-fg-muted">No reviews yet.</p>
        ) : (
          <ul className="flex flex-col gap-2">
            {p.reviewers.map((r) => (
              <li key={r.user.id} className="flex items-center gap-2 type-label">
                <Person user={r.user} />
                <span className="ml-auto flex items-center gap-1">
                  {r.stale ? <Badge tone="neutral">outdated</Badge> : null}
                  <Tooltip content={r.state === "" ? "Asked to review" : r.state.replace("_", " ")}>
                    <span>{REVIEWER_ICON[r.state]}</span>
                  </Tooltip>
                  {r.requested && (p.can_edit || r.user.id === me?.id) && p.state === "open" && reposSupports(repo, "reviewers") ? (
                    <IconButton label={`Remove ${r.user.display_name}`} icon={<X />} variant="ghost" size="sm" onClick={() => remove.mutate(r.user.id)} />
                  ) : null}
                </span>
              </li>
            ))}
          </ul>
        )}
      </section>
      <section className="flex flex-col gap-2">
        <h3 className="type-label font-semibold text-fg">Check out locally</h3>
        <pre tabIndex={0} aria-label="Commands to check out this branch" className="overflow-x-auto whitespace-pre rounded-md bg-sunken p-3 font-mono type-caption text-fg">{checkout}</pre>
        <Button variant="ghost" size="sm" className="self-start" onClick={() => void copyText(checkout, "Commands copied")}>
          Copy
        </Button>
      </section>
      <Dialog
        open={asking}
        onOpenChange={setAsking}
        title="Ask for reviews"
        footer={
          <>
            <Button variant="ghost" onClick={() => setAsking(false)}>
              Cancel
            </Button>
            <Button
              loading={request.isPending}
              disabled={picked.length === 0}
              onClick={async () => {
                try {
                  await request.mutateAsync(picked.map((x) => x.id));
                  setAsking(false);
                } catch (e) {
                  toast.error(errorMessage(e, "Couldn't ask them."));
                }
              }}
            >
              Ask {picked.length || ""}
            </Button>
          </>
        }
      >
        <ReviewerPicker repoId={repo.id} value={picked} onChange={setPicked} exclude={[...(p.author ? [p.author.id] : []), ...p.reviewers.filter((r) => r.requested).map((r) => r.user.id)]} />
      </Dialog>
    </aside>
  );
}

/** Assignees, labels, milestone, linked issues and draft, for the app's own repositories. */
function TriageSidebar({ repo, pull: p }: { repo: ReposRepo; pull: ReposPull }) {
  const [labelsOpen, setLabelsOpen] = useState(false);
  const [assigning, setAssigning] = useState(false);
  const [picked, setPicked] = useState<ReposPerson[]>([]);
  const setLabels = useSetReposPullLabels(repo.id, p.number);
  const setAssignees = useSetReposPullAssignees(repo.id, p.number);
  const update = useUpdateReposPull(repo.id, p.number);
  const setDraft = useSetReposPullDraft(repo.id, p.number);
  const triage = !!p.can_triage;
  return (
    <div className="flex flex-col gap-4">
      <SideSection
        title="Assignees"
        action={
          triage ? (
            <GearButton
              label="Assign people"
              onClick={() => {
                setPicked((p.assignees ?? []).map((a) => ({ id: a.id, display_name: a.display_name, avatar_url: a.avatar_url, level: "view" })));
                setAssigning(true);
              }}
            />
          ) : undefined
        }
      >
        <PeopleList people={p.assignees ?? []} empty="No one yet." />
      </SideSection>
      <SideSection title="Labels" action={triage ? <GearButton label="Change labels" onClick={() => setLabelsOpen(true)} /> : undefined}>
        {p.labels?.length ? <LabelList labels={p.labels} /> : <p className="type-label text-fg-muted">None yet.</p>}
      </SideSection>
      <SideSection
        title="Milestone"
        action={
          triage ? (
            <MilestoneMenu
              repoId={repo.id}
              value={p.milestone?.id ?? null}
              onChange={(id) => update.mutate({ milestone_id: id ?? "" }, { onError: (e) => toast.error(errorMessage(e, "Couldn't change it.")) })}
              trigger={<IconButton label="Choose a milestone" icon={<MilestoneIcon />} variant="ghost" size="sm" />}
            />
          ) : undefined
        }
      >
        {p.milestone ? <span className="type-label font-semibold text-fg">{p.milestone.title}</span> : <p className="type-label text-fg-muted">No milestone.</p>}
      </SideSection>
      <SideSection title="Closes">
        {(p.closing_issues ?? []).length === 0 ? (
          <p className="type-label text-fg-muted">Write “Fixes #12” in the description to close an issue when this merges into {repo.default_branch}.</p>
        ) : (
          <ul className="flex flex-col gap-2">
            {p.closing_issues!.map((is) => (
              <li key={is.number} className="flex items-start gap-2 type-label">
                <IssueStateIcon state={is.state} className="mt-0.5 size-4 shrink-0" />
                <Link href={repoHref(repo.id, `issues/${is.number}`)} className="min-w-0 flex-1 text-fg hover:underline">
                  {is.title} <span className="text-fg-muted">#{is.number}</span>
                </Link>
              </li>
            ))}
          </ul>
        )}
      </SideSection>
      {p.can_edit && p.state === "open" && !p.draft ? (
        <Button size="sm" variant="ghost" className="self-start" leftIcon={<GitPullRequestDraft />} loading={setDraft.isPending} onClick={() => setDraft.mutate(true, { onError: (e) => toast.error(errorMessage(e, "Couldn't change it.")) })}>
          Convert to draft
        </Button>
      ) : null}
      <LabelPickerDialog repoId={repo.id} open={labelsOpen} onOpenChange={setLabelsOpen} value={(p.labels ?? []).map((l) => l.id)} onSave={(ids) => setLabels.mutateAsync(ids)} saving={setLabels.isPending} />
      <Dialog
        open={assigning}
        onOpenChange={setAssigning}
        title="Assignees"
        footer={
          <>
            <Button variant="ghost" onClick={() => setAssigning(false)}>
              Cancel
            </Button>
            <Button
              loading={setAssignees.isPending}
              onClick={async () => {
                try {
                  await setAssignees.mutateAsync(picked.map((x) => x.id));
                  setAssigning(false);
                } catch (e) {
                  toast.error(errorMessage(e, "Couldn't assign them."));
                }
              }}
            >
              Save
            </Button>
          </>
        }
      >
        <PeoplePicker repoId={repo.id} value={picked} onChange={setPicked} title="People who can open the repository" />
      </Dialog>
    </div>
  );
}

// ---- commits and files ---------------------------------------------------------

function PullCommits({ repo, commits }: { repo: ReposRepo; pull: ReposPull; commits: ReturnType<typeof useReposPullCommits> }) {
  if (commits.isPending) return <Skeleton className="h-40 w-full" />;
  if (commits.isError) {
    return (
      <Card>
        <ErrorState info={errorMessage(commits.error, "Try again.")} onRetry={() => void commits.refetch()} />
      </Card>
    );
  }
  if (commits.data.items.length === 0) {
    return (
      <Card>
        <EmptyState icon={<GitCommitHorizontal />} title="No commits" />
      </Card>
    );
  }
  return (
    <Card padding="none" className="divide-y divide-border">
      {commits.data.items.map((c) => (
        <div key={c.sha} className="flex min-h-row items-center gap-3 px-4 py-2 type-label">
          <GitCommitHorizontal className="size-4 shrink-0 text-fg-muted" aria-hidden />
          <Link href={repoHref(repo.id, `commits/${c.sha}`)} className="min-w-0 flex-1 truncate font-semibold text-fg hover:underline">
            {c.subject}
          </Link>
          <span className="hidden text-fg-muted sm:inline">{c.author.name}</span>
          <span className="type-caption text-fg-muted">{reposRelativeTime(c.committer.at)}</span>
          <span className="font-mono type-caption text-fg-muted">{reposShortSha(c.sha)}</span>
        </div>
      ))}
    </Card>
  );
}

function PullFiles({
  repo,
  pull: p,
  diff,
  ignoreWhitespace,
  onIgnoreWhitespace,
}: {
  repo: ReposRepo;
  pull: ReposPull;
  diff: ReturnType<typeof useReposPullDiff>;
  ignoreWhitespace: boolean;
  onIgnoreWhitespace: (on: boolean) => void;
}) {
  const [mode, setMode] = useDiffMode();
  const threads = useReposThreads(repo.id, p.number);
  const add = useAddReposComment(repo.id, p.number);
  const loadFile = useReposDiffFileLoader(repo.id, { pull: p.number });
  const [reviewing, setReviewing] = useState(false);
  const canComment = repo.my_level !== "view";
  const own = !reposPlatformName(repo);
  const viewed = useReposViewedFiles(repo.id, p.number, own);
  const setViewed = useSetReposViewed(repo.id, p.number);
  const pending = useReposPendingReview(repo.id, p.number, own && canComment);
  if (diff.isPending) return <DiffSkeleton />;
  if (diff.isError) {
    return (
      <Card>
        <ErrorState info={errorMessage(diff.error, "Try again.")} onRetry={() => void diff.refetch()} />
      </Card>
    );
  }
  const d = diff.data.diff;
  return (
    <div className="flex flex-col gap-4">
      <div className="sticky top-14 z-10 flex flex-wrap items-center gap-3 border-b border-border bg-canvas py-2 lg:top-16">
        <span className="type-label text-fg-muted">
          {d.files.length} {d.files.length === 1 ? "file" : "files"}, <span className="text-success-strong">+{d.additions}</span> <span className="text-danger-strong">−{d.deletions}</span>
        </span>
        {own && viewed.data ? (
          <span className="type-caption tabular-nums text-fg-muted">
            {d.files.filter((f) => viewed.data.items.includes(f.path)).length} of {d.files.length} viewed
          </span>
        ) : null}
        <div className="ml-auto flex flex-wrap items-center gap-2">
          {own ? <Checkbox checked={ignoreWhitespace} onCheckedChange={onIgnoreWhitespace} label={<span className="type-label">Hide whitespace</span>} /> : null}
          <DiffModeToggle mode={mode} onChange={setMode} />
          {canComment && p.state === "open" ? (
            <Button size="sm" onClick={() => setReviewing(true)}>
              {pending.data?.comments ? `Finish review (${pending.data.comments})` : "Review changes"}
            </Button>
          ) : null}
        </div>
      </div>
      <DiffView
        files={d.files}
        mode={mode}
        repoId={repo.id}
        headRef={p.head_branch}
        filesTruncated={d.files_truncated}
        loadFile={loadFile}
        {...(own
          ? {
              viewed: { paths: viewed.data?.items ?? [], onToggle: (path: string, on: boolean) => setViewed.mutate({ path, viewed: on }) },
              images: { base: diff.data.base, head: diff.data.head },
            }
          : {})}
        comments={{
          threads: threads.data?.items ?? [],
          canComment: canComment,
          reviews: own && p.state === "open",
          suggestions: own && p.state === "open",
          onComment: async (c) => {
            try {
              await add.mutateAsync(c);
            } catch (e) {
              toast.error(errorMessage(e, "Couldn't comment."));
              throw e;
            }
          },
          renderThread: (t) => <ThreadCard repo={repo} pull={p} thread={t} />,
        }}
      />
      {(threads.data?.items ?? []).some((t) => t.outdated) ? (
        <section className="flex flex-col gap-3">
          <h3 className="type-title-sm text-fg">On earlier versions</h3>
          {(threads.data?.items ?? [])
            .filter((t) => t.outdated)
            .map((t) => (
              <ThreadCard key={t.root.id} repo={repo} pull={p} thread={t} showPath />
            ))}
        </section>
      ) : null}
      <ReviewDialog open={reviewing} onOpenChange={setReviewing} repo={repo} pull={p} />
    </div>
  );
}

function ReviewDialog({ open, onOpenChange, repo, pull: p }: { open: boolean; onOpenChange: (o: boolean) => void; repo: ReposRepo; pull: ReposPull }) {
  const me = useMe().data;
  const submit = useSubmitReposReview(repo.id, p.number);
  const own = p.is_author ?? (!!me && p.author?.id === me.id);
  const canDecide = repo.can_push && !own;
  const canRequestChanges = reposSupports(repo, "request_changes");
  const [state, setState] = useState<"comment" | "approve" | "request_changes">("comment");
  const [body, setBody] = useState("");
  const appRepo = !reposPlatformName(repo);
  const pending = useReposPendingReview(repo.id, p.number, appRepo && open);
  const discard = useDiscardReposPendingReview(repo.id, p.number);
  const held = pending.data?.comments ?? 0;
  return (
    <Dialog
      open={open}
      onOpenChange={onOpenChange}
      title="Review changes"
      info={!canDecide ? (own ? `You can't approve your own ${reposPullNoun(repo).toLowerCase()}.` : "Approving and requesting changes needs edit access.") : undefined}
      footer={
        <>
          {held ? (
            <Button
              variant="ghost"
              className="sm:mr-auto"
              loading={discard.isPending}
              onClick={() => discard.mutate(undefined, { onSuccess: () => (toast.success("Pending comments discarded"), onOpenChange(false)), onError: (e) => toast.error(errorMessage(e, "Couldn't discard them.")) })}
            >
              Discard {held} pending
            </Button>
          ) : null}
          <Button variant="ghost" onClick={() => onOpenChange(false)}>
            Cancel
          </Button>
          <Button
            loading={submit.isPending}
            disabled={state !== "approve" && !body.trim() && held === 0}
            onClick={async () => {
              try {
                await submit.mutateAsync({ state, body });
                toast.success(state === "approve" ? "Approved" : state === "request_changes" ? "Changes requested" : "Review sent");
                setBody("");
                onOpenChange(false);
              } catch (e) {
                toast.error(errorMessage(e, "Couldn't send the review."));
              }
            }}
          >
            Send review
          </Button>
        </>
      }
    >
      <div className="flex flex-col gap-4">
        {held ? (
          <p className="type-label text-fg-muted">
            {held} pending {held === 1 ? "comment goes" : "comments go"} out with this review.
          </p>
        ) : null}
        <Field label="Summary">
          <TextArea value={body} onChange={(e) => setBody(e.target.value)} minRows={4} autoFocus placeholder={state === "approve" ? "Optional" : "What you found"} />
        </Field>
        <SegmentedControl<"comment" | "approve" | "request_changes">
          aria-label="Decision"
          value={state}
          onValueChange={setState}
          options={[
            { value: "comment", label: "Comment" },
            { value: "approve", label: "Approve", disabled: !canDecide },
            ...(canRequestChanges ? [{ value: "request_changes" as const, label: "Request changes", disabled: !canDecide }] : []),
          ]}
        />
      </div>
    </Dialog>
  );
}
