import {
  errorMessage,
  reposRelativeTime,
  reposTopic,
  useAddReposComment,
  useMe,
  useRepo,
  useReposMergeability,
  useReposPendingReview,
  useReposPull,
  useReposPullDiff,
  useReposPulls,
  useReposThreads,
  useReposReaction,
  useReposTimeline,
  useResolveReposThread,
  reposSupports,
  useSubmitReposReview,
  useTopic,
  type ReposEvent,
  type ReposPull,
  type ReposPullFilter,
  type ReposRepo,
  type ReposThread,
  type ReposTimelineItem,
} from "@scaffoldapp/shared";
import { Badge, Button, Card, EmptyState, ErrorState, Field, ListGroup, ListRow, Screen, SegmentedControl, Sheet, Skeleton, SkeletonRow, SourceBadge, Text, TextArea, toast, useTheme } from "@scaffoldapp/ui-native";
import { Stack, router, useLocalSearchParams } from "expo-router";
import { AlertTriangle, Check, GitMerge, GitPullRequest, GitPullRequestClosed, GitPullRequestDraft } from "lucide-react-native";
import { useMemo, useState } from "react";
import { RefreshControl, View } from "react-native";
import { CommentBody, LabelBadge, Reactions, withMentions } from "./comments";
import { DiffFileCard, MONO, PersonLine, RepoMarkdown, repoPath } from "./parts";

function stateIcon(state: ReposPull["state"], colors: ReturnType<typeof useTheme>["colors"], draft?: boolean) {
  if (state === "merged") return <GitMerge size={20} color={colors.accentStrong} />;
  if (state === "closed") return <GitPullRequestClosed size={20} color={colors.dangerStrong} />;
  if (draft) return <GitPullRequestDraft size={20} color={colors.textMuted} accessibilityLabel="Draft" />;
  return <GitPullRequest size={20} color={colors.successStrong} />;
}

const CHECKS: Record<"success" | "failure" | "pending", { tone: "success" | "danger" | "warning"; label: string }> = {
  success: { tone: "success", label: "Checks passed" },
  failure: { tone: "danger", label: "Checks failed" },
  pending: { tone: "warning", label: "Checks running" },
};

/** A repository's pull requests. */
export function PullsScreen() {
  const theme = useTheme();
  const { repoID } = useLocalSearchParams<{ repoID: string }>();
  const [state, setState] = useState<ReposPullFilter>("open");
  const pulls = useReposPulls(repoID, { state });
  const rows = pulls.data?.items ?? [];
  return (
    <Screen edges={["bottom"]} refreshControl={<RefreshControl refreshing={pulls.isRefetching} onRefresh={() => void pulls.refetch()} />}>
      <Stack.Screen options={{ title: "Pull requests" }} />
      <SegmentedControl<ReposPullFilter> accessibilityLabel="State" value={state} onValueChange={setState} options={[{ value: "open", label: "Open" }, { value: "closed", label: "Closed" }, { value: "all", label: "All" }]} />
      <ListGroup>
        {pulls.isPending ? (
          <View accessibilityLabel="Loading" accessibilityRole="progressbar">
            <SkeletonRow />
            <SkeletonRow />
          </View>
        ) : pulls.isError ? (
          <ErrorState compact title="Couldn't load pull requests" info={errorMessage(pulls.error)} onRetry={() => void pulls.refetch()} />
        ) : rows.length === 0 ? (
          <EmptyState compact icon={GitPullRequest} title={state === "open" ? "No open pull requests" : "No pull requests"} />
        ) : (
          rows.map((p) => (
            <ListRow
              key={p.id}
              title={`${p.title} #${p.number}`}
              leading={stateIcon(p.state, theme.colors, p.draft)}
              trailing={
                p.draft && p.state === "open" ? (
                  <Badge tone="neutral">Draft</Badge>
                ) : p.approvals > 0 ? (
                  <Badge tone="success">{p.approvals === 1 ? "1 approval" : `${p.approvals} approvals`}</Badge>
                ) : undefined
              }
              onPress={() => router.push(repoPath(repoID, `pulls/${p.number}`) as never)}
            />
          ))
        )}
        {pulls.hasNextPage ? (
          <Button variant="ghost" loading={pulls.isFetchingNextPage} onPress={() => void pulls.fetchNextPage()}>
            Load more
          </Button>
        ) : null}
      </ListGroup>
    </Screen>
  );
}

