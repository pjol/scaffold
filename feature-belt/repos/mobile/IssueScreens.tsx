import {
  errorMessage,
  reposRelativeTime,
  reposShortSha,
  reposTopic,
  useAddReposIssueComment,
  useCreateReposIssue,
  useRepo,
  useReposIssue,
  useReposIssues,
  useReposIssueTimeline,
  useReposReaction,
  useReposTemplates,
  useTopic,
  useUpdateReposIssue,
  type ReposIssue,
  type ReposIssueEvent,
  type ReposIssueTimelineItem,
  type ReposMention,
  type ReposRepo,
  type ReposTemplate,
} from "@scaffoldapp/shared";
import {
  Avatar,
  Badge,
  Button,
  Card,
  Chip,
  EmptyState,
  ErrorState,
  Field,
  IconButton,
  Input,
  ListGroup,
  ListRow,
  Screen,
  SegmentedControl,
  Skeleton,
  SkeletonRow,
  Text,
  TextArea,
  toast,
  useTheme,
} from "@scaffoldapp/ui-native";
import { Stack, router, useLocalSearchParams } from "expo-router";
import { CircleCheck, CircleDot, CircleSlash, GitPullRequest, Lock, MessageSquare, Milestone, Pin, Plus, Search, Tag, Users } from "lucide-react-native";
import { useState } from "react";
import { Pressable, RefreshControl, View } from "react-native";
import { LabelBadge, Reactions, withMentions } from "./comments";
import { PersonLine, RepoMarkdown, repoPath } from "./parts";

type IssueState = "open" | "closed";

function IssueStateIcon({ issue, size = 20 }: { issue: Pick<ReposIssue, "state" | "state_reason">; size?: number }) {
  const theme = useTheme();
  if (issue.state === "open") return <CircleDot size={size} color={theme.colors.successStrong} accessibilityLabel="Open" />;
  if (issue.state_reason === "not_planned") return <CircleSlash size={size} color={theme.colors.textMuted} accessibilityLabel="Closed as not planned" />;
  return <CircleCheck size={size} color={theme.colors.accentStrong} accessibilityLabel="Closed as completed" />;
}

function stateBadge(issue: Pick<ReposIssue, "state" | "state_reason">) {
  if (issue.state === "open") return <Badge tone="success">Open</Badge>;
  return issue.state_reason === "not_planned" ? <Badge tone="neutral">Not planned</Badge> : <Badge tone="accent">Closed</Badge>;
}

/** One issue in a list: state, title with number, labels, assignees and comments. */
function IssueRow({ repoId, issue: i, pinned }: { repoId: string; issue: ReposIssue; pinned?: boolean }) {
  const theme = useTheme();
  return (
    <Pressable
      onPress={() => router.push(repoPath(repoId, `issues/${i.number}`) as never)}
      accessibilityRole="button"
      accessibilityLabel={`${i.title}, #${i.number}, ${i.state === "open" ? "open" : i.state_reason === "not_planned" ? "closed as not planned" : "closed"}`}
      style={({ pressed }) => ({
        flexDirection: "row",
        gap: theme.space[3],
        paddingVertical: theme.space[3],
        paddingHorizontal: theme.space[4],
        minHeight: theme.tap.lg,
        backgroundColor: pressed ? theme.colors.sunken : undefined,
      })}
    >
      <View style={{ paddingTop: theme.space[1] }}>{pinned ? <Pin size={20} color={theme.colors.textMuted} accessibilityLabel="Pinned" /> : <IssueStateIcon issue={i} />}</View>
      <View style={{ flex: 1, gap: theme.space[2] }}>
        <Text role="body" weight={600}>
          {i.title}{" "}
          <Text role="body" tone="muted">
            #{i.number}
          </Text>
        </Text>
        {i.labels.length || i.assignees.length || i.comment_count || i.milestone || i.linked_pulls ? (
          <View style={{ flexDirection: "row", flexWrap: "wrap", alignItems: "center", gap: theme.space[2] }}>
            {i.labels.map((l) => (
              <LabelBadge key={l.id} label={l} />
            ))}
            {i.milestone ? (
              <View style={{ flexDirection: "row", alignItems: "center", gap: theme.space[1] }}>
                <Milestone size={14} color={theme.colors.textMuted} />
                <Text role="caption" tone="muted">
                  {i.milestone.title}
                </Text>
              </View>
            ) : null}
            {i.linked_pulls ? (
              <View style={{ flexDirection: "row", alignItems: "center", gap: theme.space[1] }} accessibilityLabel={`${i.linked_pulls} linked pull requests`}>
                <GitPullRequest size={14} color={theme.colors.textMuted} />
                <Text role="caption" tone="muted" tabular>
                  {i.linked_pulls}
                </Text>
              </View>
            ) : null}
            {i.comment_count ? (
              <View style={{ flexDirection: "row", alignItems: "center", gap: theme.space[1] }} accessibilityLabel={`${i.comment_count} comments`}>
                <MessageSquare size={14} color={theme.colors.textMuted} />
                <Text role="caption" tone="muted" tabular>
                  {i.comment_count}
                </Text>
              </View>
            ) : null}
            {i.assignees.slice(0, 3).map((a) => (
              <Avatar key={a.id} name={a.display_name} src={a.avatar_url} size="xs" />
            ))}
          </View>
        ) : null}
      </View>
    </Pressable>
  );
}

