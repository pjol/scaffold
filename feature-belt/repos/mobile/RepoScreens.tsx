import {
  errorMessage,
  reposDirname,
  reposFormatBytes,
  reposRelativeTime,
  reposShortSha,
  reposSupports,
  reposTopic,
  useActiveOrg,
  useRepo,
  useReposConnectedList,
  useReposSources,
  useReposBlob,
  useReposCommit,
  useReposCommits,
  useReposList,
  useReposTree,
  useTopic,
  type ReposRepo,
  type ReposSummary,
} from "@scaffoldapp/shared";
import {
  Badge,
  Button,
  Card,
  ConnectedSection,
  ConnectPrompt,
  EmptyState,
  ErrorState,
  Input,
  ListGroup,
  ListRow,
  Screen,
  SegmentedControl,
  Skeleton,
  SkeletonRow,
  SourceBadge,
  Text,
  connectedSectionItems,
  toast,
  useTheme,
} from "@scaffoldapp/ui-native";
import { router, Stack, useLocalSearchParams } from "expo-router";
import { Building2, ChevronDown, Copy, ExternalLink, FileCode, Folder, FolderGit2, GitBranch, GitCommitHorizontal, GitPullRequest, Search } from "lucide-react-native";
import { useState } from "react";
import { Image, Linking, Pressable, RefreshControl, ScrollView, Share as RNShare, View } from "react-native";
import { ResourceMenu } from "@/components/resource/ResourceMenu";
import { CodeLines, DiffFileCard, MONO, RefSheet, RepoMarkdown, repoPath } from "./parts";

/** A repository row: the app's own and connected ones alike, connected ones badged. */
function RepoListRow({ repo: r }: { repo: ReposSummary }) {
  const theme = useTheme();
  const connected = r.source?.type === "connected";
  return (
    <ListRow
      title={connected ? (r.full_name ?? r.name) : r.name}
      leading={<GitBranch size={20} color={theme.colors.textMuted} />}
      trailing={
        <View style={{ flexDirection: "row", alignItems: "center", gap: theme.space[2] }}>
          {r.open_pulls > 0 ? (
            <Badge tone="neutral">{`${r.open_pulls} open`}</Badge>
          ) : (
            <Text role="caption" tone="muted">
              {r.pushed_at ? reposRelativeTime(r.pushed_at) : "empty"}
            </Text>
          )}
          <SourceBadge source={r.source} iconOnly />
        </View>
      }
      onPress={() => router.push(`/repos/${r.id}` as never)}
    />
  );
}

/** Repositories in the active workspace, then each connected GitHub / GitLab account. */
export function ReposListScreen() {
  const theme = useTheme();
  const { org, isPending } = useActiveOrg();
  const [q, setQ] = useState("");
  const repos = useReposList(org?.id, q);
  const sources = useReposSources(org?.id);
  const connected = (sources.data?.sources ?? []).filter((x) => x.type === "connected");
  const rows = repos.data?.items ?? [];
  if (!isPending && !org) {
    return (
      <Screen edges={["bottom"]}>
        <Stack.Screen options={{ title: "Repositories" }} />
        <EmptyState icon={Building2} title="Join or create a workspace first" />
      </Screen>
    );
  }
  return (
    <Screen edges={["bottom"]} refreshControl={<RefreshControl refreshing={repos.isRefetching} onRefresh={() => void repos.refetch()} />}>
      <Stack.Screen options={{ title: "Repositories" }} />
      <Input value={q} onChangeText={setQ} placeholder="Search repositories" leadingIcon={Search} autoCapitalize="none" autoCorrect={false} accessibilityLabel="Search repositories" />
      <ListGroup>
        {repos.isPending || isPending ? (
          <View accessibilityLabel="Loading" accessibilityRole="progressbar">
            <SkeletonRow />
            <SkeletonRow />
          </View>
        ) : repos.isError ? (
          <ErrorState compact title="Couldn't load repositories" info={errorMessage(repos.error)} onRetry={() => void repos.refetch()} retrying={repos.isRefetching} />
        ) : rows.length === 0 ? (
          <EmptyState compact icon={FolderGit2} title={q ? "No repository matches" : "No repositories yet"} info="Create one on the web, in the drive." />
        ) : (
          rows.map((r) => <RepoListRow key={r.id} repo={r} />)
        )}
        {repos.hasNextPage ? (
          <Button variant="ghost" loading={repos.isFetchingNextPage} onPress={() => void repos.fetchNextPage()}>
            Load more
          </Button>
        ) : null}
      </ListGroup>
      <ConnectedSection
        items={connectedSectionItems(sources.data?.sources)}
        loading={sources.isPending && !!org}
        onSelect={(id) => router.push(`/repos/connected/${id}` as never)}
        onFix={(item) => item.fixUrl && router.push(item.fixUrl.slice(1) as never)}
        onAdd={() => router.push("settings/connections?capability=repos.github.read" as never)}
      />
      {connected.length === 0 ? (
        <ConnectPrompt offers={sources.data?.offers} title="Bring in repositories from" onConnect={(o) => router.push(o.connect_url.slice(1) as never)} style={{ marginTop: theme.space[2] }} />
      ) : null}
    </Screen>
  );
}