type PullTab = "conversation" | "files";

/** A pull request: conversation with comments and reviews, the changed files with threads, and review. */
export function PullScreen() {
  const theme = useTheme();
  const { repoID, number } = useLocalSearchParams<{ repoID: string; number: string }>();
  const n = Number(number) || 0;
  const repo = useRepo(repoID);
  const pull = useReposPull(repoID, n);
  useTopic(repo.data ? reposTopic(repo.data.id) : null);
  const [tab, setTab] = useState<PullTab>("conversation");
  const [reviewing, setReviewing] = useState(false);
  if (pull.isPending || repo.isPending) {
    return (
      <Screen edges={["bottom"]}>
        <Stack.Screen options={{ title: `#${n}` }} />
        <Skeleton height={64} />
        <Skeleton height={200} />
      </Screen>
    );
  }
  if (pull.isError || repo.isError) {
    return (
      <Screen edges={["bottom"]}>
        <Stack.Screen options={{ title: `#${n}` }} />
        <ErrorState title="Couldn't load this pull request" info={errorMessage(pull.error ?? repo.error)} onRetry={() => void pull.refetch()} />
      </Screen>
    );
  }
  const p = pull.data;
  const r = repo.data;
  const canComment = r.my_level !== "view";
  return (
    <Screen
      edges={["bottom"]}
      refreshControl={<RefreshControl refreshing={pull.isRefetching} onRefresh={() => void pull.refetch()} />}
      footer={canComment && p.state === "open" ? <Button fullWidth onPress={() => setReviewing(true)}>Review</Button> : undefined}
    >
      <Stack.Screen options={{ title: `#${p.number}` }} />
      <View style={{ gap: theme.space[2] }}>
        <Text role="title" selectable>
          {p.title}
        </Text>
        <View style={{ flexDirection: "row", alignItems: "center", gap: theme.space[2], flexWrap: "wrap" }}>
          <Badge tone={p.state === "merged" ? "accent" : p.state === "closed" ? "danger" : p.draft ? "neutral" : "success"}>
            {p.state === "merged" ? "Merged" : p.state === "closed" ? "Closed" : p.draft ? "Draft" : "Open"}
          </Badge>
          {p.state === "open" && p.checks_state ? <Badge tone={CHECKS[p.checks_state].tone}>{CHECKS[p.checks_state].label}</Badge> : null}
          <Text role="caption" tone="muted" style={{ fontFamily: MONO }}>
            {p.head_branch} → {p.base_branch}
          </Text>
          <SourceBadge source={p.source ?? r.source} />
        </View>
        {p.labels?.length ? (
          <View style={{ flexDirection: "row", alignItems: "center", gap: theme.space[2], flexWrap: "wrap" }}>
            {p.labels.map((l) => (
              <LabelBadge key={l.id} label={l} />
            ))}
          </View>
        ) : null}
      </View>
      <SegmentedControl<PullTab> accessibilityLabel="Pull request" value={tab} onValueChange={setTab} options={[{ value: "conversation", label: "Conversation" }, { value: "files", label: "Files" }]} />
      {tab === "conversation" ? <Conversation repo={r} pull={p} canComment={canComment} /> : <Files repo={r} pull={p} canComment={canComment} />}
      <ReviewSheet repo={r} pull={p} open={reviewing} onClose={() => setReviewing(false)} />
    </Screen>
  );
}