/** A repository's issues: open or closed, the search syntax (label:bug assignee:@me …), pinned ones first. */
export function IssuesScreen() {
  const theme = useTheme();
  const { repoID } = useLocalSearchParams<{ repoID: string }>();
  const repo = useRepo(repoID);
  useTopic(repo.data ? reposTopic(repo.data.id) : null);
  const [state, setState] = useState<IssueState>("open");
  const [q, setQ] = useState("");
  const issues = useReposIssues(repoID, `is:${state} ${q}`);
  const pinned = useReposIssues(repoID, "is:pinned is:open");
  const rows = issues.data?.items ?? [];
  const pinnedRows = !q && state === "open" ? (pinned.data?.items ?? []) : [];
  const pinnedIds = new Set(pinnedRows.map((p) => p.id));
  const counts = issues.meta?.counts;
  const canOpen = !!repo.data && repo.data.my_level !== "view";
  return (
    <Screen edges={["bottom"]} refreshControl={<RefreshControl refreshing={issues.isRefetching} onRefresh={() => void issues.refetch()} />}>
      <Stack.Screen
        options={{
          title: "Issues",
          headerRight: canOpen ? () => <IconButton label="New issue" icon={Plus} onPress={() => router.push(repoPath(repoID, "issues/new") as never)} /> : undefined,
        }}
      />
      <Input
        value={q}
        onChangeText={setQ}
        placeholder="label:bug assignee:@me milestone:…"
        leadingIcon={Search}
        autoCapitalize="none"
        autoCorrect={false}
        accessibilityLabel="Search issues"
      />
      <SegmentedControl<IssueState>
        accessibilityLabel="State"
        value={state}
        onValueChange={setState}
        options={[
          { value: "open", label: counts ? `Open ${counts.open}` : "Open" },
          { value: "closed", label: counts ? `Closed ${counts.closed}` : "Closed" },
        ]}
      />
      {issues.meta?.ignored.length ? (
        <Text role="caption" tone="muted">
          Not understood: {issues.meta.ignored.join(", ")}
        </Text>
      ) : null}
      {pinnedRows.length ? (
        <ListGroup>
          {pinnedRows.map((i) => (
            <IssueRow key={i.id} repoId={repoID} issue={i} pinned />
          ))}
        </ListGroup>
      ) : null}
      <ListGroup>
        {issues.isPending ? (
          <View accessibilityLabel="Loading" accessibilityRole="progressbar">
            <SkeletonRow />
            <SkeletonRow />
            <SkeletonRow />
          </View>
        ) : issues.isError ? (
          <ErrorState compact title="Couldn't load issues" info={errorMessage(issues.error)} onRetry={() => void issues.refetch()} retrying={issues.isRefetching} />
        ) : rows.length === 0 ? (
          <EmptyState
            compact
            icon={CircleDot}
            title={q ? "No issue matches" : state === "open" ? "No open issues" : "No closed issues"}
            action={
              canOpen && !q && state === "open" ? (
                <Button size="md" variant="secondary" leftIcon={Plus} onPress={() => router.push(repoPath(repoID, "issues/new") as never)}>
                  New issue
                </Button>
              ) : undefined
            }
          />
        ) : (
          rows.filter((i) => !pinnedIds.has(i.id)).map((i) => <IssueRow key={i.id} repoId={repoID} issue={i} />)
        )}
        {issues.hasNextPage ? (
          <Button variant="ghost" loading={issues.isFetchingNextPage} onPress={() => void issues.fetchNextPage()}>
            Load more
          </Button>
        ) : null}
      </ListGroup>
      <View style={{ height: theme.space[2] }} />
    </Screen>
  );
}