/** One connected account's repositories, most recently active first (or by name). */
export function ConnectedReposScreen() {
  const theme = useTheme();
  const { connectionID } = useLocalSearchParams<{ connectionID: string }>();
  const { org } = useActiveOrg();
  const sources = useReposSources(org?.id);
  const source = sources.data?.sources.find((x) => x.id === connectionID);
  const [q, setQ] = useState("");
  const [sort, setSort] = useState<"recent" | "name">("recent");
  const list = useReposConnectedList(connectionID, { q, sort, orgId: org?.id });
  const rows = list.data?.items ?? [];
  const title = source ? `${source.label}${source.account ? ` · ${source.account}` : ""}` : "Connected";
  return (
    <Screen edges={["bottom"]} refreshControl={<RefreshControl refreshing={list.isRefetching} onRefresh={() => void list.refetch()} />}>
      <Stack.Screen options={{ title }} />
      <Input value={q} onChangeText={setQ} placeholder="Search repositories" leadingIcon={Search} autoCapitalize="none" autoCorrect={false} accessibilityLabel="Search repositories" />
      <SegmentedControl<"recent" | "name">
        accessibilityLabel="Sort"
        value={sort}
        onValueChange={setSort}
        options={[
          { value: "recent", label: "Recent activity" },
          { value: "name", label: "Name" },
        ]}
      />
      <ListGroup>
        {list.isPending ? (
          <View accessibilityLabel="Loading" accessibilityRole="progressbar">
            <SkeletonRow />
            <SkeletonRow />
            <SkeletonRow />
          </View>
        ) : list.isError ? (
          <ErrorState compact title="Couldn't load repositories" info={errorMessage(list.error)} onRetry={() => void list.refetch()} retrying={list.isRefetching} />
        ) : rows.length === 0 ? (
          <EmptyState compact icon={FolderGit2} title={q ? "No repository matches" : "No repositories on this account"} />
        ) : (
          rows.map((r) => <RepoListRow key={r.id} repo={r} />)
        )}
        {list.hasNextPage ? (
          <Button variant="ghost" loading={list.isFetchingNextPage} onPress={() => void list.fetchNextPage()}>
            Load more
          </Button>
        ) : null}
      </ListGroup>
      <View style={{ height: theme.space[4] }} />
    </Screen>
  );
}