function Conversation({ repo, pull: p, canComment }: { repo: ReposRepo; pull: ReposPull; canComment: boolean }) {
  const theme = useTheme();
  const timeline = useReposTimeline(repo.id, p.number);
  const threads = useReposThreads(repo.id, p.number);
  const merge = useReposMergeability(repo.id, p.number, p.state === "open");
  const add = useAddReposComment(repo.id, p.number);
  const react = useReposReaction(repo.id);
  const [body, setBody] = useState("");
  const byId = useMemo(() => new Map((threads.data?.items ?? []).map((t) => [t.root.id, t])), [threads.data]);
  const own = !repo.source || repo.source.type !== "connected";
  return (
    <View style={{ gap: theme.space[4] }}>
      <Card>
        <View style={{ gap: theme.space[3] }}>
          <PersonLine user={p.author} trailing={<Text role="caption" tone="muted">opened {reposRelativeTime(p.created_at)}</Text>} />
          {p.body ? <RepoMarkdown source={withMentions(p.body, p.mentions)} repoId={repo.id} gitRef={p.head_branch} dir="" /> : <Text tone="muted">No description.</Text>}
          {own ? (
            <Reactions
              items={p.reactions}
              onToggle={canComment ? (emoji, add) => react.mutate({ type: "pull", subjectId: p.id, emoji, add, number: p.number }, { onError: (e) => toast.error(errorMessage(e, "Couldn't react.")) }) : undefined}
            />
          ) : null}
        </View>
      </Card>
      {p.closing_issues?.length ? (
        <ListGroup>
          {p.closing_issues.map((is) => (
            <ListRow
              key={is.number}
              title={`Closes ${is.title} #${is.number}`}
              trailing={<Badge tone={is.state === "open" ? "success" : "accent"}>{is.state === "open" ? "Open" : "Closed"}</Badge>}
              onPress={() => router.push(repoPath(repo.id, `issues/${is.number}`) as never)}
            />
          ))}
        </ListGroup>
      ) : null}
      {timeline.isPending ? (
        <Skeleton height={120} />
      ) : timeline.isError ? (
        <ErrorState compact title="Couldn't load the conversation" onRetry={() => void timeline.refetch()} />
      ) : (
        timeline.data.items.map((item, i) => <Entry key={i} repo={repo} pull={p} item={item} threads={byId} />)
      )}
      {p.state === "open" && merge.data ? (
        <Card>
          <View style={{ gap: theme.space[2] }}>
            <View style={{ flexDirection: "row", gap: theme.space[2], alignItems: "center" }}>
              {merge.data.mergeable ? <Check size={18} color={theme.colors.successStrong} /> : <AlertTriangle size={18} color={theme.colors.warningStrong} />}
              <Text role="label" weight={600}>
                {merge.data.state === "clean" ? "Ready to merge" : merge.data.state === "conflicts" ? "Has conflicts" : "Merging is blocked"}
              </Text>
            </View>
            {merge.data.conflicts.map((c) => (
              <Text key={c} role="caption" style={{ fontFamily: MONO }}>
                {c}
              </Text>
            ))}
            {merge.data.blockers.map((b) => (
              <Text key={b} role="caption" tone="muted">
                {b}
              </Text>
            ))}
            <Text role="caption" tone="muted">
              Merge on the web.
            </Text>
          </View>
        </Card>
      ) : null}
      {canComment ? (
        <View style={{ gap: theme.space[2] }}>
          <Field label="Add a comment">
            <TextArea value={body} onChangeText={setBody} placeholder="Markdown works here" />
          </Field>
          <Button
            disabled={!body.trim()}
            loading={add.isPending}
            onPress={() =>
              add.mutate(
                { body },
                {
                  onSuccess: () => setBody(""),
                  onError: (e) => toast.error(errorMessage(e, "Couldn't comment.")),
                },
              )
            }
          >
            Comment
          </Button>
        </View>
      ) : null}
    </View>
  );
}