/** One issue: description, labels, assignees, milestone, linked pull requests, the conversation and the composer. */
export function IssueScreen() {
  const theme = useTheme();
  const { repoID, number } = useLocalSearchParams<{ repoID: string; number: string }>();
  const n = Number(number) || 0;
  const repo = useRepo(repoID);
  const issue = useReposIssue(repoID, n);
  useTopic(repo.data ? reposTopic(repo.data.id) : null);
  if (issue.isPending || repo.isPending) {
    return (
      <Screen edges={["bottom"]}>
        <Stack.Screen options={{ title: `#${n}` }} />
        <View accessibilityLabel="Loading" accessibilityRole="progressbar" style={{ gap: theme.space[4] }}>
          <Skeleton height={64} />
          <Skeleton height={160} />
          <Skeleton height={120} />
        </View>
      </Screen>
    );
  }
  if (issue.isError || repo.isError) {
    const err = issue.error ?? repo.error;
    const isPull = err?.code === "is_pull_request";
    return (
      <Screen edges={["bottom"]}>
        <Stack.Screen options={{ title: `#${n}` }} />
        {isPull ? (
          <EmptyState
            icon={GitPullRequest}
            title={`#${n} is a pull request`}
            action={
              <Button size="md" variant="secondary" onPress={() => router.replace(repoPath(repoID, `pulls/${n}`) as never)}>
                Open the pull request
              </Button>
            }
          />
        ) : (
          <ErrorState title={err?.status === 404 ? "Issue not found" : "Couldn't load this issue"} info={errorMessage(err)} onRetry={() => void issue.refetch()} />
        )}
      </Screen>
    );
  }
  return <IssueDetail repo={repo.data} issue={issue.data} refreshing={issue.isRefetching} onRefresh={() => void issue.refetch()} />;
}

function IssueDetail({ repo, issue: i, refreshing, onRefresh }: { repo: ReposRepo; issue: ReposIssue; refreshing: boolean; onRefresh: () => void }) {
  const theme = useTheme();
  const timeline = useReposIssueTimeline(repo.id, i.number);
  const react = useReposReaction(repo.id);
  const canReact = repo.my_level !== "view";
  const mentions = [...(i.mentions ?? []), ...(timeline.data?.mentions ?? [])];
  return (
    <Screen edges={["bottom"]} refreshControl={<RefreshControl refreshing={refreshing} onRefresh={onRefresh} />}>
      <Stack.Screen options={{ title: `#${i.number}` }} />
      <View style={{ gap: theme.space[2] }}>
        <Text role="title" selectable>
          {i.title}
        </Text>
        <View style={{ flexDirection: "row", alignItems: "center", gap: theme.space[2], flexWrap: "wrap" }}>
          {stateBadge(i)}
          {i.locked ? (
            <Badge tone="warning" icon={Lock}>
              Locked
            </Badge>
          ) : null}
          {i.pinned ? (
            <Badge tone="neutral" icon={Pin}>
              Pinned
            </Badge>
          ) : null}
        </View>
      </View>
      <Card>
        <View style={{ gap: theme.space[3] }}>
          <PersonLine user={i.author} trailing={<Text role="caption" tone="muted">opened {reposRelativeTime(i.created_at)}</Text>} />
          {i.body ? <RepoMarkdown source={withMentions(i.body, mentions)} repoId={repo.id} dir="" /> : <Text tone="muted">No description.</Text>}
          <Reactions
            items={i.reactions}
            onToggle={canReact ? (emoji, add) => react.mutate({ type: "issue", subjectId: i.id, emoji, add, number: i.number }, { onError: (e) => toast.error(errorMessage(e, "Couldn't react.")) }) : undefined}
          />
        </View>
      </Card>
      <IssueMeta repo={repo} issue={i} />
      <View style={{ gap: theme.space[4] }}>
        {timeline.isPending ? (
          <Skeleton height={120} />
        ) : timeline.isError ? (
          <ErrorState compact title="Couldn't load the conversation" info={errorMessage(timeline.error)} onRetry={() => void timeline.refetch()} />
        ) : timeline.data.items.length === 0 ? null : (
          timeline.data.items.map((item) => <TimelineEntry key={item.type === "comment" ? item.comment.id : item.event.id} repo={repo} issue={i} item={item} mentions={mentions} />)
        )}
      </View>
      <Composer repo={repo} issue={i} />
    </Screen>
  );
}