/** The repository's header actions: copy the clone URL and the shared ⋮ menu. */
function RepoMenu({ repo }: { repo: ReposRepo }) {
  const connected = repo.source?.type === "connected" ? repo.source : null;
  return (
    <ResourceMenu
      feature="repos"
      kind={connected ? "repos.external" : "repos.repository"}
      id={connected ? `${connected.connection_id}:${repo.full_name ?? repo.name}` : repo.id}
      title={repo.full_name ?? repo.name}
      orgId={repo.org_id || undefined}
      actions={[
        ...(connected && repo.web_url
          ? [{ id: "repos-open-platform", label: `Open in ${connected.provider === "gitlab" ? "GitLab" : "GitHub"}`, icon: ExternalLink, onSelect: () => void Linking.openURL(repo.web_url!) }]
          : []),
        {
          id: "repos-share-url",
          label: "Share clone URL",
          icon: Copy,
          onSelect: () => {
            RNShare.share({ message: repo.clone_url }).catch(() => toast.error("Couldn't open the share sheet."));
          },
        },
      ]}
    />
  );
}

/** A folder of the repository at a ref, its README, and the way to pull requests and history. */
export function RepoScreen() {
  const theme = useTheme();
  const { repoID, ref, path } = useLocalSearchParams<{ repoID: string; ref?: string; path?: string }>();
  const repo = useRepo(repoID);
  useTopic(repo.data && reposSupports(repo.data, "realtime") ? reposTopic(repo.data.id) : null);
  const gitRef = ref || repo.data?.default_branch || "";
  const dir = path ?? "";
  const tree = useReposTree(repoID, { ref: gitRef, path: dir }, !!repo.data && !repo.data.empty);
  const [picking, setPicking] = useState(false);

  if (repo.isPending) {
    return (
      <Screen edges={["bottom"]}>
        <Stack.Screen options={{ title: "Repository" }} />
        <Skeleton height={48} />
        <Skeleton height={240} />
      </Screen>
    );
  }
  if (repo.isError) {
    return (
      <Screen edges={["bottom"]}>
        <Stack.Screen options={{ title: "Repository" }} />
        <ErrorState title="Couldn't open this repository" info={errorMessage(repo.error)} onRetry={() => void repo.refetch()} />
      </Screen>
    );
  }
  const r = repo.data;
  const parts = dir ? dir.split("/") : [];
  return (
    <Screen edges={["bottom"]} refreshControl={<RefreshControl refreshing={tree.isRefetching} onRefresh={() => void tree.refetch()} />}>
      <Stack.Screen options={{ title: dir ? parts[parts.length - 1]! : r.name, headerRight: () => <RepoMenu repo={r} /> }} />
      {!dir && r.source?.type === "connected" ? <SourceBadge source={r.source} showAccount /> : null}
      {!dir ? (
        <ListGroup>
          <ListRow title={r.source?.type === "connected" && r.source.provider === "gitlab" ? "Merge requests" : "Pull requests"} leading={<GitPullRequest size={20} color={theme.colors.textMuted} />} trailing={r.open_pulls ? <Badge tone="primary">{String(r.open_pulls)}</Badge> : undefined} onPress={() => router.push(repoPath(r.id, "pulls") as never)} />
          <ListRow title="Commits" leading={<GitCommitHorizontal size={20} color={theme.colors.textMuted} />} onPress={() => router.push(repoPath(r.id, "commits", { ref: gitRef }) as never)} />
        </ListGroup>
      ) : null}
      {r.empty ? (
        <EmptyState icon={FolderGit2} title="This repository is empty" info="Push to it from your computer, or add a file on the web." />
      ) : (
        <View style={{ gap: theme.space[3] }}>
          <Pressable
            onPress={() => setPicking(true)}
            accessibilityRole="button"
            accessibilityLabel={`Branch: ${gitRef}. Switch branch or tag`}
            style={{ flexDirection: "row", alignItems: "center", gap: theme.space[2], alignSelf: "flex-start", minHeight: 44, paddingHorizontal: theme.space[3], borderRadius: theme.radius.md, borderWidth: 1, borderColor: theme.colors.borderStrong }}
          >
            <GitBranch size={16} color={theme.colors.text} />
            <Text role="label" style={{ fontFamily: MONO }} numberOfLines={1}>
              {/^[0-9a-f]{40}$/.test(gitRef) ? reposShortSha(gitRef) : gitRef}
            </Text>
            <ChevronDown size={16} color={theme.colors.textMuted} />
          </Pressable>
          {tree.data?.last_commit ? (
            <Text role="caption" tone="muted" numberOfLines={2}>
              {tree.data.last_commit.author.name} · {tree.data.last_commit.subject} · {reposRelativeTime(tree.data.last_commit.committer.at)}
            </Text>
          ) : null}
          <ListGroup>
            {tree.isPending ? (
              <View accessibilityLabel="Loading" accessibilityRole="progressbar">
                <SkeletonRow />
                <SkeletonRow />
                <SkeletonRow />
              </View>
            ) : tree.isError ? (
              <ErrorState compact title="Couldn't load this folder" info={errorMessage(tree.error)} onRetry={() => void tree.refetch()} />
            ) : (
              tree.data.entries.map((e) => (
                <ListRow
                  key={e.path}
                  title={e.name}
                  leading={e.type === "tree" ? <Folder size={20} color={theme.colors.primaryStrong} /> : <FileCode size={20} color={theme.colors.textMuted} />}
                  onPress={() => router.push((e.type === "tree" ? repoPath(r.id, "", { ref: gitRef, path: e.path }) : repoPath(r.id, "blob", { ref: gitRef, path: e.path })) as never)}
                />
              ))
            )}
          </ListGroup>
          {tree.data?.readme?.encoding === "utf-8" ? (
            <Card>
              <View style={{ gap: theme.space[3] }}>
                <Text role="label" weight={600}>
                  {tree.data.readme.name}
                </Text>
                <RepoMarkdown source={tree.data.readme.content} repoId={r.id} gitRef={gitRef} dir={dir} />
              </View>
            </Card>
          ) : null}
        </View>
      )}
      <RefSheet repoId={r.id} value={gitRef} open={picking} onClose={() => setPicking(false)} onPick={(next) => router.setParams({ ref: next })} />
    </Screen>
  );
}

