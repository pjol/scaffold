import {
  errorMessage,
  reposRelativeTime,
  reposTopic,
  useAddReposComment,
  useMe,
  useRepo,
  useReposMergeability,
  useReposPull,
  useReposPullDiff,
  useReposPulls,
  useReposThreads,
  useReposTimeline,
  useResolveReposThread,
  reposSupports,
  useSubmitReposReview,
  useTopic,
  type ReposPull,
  type ReposPullFilter,
  type ReposRepo,
  type ReposThread,
  type ReposTimelineItem,
} from "@scaffoldapp/shared";
import { Badge, Button, Card, EmptyState, ErrorState, Field, ListGroup, ListRow, Screen, SegmentedControl, Sheet, Skeleton, SkeletonRow, SourceBadge, Text, TextArea, toast, useTheme } from "@scaffoldapp/ui-native";
import { Stack, router, useLocalSearchParams } from "expo-router";
import { AlertTriangle, Check, GitMerge, GitPullRequest, GitPullRequestClosed } from "lucide-react-native";
import { useMemo, useState } from "react";
import { RefreshControl, View } from "react-native";
import { DiffFileCard, MONO, PersonLine, RepoMarkdown, repoPath } from "./parts";

function stateIcon(state: ReposPull["state"], colors: ReturnType<typeof useTheme>["colors"]) {
  if (state === "merged") return <GitMerge size={20} color={colors.accentStrong} />;
  if (state === "closed") return <GitPullRequestClosed size={20} color={colors.dangerStrong} />;
  return <GitPullRequest size={20} color={colors.successStrong} />;
}

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
              leading={stateIcon(p.state, theme.colors)}
              trailing={p.approvals > 0 ? <Badge tone="success">{p.approvals === 1 ? "1 approval" : `${p.approvals} approvals`}</Badge> : undefined}
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
          <Badge tone={p.state === "merged" ? "accent" : p.state === "closed" ? "danger" : "success"}>{p.state === "merged" ? "Merged" : p.state === "closed" ? "Closed" : "Open"}</Badge>
          <Text role="caption" tone="muted" style={{ fontFamily: MONO }}>
            {p.head_branch} → {p.base_branch}
          </Text>
          <SourceBadge source={p.source ?? r.source} />
        </View>
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
  const [body, setBody] = useState("");
  const byId = useMemo(() => new Map((threads.data?.items ?? []).map((t) => [t.root.id, t])), [threads.data]);
  return (
    <View style={{ gap: theme.space[4] }}>
      <Card>
        <View style={{ gap: theme.space[3] }}>
          <PersonLine user={p.author} trailing={<Text role="caption" tone="muted">opened {reposRelativeTime(p.created_at)}</Text>} />
          {p.body ? <RepoMarkdown source={p.body} repoId={repo.id} gitRef={p.head_branch} dir="" /> : <Text tone="muted">No description.</Text>}
        </View>
      </Card>
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
            {item.comment.deleted ? <Text tone="muted">This comment was deleted.</Text> : <RepoMarkdown source={item.comment.body} repoId={repo.id} gitRef={pull.head_branch} dir="" />}
          </View>
        </Card>
      );
    case "review":
      return (
        <View style={{ gap: theme.space[2] }}>
          <PersonLine user={item.review.author} trailing={<Text role="caption" tone="muted">{REVIEW_TEXT[item.review.state]} {reposRelativeTime(item.review.created_at)}</Text>} />
          {item.review.body ? <RepoMarkdown source={item.review.body} repoId={repo.id} gitRef={pull.head_branch} dir="" /> : null}
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
          {item.event.kind.replace(/_/g, " ")} · {reposRelativeTime(item.event.created_at)}
        </Text>
      );
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
          {t.root.path}:{t.root.line}
        </Text>
        {t.outdated ? <Badge tone="warning">outdated</Badge> : null}
        {resolved ? <Badge tone="success">Resolved</Badge> : null}
      </View>
      {[t.root, ...t.replies].map((c) => (
        <View key={c.id} style={{ gap: theme.space[1] }}>
          <PersonLine user={c.author} trailing={<Text role="caption" tone="muted">{reposRelativeTime(c.created_at)}</Text>} />
          <Text selectable>{c.deleted ? "This comment was deleted." : c.body}</Text>
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
          <Button
            fullWidth
            disabled={!body.trim()}
            loading={add.isPending}
            onPress={() =>
              target &&
              add.mutate(
                { body, path: target.path, line: target.line, side: target.side },
                {
                  onSuccess: () => {
                    setBody("");
                    setTarget(null);
                  },
                  onError: (e) => toast.error(errorMessage(e, "Couldn't comment.")),
                },
              )
            }
          >
            Comment
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
  const own = p.is_author ?? (!!me && p.author?.id === me.id);
  const canDecide = repo.can_push && !own;
  const [state, setState] = useState<"comment" | "approve" | "request_changes">("comment");
  const [body, setBody] = useState("");
  return (
    <Sheet open={open} onClose={onClose} title="Review changes" snapPoints={["70%"]} info={canDecide ? undefined : "Approving and requesting changes needs edit access, and isn't for your own pull request."}>
      <View style={{ gap: theme.space[4] }}>
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
          disabled={state !== "approve" && !body.trim()}
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