function IssueMeta({ repo, issue: i }: { repo: ReposRepo; issue: ReposIssue }) {
  const theme = useTheme();
  return (
    <ListGroup>
      <ListRow
        title="Labels"
        leading={<Tag size={20} color={theme.colors.textMuted} />}
        chevron={false}
        trailing={
          i.labels.length ? (
            <View style={{ flexDirection: "row", flexWrap: "wrap", justifyContent: "flex-end", gap: theme.space[1], maxWidth: 220 }}>
              {i.labels.map((l) => (
                <LabelBadge key={l.id} label={l} />
              ))}
            </View>
          ) : (
            <Text role="label" tone="muted">
              None
            </Text>
          )
        }
      />
      <ListRow
        title="Assignees"
        leading={<Users size={20} color={theme.colors.textMuted} />}
        chevron={false}
        trailing={
          <Text role="label" tone={i.assignees.length ? "default" : "muted"} numberOfLines={2} style={{ maxWidth: 200, textAlign: "right" }}>
            {i.assignees.length ? i.assignees.map((a) => a.display_name).join(", ") : "No one"}
          </Text>
        }
      />
      <ListRow
        title="Milestone"
        leading={<Milestone size={20} color={theme.colors.textMuted} />}
        chevron={false}
        trailing={
          <Text role="label" tone={i.milestone ? "default" : "muted"} numberOfLines={2} style={{ maxWidth: 200, textAlign: "right" }}>
            {i.milestone ? `${i.milestone.title}${i.milestone.due_on ? ` · due ${i.milestone.due_on}` : ""}` : "None"}
          </Text>
        }
      />
      {(i.pulls ?? []).map((p) => (
        <ListRow
          key={p.number}
          title={`${p.title} #${p.number}`}
          leading={<GitPullRequest size={20} color={p.state === "merged" ? theme.colors.accentStrong : p.state === "closed" ? theme.colors.dangerStrong : theme.colors.successStrong} />}
          trailing={p.closes ? <Badge tone="neutral">Closes</Badge> : undefined}
          onPress={() => router.push(repoPath(repo.id, `pulls/${p.number}`) as never)}
        />
      ))}
    </ListGroup>
  );
}

function nameOf(id: unknown, mentions: ReposMention[]): string {
  return mentions.find((m) => m.id === id)?.display_name ?? "someone";
}