/** One file: highlighted code, rendered Markdown or an image. Editing happens on the web. */
export function FileScreen() {
  const theme = useTheme();
  const { repoID, ref, path } = useLocalSearchParams<{ repoID: string; ref?: string; path: string }>();
  const blob = useReposBlob(repoID, { ref, path: path ?? "" });
  const isMd = /\.(md|markdown)$/i.test(path ?? "");
  const [view, setView] = useState<"preview" | "code">(isMd ? "preview" : "code");
  const name = (path ?? "").split("/").pop() ?? "File";
  return (
    <Screen edges={["bottom"]} padded={false}>
      <Stack.Screen options={{ title: name }} />
      {blob.isPending ? (
        <View style={{ padding: theme.space[4], gap: theme.space[2] }}>
          <Skeleton height={16} />
          <Skeleton height={16} width="70%" />
          <Skeleton height={16} width="85%" />
        </View>
      ) : blob.isError ? (
        <ErrorState title="Couldn't open this file" info={errorMessage(blob.error)} onRetry={() => void blob.refetch()} />
      ) : (
        <View style={{ gap: theme.space[3] }}>
          <View style={{ paddingHorizontal: theme.space[4], paddingTop: theme.space[3], gap: theme.space[3] }}>
            <Text role="caption" tone="muted" style={{ fontFamily: MONO }}>
              {path}
            </Text>
            <Text role="caption" tone="muted">
              {blob.data.encoding === "utf-8" ? `${blob.data.lines} lines · ` : ""}
              {reposFormatBytes(blob.data.size)}
            </Text>
            {isMd && blob.data.encoding === "utf-8" ? (
              <SegmentedControl<"preview" | "code"> accessibilityLabel="View" value={view} onValueChange={setView} options={[{ value: "preview", label: "Preview" }, { value: "code", label: "Code" }]} />
            ) : null}
          </View>
          {blob.data.image && blob.data.encoding === "base64" ? (
            <Image source={{ uri: `data:image/png;base64,${blob.data.content}` }} style={{ width: "100%", aspectRatio: 1 }} resizeMode="contain" accessibilityLabel={name} />
          ) : blob.data.binary || blob.data.too_large ? (
            <EmptyState compact icon={FileCode} title={blob.data.too_large ? "Too large to show on a phone" : "Binary file"} />
          ) : view === "preview" ? (
            <View style={{ paddingHorizontal: theme.space[4] }}>
              <RepoMarkdown source={blob.data.content} repoId={repoID} gitRef={ref} dir={reposDirname(path ?? "")} />
            </View>
          ) : (
            <View style={{ backgroundColor: theme.colors.surface, borderTopWidth: 1, borderBottomWidth: 1, borderColor: theme.colors.border }}>
              <CodeLines code={blob.data.content} language={blob.data.language} />
            </View>
          )}
        </View>
      )}
    </Screen>
  );
}

