import { errorMessage, reposFormatBytes, reposRelativeTime, reposShortSha, useDownloadReposReleaseAsset, useReposReleases, type ReposRelease } from "@scaffoldapp/shared";
import { Badge, Button, Card, EmptyState, ErrorState, ListGroup, ListRow, Screen, Skeleton, Text, toast, useTheme } from "@scaffoldapp/ui-native";
import { Stack, useLocalSearchParams } from "expo-router";
import { Download, Package, Tag } from "lucide-react-native";
import { Linking, RefreshControl, View } from "react-native";
import { withMentions } from "./comments";
import { MONO, PersonLine, RepoMarkdown } from "./parts";

/** A repository's releases, newest first: tag, notes and files to download (making releases is on the web). */
export function ReleasesScreen() {
  const theme = useTheme();
  const { repoID } = useLocalSearchParams<{ repoID: string }>();
  const releases = useReposReleases(repoID);
  const rows = releases.data?.items ?? [];
  return (
    <Screen edges={["bottom"]} refreshControl={<RefreshControl refreshing={releases.isRefetching} onRefresh={() => void releases.refetch()} />}>
      <Stack.Screen options={{ title: "Releases" }} />
      {releases.isPending ? (
        <View accessibilityLabel="Loading" accessibilityRole="progressbar" style={{ gap: theme.space[4] }}>
          <Skeleton height={140} />
          <Skeleton height={140} />
        </View>
      ) : releases.isError ? (
        <ErrorState title="Couldn't load releases" info={errorMessage(releases.error)} onRetry={() => void releases.refetch()} retrying={releases.isRefetching} />
      ) : rows.length === 0 ? (
        <EmptyState icon={Tag} title="No releases yet" info="Publish one on the web: a tag, notes and files." />
      ) : (
        <View style={{ gap: theme.space[4] }}>
          {rows.map((r) => (
            <ReleaseCard key={r.id} repoId={repoID} release={r} />
          ))}
          {releases.hasNextPage ? (
            <Button variant="ghost" loading={releases.isFetchingNextPage} onPress={() => void releases.fetchNextPage()}>
              Load more
            </Button>
          ) : null}
        </View>
      )}
    </Screen>
  );
}

function ReleaseCard({ repoId, release: r }: { repoId: string; release: ReposRelease }) {
  const theme = useTheme();
  const download = useDownloadReposReleaseAsset(repoId);
  return (
    <Card>
      <View style={{ gap: theme.space[3] }}>
        <View style={{ gap: theme.space[2] }}>
          <Text role="titleSm" selectable>
            {r.name || r.tag}
          </Text>
          <View style={{ flexDirection: "row", flexWrap: "wrap", alignItems: "center", gap: theme.space[2] }}>
            <Badge tone="neutral" icon={Tag}>
              {r.tag}
            </Badge>
            {r.latest ? <Badge tone="success">Latest</Badge> : null}
            {r.prerelease ? <Badge tone="warning">Pre-release</Badge> : null}
            {r.draft ? <Badge tone="neutral">Draft</Badge> : null}
            <Text role="caption" tone="muted" style={{ fontFamily: MONO }}>
              {reposShortSha(r.target_sha)}
            </Text>
          </View>
          <PersonLine user={r.author} trailing={<Text role="caption" tone="muted">{r.published_at ? `released ${reposRelativeTime(r.published_at)}` : `drafted ${reposRelativeTime(r.created_at)}`}</Text>} />
        </View>
        {r.body ? <RepoMarkdown source={withMentions(r.body, r.mentions)} repoId={repoId} gitRef={r.tag} dir="" /> : <Text tone="muted">No release notes.</Text>}
        {r.assets.length ? (
          <ListGroup>
            {r.assets.map((a) => (
              <ListRow
                key={a.id}
                title={a.name}
                leading={<Package size={20} color={theme.colors.textMuted} />}
                trailing={
                  <View style={{ flexDirection: "row", alignItems: "center", gap: theme.space[2] }}>
                    <Text role="caption" tone="muted" tabular>
                      {reposFormatBytes(a.size_bytes)}
                    </Text>
                    <Download size={18} color={theme.colors.primaryStrong} />
                  </View>
                }
                chevron={false}
                accessibilityHint="Downloads the file"
                onPress={() =>
                  download.mutate(
                    { releaseId: r.id, assetId: a.id },
                    {
                      onSuccess: ({ url }) => void Linking.openURL(url).catch(() => toast.error("Couldn't open the download.")),
                      onError: (e) => toast.error(errorMessage(e, "Couldn't download the file.")),
                    },
                  )
                }
              />
            ))}
          </ListGroup>
        ) : null}
      </View>
    </Card>
  );
}