const REVIEW_TEXT = { approved: "approved", changes_requested: "requested changes", commented: "reviewed" } as const;

function Entry({ repo, pull, item, threads }: { repo: ReposRepo; pull: ReposPull; item: ReposTimelineItem; threads: Map<string, ReposThread> }) {
  const theme = useTheme();
  switch (item.type) {
    case "comment":
      return (
        <Card>
          <View style={{ gap: theme.space[2] }}>
            <PersonLine user={item.comment.author} trailing={<Text role="caption" tone="muted">{reposRelativeTime(item.comment.created_at)}</Text>} />
            {item.comment.deleted ? <Text tone="muted">This comment was deleted.</Text> : <CommentBody body={item.comment.body} repoId={repo.id} gitRef={pull.head_branch} mentions={pull.mentions} />}
          </View>
        </Card>
      );
    case "review":
      return (
        <View style={{ gap: theme.space[2] }}>
          <PersonLine user={item.review.author} trailing={<Text role="caption" tone="muted">{REVIEW_TEXT[item.review.state]} {reposRelativeTime(item.review.created_at)}</Text>} />
          {item.review.body ? <RepoMarkdown source={withMentions(item.review.body, pull.mentions)} repoId={repo.id} gitRef={pull.head_branch} dir="" /> : null}
          {item.threads?.map((t) => {
            const full = threads.get(t.id);
            return full ? <ThreadView key={t.id} repo={repo} pull={pull} thread={full} /> : null;
          })}
        </View>
      );
    case "thread": {
      const full = threads.get(item.thread.id);
      return full ? <ThreadView repo={repo} pull={pull} thread={full} /> : null;
    }
    case "event":
      return (
        <Text role="caption" tone="muted">
          {(item.event.actor?.display_name || "Someone") + " "}
          {pullEventText(item.event)} · {reposRelativeTime(item.event.created_at)}
        </Text>
      );
  }
}

/** An event as words ("added the bug label"); kinds without a phrase read as their name. */
function pullEventText(ev: ReposEvent): string {
  const d = ev.data;
  switch (ev.kind) {
    case "labeled":
      return `added the ${String(d["name"] ?? "")} label`;
    case "unlabeled":
      return `removed the ${String(d["name"] ?? "")} label`;
    case "milestoned":
      return `added this to ${String(d["title"] ?? "a milestone")}`;
    case "demilestoned":
      return `removed this from ${String(d["title"] ?? "a milestone")}`;
    case "ready_for_review":
      return "marked this ready for review";
    case "converted_to_draft":
      return "turned this back into a draft";
    case "auto_merge_enabled":
      return `turned on auto-merge (${String(d["method"] ?? "merge")})`;
    case "auto_merge_disabled":
      return "turned off auto-merge";
    case "branch_updated":
      return `merged ${String(d["base"] ?? "the base")} into this branch`;
    case "suggestions_applied":
      return typeof d["count"] === "number" && d["count"] > 1 ? `applied ${d["count"]} suggestions` : "applied a suggestion";
    case "referenced":
      return typeof d["number"] === "number" ? `mentioned this in #${d["number"]}` : "referenced this";
    case "merged":
      return d["method"] === "rebase" ? "rebased and merged this" : d["method"] === "squash" ? "squashed and merged this" : "merged this";
    default:
      return ev.kind.replace(/_/g, " ");
  }
}