/** Commit history of a branch. */
export function CommitsScreen() {
  const { repoID, ref, path } = useLocalSearchParams<{ repoID: string; ref?: string; path?: string }>();
  const commits = useReposCommits(repoID, { ref, path });
  const rows = commits.data?.items ?? [];
  return (
    <Screen edges={["bottom"]} refreshControl={<RefreshControl refreshing={commits.isRefetching} onRefresh={() => void commits.refetch()} />}>
      <Stack.Screen options={{ title: "Commits" }} />
      <ListGroup>
        {commits.isPending ? (
          <View accessibilityLabel="Loading" accessibilityRole="progressbar">
            <SkeletonRow />
            <SkeletonRow />
          </View>
        ) : commits.isError ? (
          <ErrorState compact title="Couldn't load commits" info={errorMessage(commits.error)} onRetry={() => void commits.refetch()} />
        ) : rows.length === 0 ? (
          <EmptyState compact icon={GitCommitHorizontal} title="No commits yet" />
        ) : (
          rows.map((c) => (
            <ListRow key={c.sha} title={c.subject} trailing={<Text role="caption" tone="muted">{reposRelativeTime(c.committer.at)}</Text>} onPress={() => router.push(repoPath(repoID, `commits/${c.sha}`) as never)} />
          ))
        )}
        {commits.hasNextPage ? (
          <Button variant="ghost" loading={commits.isFetchingNextPage} onPress={() => void commits.fetchNextPage()}>
            Load more
          </Button>
        ) : null}
      </ListGroup>
    </Screen>
  );
}

/** One commit and its changes. */
export function CommitScreen() {
  const theme = useTheme();
  const { repoID, sha } = useLocalSearchParams<{ repoID: string; sha: string }>();
  const commit = useReposCommit(repoID, sha);
  return (
    <Screen edges={["bottom"]}>
      <Stack.Screen options={{ title: reposShortSha(sha ?? "") }} />
      {commit.isPending ? (
        <Skeleton height={160} />
      ) : commit.isError ? (
        <ErrorState title="Couldn't load this commit" info={errorMessage(commit.error)} onRetry={() => void commit.refetch()} />
      ) : (
        <>
          <View style={{ gap: theme.space[2] }}>
            <Text role="titleSm" selectable>
              {commit.data.subject}
            </Text>
            {commit.data.body ? (
              <Text tone="muted" selectable>
                {commit.data.body}
              </Text>
            ) : null}
            <Text role="caption" tone="muted">
              {commit.data.author.name} · {reposRelativeTime(commit.data.committer.at)} · {commit.data.diff.files.length} {commit.data.diff.files.length === 1 ? "file" : "files"} · +{commit.data.diff.additions} −{commit.data.diff.deletions}
            </Text>
          </View>
          <ScrollView horizontal={false}>
            <View style={{ gap: theme.space[3] }}>
              {commit.data.diff.files.map((f) => (
                <DiffFileCard key={f.path} file={f} />
              ))}
            </View>
          </ScrollView>
        </>
      )}
    </Screen>
  );
}