/** An event as one line ("added the bug label"); `to` opens what it points at. */
function eventText(ev: ReposIssueEvent, mentions: ReposMention[]): { text: string; to?: string } {
  const d = ev.data;
  switch (ev.kind) {
    case "closed":
      if (typeof d["pull"] === "number") return { text: `closed this in #${d["pull"]}`, to: `pulls/${d["pull"]}` };
      if (typeof d["commit"] === "string") return { text: `closed this in commit ${reposShortSha(d["commit"])}`, to: `commits/${d["commit"]}` };
      return { text: d["reason"] === "not_planned" ? "closed this as not planned" : "closed this as completed" };
    case "reopened":
      return { text: "reopened this" };
    case "renamed":
      return { text: `changed the title from “${String(d["from"] ?? "")}” to “${String(d["to"] ?? "")}”` };
    case "labeled":
      return { text: `added the ${String(d["name"] ?? "")} label` };
    case "unlabeled":
      return { text: `removed the ${String(d["name"] ?? "")} label` };
    case "assigned":
      return { text: d["user_id"] === ev.actor?.id ? "self-assigned this" : `assigned ${nameOf(d["user_id"], mentions)}` };
    case "unassigned":
      return { text: `unassigned ${nameOf(d["user_id"], mentions)}` };
    case "milestoned":
      return { text: `added this to ${String(d["title"] ?? "a milestone")}` };
    case "demilestoned":
      return { text: `removed this from ${String(d["title"] ?? "a milestone")}` };
    case "locked":
      return { text: "locked the conversation" };
    case "unlocked":
      return { text: "opened the conversation again" };
    case "pinned":
      return { text: "pinned this issue" };
    case "unpinned":
      return { text: "unpinned this issue" };
    case "referenced":
      if (d["source"] === "commit" && typeof d["sha"] === "string") return { text: `referenced this in commit ${reposShortSha(d["sha"])}`, to: `commits/${d["sha"]}` };
      if (typeof d["number"] === "number") {
        const where = d["source"] === "pull" ? "pulls" : "issues";
        return { text: `${d["closes"] ? "linked a pull request that closes this:" : "mentioned this in"} #${d["number"]} ${String(d["title"] ?? "")}`.trim(), to: `${where}/${d["number"]}` };
      }
      return { text: "referenced this" };
  }
}

function TimelineEntry({ repo, issue, item, mentions }: { repo: ReposRepo; issue: ReposIssue; item: ReposIssueTimelineItem; mentions: ReposMention[] }) {
  const theme = useTheme();
  const react = useReposReaction(repo.id);
  if (item.type === "comment") {
    const c = item.comment;
    return (
      <Card>
        <View style={{ gap: theme.space[2] }}>
          <PersonLine user={c.author} trailing={<Text role="caption" tone="muted">{reposRelativeTime(c.created_at)}{c.edited_at ? " · edited" : ""}</Text>} />
          {c.deleted ? <Text tone="muted">This comment was deleted.</Text> : <RepoMarkdown source={withMentions(c.body, mentions)} repoId={repo.id} dir="" />}
          {!c.deleted ? (
            <Reactions
              items={c.reactions}
              onToggle={
                repo.my_level !== "view"
                  ? (emoji, add) => react.mutate({ type: "issue_comment", subjectId: c.id, emoji, add, number: issue.number }, { onError: (e) => toast.error(errorMessage(e, "Couldn't react.")) })
                  : undefined
              }
            />
          ) : null}
        </View>
      </Card>
    );
  }
  const { text, to } = eventText(item.event, mentions);
  const line = `${item.event.actor?.display_name || "Someone"} ${text} · ${reposRelativeTime(item.event.created_at)}`;
  return to ? (
    <Text role="caption" tone="muted" accessibilityRole="link" onPress={() => router.push(repoPath(repo.id, to) as never)}>
      {line}
    </Text>
  ) : (
    <Text role="caption" tone="muted">
      {line}
    </Text>
  );
}