function ThreadView({ repo, pull, thread: t }: { repo: ReposRepo; pull: ReposPull; thread: ReposThread }) {
  const theme = useTheme();
  const add = useAddReposComment(repo.id, pull.number);
  const resolve = useResolveReposThread(repo.id, pull.number);
  const [reply, setReply] = useState("");
  const resolved = !!t.root.resolved_at;
  return (
    <View style={{ gap: theme.space[2], borderWidth: 1, borderColor: theme.colors.border, borderRadius: theme.radius.md, padding: theme.space[3], backgroundColor: theme.colors.surface }}>
      <View style={{ flexDirection: "row", alignItems: "center", gap: theme.space[2], flexWrap: "wrap" }}>
        <Text role="caption" tone="muted" style={{ fontFamily: MONO, flexShrink: 1 }}>
          {t.root.path}:{t.root.start_line && t.root.start_line !== t.root.line ? `${t.root.start_line}–${t.root.line}` : t.root.line}
        </Text>
        {t.outdated ? <Badge tone="warning">outdated</Badge> : null}
        {resolved ? <Badge tone="success">Resolved</Badge> : null}
      </View>
      {[t.root, ...t.replies].map((c) => (
        <View key={c.id} style={{ gap: theme.space[1] }}>
          <PersonLine
            user={c.author}
            trailing={
              <View style={{ flexDirection: "row", alignItems: "center", gap: theme.space[2] }}>
                {c.pending ? <Badge tone="warning">Pending</Badge> : null}
                <Text role="caption" tone="muted">
                  {reposRelativeTime(c.created_at)}
                </Text>
              </View>
            }
          />
          {c.deleted ? (
            <Text tone="muted">This comment was deleted.</Text>
          ) : (
            <CommentBody body={c.body} repoId={repo.id} gitRef={pull.head_branch} mentions={pull.mentions} applied={!!c.suggestion_applied_sha} />
          )}
        </View>
      ))}
      {repo.my_level !== "view" ? (
        <View style={{ gap: theme.space[2] }}>
          <TextArea value={reply} onChangeText={setReply} placeholder="Reply" accessibilityLabel="Reply" />
          <View style={{ flexDirection: "row", gap: theme.space[2] }}>
            {reposSupports(repo, "resolve_threads") ? (
              <Button size="md" variant="secondary" loading={resolve.isPending} onPress={() => resolve.mutate({ commentId: t.root.id, resolved: !resolved })}>
                {resolved ? "Unresolve" : "Resolve"}
              </Button>
            ) : null}
            <Button
              size="md"
              disabled={!reply.trim()}
              loading={add.isPending}
              onPress={() => add.mutate({ body: reply, reply_to: t.root.id }, { onSuccess: () => setReply(""), onError: (e) => toast.error(errorMessage(e, "Couldn't reply.")) })}
            >
              Reply
            </Button>
          </View>
        </View>
      ) : null}
    </View>
  );
}

function Files({ repo, pull: p, canComment }: { repo: ReposRepo; pull: ReposPull; canComment: boolean }) {
  const theme = useTheme();
  const diff = useReposPullDiff(repo.id, p.number);
  const threads = useReposThreads(repo.id, p.number);
  const add = useAddReposComment(repo.id, p.number);
  const [target, setTarget] = useState<{ path: string; line: number; side: "old" | "new" } | null>(null);
  const [body, setBody] = useState("");
  const [asPending, setAsPending] = useState(false);
  const pendingAllowed = !repo.source || repo.source.type !== "connected";
  const send = (pending: boolean) => {
    if (!target) return;
    setAsPending(pending);
    add.mutate(
      { body, path: target.path, line: target.line, side: target.side, ...(pending ? { pending: true } : {}) },
      {
        onSuccess: () => {
          setBody("");
          setTarget(null);
          if (pending) toast.success("Added to your review");
        },
        onError: (e) => toast.error(errorMessage(e, "Couldn't comment.")),
      },
    );
  };
  if (diff.isPending) return <Skeleton height={240} />;
  if (diff.isError) return <ErrorState compact title="Couldn't load the changes" info={errorMessage(diff.error)} onRetry={() => void diff.refetch()} />;
  return (
    <View style={{ gap: theme.space[3] }}>
      <Text role="caption" tone="muted">
        {diff.data.diff.files.length} {diff.data.diff.files.length === 1 ? "file" : "files"} · +{diff.data.diff.additions} −{diff.data.diff.deletions}
        {canComment ? " · tap a line to comment" : ""}
      </Text>
      {diff.data.diff.files.map((f) => (
        <DiffFileCard
          key={f.path}
          file={f}
          threads={threads.data?.items ?? []}
          renderThread={(t) => <ThreadView repo={repo} pull={p} thread={t} />}
          {...(canComment ? { onLine: (line: number, side: "old" | "new") => setTarget({ path: f.path, line, side }) } : {})}
        />
      ))}
      <Sheet open={!!target} onClose={() => setTarget(null)} title={target ? `Comment on line ${target.line}` : "Comment"} snapPoints={["60%"]}>
        <View style={{ gap: theme.space[3] }}>
          <Text role="caption" tone="muted" style={{ fontFamily: MONO }}>
            {target?.path}
          </Text>
          <TextArea value={body} onChangeText={setBody} placeholder="Leave a comment" accessibilityLabel="Comment" autoFocus />
          {pendingAllowed && p.state === "open" ? (
            <Button fullWidth variant="secondary" disabled={!body.trim()} loading={add.isPending && asPending} onPress={() => send(true)}>
              Add to review
            </Button>
          ) : null}
          <Button fullWidth disabled={!body.trim()} loading={add.isPending && !asPending} onPress={() => send(false)}>
            Comment now
          </Button>
        </View>
      </Sheet>
    </View>
  );
}

function ReviewSheet({ repo, pull: p, open, onClose }: { repo: ReposRepo; pull: ReposPull; open: boolean; onClose: () => void }) {
  const theme = useTheme();
  const me = useMe().data;
  const submit = useSubmitReposReview(repo.id, p.number);
  const pendingAllowed = !repo.source || repo.source.type !== "connected";
  const pending = useReposPendingReview(repo.id, p.number, open && pendingAllowed);
  const pendingCount = pending.data?.comments ?? 0;
  const own = p.is_author ?? (!!me && p.author?.id === me.id);
  const canDecide = repo.can_push && !own;
  const [state, setState] = useState<"comment" | "approve" | "request_changes">("comment");
  const [body, setBody] = useState("");
  return (
    <Sheet open={open} onClose={onClose} title="Review changes" snapPoints={["70%"]} info={canDecide ? undefined : "Approving and requesting changes needs edit access, and isn't for your own pull request."}>
      <View style={{ gap: theme.space[4] }}>
        {pendingCount > 0 ? (
          <Text role="label" tone="muted">
            {pendingCount === 1 ? "Sends your 1 pending comment with the review." : `Sends your ${pendingCount} pending comments with the review.`}
          </Text>
        ) : null}
        <SegmentedControl<"comment" | "approve" | "request_changes">
          accessibilityLabel="Decision"
          value={state}
          onValueChange={setState}
          options={[
            { value: "comment", label: "Comment" },
            { value: "approve", label: "Approve", disabled: !canDecide },
            ...(reposSupports(repo, "request_changes") ? [{ value: "request_changes" as const, label: "Changes", disabled: !canDecide }] : []),
          ]}
        />
        <Field label="Summary">
          <TextArea value={body} onChangeText={setBody} placeholder={state === "approve" ? "Optional" : "What you found"} />
        </Field>
        <Button
          fullWidth
          loading={submit.isPending}
          disabled={state !== "approve" && !body.trim() && pendingCount === 0}
          onPress={() =>
            submit.mutate(
              { state, body },
              {
                onSuccess: () => {
                  toast.success(state === "approve" ? "Approved" : "Review sent");
                  setBody("");
                  onClose();
                },
                onError: (e) => toast.error(errorMessage(e, "Couldn't send the review.")),
              },
            )
          }
        >
          Send review
        </Button>
      </View>
    </Sheet>
  );
}