function Composer({ repo, issue: i }: { repo: ReposRepo; issue: ReposIssue }) {
  const theme = useTheme();
  const add = useAddReposIssueComment(repo.id, i.number);
  const update = useUpdateReposIssue(repo.id, i.number);
  const [body, setBody] = useState("");
  const setState = (state: IssueState, reason?: "completed" | "not_planned") =>
    update.mutate(
      { state, ...(reason ? { state_reason: reason } : {}) },
      {
        onSuccess: () => toast.success(state === "open" ? "Reopened" : reason === "not_planned" ? "Closed as not planned" : "Closed"),
        onError: (e) => toast.error(errorMessage(e, "Couldn't change the issue.")),
      },
    );
  return (
    <View style={{ gap: theme.space[3] }}>
      {i.can_comment ? (
        <View style={{ gap: theme.space[2] }}>
          <Field label="Add a comment" info={i.locked ? "The conversation is locked; you can still comment because you can edit the repository." : undefined}>
            <TextArea value={body} onChangeText={setBody} placeholder="Markdown works here; #12 links an issue" />
          </Field>
          <Button
            disabled={!body.trim()}
            loading={add.isPending}
            onPress={() => add.mutate(body, { onSuccess: () => setBody(""), onError: (e) => toast.error(errorMessage(e, "Couldn't comment.")) })}
          >
            Comment
          </Button>
        </View>
      ) : i.locked ? (
        <View style={{ flexDirection: "row", alignItems: "center", gap: theme.space[2] }}>
          <Lock size={16} color={theme.colors.textMuted} />
          <Text role="label" tone="muted" style={{ flex: 1 }}>
            This conversation is locked. People who can edit the repository can still comment.
          </Text>
        </View>
      ) : null}
      {i.can_edit ? (
        i.state === "open" ? (
          <View style={{ flexDirection: "row", flexWrap: "wrap", gap: theme.space[2] }}>
            <Button size="md" variant="secondary" leftIcon={CircleCheck} loading={update.isPending && update.variables?.state_reason !== "not_planned"} onPress={() => setState("closed", "completed")}>
              Close as completed
            </Button>
            <Button size="md" variant="ghost" leftIcon={CircleSlash} loading={update.isPending && update.variables?.state_reason === "not_planned"} onPress={() => setState("closed", "not_planned")}>
              Close as not planned
            </Button>
          </View>
        ) : (
          <Button size="md" variant="secondary" leftIcon={CircleDot} loading={update.isPending} onPress={() => setState("open")}>
            Reopen
          </Button>
        )
      ) : null}
    </View>
  );
}

/** Open an issue: a template (from the repository's .github/ISSUE_TEMPLATE) or blank, a title and a description. */
export function NewIssueScreen() {
  const theme = useTheme();
  const { repoID } = useLocalSearchParams<{ repoID: string }>();
  const templates = useReposTemplates(repoID, "issue");
  const create = useCreateReposIssue(repoID);
  const [template, setTemplate] = useState<ReposTemplate | null>(null);
  const [title, setTitle] = useState("");
  const [body, setBody] = useState("");
  const pick = (t: ReposTemplate | null) => {
    setTemplate(t);
    setTitle(t?.title ?? "");
    setBody(t?.body ?? "");
  };
  const submit = () =>
    create.mutate(
      { title, body, ...(template ? { template: template.file } : {}) },
      {
        onSuccess: (is) => {
          toast.success(`Opened #${is.number}`);
          router.replace(repoPath(repoID, `issues/${is.number}`) as never);
        },
        onError: (e) => toast.error(errorMessage(e, "Couldn't open the issue.")),
      },
    );
  const list = templates.data?.items ?? [];
  return (
    <Screen
      edges={["bottom"]}
      keyboard
      footer={
        <Button fullWidth disabled={!title.trim()} loading={create.isPending} onPress={submit}>
          Open issue
        </Button>
      }
    >
      <Stack.Screen options={{ title: "New issue" }} />
      {templates.isPending ? (
        <Skeleton height={48} />
      ) : list.length ? (
        <Field label="Template" info={template?.about || "Templates come from .github/ISSUE_TEMPLATE on the default branch."}>
          <View style={{ flexDirection: "row", flexWrap: "wrap", gap: theme.space[2] }}>
            <Chip size="sm" selected={!template} onPress={() => pick(null)}>
              Blank
            </Chip>
            {list.map((t) => (
              <Chip key={t.file} size="sm" selected={template?.file === t.file} onPress={() => pick(t)}>
                {t.name}
              </Chip>
            ))}
          </View>
        </Field>
      ) : null}
      {template?.labels.length ? (
        <View style={{ flexDirection: "row", flexWrap: "wrap", alignItems: "center", gap: theme.space[2] }}>
          <Text role="caption" tone="muted">
            Adds
          </Text>
          {template.labels.map((l) => (
            <Badge key={l} tone="neutral">
              {l}
            </Badge>
          ))}
        </View>
      ) : null}
      <Field label="Title" required>
        <Input value={title} onChangeText={setTitle} placeholder="What's wrong or what's needed" autoFocus={!list.length} returnKeyType="next" maxLength={256} />
      </Field>
      <Field label="Description">
        <TextArea value={body} onChangeText={setBody} placeholder="Markdown works here" style={{ minHeight: 200 }} />
      </Field>
    </Screen>
  );
}
